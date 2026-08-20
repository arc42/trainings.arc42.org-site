package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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
		"status":           {"full"}, // status != open warns; this test is about the draft
		"confirm_warnings": {"1"},
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

// TestBranchNamesAreUniquePerProposal reproduces a bug that cost a maintainer a
// deletion: the branch name used to be a pure function of (day, first change),
// so proposing twice on the same day about the same date produced the same ref
// twice. Editing a row and later removing it is ordinary — it happened within
// three hours on 2026-08-20 — and the second proposal died on
// POST /git/refs -> 422 "Reference already exists", surfacing only as
// "could not open the pull request".
func TestBranchNamesAreUniquePerProposal(t *testing.T) {
	now := time.Date(2026, 8, 20, 15, 29, 8, 0, time.UTC)
	changes := []Change{{Kind: "removed", DateID: "msa-27-02-online"}}

	first := branchName(now, changes)
	second := branchName(now, changes)

	if first == second {
		t.Fatalf("two proposals about the same date on the same day share a branch name %q;\n"+
			"the second one cannot be pushed (422 Reference already exists)", first)
	}
	for _, got := range []string{first, second} {
		if !strings.HasPrefix(got, "trainings-admin/2026-08-20-msa-27-02-online-") {
			t.Errorf("branchName = %q, want the readable date slug kept", got)
		}
		if strings.ContainsAny(got, " ~^:?*[\\") {
			t.Errorf("branchName %q contains characters git refs forbid", got)
		}
	}
}

// A DateID of nothing but punctuation slugs down to "", which used to yield a
// "--" run in the middle of the ref. Git accepts it, but the branch reads as
// broken in the PR list.
func TestBranchNameSurvivesAnUnslugifiableDateID(t *testing.T) {
	got := branchName(time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
		[]Change{{Kind: "removed", DateID: "///"}})
	if strings.Contains(got, "--") || strings.HasSuffix(got, "-") {
		t.Errorf("branchName = %q, want no empty slug segment", got)
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

// refTrackingGitHub behaves like GitHub's ref API in the one way the plain fake
// does not: it refuses to create a ref that already exists. The plain fake
// accepts every POST /git/refs, which is exactly why the collision below
// shipped unnoticed.
func refTrackingGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	seen := map[string]bool{}
	var mu sync.Mutex
	inner := fakeGitHub(t, nil)
	t.Cleanup(inner.Close)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/refs") {
			var body struct {
				Ref string `json:"ref"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			duplicate := seen[body.Ref]
			seen[body.Ref] = true
			mu.Unlock()
			if duplicate {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"message":"Reference already exists"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		r.URL.Scheme, r.URL.Host = "http", strings.TrimPrefix(inner.URL, "http://")
		proxied, err := http.NewRequest(r.Method, r.URL.String(), r.Body)
		if err != nil {
			t.Fatalf("proxy request: %v", err)
		}
		resp, err := inner.Client().Do(proxied)
		if err != nil {
			t.Fatalf("proxy: %v", err)
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
}

// TestTwoProposalsAboutOneDateOnOneDayBothSucceed is the regression test for the
// reported failure: a maintainer edited msa-27-02-online, then tried to remove
// it the same afternoon and got "could not open the pull request" with no clue
// why. Both proposals must reach the /pulls call.
func TestTwoProposalsAboutOneDateOnOneDayBothSucceed(t *testing.T) {
	gh := refTrackingGitHub(t)
	defer gh.Close()

	propose := func(t *testing.T, s *Server) string {
		t.Helper()
		rec := httptest.NewRecorder()
		s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodPost, "/propose", url.Values{
			"title": {"Training dates: 1 change"}, "body": {"b"},
		}))
		return rec.Body.String()
	}

	// Morning: edit the date.
	if got := propose(t, dirtyServer(t, gh.URL)); !strings.Contains(got, "pull/7") {
		t.Fatalf("first proposal failed:\n%s", got)
	}

	// Afternoon: remove the very same date, in a fresh session.
	s := testServer(t, gh.URL)
	s.Routes().ServeHTTP(httptest.NewRecorder(),
		signedIn(t, s, http.MethodPost, "/dates/msa-a/delete", url.Values{}))
	if got := propose(t, s); !strings.Contains(got, "pull/7") {
		t.Fatalf("removing a date after editing it the same day failed:\n%s", got)
	}
}
