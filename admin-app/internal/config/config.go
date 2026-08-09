// Package config loads and validates the app's environment configuration.
package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Addr         string
	GitHubRepo   string
	ClientID     string
	ClientSecret string
	SessionKey   string
	Environment  string
	// PublicURL is the origin GitHub redirects back to. It must match the
	// OAuth app's registered callback byte for byte, so it is configurable:
	// a renamed fly app or a custom domain must not need a code change.
	PublicURL string
}

// Load reads configuration from the environment, applying defaults and
// failing loudly on anything missing that the app cannot invent.
func Load() (Config, error) {
	c := Config{
		Addr:         envOr("ADDR", ":8080"),
		GitHubRepo:   envOr("GITHUB_REPO", "arc42/trainings.arc42.org-site"),
		ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		SessionKey:   os.Getenv("SESSION_KEY"),
		// Defaults to PRODUCTION because that is now the only place this app
		// runs (there is no local mode). Environment still gates the Secure
		// cookie flag and the https check below, so the default has to be the
		// strict one: an ENVIRONMENT accidentally dropped from fly.toml must
		// not silently downgrade session cookies. DEVELOPMENT remains valid
		// and is what the tests use.
		Environment: envOr("ENVIRONMENT", "PRODUCTION"),
	}
	defaultPublic := "https://arc42-trainings-admin.fly.dev"
	if c.Environment != "PRODUCTION" {
		defaultPublic = "http://localhost:8080"
	}
	// A trailing slash would build "…//auth/callback", which GitHub rejects as
	// a redirect_uri mismatch — an error that reads as an app bug, not a typo.
	c.PublicURL = strings.TrimRight(envOr("PUBLIC_URL", defaultPublic), "/")
	// Real GitHub OAuth credentials are long: client ids are 20 hex characters
	// (classic) or ~22 starting "Ov23li" (current); secrets are 40 hex. A short
	// value is always a placeholder, and letting one through is expensive — the
	// app builds a valid-looking authorize URL, the user signs in, and GitHub
	// answers 404 for the unknown client_id with nothing logged locally to say
	// why. Failing here turns that into one readable line at startup.
	const minCredentialLen = 16

	var missing []string
	switch {
	case c.ClientID == "":
		missing = append(missing, "GITHUB_CLIENT_ID")
	case len(c.ClientID) < minCredentialLen:
		missing = append(missing, fmt.Sprintf("GITHUB_CLIENT_ID (%q is too short to be real)", c.ClientID))
	}
	switch {
	case c.ClientSecret == "":
		missing = append(missing, "GITHUB_CLIENT_SECRET")
	case len(c.ClientSecret) < minCredentialLen:
		missing = append(missing, "GITHUB_CLIENT_SECRET (too short to be real)")
	}
	if len(c.SessionKey) < 32 {
		// Unlike the OAuth pair, this one is not issued by anybody — it is a
		// local random key for encrypting the session cookie. Say so, or the
		// reader goes hunting for a value that does not exist.
		missing = append(missing, "SESSION_KEY (not issued by anyone — generate one: openssl rand -hex 32)")
	}
	if !strings.Contains(c.GitHubRepo, "/") {
		missing = append(missing, `GITHUB_REPO (needs "owner/name" form)`)
	}
	if c.Environment == "PRODUCTION" && !strings.HasPrefix(c.PublicURL, "https://") {
		missing = append(missing, fmt.Sprintf("PUBLIC_URL (%q must be https:// in PRODUCTION)", c.PublicURL))
	}
	if len(missing) > 0 {
		// The only place this app runs is fly.io, so the only place these come
		// from is fly secrets. Naming the command beats naming the variables:
		// this message is read in a crash log, minutes after a deploy.
		return Config{}, fmt.Errorf("missing configuration: %s\n\n"+
			"Set these with: flyctl secrets set NAME=value -a arc42-trainings-admin\n"+
			"(GITHUB_REPO, ENVIRONMENT and PUBLIC_URL come from fly.toml instead)",
			strings.Join(missing, ", "))
	}
	return c, nil
}

// Repo splits GitHubRepo into its owner and name halves.
func (c Config) Repo() (owner, name string) {
	owner, name, _ = strings.Cut(c.GitHubRepo, "/")
	return owner, name
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
