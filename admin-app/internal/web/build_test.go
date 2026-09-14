package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The footer's build line is how a maintainer tells whether a deploy landed,
// so both shapes are checked: a stamped build, and the unstamped one every
// test and `go run` produces, which must not render as an empty "Build ,".
func TestFooterShowsTheBuild(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	get := func(req *http.Request) string {
		rec := httptest.NewRecorder()
		s.Routes().ServeHTTP(rec, req)
		return rec.Body.String()
	}

	if body := get(httptest.NewRequest(http.MethodGet, "/", nil)); !strings.Contains(body, "Development build") {
		t.Errorf("an unstamped binary does not say it is a development build:\n%s", body)
	}

	oldCommit, oldTime := buildCommit, buildTime
	defer func() { buildCommit, buildTime = oldCommit, oldTime }()
	buildCommit = "e2f7d43c0ffee0123456789abcdef0123456789a-dirty"
	buildTime = "2026-09-14T11:25:08Z"

	// Signed in too: the footer is in the layout, not only on the sign-in page.
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/", nil),
		signedIn(t, s, http.MethodGet, "/courses", nil),
	} {
		body := get(req)
		for _, want := range []string{
			`href="https://github.com/arc42/site/commit/e2f7d43c0ffee0123456789abcdef0123456789a"`,
			"<code>e2f7d43</code>",
			"plus uncommitted changes",
			"deployed 2026-09-14 11:25 UTC",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: footer lacks %s", req.URL.Path, want)
			}
		}
	}
}
