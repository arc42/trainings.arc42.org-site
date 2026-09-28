// Package config reads the service's settings from the environment. Every
// check here guards a mistake that would otherwise surface as a silent
// misdelivery: mail to the world from a test deployment, or confirmation links
// nobody can open.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Addr        string
	Environment string // "PRODUCTION" or anything else

	Mailer         string // "mailjet" (default), "brevo", or "log" (no provider: mails go to the log)
	MailjetPublic  string
	MailjetPrivate string
	BrevoKey       string
	// Turso holds which registrations are confirmed and how many wrong
	// codes each got (package store). Required in production; a test
	// deployment without it uses an in-memory store that forgets.
	TursoURL, TursoToken string

	TokenKey []byte // 32 bytes, AES-256

	MailFrom     string
	MailFromName string
	BackofficeTo []string // one or more mailboxes, comma-separated in BACKOFFICE_TO
	ReplyTo      string

	FeedURL   string // the public course feed
	SiteURL   string // where the success, fail and confirmed pages live
	PublicURL string // this service's own base URL, for confirm links

	AllowedOrigins []string
	TestRecipients []string // non-empty means test mode
}

// TestMode is on whenever an allow-list is configured. Outside production it
// is mandatory (see Load), so a test deployment cannot mail the world.
func (c Config) TestMode() bool { return len(c.TestRecipients) > 0 }

func Load() (Config, error) {
	c := Config{
		Addr:           ":" + env("PORT", "8080"),
		Environment:    os.Getenv("ENVIRONMENT"),
		Mailer:         env("MAILER", "mailjet"),
		MailjetPublic:  os.Getenv("MJ_APIKEY_PUBLIC"),
		MailjetPrivate: os.Getenv("MJ_APIKEY_PRIVATE"),
		BrevoKey:       os.Getenv("BREVO_API_KEY"),
		TursoURL:       os.Getenv("TURSO_DATABASE_URL"),
		TursoToken:     os.Getenv("TURSO_AUTH_TOKEN"),
		MailFrom:       env("MAIL_FROM", "trainings@arc42.org"),
		MailFromName:   env("MAIL_FROM_NAME", "arc42 Trainings"),
		BackofficeTo:   list(os.Getenv("BACKOFFICE_TO")),
		ReplyTo:        env("REPLY_TO", "info@arc42.de"),
		FeedURL:        env("FEED_URL", "https://trainings.arc42.org/api/trainings.json"),
		SiteURL:        strings.TrimRight(env("SITE_URL", "https://trainings.arc42.org"), "/"),
		PublicURL:      strings.TrimRight(os.Getenv("PUBLIC_URL"), "/"),
		AllowedOrigins: list(env("ALLOWED_ORIGINS", "https://trainings.arc42.org")),
		TestRecipients: list(os.Getenv("TEST_RECIPIENTS")),
	}

	key, err := base64.StdEncoding.DecodeString(os.Getenv("TOKEN_KEY"))
	if err != nil || len(key) != 32 {
		return Config{}, errors.New("TOKEN_KEY must be 32 random bytes, base64-encoded (openssl rand -base64 32)")
	}
	c.TokenKey = key

	switch c.Mailer {
	case "log":
	case "mailjet":
		// Same lesson as the admin app's config: a placeholder that is merely
		// non-empty sails through a presence check and fails only at the
		// first real send, in production, with a 401 nobody sees.
		if len(c.MailjetPublic) < 20 || len(c.MailjetPrivate) < 20 {
			return Config{}, errors.New("MJ_APIKEY_PUBLIC and MJ_APIKEY_PRIVATE are missing or too short to be real Mailjet keys")
		}
	case "brevo":
		// Brevo API keys are one string, "xkeysib-" and about 80 more
		// characters. Same reason as above for refusing a placeholder.
		if !strings.HasPrefix(c.BrevoKey, "xkeysib-") || len(c.BrevoKey) < 40 {
			return Config{}, errors.New("BREVO_API_KEY is missing or does not look like a Brevo API key (xkeysib-...)")
		}
	default:
		return Config{}, fmt.Errorf("MAILER must be mailjet, brevo or log, not %q", c.Mailer)
	}

	if len(c.BackofficeTo) == 0 {
		return Config{}, errors.New("BACKOFFICE_TO is required")
	}
	// Mail clients separate with semicolons, this list with commas. A
	// semicolon here would reach Mailjet as one broken address and fail
	// every back-office mail, silently, in the logs.
	for _, a := range c.BackofficeTo {
		if strings.ContainsAny(a, "; ") || !strings.Contains(a, "@") {
			return Config{}, fmt.Errorf("BACKOFFICE_TO: %q is not one address; separate several with commas", a)
		}
	}
	if c.PublicURL == "" {
		return Config{}, errors.New("PUBLIC_URL is required: it is the base of every confirm link")
	}
	if (c.TursoURL == "") != (c.TursoToken == "") {
		return Config{}, errors.New("TURSO_DATABASE_URL and TURSO_AUTH_TOKEN go together: set both or neither")
	}
	if c.Environment == "PRODUCTION" && c.TursoURL == "" {
		return Config{}, errors.New("refusing to start: production needs TURSO_DATABASE_URL and TURSO_AUTH_TOKEN, or confirm links would work more than once")
	}
	if c.Environment != "PRODUCTION" && c.Mailer != "log" && !c.TestMode() {
		return Config{}, errors.New("refusing to start: outside ENVIRONMENT=PRODUCTION, TEST_RECIPIENTS must list who may receive mail")
	}
	return c, nil
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func list(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, strings.ToLower(p))
		}
	}
	return out
}
