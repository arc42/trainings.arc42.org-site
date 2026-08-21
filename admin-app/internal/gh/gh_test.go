package gh

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestViewerReportsPushPermission(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"login": "gernotstarke"})
		case "/repos/arc42/trainings.arc42.org-site":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"permissions": map[string]any{"push": true},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "arc42", "trainings.arc42.org-site", "tok")
	login, canPush, err := c.Viewer(context.Background())
	if err != nil {
		t.Fatalf("Viewer: %v", err)
	}
	if login != "gernotstarke" || !canPush {
		t.Errorf("Viewer = %q, %v", login, canPush)
	}
}

func TestViewerDeniesWithoutPush(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			_ = json.NewEncoder(w).Encode(map[string]any{"login": "stranger"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"permissions": map[string]any{"push": false},
		})
	}))
	defer srv.Close()

	_, canPush, err := New(srv.URL, "arc42", "trainings.arc42.org-site", "tok").Viewer(context.Background())
	if err != nil {
		t.Fatalf("Viewer: %v", err)
	}
	if canPush {
		t.Error("canPush = true for a user without push permission")
	}
}

func TestReadFileDecodesAndReturnsSHAs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/arc42/site/contents/_data/trainings.yml":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content":  base64.StdEncoding.EncodeToString([]byte("courses: []\n")),
				"encoding": "base64",
				"sha":      "filesha",
			})
		case "/repos/arc42/site/git/ref/heads/main":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": map[string]any{"sha": "headsha"},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	content, sha, head, err := New(srv.URL, "arc42", "site", "tok").
		ReadFile(context.Background(), "_data/trainings.yml")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "courses: []\n" || sha != "filesha" || head != "headsha" {
		t.Errorf("ReadFile = %q, %q, %q", content, sha, head)
	}
}

// TestOpenPRCallSequence pins the publish contract: create a ref from the base
// SHA, PUT the file onto that branch, then open the PR. Getting this order
// wrong is the difference between a clean PR and a commit on main.
func TestOpenPRCallSequence(t *testing.T) {
	var calls []string
	var putBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == "POST" && r.URL.Path == "/repos/arc42/site/git/refs":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"ref":"refs/heads/trainings-admin/x"}`))
		case r.Method == "PUT" && r.URL.Path == "/repos/arc42/site/contents/_data/trainings.yml":
			_ = json.NewDecoder(r.Body).Decode(&putBody)
			_, _ = w.Write([]byte(`{"commit":{"sha":"newsha"}}`))
		case r.Method == "POST" && r.URL.Path == "/repos/arc42/site/pulls":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"html_url":"https://github.com/arc42/site/pull/42"}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	url, err := New(srv.URL, "arc42", "site", "tok").OpenPR(context.Background(), PRRequest{
		Branch:  "trainings-admin/x",
		Path:    "_data/trainings.yml",
		Content: "courses: []\n",
		BaseSHA: "headsha",
		FileSHA: "filesha",
		Title:   "Training dates: 1 change",
		Body:    "- updated a",
	})
	if err != nil {
		t.Fatalf("OpenPR: %v", err)
	}
	if url != "https://github.com/arc42/site/pull/42" {
		t.Errorf("url = %q", url)
	}
	want := []string{
		"POST /repos/arc42/site/git/refs",
		"PUT /repos/arc42/site/contents/_data/trainings.yml",
		"POST /repos/arc42/site/pulls",
	}
	for i := range want {
		if i >= len(calls) || calls[i] != want[i] {
			t.Fatalf("call sequence = %v, want %v", calls, want)
		}
	}
	if putBody["branch"] != "trainings-admin/x" {
		t.Errorf("PUT targeted branch %v, not the new branch", putBody["branch"])
	}
	if putBody["sha"] != "filesha" {
		t.Errorf("PUT sha = %v, want the file's blob sha", putBody["sha"])
	}
}

// TestEndpointForPairsSignInWithTheAPIHost pins the rule that lets the offline
// demo run the real sign-in flow: whoever answers the REST API also answers the
// OAuth endpoints. Production must keep pointing at github.com, and anything
// else must point at itself — never at github.com, which would send a demo
// user's browser to a real sign-in screen for an app id that does not exist.
func TestEndpointForPairsSignInWithTheAPIHost(t *testing.T) {
	prod := EndpointFor("https://api.github.com")
	if !strings.HasPrefix(prod.AuthURL, "https://github.com/") ||
		!strings.HasPrefix(prod.TokenURL, "https://github.com/") {
		t.Errorf("production sign-in does not go to github.com: %+v", prod)
	}
	if def := EndpointFor(""); def != prod {
		t.Errorf("an unset API base must mean production, got %+v", def)
	}

	standIn := EndpointFor("http://127.0.0.1:9999")
	if standIn.AuthURL != "http://127.0.0.1:9999/login/oauth/authorize" {
		t.Errorf("AuthURL = %q", standIn.AuthURL)
	}
	if standIn.TokenURL != "http://127.0.0.1:9999/login/oauth/access_token" {
		t.Errorf("TokenURL = %q", standIn.TokenURL)
	}
}
