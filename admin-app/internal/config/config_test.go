package config

import (
	"strings"
	"testing"
)

func TestLoadRequiresClientID(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "")
	t.Setenv("GITHUB_CLIENT_SECRET", "s")
	t.Setenv("SESSION_KEY", "0123456789abcdef0123456789abcdef")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when GITHUB_CLIENT_ID is empty")
	}
}

func TestLoadDefaultsRepoAndAddr(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "Ov23liABCDEFGHIJKLMN")
	t.Setenv("GITHUB_CLIENT_SECRET", "0123456789abcdef0123456789abcdef01234567")
	t.Setenv("SESSION_KEY", "0123456789abcdef0123456789abcdef")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.GitHubRepo != "arc42/trainings.arc42.org-site" {
		t.Errorf("GitHubRepo = %q", c.GitHubRepo)
	}
	if c.Addr != ":8080" {
		t.Errorf("Addr = %q", c.Addr)
	}
}

func TestRepoSplits(t *testing.T) {
	c := Config{GitHubRepo: "gernotstarke/trainings.arc42.org-site"}
	owner, name := c.Repo()
	if owner != "gernotstarke" || name != "trainings.arc42.org-site" {
		t.Errorf("Repo() = %q, %q", owner, name)
	}
}

// TestLoadRejectsPlaceholderCredentials pins a failure that cost a real
// debugging session. The smoke-test recipe appended GITHUB_CLIENT_ID=x to
// .env; Load only checked for emptiness, so "x" sailed through, and the app
// dutifully built an authorize URL for a client_id that does not exist. The
// user authenticated with a passkey and got an unexplained 404 from
// github.com — a failure with no local log line and no obvious cause.
// Credentials that cannot possibly be real must fail here, loudly, instead.
func TestLoadRejectsPlaceholderCredentials(t *testing.T) {
	cases := []struct {
		name, id, secret string
	}{
		{"dummy id", "x", "0123456789abcdef0123456789abcdef01234567"},
		{"dummy secret", "0123456789abcdef0123", "y"},
		{"both dummy", "x", "y"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("GITHUB_CLIENT_ID", c.id)
			t.Setenv("GITHUB_CLIENT_SECRET", c.secret)
			t.Setenv("SESSION_KEY", "0123456789abcdef0123456789abcdef")
			_, err := Load()
			if err == nil {
				t.Fatalf("Load accepted id=%q secret=%q", c.id, c.secret)
			}
			if !strings.Contains(err.Error(), "too short to be real") {
				t.Errorf("error should explain the credential is implausible, got: %v", err)
			}
		})
	}
}

func TestLoadAcceptsRealisticCredentials(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "Ov23liABCDEFGHIJKLMN")
	t.Setenv("GITHUB_CLIENT_SECRET", "0123456789abcdef0123456789abcdef01234567")
	t.Setenv("SESSION_KEY", "0123456789abcdef0123456789abcdef")
	if _, err := Load(); err != nil {
		t.Fatalf("Load rejected realistic credentials: %v", err)
	}
}
