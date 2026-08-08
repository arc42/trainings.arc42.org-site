package config

import "testing"

func TestLoadRequiresClientID(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "")
	t.Setenv("GITHUB_CLIENT_SECRET", "s")
	t.Setenv("SESSION_KEY", "0123456789abcdef0123456789abcdef")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when GITHUB_CLIENT_ID is empty")
	}
}

func TestLoadDefaultsRepoAndAddr(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "id")
	t.Setenv("GITHUB_CLIENT_SECRET", "secret")
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
