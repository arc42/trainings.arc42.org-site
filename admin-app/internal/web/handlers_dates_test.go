package web

import (
	"encoding/base64"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"arc42-trainings-admin/internal/config"
)

const fixtureYAML = `courses:
  - id: msa
    short_title: "MSA"
    title: "Mastering Software Architectures"
    url: "https://example.org/msa"
    trainers: ["Peter Hruschka"]
    dates:
      - id: msa-a
        code: "26-01 MSA"
        start: "2026-01-01"
        end: "2026-01-02"
        city: "München"
        country: "DE"
        language: de
        format: public
        url: "https://example.org/a"
        status: open
`

// fakeGitHub serves just enough of the API for handler tests: the file read,
// the head ref, and the three publish calls.
func fakeGitHub(t *testing.T, calls *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			*calls = append(*calls, r.Method+" "+r.URL.Path)
		}
		switch {
		case r.URL.Path == "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"login": "gernotstarke"})
		case r.URL.Path == "/repos/arc42/site" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{"permissions": map[string]any{"push": true}})
		case strings.HasSuffix(r.URL.Path, "/contents/_data/trainings.yml") && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content":  base64.StdEncoding.EncodeToString([]byte(fixtureYAML)),
				"encoding": "base64", "sha": "filesha",
			})
		case strings.HasSuffix(r.URL.Path, "/contents/api/trainings.schema.json") && r.Method == "GET":
			schema := `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object"}`
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content":  base64.StdEncoding.EncodeToString([]byte(schema)),
				"encoding": "base64", "sha": "schemasha",
			})
		case strings.HasSuffix(r.URL.Path, "/git/ref/heads/main"):
			_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]any{"sha": "headsha"}})
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/git/refs"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/contents/_data/trainings.yml"):
			_, _ = w.Write([]byte(`{}`))
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/pulls"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"html_url":"https://github.com/arc42/site/pull/7"}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
}

func testServer(t *testing.T, apiBase string) *Server {
	t.Helper()
	cfg := config.Config{
		Addr: ":0", GitHubRepo: "arc42/site", ClientID: "id", ClientSecret: "secret",
		SessionKey: "0123456789abcdef0123456789abcdef", Environment: "DEVELOPMENT",
	}
	s, err := NewServer(cfg, apiBase, "http://localhost:8080")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s
}

// signedIn returns a request carrying a valid session cookie.
func signedIn(t *testing.T, s *Server, method, target string, form url.Values) *http.Request {
	t.Helper()
	var req *http.Request
	if form == nil {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	if err := s.sessions.Set(rec, Session{ID: "sid", Login: "gernotstarke", Token: "tok"}); err != nil {
		t.Fatalf("Set session: %v", err)
	}
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	return req
}

func TestListRequiresSignIn(t *testing.T) {
	gh := fakeGitHub(t, nil)
	defer gh.Close()
	s := testServer(t, gh.URL)

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "Sign in with GitHub") {
		t.Errorf("anonymous request did not get the sign-in page:\n%s", rec.Body.String())
	}
}

func TestListShowsDates(t *testing.T) {
	gh := fakeGitHub(t, nil)
	defer gh.Close()
	s := testServer(t, gh.URL)

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "26-01 MSA") {
		t.Errorf("list is missing the booking code:\n%s", body)
	}
	if !strings.Contains(body, "München") {
		t.Error("list is missing the city")
	}
}

func TestSaveDateMarksDraftDirty(t *testing.T) {
	gh := fakeGitHub(t, nil)
	defer gh.Close()
	s := testServer(t, gh.URL)

	form := url.Values{
		"course_id": {"msa"}, "id": {"msa-a"}, "code": {"26-01 MSA"},
		"start": {"2026-01-01"}, "end": {"2026-01-02"}, "city": {"München"},
		"country": {"DE"}, "language": {"de"}, "format": {"public"},
		"url": {"https://example.org/a"}, "status": {"full"},
	}
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodPost, "/dates/msa-a", form))
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body:\n%s", rec.Code, rec.Body.String())
	}
	d, ok := s.drafts.Get("sid")
	if !ok || !d.Dirty() {
		t.Fatal("draft is not dirty after a save")
	}
	if !strings.Contains(string(d.Doc.Bytes()), "status: full") {
		t.Error("edit not applied to the document")
	}
}

func TestSaveRejectsInvalidDate(t *testing.T) {
	gh := fakeGitHub(t, nil)
	defer gh.Close()
	s := testServer(t, gh.URL)

	form := url.Values{
		"course_id": {"msa"}, "id": {"msa-a"}, "code": {"26-01 MSA"},
		"start": {"2026-01-05"}, "end": {"2026-01-02"}, // end before start
		"city": {"München"}, "language": {"de"}, "format": {"public"},
		"url": {"https://example.org/a"}, "status": {"open"},
	}
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodPost, "/dates/msa-a", form))
	if !strings.Contains(rec.Body.String(), "before start") {
		t.Errorf("invalid date was accepted:\n%s", rec.Body.String())
	}
}

func TestDeleteDateRecordsRemoval(t *testing.T) {
	gh := fakeGitHub(t, nil)
	defer gh.Close()
	s := testServer(t, gh.URL)

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodPost, "/dates/msa-a/delete", url.Values{}))
	d, ok := s.drafts.Get("sid")
	if !ok || len(d.Changes) != 1 || d.Changes[0].Kind != "removed" {
		t.Fatalf("delete not recorded: %+v", d)
	}
}

func TestDiscardClearsTheDraft(t *testing.T) {
	gh := fakeGitHub(t, nil)
	defer gh.Close()
	s := testServer(t, gh.URL)

	s.Routes().ServeHTTP(httptest.NewRecorder(),
		signedIn(t, s, http.MethodPost, "/dates/msa-a/delete", url.Values{}))
	s.Routes().ServeHTTP(httptest.NewRecorder(),
		signedIn(t, s, http.MethodPost, "/discard", url.Values{}))
	if _, ok := s.drafts.Get("sid"); ok {
		t.Error("draft survived discard")
	}
}

func TestCourseSaveLeavesDatesAlone(t *testing.T) {
	gh := fakeGitHub(t, nil)
	defer gh.Close()
	s := testServer(t, gh.URL)

	form := url.Values{
		"id": {"msa"}, "short_title": {"MSA neu"},
		"title": {"Mastering Software Architectures"},
		"url":   {"https://example.org/msa"},
		// Roster checkboxes plus the free-text field, which is how the form
		// posts trainers now.
		"trainer":        {"Dr. Peter Hruschka", "Dr. Gernot Starke"},
		"trainers_other": {"Jane Guest"},
	}
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodPost, "/courses/msa", form))

	d, ok := s.drafts.Get("sid")
	if !ok {
		t.Fatal("no draft after course save")
	}
	out := string(d.Doc.Bytes())
	if !strings.Contains(out, `short_title: "MSA neu"`) {
		t.Errorf("course not updated:\n%s", out)
	}
	if !strings.Contains(out, "id: msa-a") || !strings.Contains(out, `code: "26-01 MSA"`) {
		t.Errorf("a course-level edit disturbed the dates:\n%s", out)
	}
}

// TestUnauthenticatedWriteIsRejected pins the status code, not just the body.
// A logged-out POST must not answer 200: the draft was keyed by the old
// session id and is already gone, so a success status would let "Open pull
// request" look like it worked while nothing was published.
func TestUnauthenticatedWriteIsRejected(t *testing.T) {
	gh := fakeGitHub(t, nil)
	defer gh.Close()
	s := testServer(t, gh.URL)

	for _, target := range []string{"/propose", "/dates/msa-a", "/dates/msa-a/delete", "/courses/msa"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		s.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("POST %s without a session: status = %d, want 401", target, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "was not saved") {
			t.Errorf("POST %s: response does not say the change was not saved", target)
		}
	}

	// Reading anonymously stays an ordinary 200 sign-in page.
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Sign in with GitHub") {
		t.Errorf("anonymous GET /: status = %d, want a 200 sign-in page", rec.Code)
	}
}

// TestNewDateFormRendersCompletely is the regression test for a form that
// rendered its heading, the Course label and an open <select>, then stopped.
// html/template streams output, so an execution error mid-render leaves a
// half-written page — served as 200, because the status was already sent.
// Two bugs in one: the template blew up on a map key the handler never set,
// and render() had no way to take the partial output back.
func TestNewDateFormRendersCompletely(t *testing.T) {
	gh := fakeGitHub(t, nil)
	defer gh.Close()
	s := testServer(t, gh.URL)

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/dates/new", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()

	// The course options must actually be there — an empty <select> was the
	// visible symptom.
	if !strings.Contains(body, `value="msa"`) {
		t.Error("course dropdown has no options")
	}
	// Every required field must render, i.e. execution reached the end.
	for _, field := range []string{
		`name="id"`, `name="code"`, `name="start"`, `name="end"`,
		`name="format"`, `name="language"`, `name="status"`, `name="url"`,
	} {
		if !strings.Contains(body, field) {
			t.Errorf("form is missing %s — rendering stopped early", field)
		}
	}
	if !strings.Contains(body, "</form>") {
		t.Error("form element is not closed — rendering stopped early")
	}
}

// TestRenderFailsLoudlyNotPartially pins the framework guarantee: a broken
// template must never emit a 200 with half a page.
func TestRenderFailsLoudlyNotPartially(t *testing.T) {
	s := testServer(t, "http://127.0.0.1:1")
	// index-out-of-range fails at EXECUTION time, after "start" is already
	// written — which is precisely the shape of the real bug.
	s.set["boom.gohtml"] = template.Must(template.New("boom.gohtml").
		Parse(`start{{ index .Empty 5 }}end`))

	rec := httptest.NewRecorder()
	s.render(rec, "boom.gohtml", map[string]any{"Empty": []string{}})

	if rec.Code == http.StatusOK {
		t.Errorf("status = 200 for a template that failed to execute")
	}
	if strings.Contains(rec.Body.String(), "start") {
		t.Errorf("partial output was written: %q", rec.Body.String())
	}
}

func TestNewCourseFormRendersAndCreates(t *testing.T) {
	gh := fakeGitHub(t, nil)
	defer gh.Close()
	s := testServer(t, gh.URL)

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/courses/new", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "New course") {
		t.Fatalf("status %d, body:\n%s", rec.Code, body)
	}
	for _, want := range []string{`name="id"`, `name="short_title"`, `name="title"`,
		`name="url"`, `name="trainer"`, `name="trainers_other"`, "</form>"} {
		if !strings.Contains(body, want) {
			t.Errorf("new-course form is missing %s", want)
		}
	}
	// The roster must be offered as checkboxes.
	for _, name := range []string{"Dr. Carola Lilienthal", "Dr. Peter Hruschka",
		"Dr. Gernot Starke", "Wolfgang Reimesch"} {
		if !strings.Contains(body, name) {
			t.Errorf("roster is missing %q", name)
		}
	}

	form := url.Values{
		"id": {"flex"}, "short_title": {"FLEX"}, "title": {"Flexible Architectures"},
		"url": {"https://example.org/flex"}, "trainer": {"Dr. Gernot Starke"},
	}
	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodPost, "/courses/new", form))

	d, ok := s.drafts.Get("sid")
	if !ok {
		t.Fatal("no draft after creating a course")
	}
	m := d.Doc.Model()
	if len(m.Courses) != 2 || m.Courses[1].ID != "flex" {
		t.Fatalf("course not added:\n%s", d.Doc.Bytes())
	}
	if len(m.Courses[1].Trainers) != 1 || m.Courses[1].Trainers[0] != "Dr. Gernot Starke" {
		t.Errorf("trainers = %v", m.Courses[1].Trainers)
	}
}

func TestNewCourseRejectsDuplicateIDAndMissingTrainer(t *testing.T) {
	gh := fakeGitHub(t, nil)
	defer gh.Close()
	s := testServer(t, gh.URL)

	// "msa" already exists in the fixture.
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodPost, "/courses/new", url.Values{
		"id": {"msa"}, "short_title": {"Dup"}, "title": {"Dup"},
		"url": {"https://example.org/x"}, "trainer": {"Dr. Gernot Starke"},
	}))
	if !strings.Contains(rec.Body.String(), "already exists") {
		t.Errorf("duplicate course id was accepted:\n%s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodPost, "/courses/new", url.Values{
		"id": {"flex"}, "short_title": {"FLEX"}, "title": {"Flexible"},
		"url": {"https://example.org/flex"},
	}))
	if !strings.Contains(rec.Body.String(), "at least one trainer") {
		t.Errorf("course without a trainer was accepted:\n%s", rec.Body.String())
	}
}

// TestExistingTrainerNamesArePreserved guards the deliberate decision not to
// normalise. _data/trainings.yml stores "Peter Hruschka"; the roster offers
// "Dr. Peter Hruschka". Opening and re-saving a date must not quietly rename a
// trainer on the public website.
func TestExistingTrainerNamesArePreserved(t *testing.T) {
	gh := fakeGitHub(t, nil)
	defer gh.Close()
	s := testServer(t, gh.URL)

	stored := []string{"Peter Hruschka"}
	if got := otherTrainers(stored); len(got) != 1 || got[0] != "Peter Hruschka" {
		t.Fatalf("otherTrainers(%v) = %v — an off-roster name must survive verbatim", stored, got)
	}

	req := signedIn(t, s, http.MethodPost, "/dates/msa-a", url.Values{
		"course_id": {"msa"}, "id": {"msa-a"}, "code": {"26-01 MSA"},
		"start": {"2026-01-01"}, "end": {"2026-01-02"}, "city": {"München"},
		"language": {"de"}, "format": {"public"}, "status": {"open"},
		"url": {"https://example.org/a"}, "trainers_other": {"Peter Hruschka"},
	})
	s.Routes().ServeHTTP(httptest.NewRecorder(), req)

	d, _ := s.drafts.Get("sid")
	if !strings.Contains(string(d.Doc.Bytes()), `"Peter Hruschka"`) {
		t.Errorf("stored trainer name was rewritten:\n%s", d.Doc.Bytes())
	}
	if strings.Contains(string(d.Doc.Bytes()), `"Dr. Peter Hruschka"`) {
		t.Error("a title was silently added to a published trainer name")
	}
}

// TestDeniedPageIsInformativeNotAnError covers the screen most people who ever
// click the site's "Maintainers" link will see. Being turned away is the
// EXPECTED outcome for everyone except two people, so the page has to explain
// and redirect attention, not report a failure.
func TestDeniedPageIsInformativeNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			_, _ = w.Write([]byte(`{"login":"curious-visitor"}`))
		default:
			_, _ = w.Write([]byte(`{"permissions":{"push":false}}`))
		}
	}))
	defer srv.Close()
	s := testServer(t, srv.URL)

	rec := httptest.NewRecorder()
	s.renderDenied(rec, "curious-visitor")
	body := rec.Body.String()

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	// Names who you are — the fact that makes the wrong-account case solvable.
	if !strings.Contains(body, "curious-visitor") {
		t.Error("page does not say which account is signed in")
	}
	// Sends them where they actually wanted to go.
	if !strings.Contains(body, "https://trainings.arc42.org") {
		t.Error("page does not offer the public training dates")
	}
	// Lets a maintainer who picked the wrong account recover.
	if !strings.Contains(body, "/auth/logout") {
		t.Error("page offers no way to sign out and switch account")
	}
	// Must NOT dangle links the visitor cannot use.
	if strings.Contains(body, `href="/courses"`) {
		t.Error("masthead still offers Courses, which is a dead end here")
	}
	// Must not imply a request process that does not exist.
	for _, forbidden := range []string{"grant push access", "request access", "No access"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("page still contains unhelpful copy: %q", forbidden)
		}
	}
}

// TestNavIsHiddenBeforeAuthentication: the masthead offered "Dates" and
// "Courses" on the sign-in page. Both bounce straight back to sign-in, so they
// are pure dead ends — and they advertise structure to someone who has not
// established they may see any of it.
func TestNavIsHiddenBeforeAuthentication(t *testing.T) {
	gh := fakeGitHub(t, nil)
	defer gh.Close()
	s := testServer(t, gh.URL)

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	if !strings.Contains(body, "Sign in with GitHub") {
		t.Fatalf("expected the sign-in page:\n%s", body)
	}
	for _, dead := range []string{`href="/courses"`, `>Dates<`} {
		if strings.Contains(body, dead) {
			t.Errorf("sign-in page still shows nav item %s", dead)
		}
	}

	// After signing in it must come back.
	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/", nil))
	body = rec.Body.String()
	if !strings.Contains(body, `href="/courses"`) {
		t.Error("nav is missing for an authenticated maintainer")
	}
}
