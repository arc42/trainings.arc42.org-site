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
		Environment:  envOr("ENVIRONMENT", "DEVELOPMENT"),
	}
	var missing []string
	if c.ClientID == "" {
		missing = append(missing, "GITHUB_CLIENT_ID")
	}
	if c.ClientSecret == "" {
		missing = append(missing, "GITHUB_CLIENT_SECRET")
	}
	if len(c.SessionKey) < 32 {
		missing = append(missing, "SESSION_KEY (needs >= 32 chars)")
	}
	if !strings.Contains(c.GitHubRepo, "/") {
		missing = append(missing, `GITHUB_REPO (needs "owner/name" form)`)
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing configuration: %s\n\nCopy admin-app/.env.template to admin-app/.env and fill it in", strings.Join(missing, ", "))
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
