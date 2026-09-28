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

// The back office can be more than one mailbox (info@arc42.de and
// trainings@arc42.org). Same syntax as TEST_RECIPIENTS: commas. A semicolon
// is refused rather than handed to Mailjet as one broken address, which
// would fail every back-office mail.
func TestBackofficeToIsACommaSeparatedList(t *testing.T) {
	base(t)
	t.Setenv("BACKOFFICE_TO", "info@arc42.de, Trainings@arc42.org")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.BackofficeTo) != 2 || c.BackofficeTo[0] != "info@arc42.de" || c.BackofficeTo[1] != "trainings@arc42.org" {
		t.Errorf("BackofficeTo = %v", c.BackofficeTo)
	}
	t.Setenv("BACKOFFICE_TO", "info@arc42.de; trainings@arc42.org")
	if _, err := Load(); err == nil {
		t.Error("a semicolon-separated BACKOFFICE_TO was accepted")
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

// Brevo is the second provider, chosen with MAILER=brevo. Its key is one
// string starting "xkeysib-"; a placeholder must stop the start, like a
// placeholder Mailjet key does, instead of failing at the first real send.
func TestBrevoNeedsARealLookingKey(t *testing.T) {
	base(t)
	t.Setenv("MAILER", "brevo")
	t.Setenv("MJ_APIKEY_PUBLIC", "")
	t.Setenv("MJ_APIKEY_PRIVATE", "")
	t.Setenv("BREVO_API_KEY", "xkeysib-0123456789abcdef0123456789abcdef-AbCdEf")
	c, err := Load()
	if err != nil || c.Mailer != "brevo" || c.BrevoKey == "" {
		t.Fatalf("Load with a Brevo key: %v %+v", err, c.Mailer)
	}
	for _, bad := range []string{"", "x", "sk-0123456789abcdef0123456789abcdef"} {
		t.Setenv("BREVO_API_KEY", bad)
		if _, err := Load(); err == nil {
			t.Errorf("BREVO_API_KEY %q was accepted", bad)
		}
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
