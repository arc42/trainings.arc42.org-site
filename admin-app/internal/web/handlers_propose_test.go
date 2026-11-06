package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"arc42-trainings-admin/internal/ghfake"
	"arc42-trainings-admin/internal/model"
)

func dirtyServer(t *testing.T, apiBase string) *Server {
	t.Helper()
	s := testServer(t, apiBase)
	form := url.Values{
		"course_id": {"msa"}, "id": {"msa-a"}, "code": {"26-01 MSA"},
		"start": {"2026-01-01"}, "end": {"2026-01-02"}, "city": {"München"},
		"country": {"DE"}, "language": {"de"}, "format": {"public"},
		"availability":     {"full"}, // status != open warns; this test is about the draft
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
		"availability": {"few seats"}, "confirm_warnings": {"1"},
	}
	s.Routes().ServeHTTP(httptest.NewRecorder(),
		signedIn(t, s, http.MethodPost, "/dates/msa-a", form))

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/propose", nil))
	body := rec.Body.String()

	for _, want := range []string{
		`class="kind kind-updated"`,
		"MSA · 26-01 MSA · 2026-01-01 to 2026-01-02",
		"Availability",
		`<td class="was">open</td>`,
		`<td class="now">few seats</td>`,
		`value="MSA 26-01 MSA:`,
		// The button disables itself on the first click; see form.js.
		`data-submit-once`, `src="/static/form.js"`,
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

// A double click on "Open pull request" sends the same form twice, the second
// before the first has come back. Both used to find the draft still dirty —
// it is only discarded once GitHub has answered — and both opened a PR. The
// browser shows whichever response it is still waiting for, so every one of
// them has to carry the link to the one PR that exists.
func TestProposeDoubleSubmitOpensOnePR(t *testing.T) {
	gh, fake := fakeGitHub(t)
	defer gh.Close()
	s := dirtyServer(t, gh.URL)

	const clicks = 3
	bodies := make([]string, clicks)
	var wg sync.WaitGroup
	for i := range bodies {
		req := signedIn(t, s, http.MethodPost, "/propose", url.Values{
			"title": {"Training dates: 1 change"}, "body": {"b"},
		})
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			s.Routes().ServeHTTP(rec, req)
			bodies[i] = rec.Body.String()
		}(i)
	}
	wg.Wait()

	if n := fake.Proposals(); n != 1 {
		t.Errorf("opened %d PRs, want exactly 1", n)
	}
	for i, body := range bodies {
		if !strings.Contains(body, "/pull/1") {
			t.Errorf("response %d does not link the PR:\n%s", i, body)
		}
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
	reopened := func() Change {
		c := statusChange("open")
		before := *c.Before
		before.Status = "waitlist"
		c.Before = &before
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
	fewSeatsToWaitlist := func() Change {
		c := updatedSeats()
		before, after := *c.After, *c.After
		after.SeatsLimited, after.Status = false, "waitlist"
		c.Before, c.After = &before, &after
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
		{"a status change says what it means",
			[]Change{statusChange("full")}, "MSA 26-09 MSA-EN: fully booked"},
		{"waitlist says there is a waiting list",
			[]Change{statusChange("waitlist")}, "MSA 26-09 MSA-EN: fully booked, waiting list"},
		{"back to open",
			[]Change{reopened()}, "MSA 26-09 MSA-EN: open for registration again"},
		{"a couple of fields are named",
			[]Change{twoFields()}, "MSA 26-09 MSA-EN: city and availability changed"},
		{"few seats to waitlist is one decision, not two fields",
			[]Change{fewSeatsToWaitlist()}, "MSA 26-09 MSA-EN: fully booked, waiting list"},
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
		"| Availability | open | few seats |",
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
	after.BlurbEN = "one | two\nthree"
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
		"url": {"https://example.org/a"}, "availability": {"open"},
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

// fakeGitHubFiles is fakeGitHub over an explicit file set, for the tests that
// need the real schema, or no schema at all.
func fakeGitHubFiles(t *testing.T, files map[string][]byte) (*httptest.Server, *ghfake.Fake) {
	t.Helper()
	f := ghfake.New(ghfake.Options{
		Repo: "arc42/site", Login: "gernotstarke", CanPush: true, Files: files,
		OnUnexpected: func(method, path string) { t.Errorf("unexpected %s %s", method, path) },
	})
	return httptest.NewServer(f.Handler()), f
}

func realSchema(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "trainings.schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	return b
}

// between returns the part of body from the first from up to the next to, or
// "" when from is absent.
func between(body, from, to string) string {
	i := strings.Index(body, from)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	if j := strings.Index(rest, to); j >= 0 {
		return rest[:j]
	}
	return rest
}

// The report that started this: a new course with no dates became
// "courses/4/dates: expected array, but got null" and no way to open the pull
// request. Against the repository's real schema, a course without dates is
// fine, and the page says what it means for the home pages.
func TestProposeNewCourseWithoutDatesOpensPR(t *testing.T) {
	gh, fake := fakeGitHubFiles(t, map[string][]byte{
		"_data/trainings.yml":       []byte(fixtureYAML),
		"api/trainings.schema.json": realSchema(t),
	})
	defer gh.Close()
	s := testServer(t, gh.URL)
	if rec, _, ok := postCourse(t, s, "/courses/new", newMASForm()); !ok {
		t.Fatalf("course not saved:\n%s", rec.Body.String())
	}

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/propose", nil))
	body := rec.Body.String()
	if strings.Contains(body, `class="problems`) {
		t.Errorf("a course without dates shows problems:\n%s", body)
	}
	if !strings.Contains(body, "MAS has no dates yet. It will appear on the home pages once it has one.") {
		t.Errorf("no note about the missing dates:\n%s", body)
	}
	if !strings.Contains(body, `action="/propose"`) {
		t.Errorf("the pull request form is missing:\n%s", body)
	}

	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodPost, "/propose", url.Values{
		"title": {"New course: MAS"}, "body": {"b"},
	}))
	if n := fake.Proposals(); n != 1 {
		t.Errorf("opened %d PRs, want 1:\n%s", n, rec.Body.String())
	}
}

// When the app cannot check the draft itself, that is not the operator's
// doing, and it must not hold the proposal hostage: CI checks it again.
func TestProposeAppFaultDoesNotBlock(t *testing.T) {
	gh, fake := fakeGitHubFiles(t, map[string][]byte{
		"_data/trainings.yml": []byte(fixtureYAML), // no schema: the read fails
	})
	defer gh.Close()
	s := dirtyServer(t, gh.URL)

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/propose", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"This is not caused by your entries, and your draft is saved.",
		"Please send Gernot a screenshot of this page.",
		"You can still open the pull request: CI checks it again.",
		`action="/propose"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q:\n%s", want, body)
		}
	}
	detail := between(body, "<details>", "</details>")
	if !strings.Contains(detail, "<summary>Technical detail</summary>") ||
		!strings.Contains(detail, "could not fetch the schema") {
		t.Errorf("the technical detail is not inside a <details>:\n%s", body)
	}
	if strings.Contains(body, "Fix these in your entries") || strings.Contains(body, `role="alert"`) {
		t.Error("an app fault is presented as the operator's to fix")
	}
	if !strings.Contains(body, `<div class="app-fault" role="status">`) {
		t.Errorf("the app fault is not in its calmer status box:\n%s", body)
	}

	// Submitting applies the same rule.
	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodPost, "/propose", url.Values{
		"title": {"t"}, "body": {"b"},
	}))
	if n := fake.Proposals(); n != 1 {
		t.Errorf("opened %d PRs, want 1:\n%s", n, rec.Body.String())
	}
}

// masBadDateYAML has a date whose last day precedes its first. The date form
// would refuse it; a hand edit on GitHub does not.
const masBadDateYAML = `courses:
  - id: mas
    short_title: "MAS"
    title: "Methodische Architektur-Skills"
    url: "https://www.arc42.de/info-mas/"
    trainers: ["Dr. Peter Hruschka"]
    dates:
      - id: mas-jan-2027
        code: "27-01 MAS"
        start: "2027-01-13"
        end: "2027-01-12"
        language: de
        format: online
        url: "https://trainings.arc42.org/#mas-jan-2027"
        status: open
`

func TestProposeEntryProblemLinksToForm(t *testing.T) {
	gh, fake := fakeGitHubWith(t, masBadDateYAML)
	defer gh.Close()
	s := testServer(t, gh.URL)
	// Any edit makes the draft dirty; a course edit does not re-run the date
	// rules, so it gets through to the review.
	form := newMASForm()
	form.Set("title", "Methodische Architektur-Skills, neu")
	if rec, _, ok := postCourse(t, s, "/courses/mas", form); !ok {
		t.Fatalf("course edit not saved:\n%s", rec.Body.String())
	}

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/propose", nil))
	body := rec.Body.String()
	list := between(body, "Fix these in your entries", "CI would reject them too.")
	if list == "" {
		t.Fatalf("no entry problems shown:\n%s", body)
	}
	for _, want := range []string{"MAS, 13 Jan 2027 online", "Last day", `<a href="/dates/mas-jan-2027"`} {
		if !strings.Contains(list, want) {
			t.Errorf("problem list is missing %q:\n%s", want, list)
		}
	}
	for _, leak := range []string{"dates.", "courses/"} {
		if strings.Contains(list, leak) {
			t.Errorf("problem list shows the internal %q:\n%s", leak, list)
		}
	}
	if strings.Contains(body, `action="/propose"`) {
		t.Error("the pull request form is offered despite a problem in the entries")
	}

	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodPost, "/propose", url.Values{
		"title": {"t"}, "body": {"b"},
	}))
	if n := fake.Proposals(); n != 0 {
		t.Errorf("opened %d PRs past a problem in the entries", n)
	}
	if !strings.Contains(rec.Body.String(), "MAS, 13 Jan 2027 online") {
		t.Errorf("submit does not say what to fix:\n%s", rec.Body.String())
	}
}

// "CI would reject them anyway" was said about problems CI would never have
// seen. It is only true of the operator's entries.
func TestProposePageNoLongerClaimsCI(t *testing.T) {
	gh, _ := fakeGitHubFiles(t, map[string][]byte{"_data/trainings.yml": []byte(fixtureYAML)})
	defer gh.Close()
	s := dirtyServer(t, gh.URL)

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/propose", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "This is not caused by your entries, and your draft is saved.") {
		t.Fatalf("the page does not show the app fault this test is about:\n%s", body)
	}
	for _, claim := range []string{"CI would reject them anyway", "CI would reject them too"} {
		if strings.Contains(body, claim) {
			t.Errorf("app faults only, and the page still says %q", claim)
		}
	}
}

// http:// on the course URL is only a warning on the course form, so it can
// reach the review. The schema's answer is a regular expression; the operator
// gets the field and plain words.
func TestProposeSchemaProblemSpeaksPlainly(t *testing.T) {
	gh, fake := fakeGitHubFiles(t, map[string][]byte{
		"_data/trainings.yml":       []byte(fixtureYAML),
		"api/trainings.schema.json": realSchema(t),
	})
	defer gh.Close()
	s := testServer(t, gh.URL)
	form := newMASForm()
	form.Set("url", "http://www.arc42.de/info-mas/")
	form.Set("confirm_warnings", "1")
	if rec, _, ok := postCourse(t, s, "/courses/new", form); !ok {
		t.Fatalf("course not saved:\n%s", rec.Body.String())
	}

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/propose", nil))
	body := rec.Body.String()
	list := between(body, "Fix these in your entries", "CI would reject them too.")
	if !strings.Contains(list, "MAS — Course URL: must start with https://") {
		t.Errorf("the problem is not in plain words:\n%s", body)
	}
	// "courses/1", a pointer; "/courses/mas" is the Edit link and fine.
	for _, leak := range []string{"pattern", "^", "courses/1"} {
		if strings.Contains(list, leak) {
			t.Errorf("problem list shows %q:\n%s", leak, list)
		}
	}
	if strings.Contains(body, "This is not caused by your entries") {
		t.Errorf("an entry problem also shows up as the app's fault:\n%s", body)
	}
	if strings.Contains(body, `action="/propose"`) || fake.Proposals() != 0 {
		t.Error("the pull request form is offered despite a problem in the entries")
	}
}

// A public date without a city fails a rule and the schema's "required". The
// schema reports that at the date, not at the city; it used to land as an app
// fault next to the rule's entry problem, contradicting it.
func TestProposeMissingCityIsOneEntryProblem(t *testing.T) {
	yaml := strings.Replace(masBadDateYAML, `end: "2027-01-12"`, `end: "2027-01-14"`, 1)
	yaml = strings.Replace(yaml, "format: online", "format: public", 1)
	gh, _ := fakeGitHubFiles(t, map[string][]byte{
		"_data/trainings.yml":       []byte(yaml),
		"api/trainings.schema.json": realSchema(t),
	})
	defer gh.Close()
	s := testServer(t, gh.URL)
	form := newMASForm()
	form.Set("title", "Methodische Architektur-Skills, neu")
	if rec, _, ok := postCourse(t, s, "/courses/mas", form); !ok {
		t.Fatalf("course edit not saved:\n%s", rec.Body.String())
	}

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/propose", nil))
	body := rec.Body.String()
	list := between(body, "Fix these in your entries", "CI would reject them too.")
	if n := strings.Count(list, "— City:"); n != 1 {
		t.Errorf("City is named %d times, want once:\n%s", n, list)
	}
	if strings.Contains(body, "This is not caused by your entries") {
		t.Errorf("the missing city is also filed as the app's fault:\n%s", body)
	}
}

// Spec §8 point 3, end to end: MAS is proposed without dates, that pull
// request becomes main, and then two German January 2027 dates are added. Both
// derived "mas-jan-2027" and "27-01 MAS"; the second was refused as a
// duplicate and the warnings advised the taken value. The second now gets
// "-2", with no warning to acknowledge.
func TestTwoDatesInOneMonthBothProposeAfterTheCourse(t *testing.T) {
	var proposed string
	fake := func(yaml string) (*httptest.Server, *ghfake.Fake) {
		f := ghfake.New(ghfake.Options{
			Repo: "arc42/site", Login: "gernotstarke", CanPush: true,
			Files: map[string][]byte{
				"_data/trainings.yml":       []byte(yaml),
				"api/trainings.schema.json": realSchema(t),
			},
			OnProposal:   func(p ghfake.Proposal) { proposed = p.Content },
			OnUnexpected: func(method, path string) { t.Errorf("unexpected %s %s", method, path) },
		})
		return httptest.NewServer(f.Handler()), f
	}
	propose := func(s *Server) string {
		rec := httptest.NewRecorder()
		s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodPost, "/propose", url.Values{
			"title": {"t"}, "body": {"b"},
		}))
		return rec.Body.String()
	}

	gh1, f1 := fake(fixtureYAML)
	defer gh1.Close()
	s1 := testServer(t, gh1.URL)
	if rec, _, ok := postCourse(t, s1, "/courses/new", newMASForm()); !ok {
		t.Fatalf("course not saved:\n%s", rec.Body.String())
	}
	if body := propose(s1); f1.Proposals() != 1 {
		t.Fatalf("the course alone opened %d PRs, want 1:\n%s", f1.Proposals(), body)
	}

	// The pull request was merged: its content is now main.
	gh2, f2 := fake(proposed)
	defer gh2.Close()
	s2 := testServer(t, gh2.URL)
	for _, run := range [][2]string{{"2027-01-13", "2027-01-14"}, {"2027-01-27", "2027-01-28"}} {
		// No id and no code, as with scripting off, and no confirm_warnings:
		// a warning would stop the save and show "Save anyway".
		form := url.Values{
			"course_id": {"mas"}, "start": {run[0]}, "end": {run[1]},
			"language": {"de"}, "format": {"online"}, "availability": {"open"},
		}
		rec := httptest.NewRecorder()
		s2.Routes().ServeHTTP(rec, signedIn(t, s2, http.MethodPost, "/dates/new", form))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("date %s not saved (status %d):\n%s", run[0], rec.Code, rec.Body.String())
		}
	}
	d, _ := s2.drafts.Get("sid")
	doc := string(d.Doc.Bytes())
	for _, want := range []string{"id: mas-jan-2027\n", `"27-01 MAS"`, "id: mas-jan-2027-2\n", `"27-01 MAS-2"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("draft lacks %q:\n%s", want, doc)
		}
	}

	rec := httptest.NewRecorder()
	s2.Routes().ServeHTTP(rec, signedIn(t, s2, http.MethodGet, "/propose", nil))
	if body := rec.Body.String(); strings.Contains(body, `class="problems`) {
		t.Errorf("two dates in one month show problems:\n%s", body)
	}
	if body := propose(s2); f2.Proposals() != 1 {
		t.Errorf("opened %d PRs, want exactly 1:\n%s", f2.Proposals(), body)
	}
}

// A warning-free course save goes straight back to the course list, so the
// course form's notes ("only a German description …") were never seen when a
// course was created. The review page repeats them for every course the
// draft adds or changes.
func TestProposeShowsTheCourseNotes(t *testing.T) {
	gh, _ := fakeGitHubFiles(t, map[string][]byte{
		"_data/trainings.yml":       []byte(fixtureYAML),
		"api/trainings.schema.json": realSchema(t),
	})
	defer gh.Close()
	s := testServer(t, gh.URL)
	if rec, _, ok := postCourse(t, s, "/courses/new", newMASForm()); !ok {
		t.Fatalf("course not saved:\n%s", rec.Body.String())
	}

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/propose", nil))
	notes := between(rec.Body.String(), `<div class="notes"`, "</div>")
	for _, want := range []string{
		"MAS — Only a German description: the English home page shows it in German too.",
		"MAS has no dates yet.",
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("the notes lack %q:\n%s", want, rec.Body.String())
		}
	}
}
