package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"arc42-trainings-admin/internal/model"
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
	gh, _ := fakeGitHub(t)
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

// The review screen has to show the edit that was made, not the file format it
// lands in. Marking one run as "few seats left" used to be four lines of YAML
// context around one key.
func TestProposeShowsAReadableReport(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)
	form := url.Values{
		"course_id": {"msa"}, "id": {"msa-a"}, "code": {"26-01 MSA"},
		"start": {"2026-01-01"}, "end": {"2026-01-02"}, "city": {"München"},
		"country": {"DE"}, "language": {"de"}, "format": {"public"},
		"status": {"open"}, "seats_limited": {"on"}, "confirm_warnings": {"1"},
	}
	s.Routes().ServeHTTP(httptest.NewRecorder(),
		signedIn(t, s, http.MethodPost, "/dates/msa-a", form))

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/propose", nil))
	body := rec.Body.String()

	for _, want := range []string{
		`class="kind kind-updated"`,
		"MSA · 26-01 MSA · 2026-01-01 to 2026-01-02",
		"Few seats left",
		`<td class="was">no</td>`,
		`<td class="now">yes</td>`,
		`value="MSA 26-01 MSA:`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the report is missing %q:\n%s", want, body)
		}
	}
	// The diff is still there, and still second.
	if !strings.Contains(body, `<details class="rawdiff">`) {
		t.Errorf("the YAML diff is gone; it is the only view that shows collateral damage:\n%s", body)
	}
	if strings.Index(body, "What changed") > strings.Index(body, "rawdiff") {
		t.Error("the diff comes before the report")
	}
}

func TestProposeSubmitOpensOnePR(t *testing.T) {
	gh, fake := fakeGitHub(t)
	defer gh.Close()
	s := dirtyServer(t, gh.URL)

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodPost, "/propose", url.Values{
		"title": {"Training dates: 1 change"}, "body": {"- updated 26-01 MSA"},
	}))
	if !strings.Contains(rec.Body.String(), "pull/1") {
		t.Errorf("PR link not shown:\n%s", rec.Body.String())
	}
	var pulls int
	for _, c := range fake.Calls() {
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
	gh, _ := fakeGitHub(t)
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

func aDate() model.Date {
	return model.Date{
		ID: "msa-online-sep-2026", Code: "26-09 MSA-EN",
		Start: "2026-09-21", End: "2026-09-24",
		Country: "DE", Language: "en", Format: "online", Status: "open",
	}
}

func updatedSeats() Change {
	before := aDate()
	after := before
	after.SeatsLimited = true
	return Change{
		Kind: "updated", DateID: after.ID, Code: after.Code,
		CourseID: "msa", CourseShortTitle: "MSA",
		Before: &before, After: &after,
	}
}

// "Training dates: 1 change" told a reviewer nothing the pull request list did
// not already say. The title has to name the course.
func TestPRTitleNamesTheCourse(t *testing.T) {
	statusChange := func(status string) Change {
		c := updatedSeats()
		after := *c.After
		after.SeatsLimited = false
		after.Status = status
		c.After = &after
		return c
	}
	twoFields := func() Change {
		c := updatedSeats()
		after := *c.After
		after.SeatsLimited = false
		after.City = "Köln"
		after.Status = "waitlist"
		c.After = &after
		return c
	}
	added := func() Change {
		d := aDate()
		d.ID, d.Code, d.Start, d.End = "msa-feb-2027", "27-02 MSA-EN", "2027-02-22", "2027-02-25"
		return Change{Kind: "added", DateID: d.ID, Code: d.Code,
			CourseID: "msa", CourseShortTitle: "MSA", After: &d}
	}
	removed := func() Change {
		c := updatedSeats()
		return Change{Kind: "removed", DateID: c.DateID, Code: c.Code,
			CourseID: "msa", CourseShortTitle: "MSA", Before: c.Before}
	}
	otherCourse := func() Change {
		c := updatedSeats()
		c.DateID, c.CourseID, c.CourseShortTitle = "improve-apr-2027", "improve", "IMPROVE"
		return c
	}

	cases := []struct {
		name    string
		changes []Change
		want    string
	}{
		{"the seats checkbox says what it means",
			[]Change{updatedSeats()}, "MSA 26-09 MSA-EN: only few seats left"},
		{"a status change carries the new status",
			[]Change{statusChange("full")}, "MSA 26-09 MSA-EN: status full"},
		{"a couple of fields are named",
			[]Change{twoFields()}, "MSA 26-09 MSA-EN: city and status changed"},
		{"a new date carries its span",
			[]Change{added()}, "MSA 27-02 MSA-EN: new date, 2027-02-22 to 2027-02-25"},
		{"a removal says so",
			[]Change{removed()}, "MSA 26-09 MSA-EN: date removed"},
		{"several changes to one course name that course",
			[]Change{updatedSeats(), added()}, "MSA: 2 changes"},
		{"across courses, every course is named",
			[]Change{updatedSeats(), otherCourse()}, "Training dates: 2 changes (MSA, IMPROVE)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := prTitle(c.changes); got != c.want {
				t.Errorf("prTitle = %q, want %q", got, c.want)
			}
		})
	}
}

func TestPRTitleStaysShortAndWhole(t *testing.T) {
	c := updatedSeats()
	c.CourseShortTitle = strings.Repeat("Mastering Software Architectures ", 4)
	got := prTitle([]Change{c})
	if n := len([]rune(got)); n > prTitleMax {
		t.Errorf("title is %d runes: %q", n, got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a cut title should say it was cut: %q", got)
	}
	// Cutting mid-rune would leave replacement characters behind.
	if strings.Contains(got, "\ufffd") {
		t.Errorf("title was cut inside a rune: %q", got)
	}
}

// A date whose course could not be resolved must still produce a usable title
// rather than a leading space or an empty subject.
func TestPRTitleSurvivesAnUnknownCourse(t *testing.T) {
	c := updatedSeats()
	c.CourseID, c.CourseShortTitle, c.Code = "", "", ""
	if got := prTitle([]Change{c}); got != "msa-online-sep-2026: only few seats left" {
		t.Errorf("prTitle = %q", got)
	}
}

// The body is the same report, so a reviewer on GitHub never has to open the
// Files tab and read YAML to see what moved.
func TestPRBodyReportsTheFields(t *testing.T) {
	body := prBody([]Change{updatedSeats()}, "gernotstarke")
	for _, want := range []string{
		"@gernotstarke",
		"### updated — MSA · 26-09 MSA-EN · 2026-09-21 to 2026-09-24",
		"| Field | Before | After |",
		"| Few seats left | no | yes |",
		"validate_trainings.rb",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("prBody missing %q:\n%s", want, body)
		}
	}
}

func TestPRBodyListsAnAdditionAsValues(t *testing.T) {
	d := aDate()
	body := prBody([]Change{{Kind: "added", DateID: d.ID, Code: d.Code,
		CourseID: "msa", CourseShortTitle: "MSA", After: &d}}, "gernotstarke")
	if !strings.Contains(body, "| Field | Value |") {
		t.Errorf("an addition should not have a Before column:\n%s", body)
	}
	if !strings.Contains(body, "| Booking code | 26-09 MSA-EN |") {
		t.Errorf("prBody missing the code row:\n%s", body)
	}
}

// A pipe or a newline in a value would break the table it sits in — a course
// blurb has both.
func TestPRBodyKeepsValuesInsideTheirCell(t *testing.T) {
	before := model.Course{ID: "msa", ShortTitle: "MSA"}
	after := before
	after.Blurb = "one | two\nthree"
	body := prBody([]Change{{Kind: "updated", DateID: "course:msa",
		CourseID: "msa", CourseShortTitle: "MSA",
		BeforeCourse: &before, AfterCourse: &after}}, "gernotstarke")
	if !strings.Contains(body, `| one \| two<br>three |`) {
		t.Errorf("cell was not escaped:\n%s", body)
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
	gh, _ := fakeGitHub(t)
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

// TestTwoProposalsAboutOneDateOnOneDayBothSucceed is the regression test for the
// reported failure: a maintainer edited msa-27-02-online, then tried to remove
// it the same afternoon and got "could not open the pull request" with no clue
// why. Both proposals must reach the /pulls call — and land as two distinct
// pull requests, which is what the two numbers below check.
//
// The double refuses a duplicate ref with 422 the way GitHub does, so this test
// fails without the unique branch name. That refusal used to live in a special
// fake set up here; it now belongs to ghfake, where every test gets it.
func TestTwoProposalsAboutOneDateOnOneDayBothSucceed(t *testing.T) {
	gh, _ := fakeGitHub(t)
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
	if got := propose(t, dirtyServer(t, gh.URL)); !strings.Contains(got, "/pull/1") {
		t.Fatalf("first proposal failed:\n%s", got)
	}

	// Afternoon: remove the very same date, in a fresh session.
	s := testServer(t, gh.URL)
	s.Routes().ServeHTTP(httptest.NewRecorder(),
		signedIn(t, s, http.MethodPost, "/dates/msa-a/delete", url.Values{}))
	if got := propose(t, s); !strings.Contains(got, "/pull/2") {
		t.Fatalf("removing a date after editing it the same day failed:\n%s", got)
	}
}
