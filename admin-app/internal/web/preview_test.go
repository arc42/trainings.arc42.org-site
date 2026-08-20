package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestDumpPreview is a developer aid, not a check: it writes the rendered forms
// to disk so they can be opened in a browser. Skipped unless PREVIEW_DIR is set.
func TestDumpPreview(t *testing.T) {
	dir := os.Getenv("PREVIEW_DIR")
	if dir == "" {
		t.Skip("set PREVIEW_DIR to dump rendered pages")
	}
	gh := fakeGitHub(t, nil)
	defer gh.Close()
	s := testServer(t, gh.URL)

	for name, target := range map[string]string{
		"date-new.html":   "/dates/new",
		"date-edit.html":  "/dates/msa-a",
		"course-new.html": "/courses/new",
		"courses.html":    "/courses",
		"confirm.html":    "/dates/msa-a/delete",
		"list.html":       "/",
	} {
		rec := httptest.NewRecorder()
		s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, target, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s -> %d: %s", target, rec.Code, rec.Body.String())
		}
		if err := os.WriteFile(dir+"/"+name, rec.Body.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The sign-in page: same route, no session.
	rec0 := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec0, httptest.NewRequest(http.MethodGet, "/", nil))
	if err := os.WriteFile(dir+"/login.html", rec0.Body.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	// The denied page has no route of its own — it is rendered from the OAuth
	// callback — so drive the handler directly.
	rec := httptest.NewRecorder()
	s.renderDenied(rec, "curious-visitor")
	if err := os.WriteFile(dir+"/denied.html", rec.Body.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	css, err := assets.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/static/app.css", css, 0o644); err != nil {
		t.Fatal(err)
	}
}
