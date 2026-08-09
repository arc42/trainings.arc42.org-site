package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func dirtyServer(t *testing.T, apiBase string) *Server {
	t.Helper()
	s := testServer(t, apiBase)
	form := url.Values{
		"course_id": {"msa"}, "id": {"msa-a"}, "code": {"26-01 MSA"},
		"start": {"2026-01-01"}, "end": {"2026-01-02"}, "city": {"München"},
		"country": {"DE"}, "language": {"de"}, "format": {"public"},
		"url": {"https://example.org/a"}, "status": {"full"},
	}
	s.Routes().ServeHTTP(httptest.NewRecorder(),
		signedIn(t, s, http.MethodPost, "/dates/msa-a", form))
	return s
}

func TestProposeShowsTheDiff(t *testing.T) {
	gh := fakeGitHub(t, nil)
	defer gh.Close()
	s := dirtyServer(t, gh.URL)

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/propose", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "-        status: open") {
		t.Errorf("diff is missing the removed line:\n%s", body)
	}
	if !strings.Contains(body, "+        status: full") {
		t.Errorf("diff is missing the added line:\n%s", body)
	}
}

func TestProposeSubmitOpensOnePR(t *testing.T) {
	var calls []string
	gh := fakeGitHub(t, &calls)
	defer gh.Close()
	s := dirtyServer(t, gh.URL)

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodPost, "/propose", url.Values{
		"title": {"Training dates: 1 change"}, "body": {"- updated 26-01 MSA"},
	}))
	if !strings.Contains(rec.Body.String(), "pull/7") {
		t.Errorf("PR link not shown:\n%s", rec.Body.String())
	}
	var pulls int
	for _, c := range calls {
		if strings.HasSuffix(c, "/pulls") {
			pulls++
		}
	}
	if pulls != 1 {
		t.Errorf("opened %d PRs, want exactly 1", pulls)
	}
	if _, ok := s.drafts.Get("sid"); ok {
		t.Error("draft was not cleared after publishing")
	}
}

func TestProposeDetectsConcurrentEdit(t *testing.T) {
	gh := fakeGitHub(t, nil)
	defer gh.Close()
	s := dirtyServer(t, gh.URL)

	// Simulate the other maintainer merging something meanwhile.
	d, _ := s.drafts.Get("sid")
	d.FileSHA = "stale-sha"

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodPost, "/propose", url.Values{
		"title": {"x"}, "body": {"y"},
	}))
	if !strings.Contains(rec.Body.String(), "changed on GitHub") {
		t.Errorf("concurrent edit was not detected:\n%s", rec.Body.String())
	}
	if _, ok := s.drafts.Get("sid"); !ok {
		t.Error("draft was discarded on conflict — the user's work must survive")
	}
}

func TestPRTitleAndBody(t *testing.T) {
	changes := []Change{
		{Kind: "updated", DateID: "a", Summary: "26-01 MSA"},
		{Kind: "added", DateID: "b", Summary: "26-05 FLEX"},
	}
	if got := prTitle(changes); got != "Training dates: 2 changes" {
		t.Errorf("prTitle = %q", got)
	}
	body := prBody(changes, "gernotstarke")
	for _, want := range []string{"updated", "added", "26-05 FLEX", "gernotstarke"} {
		if !strings.Contains(body, want) {
			t.Errorf("prBody missing %q:\n%s", want, body)
		}
	}
}

func TestBranchNameIsSafe(t *testing.T) {
	got := branchName(time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC),
		[]Change{{Kind: "updated", DateID: "msa-dez-2026"}})
	if !strings.HasPrefix(got, "trainings-admin/2026-08-08-") {
		t.Errorf("branchName = %q", got)
	}
	if strings.ContainsAny(got, " ~^:?*[\\") {
		t.Errorf("branchName %q contains characters git refs forbid", got)
	}
}

// TestProposeEscapesUserContentInTheDiff guards a deliberate use of
// template.HTML. The diff is built from the edited document, which contains
// whatever the user typed into the form, so rendering it unescaped would turn
// any field into stored XSS against the other maintainer. safeDiff escapes
// first and only then marks the result trusted; this test is what keeps the
// escaping from being dropped as "redundant" later.
func TestProposeEscapesUserContentInTheDiff(t *testing.T) {
	gh := fakeGitHub(t, nil)
	defer gh.Close()
	s := testServer(t, gh.URL)

	const payload = `<script>alert(1)</script>`
	form := url.Values{
		"course_id": {"msa"}, "id": {"msa-a"}, "code": {"26-01 MSA"},
		"start": {"2026-01-01"}, "end": {"2026-01-02"},
		"city":    {payload},
		"country": {"DE"}, "language": {"de"}, "format": {"public"},
		"url": {"https://example.org/a"}, "status": {"open"},
	}
	s.Routes().ServeHTTP(httptest.NewRecorder(),
		signedIn(t, s, http.MethodPost, "/dates/msa-a", form))

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/propose", nil))
	body := rec.Body.String()

	if strings.Contains(body, payload) {
		t.Error("raw <script> from a form field reached the rendered diff")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("escaped form of the payload not found; is the diff rendering at all?\n%s", body)
	}
	// The whole reason safeDiff exists: html/template turns + into &#43;, which
	// mangles every added line. Escaping must not reintroduce that.
	if strings.Contains(body, "&#43;") {
		t.Error("diff contains &#43; — plus signs are being over-escaped again")
	}
}
