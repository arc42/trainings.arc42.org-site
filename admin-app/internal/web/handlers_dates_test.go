package web

import (
	"encoding/base64"
	"encoding/json"
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
		"url":   {"https://example.org/msa"}, "trainers": {"Peter Hruschka, Gernot Starke"},
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
