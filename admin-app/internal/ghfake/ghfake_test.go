package ghfake_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"arc42-trainings-admin/internal/ghfake"
)

// The point of these tests is not that the fake works — it is that the fake
// says no where GitHub says no. A double that accepts everything makes the
// suite that depends on it worthless, which is exactly how the duplicate-branch
// bug reached production.

func newFake(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	f := ghfake.New(ghfake.Options{
		Repo: "arc42/site", Login: "someone", CanPush: true,
		Files: map[string][]byte{"_data/trainings.yml": []byte("courses: []\n")},
	})
	srv := httptest.NewServer(f.Handler())
	t.Cleanup(srv.Close)
	return srv, srv.URL + "/repos/arc42/site"
}

func post(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestCreatingTheSameRefTwiceIsRefused(t *testing.T) {
	_, repo := newFake(t)
	ref := map[string]string{"ref": "refs/heads/trainings-admin/x", "sha": "abc"}

	if got := post(t, repo+"/git/refs", ref).StatusCode; got != http.StatusCreated {
		t.Fatalf("first create: status = %d, want 201", got)
	}
	if got := post(t, repo+"/git/refs", ref).StatusCode; got != http.StatusUnprocessableEntity {
		t.Errorf("second create: status = %d, want 422 — GitHub refuses an existing ref", got)
	}
}

func TestCommittingOverAMovedFileIsRefused(t *testing.T) {
	srv, repo := newFake(t)
	post(t, repo+"/git/refs", map[string]string{"ref": "refs/heads/b", "sha": "abc"})

	// The blob sha the app would have read at load time.
	resp, err := http.Get(srv.URL + "/repos/arc42/site/contents/_data/trainings.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var file struct{ SHA string }
	_ = json.NewDecoder(resp.Body).Decode(&file)
	if file.SHA == "" {
		t.Fatal("no blob sha in the contents response")
	}

	put := func(sha string) int {
		b, _ := json.Marshal(map[string]string{
			"message": "m", "branch": "b", "sha": sha,
			"content": base64.StdEncoding.EncodeToString([]byte("courses: []\n# edited\n")),
		})
		req, _ := http.NewRequest(http.MethodPut, repo+"/contents/_data/trainings.yml", bytes.NewReader(b))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if got := put("somebody-elses-sha"); got != http.StatusConflict {
		t.Errorf("stale sha: status = %d, want 409 — that refusal is the app's concurrent-edit guard", got)
	}
	if got := put(file.SHA); got != http.StatusOK {
		t.Errorf("current sha: status = %d, want 200", got)
	}
}

func TestAnotherRepositoryIsNotFound(t *testing.T) {
	srv, _ := newFake(t)
	resp, err := http.Get(srv.URL + "/repos/someone/else/contents/_data/trainings.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404: the fake serves exactly one repository", resp.StatusCode)
	}
}

func TestPublishingLeavesMainAlone(t *testing.T) {
	srv, repo := newFake(t)
	post(t, repo+"/git/refs", map[string]string{"ref": "refs/heads/b", "sha": "abc"})

	resp, err := http.Get(srv.URL + "/repos/arc42/site/contents/_data/trainings.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var before struct{ Content, SHA string }
	_ = json.NewDecoder(resp.Body).Decode(&before)

	b, _ := json.Marshal(map[string]string{
		"message": "m", "branch": "b", "sha": before.SHA,
		"content": base64.StdEncoding.EncodeToString([]byte("courses: []\n# proposed\n")),
	})
	req, _ := http.NewRequest(http.MethodPut, repo+"/contents/_data/trainings.yml", bytes.NewReader(b))
	if _, err := http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	}

	after, err := http.Get(srv.URL + "/repos/arc42/site/contents/_data/trainings.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer after.Body.Close()
	var now struct{ Content, SHA string }
	_ = json.NewDecoder(after.Body).Decode(&now)
	if now.Content != before.Content {
		// A commit lands on a branch; a pull request does not change main. A
		// demo that quietly published would teach the opposite of how the app
		// works — and the file on disk it read from is never written either.
		t.Error("committing to a branch changed the content served for main")
	}
}
