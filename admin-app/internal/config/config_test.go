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

func TestPublicURLDefaultsPerEnvironment(t *testing.T) {
	realish := func(t *testing.T) {
		t.Helper()
		t.Setenv("GITHUB_CLIENT_ID", "Ov23liABCDEFGHIJKLMN")
		t.Setenv("GITHUB_CLIENT_SECRET", "0123456789abcdef0123456789abcdef01234567")
		t.Setenv("SESSION_KEY", "0123456789abcdef0123456789abcdef")
	}

	// Unset means PRODUCTION: the app runs nowhere else, and the strict
	// branch (Secure cookies, https required) has to be what you get by
	// forgetting to say.
	t.Run("unset defaults to production", func(t *testing.T) {
		realish(t)
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.Environment != "PRODUCTION" {
			t.Errorf("Environment = %q, want PRODUCTION", c.Environment)
		}
		if c.PublicURL != "https://arc42-trainings-admin.fly.dev" {
			t.Errorf("PublicURL = %q", c.PublicURL)
		}
	})

	t.Run("development", func(t *testing.T) {
		realish(t)
		t.Setenv("ENVIRONMENT", "DEVELOPMENT")
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.PublicURL != "http://localhost:8080" {
			t.Errorf("PublicURL = %q", c.PublicURL)
		}
	})

	t.Run("production", func(t *testing.T) {
		realish(t)
		t.Setenv("ENVIRONMENT", "PRODUCTION")
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.PublicURL != "https://arc42-trainings-admin.fly.dev" {
			t.Errorf("PublicURL = %q", c.PublicURL)
		}
	})

	// A custom domain or a differently-named fly app must not require a code
	// change: the registered OAuth callback has to match byte for byte.
	t.Run("override", func(t *testing.T) {
		realish(t)
		t.Setenv("ENVIRONMENT", "PRODUCTION")
		t.Setenv("PUBLIC_URL", "https://admin.arc42.org/")
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		// Trailing slash stripped, or the callback becomes "…//auth/callback"
		// and GitHub rejects it as a mismatch.
		if c.PublicURL != "https://admin.arc42.org" {
			t.Errorf("PublicURL = %q, want the trailing slash stripped", c.PublicURL)
		}
	})

	t.Run("production must be https", func(t *testing.T) {
		realish(t)
		t.Setenv("ENVIRONMENT", "PRODUCTION")
		t.Setenv("PUBLIC_URL", "http://admin.arc42.org")
		if _, err := Load(); err == nil {
			t.Fatal("Load accepted a plain-http PUBLIC_URL in PRODUCTION")
		}
	})
}
