package config

import (
	"strings"
	"testing"
)

const goodKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // 32 bytes

func base(t *testing.T) {
	t.Helper()
	t.Setenv("TOKEN_KEY", goodKey)
	t.Setenv("MAILER", "mailjet")
	t.Setenv("MJ_APIKEY_PUBLIC", "0123456789abcdef0123456789abcdef")
	t.Setenv("MJ_APIKEY_PRIVATE", "fedcba9876543210fedcba9876543210")
	t.Setenv("BACKOFFICE_TO", "office@example.org")
	t.Setenv("PUBLIC_URL", "https://arc42-registration.fly.dev/")
	t.Setenv("ENVIRONMENT", "")
	t.Setenv("TEST_RECIPIENTS", "Me@Example.org, you@example.org")
}

func TestLoadAcceptsATestSetup(t *testing.T) {
	base(t)
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.TestMode() || c.TestRecipients[0] != "me@example.org" {
		t.Errorf("TestRecipients = %v, want lower-cased list", c.TestRecipients)
	}
	if c.PublicURL != "https://arc42-registration.fly.dev" {
		t.Errorf("PublicURL = %q, want trailing slash trimmed", c.PublicURL)
	}
	if c.MailFrom != "trainings@arc42.org" || c.ReplyTo != "info@arc42.de" {
		t.Errorf("defaults: from=%q reply=%q", c.MailFrom, c.ReplyTo)
	}
}

// The guard the spec asks for: a deployment that is not production and has
// no allow-list must not start, or it would mail whatever address a form
// submission names.
func TestLoadRefusesATestDeploymentWithoutAllowList(t *testing.T) {
	base(t)
	t.Setenv("TEST_RECIPIENTS", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "TEST_RECIPIENTS") {
		t.Fatalf("Load = %v, want a refusal naming TEST_RECIPIENTS", err)
	}
}

func TestLoadAllowsProductionWithoutAllowList(t *testing.T) {
	base(t)
	t.Setenv("TEST_RECIPIENTS", "")
	t.Setenv("ENVIRONMENT", "PRODUCTION")
	if _, err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestLoadRejectsBadSettings(t *testing.T) {
	cases := map[string][2]string{
		"short token key":     {"TOKEN_KEY", "c2hvcnQ="},
		"token key not b64":   {"TOKEN_KEY", "!!!"},
		"placeholder mailjet": {"MJ_APIKEY_PRIVATE", "x"},
		"unknown mailer":      {"MAILER", "smtp"},
		"no back office":      {"BACKOFFICE_TO", ""},
		"no public url":       {"PUBLIC_URL", ""},
	}
	for name, kv := range cases {
		t.Run(name, func(t *testing.T) {
			base(t)
			t.Setenv(kv[0], kv[1])
			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted %s=%q", kv[0], kv[1])
			}
		})
	}
}

func TestLogMailerNeedsNoKeysAndNoAllowList(t *testing.T) {
	base(t)
	t.Setenv("MAILER", "log")
	t.Setenv("MJ_APIKEY_PUBLIC", "")
	t.Setenv("TEST_RECIPIENTS", "")
	if _, err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
}
