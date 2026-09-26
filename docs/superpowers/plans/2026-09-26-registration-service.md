# Registration Service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the stateless registration service (`registration-app/`, fly app `arc42-registration`), deploy it in test mode, and publish a hidden test page that uses the real registration form against it, while production keeps posting to Formspark.

**Architecture:** A standard-library Go service in its own module next to `admin-app/`. `POST /submit` checks a submission (`intake`, against the public course feed via `feed`), mails the back office and the registrant through Mailjet (`mail` renders, `send` delivers), and seals the confirmation facts into the link (`token`). `GET /confirm` only shows a page; `POST /confirm` sends the second back-office mail. No database, no volume.

**Tech Stack:** Go 1.23 standard library only (`net/http`, `crypto/cipher`, `html/template`, `text/template`, `time/tzdata`), Mailjet Send API v3.1, fly.io, Jekyll for the site pages.

**Spec:** `docs/superpowers/specs/2026-09-25-registration-service-design.md` (read sections 2 to 7). This plan covers rollout stages 3 and 4 (section 7.2). Stage 5 onwards is `docs/superpowers/plans/2026-09-26-registration-go-live.md`.

**Native execution:** do not read this whole file. Follow `docs/superpowers/plans/2026-09-26-execution.md`: read one task at a time by line range, and copy each task's files from `docs/superpowers/plans/registration-service-reference/` (byte-identical to the code blocks here) instead of retyping them.

**Reference implementation:** every file in this plan was compiled and its tests run (`go test ./...`, `go vet`, `gofmt`) before the plan was written, with Go 1.27 in module mode `go 1.23.0`. Copy the code as given; if you change it, keep the tests green.

## Global Constraints

- Module path `arc42-registration`, directory `registration-app/`, `go 1.23.0` in `go.mod`, **no third-party dependencies** (no `go.sum`).
- `registration-app` MUST be added to `exclude:` in `_config.yml` in the same commit that creates the directory (CLAUDE.md: without it the Go source is published on trainings.arc42.org).
- Sender `trainings@arc42.org`, name `arc42 Trainings`, Reply-To for registrant mails `info@arc42.de`.
- Token validity **5 days** (`token.Valid`).
- Back-office mails in the **language of the registration**: DE subject `[trainings.arc42.org] ANMELDUNG <id> <code> (UNBESTÄTIGT|BESTÄTIGT)`, EN `[trainings.arc42.org] REGISTRATION <id> <code> (UNCONFIRMED|CONFIRMED)`.
- The registrant mail contains **no submitted text** and **no early-bird or alumni price**, only the regular `price.amount`.
- `GET /confirm` never sends mail.
- Click and open tracking disabled on every Mailjet message.
- Personal mail addresses (`BACKOFFICE_TO`, `TEST_RECIPIENTS`) are fly **secrets**, never in `fly.toml`: the repository is public.
- The service refuses to start outside `ENVIRONMENT=PRODUCTION` unless `TEST_RECIPIENTS` is set (or `MAILER=log`).
- No em-dashes in user-visible copy (mails, pages).
- Production forms (`/anmeldung/`, `/registration/`) are not changed by this plan except for the optional `endpoint` parameter, which they do not pass.

## Review Focus

- **Several addresses in the Email field** (the input has `multiple`): the registrant mail goes to the first address only, the back office sees all of them plus a hint. Pinned in Task 5 (`TestSeveralEmails`) and Task 8 (`ReplyTo` is `Emails[0]`).
- **The confirm URL leaking the token via Referer or caches**: the confirm and error pages send `Referrer-Policy: no-referrer`, `Cache-Control: no-store`, `X-Robots-Tag: noindex`. Pinned in Task 8 (`TestTheWholeFlowInGerman` checks the Referrer-Policy header).
- **A date that ended yesterday in Berlin but today in UTC** (submissions around midnight): "past" is decided in Europe/Berlin, and the scratch image has no zoneinfo, so `time/tzdata` is compiled in. Pinned in Task 3 (`TestPastIsDecidedInBerlinTime`); Task 9 step 4 builds the Docker image and runs it to prove the zone loads.
- **Umlauts at the length limit**: limits count characters, not bytes, so 4000 `ü` in the comments are accepted. Pinned in Task 5 (`4000 characters of umlauts are fine`).
- **The feed returning an empty course list** (a broken site build): treated as unavailable, not as "every code unknown", so real bookings are not silently dropped. Pinned in Task 3 (`TestAnEmptyFeedIsNotTrusted`).

---

### Task 1: Module skeleton, configuration, build plumbing

**Files:**
- Create: `registration-app/go.mod`, `registration-app/internal/config/config.go`, `registration-app/internal/config/config_test.go`
- Modify: `_config.yml` (`exclude:` list), `Makefile` (variables, `.PHONY`, new targets), `.gitignore` if it lists build outputs
- Create: `.github/workflows/registration-app.yml`

**Interfaces:**
- Produces: `config.Config` (fields `Addr, Environment, Mailer, MailjetPublic, MailjetPrivate, TokenKey []byte, MailFrom, MailFromName, BackofficeTo, ReplyTo, FeedURL, SiteURL, PublicURL, AllowedOrigins []string, TestRecipients []string`), `config.Load() (Config, error)`, `Config.TestMode() bool`.

- [x] **Step 1: Exclude the directory from the site first**

In `_config.yml`, directly under `  - admin-app`, add:

```yaml
  # The registration service's Go source. Same reason as admin-app: anything
  # not excluded is copied into _site/ and published.
  - registration-app
```

- [x] **Step 2: Create the module and the failing test**

`registration-app/go.mod`:

```
module arc42-registration

go 1.23.0
```

`registration-app/internal/config/config_test.go`:

```go
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
```

- [x] **Step 3: Run it to verify it fails**

Run: `cd registration-app && go test ./internal/config`
Expected: FAIL, `undefined: Load`.

- [x] **Step 4: Implement**

`registration-app/internal/config/config.go`:

```go
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

	Mailer         string // "mailjet" (default) or "log" for local runs
	MailjetPublic  string
	MailjetPrivate string

	TokenKey []byte // 32 bytes, AES-256

	MailFrom     string
	MailFromName string
	BackofficeTo string
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
		MailFrom:       env("MAIL_FROM", "trainings@arc42.org"),
		MailFromName:   env("MAIL_FROM_NAME", "arc42 Trainings"),
		BackofficeTo:   os.Getenv("BACKOFFICE_TO"),
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
	default:
		return Config{}, fmt.Errorf("MAILER must be mailjet or log, not %q", c.Mailer)
	}

	if c.BackofficeTo == "" {
		return Config{}, errors.New("BACKOFFICE_TO is required")
	}
	if c.PublicURL == "" {
		return Config{}, errors.New("PUBLIC_URL is required: it is the base of every confirm link")
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
```

- [x] **Step 5: Run it to verify it passes**

Run: `cd registration-app && go test ./internal/config -v`
Expected: PASS, 5 tests.

- [x] **Step 6: Makefile targets**

In `Makefile`: below `APP_DIR     := admin-app` add

```make
REG_DIR     := registration-app
REG_APP     := arc42-registration
```

add `reg-test reg-check reg-run reg-deploy reg-status reg-logs` to `.PHONY`, and add this section after the fly.io section:

```make
# ---------------------------------------------- registration service (Go)
#
# A third program: receives the registration form, mails via Mailjet, confirms
# from a sealed link. Stateless, own fly app. See registration-app/README.md
# and docs/superpowers/specs/2026-09-25-registration-service-design.md.

reg-test: check-go ## Run the registration service's Go tests
	cd $(REG_DIR) && go test ./...

reg-check: check-go ## Tests, vet and gofmt for the registration service (what CI gates on)
	cd $(REG_DIR) && go test ./...
	cd $(REG_DIR) && go vet ./...
	@unformatted=$$(cd $(REG_DIR) && gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		printf "==> gofmt would rewrite:\n%s\n" "$$unformatted"; exit 1; \
	fi
	@printf "==> registration service: tests, vet and gofmt are clean\n"

reg-run: check-go ## Run the registration service on :8099; mails are printed, not sent
	@# A fresh TOKEN_KEY per run: links from an earlier run stop working, which
	@# is fine for a local loop. The live course feed is used.
	cd $(REG_DIR) && MAILER=log PORT=8099 PUBLIC_URL=http://localhost:8099 \
		BACKOFFICE_TO=office@example.invalid ALLOWED_ORIGINS=http://localhost:4260 \
		TOKEN_KEY=$$(openssl rand -base64 32) go run .
```

- [x] **Step 7: CI workflow (tests only for now; Task 9 adds the deploy job)**

`.github/workflows/registration-app.yml`:

```yaml
name: Registration service

# Same shape as deploy-admin-app.yml: tests on every PR touching the service,
# and on main.
on:
  push:
    branches: [main]
    paths: ["registration-app/**", ".github/workflows/registration-app.yml"]
  pull_request:
    paths: ["registration-app/**", ".github/workflows/registration-app.yml"]
  workflow_dispatch:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.23"
          # No go.sum (standard library only); point the cache at go.mod.
          cache-dependency-path: registration-app/go.mod
      - name: Test
        working-directory: registration-app
        run: go test ./... -v
      - name: Vet
        working-directory: registration-app
        run: go vet ./...
      - name: Gofmt
        working-directory: registration-app
        run: |
          unformatted=$(gofmt -l .)
          if [ -n "$unformatted" ]; then
            echo "::error::gofmt would rewrite:"; echo "$unformatted"; exit 1
          fi
```

- [x] **Step 8: Verify and commit**

Run: `make reg-check && make site && test ! -e _site/registration-app && echo excluded`
Expected: `registration service: tests, vet and gofmt are clean` and `excluded`.

```bash
git add _config.yml Makefile registration-app .github/workflows/registration-app.yml
git commit -m "feat(registration): module skeleton, configuration and build plumbing"
```

---

### Task 2: Money and date labels matching the site

**Files:**
- Create: `registration-app/internal/labels/labels.go`, `registration-app/internal/labels/labels_test.go`

**Interfaces:**
- Produces: `labels.Money(amount int, currency, lang string) string`, `labels.DateRange(start, end, lang string) string` (long style).

- [x] **Step 1: Write the failing test**

The expected strings are the site's: check them against the built site before relying on them (`make site`, then `grep -o 'data-date="[^"]*"' _site/anmeldung/index.html` shows the short style; the long style is documented at the top of `_includes/training-date-label.html`, the money style at the top of `_includes/money.html`).

`registration-app/internal/labels/labels_test.go`:

```go
package labels

import "testing"

// Expected strings are what the site renders today: the price lines from
// _site/index.html and _site/de/index.html, the date ranges from the long
// style documented in _includes/training-date-label.html. If the site's
// format changes, these cases change with it, in the same PR.
func TestMoneyMatchesTheSite(t *testing.T) {
	cases := []struct {
		amount    int
		cur, lang string
		want      string
	}{
		{2890, "EUR", "de", "2.890 €"},
		{2890, "EUR", "en", "€2,890"},
		{2100, "", "de", "2.100 €"},
		{950, "EUR", "en", "€950"},
		{12500, "EUR", "de", "12.500 €"},
		{2200, "CHF", "de", "2.200 CHF"},
	}
	for _, c := range cases {
		if got := Money(c.amount, c.cur, c.lang); got != c.want {
			t.Errorf("Money(%d,%q,%q) = %q, want %q", c.amount, c.cur, c.lang, got, c.want)
		}
	}
}

func TestDateRangeMatchesTheSite(t *testing.T) {
	cases := []struct{ start, end, lang, want string }{
		{"2026-12-01", "2026-12-04", "de", "1.-4. Dezember 2026"},
		{"2026-12-01", "2026-12-04", "en", "December 1-4, 2026"},
		{"2026-09-29", "2026-10-01", "de", "29. September - 1. Oktober 2026"},
		{"2026-09-29", "2026-10-01", "en", "September 29 - October 1, 2026"},
		{"2026-12-30", "2027-01-02", "de", "30. Dezember 2026 - 2. Januar 2027"},
		{"2026-12-30", "2027-01-02", "en", "December 30, 2026 - January 2, 2027"},
		{"2026-11-02", "2026-11-02", "de", "2. November 2026"},
		{"2026-11-02", "", "en", "November 2, 2026"},
		{"2027-03-02", "2027-03-05", "de", "2.-5. März 2027"},
		{"garbage", "", "de", "garbage"},
	}
	for _, c := range cases {
		if got := DateRange(c.start, c.end, c.lang); got != c.want {
			t.Errorf("DateRange(%q,%q,%q) = %q, want %q", c.start, c.end, c.lang, got, c.want)
		}
	}
}
```

- [x] **Step 2: Run to verify it fails**

Run: `cd registration-app && go test ./internal/labels`
Expected: FAIL, `undefined: Money`.

- [x] **Step 3: Implement**

`registration-app/internal/labels/labels.go`:

```go
// Package labels formats money and date ranges exactly as the site does in
// _includes/money.html and _includes/training-date-label.html ("long" style).
// The mail must say what the card and the form said; the golden cases in the
// test are copied from the built site.
package labels

import (
	"strconv"
	"time"
)

var monthsDE = []string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"}
var monthsEN = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}

// Money: de "2.890 €", en "€2,890"; other currencies "2.890 CHF" / "2,890 CHF".
func Money(amount int, currency, lang string) string {
	sep := ","
	if lang == "de" {
		sep = "."
	}
	num := group(amount, sep)
	if currency == "" || currency == "EUR" {
		if lang == "de" {
			return num + " €"
		}
		return "€" + num
	}
	return num + " " + currency
}

func group(n int, sep string) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + group(-n, sep)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + sep + s[i:]
	}
	return s
}

// DateRange renders ISO dates in the site's long style:
// de "1.-4. Dezember 2026", en "December 1-4, 2026", with month and year
// spans and single days handled as in training-date-label.html. Unparsable
// input comes back verbatim rather than as an error: a mail with a raw date
// is better than no mail.
func DateRange(start, end, lang string) string {
	s, err1 := time.Parse("2006-01-02", start)
	if end == "" {
		end = start
	}
	e, err2 := time.Parse("2006-01-02", end)
	if err1 != nil || err2 != nil {
		if end != start {
			return start + " - " + end
		}
		return start
	}
	sd, ed := strconv.Itoa(s.Day()), strconv.Itoa(e.Day())
	sy, ey := strconv.Itoa(s.Year()), strconv.Itoa(e.Year())
	if lang == "de" {
		sm, em := monthsDE[s.Month()-1], monthsDE[e.Month()-1]
		switch {
		case start == end:
			return sd + ". " + sm + " " + sy
		case sy == ey && s.Month() == e.Month():
			return sd + ".-" + ed + ". " + sm + " " + sy
		case sy == ey:
			return sd + ". " + sm + " - " + ed + ". " + em + " " + sy
		default:
			return sd + ". " + sm + " " + sy + " - " + ed + ". " + em + " " + ey
		}
	}
	sm, em := monthsEN[s.Month()-1], monthsEN[e.Month()-1]
	switch {
	case start == end:
		return sm + " " + sd + ", " + sy
	case sy == ey && s.Month() == e.Month():
		return sm + " " + sd + "-" + ed + ", " + sy
	case sy == ey:
		return sm + " " + sd + " - " + em + " " + ed + ", " + sy
	default:
		return sm + " " + sd + ", " + sy + " - " + em + " " + ed + ", " + ey
	}
}
```

- [x] **Step 4: Run to verify it passes**

Run: `cd registration-app && go test ./internal/labels -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add registration-app/internal/labels
git commit -m "feat(registration): money and date labels in the site's format"
```

---

### Task 3: Course feed client

**Files:**
- Create: `registration-app/internal/feed/feed.go`, `registration-app/internal/feed/feed_test.go`

**Interfaces:**
- Produces: `feed.New(url string, client *http.Client, now func() time.Time) *Feed`; `(*Feed).Lookup(ctx, code string) (feed.Entry, feed.Result)`; `feed.Result` constants `Bookable, Closed, Unknown, Unavailable`; `feed.Entry{CourseTitle, CourseShortTitle string; Date}`; `feed.Date{ID, Code, Start, End, City, Format, Language, Status string; Trainers []string; Price *Price}`; `feed.Price{Amount int; Currency string}`; constants `TTL`, `RetryPause`.

- [x] **Step 1: Write the failing test**

`registration-app/internal/feed/feed_test.go`:

```go
package feed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const sample = `{"generated":"x","courses":[
 {"id":"msa","short_title":"MSA","title":"Mastering Software Architectures",
  "trainers":["Peter Hruschka","Gernot Starke"],"future_field":1,
  "dates":[
   {"id":"msa-dez-2026","code":"26-12 MSA","start":"2026-12-01","end":"2026-12-04","city":"München","format":"public","language":"de","status":"open","price":{"amount":2890,"currency":"EUR","early_bird":{"amount":2690,"until":"2026-11-02"}}},
   {"id":"msa-online-sep-2026","code":"26-09 MSA-EN","start":"2026-09-29","end":"2026-10-01","format":"online","language":"en","status":"open","trainers":["Wolfgang Reimesch"]},
   {"id":"msa-w","code":"27-03 MSA","start":"2027-03-02","end":"2027-03-05","format":"public","language":"de","status":"waitlist"},
   {"id":"msa-f","code":"27-06 MSA","start":"2027-06-08","end":"2027-06-11","format":"public","language":"de","status":"full"}
  ]}]}`

func server(t *testing.T, body *atomic.Value, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		b := body.Load().(string)
		if b == "" {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(b))
	}))
	t.Cleanup(s.Close)
	return s
}

func at(s string) func() time.Time {
	tm, _ := time.Parse(time.RFC3339, s)
	return func() time.Time { return tm }
}

func TestLookupClassifiesCodes(t *testing.T) {
	var body atomic.Value
	body.Store(sample)
	var hits atomic.Int32
	f := New(server(t, &body, &hits).URL, nil, at("2026-09-30T10:00:00Z"))
	ctx := context.Background()

	cases := map[string]Result{
		"26-12 MSA":    Bookable,
		"26-09 MSA-EN": Bookable, // ends 2026-10-01, today is 2026-09-30
		"27-03 MSA":    Bookable, // waitlist is registrable
		"27-06 MSA":    Closed,   // full
		"99-99 NOPE":   Unknown,
	}
	for code, want := range cases {
		if _, got := f.Lookup(ctx, code); got != want {
			t.Errorf("Lookup(%q) = %v, want %v", code, got, want)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("feed fetched %d times, want 1 (cached)", hits.Load())
	}
}

func TestLookupResolvesTrainersAndIgnoresEarlyBird(t *testing.T) {
	var body atomic.Value
	body.Store(sample)
	var hits atomic.Int32
	f := New(server(t, &body, &hits).URL, nil, at("2026-09-30T10:00:00Z"))
	e, _ := f.Lookup(context.Background(), "26-12 MSA")
	if len(e.Trainers) != 2 || e.Trainers[0] != "Peter Hruschka" {
		t.Errorf("course-default trainers not applied: %v", e.Trainers)
	}
	if e.Price == nil || e.Price.Amount != 2890 {
		t.Errorf("price = %+v, want the regular 2890", e.Price)
	}
	e, _ = f.Lookup(context.Background(), "26-09 MSA-EN")
	if len(e.Trainers) != 1 || e.Trainers[0] != "Wolfgang Reimesch" {
		t.Errorf("date trainers must win: %v", e.Trainers)
	}
}

// "Today" is the German calendar day. 23:30 UTC on 1 October is already
// 2 October in Berlin, so a date that ended on 1 October is past.
func TestPastIsDecidedInBerlinTime(t *testing.T) {
	var body atomic.Value
	body.Store(sample)
	var hits atomic.Int32
	f := New(server(t, &body, &hits).URL, nil, at("2026-10-01T23:30:00Z"))
	if _, got := f.Lookup(context.Background(), "26-09 MSA-EN"); got != Closed {
		t.Errorf("got %v, want Closed", got)
	}
}

func TestOutageKeepsTheLastGoodCopy(t *testing.T) {
	var body atomic.Value
	body.Store(sample)
	var hits atomic.Int32
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	f := New(server(t, &body, &hits).URL, nil, func() time.Time { return now })
	f.Lookup(context.Background(), "26-12 MSA")

	body.Store("")
	now = now.Add(TTL + time.Minute)
	if _, got := f.Lookup(context.Background(), "26-12 MSA"); got != Bookable {
		t.Errorf("after a failed refresh got %v, want Bookable from the cached copy", got)
	}
}

func TestNeverFetchedIsUnavailableAndRetriesAreSpaced(t *testing.T) {
	var body atomic.Value
	body.Store("")
	var hits atomic.Int32
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	f := New(server(t, &body, &hits).URL, nil, func() time.Time { return now })
	if _, got := f.Lookup(context.Background(), "26-12 MSA"); got != Unavailable {
		t.Fatalf("got %v, want Unavailable", got)
	}
	f.Lookup(context.Background(), "26-12 MSA")
	if hits.Load() != 1 {
		t.Errorf("retried after %d requests within RetryPause, want 1", hits.Load())
	}
	now = now.Add(RetryPause + time.Second)
	body.Store(sample)
	if _, got := f.Lookup(context.Background(), "26-12 MSA"); got != Bookable {
		t.Errorf("after the pause got %v, want Bookable", got)
	}
}

func TestAnEmptyFeedIsNotTrusted(t *testing.T) {
	var body atomic.Value
	body.Store(`{"courses":[]}`)
	var hits atomic.Int32
	f := New(server(t, &body, &hits).URL, nil, at("2026-09-30T10:00:00Z"))
	if _, got := f.Lookup(context.Background(), "26-12 MSA"); got != Unavailable {
		t.Errorf("got %v, want Unavailable: an empty feed would silently drop every real booking", got)
	}
}
```

- [x] **Step 2: Run to verify it fails**

Run: `cd registration-app && go test ./internal/feed`
Expected: FAIL, `undefined: New`.

- [x] **Step 3: Implement**

`registration-app/internal/feed/feed.go`:

```go
// Package feed answers two questions from the site's public course feed
// (/api/trainings.json): is this booking code bookable right now, and what are
// its facts. It is a consumer of that feed like the other arc42 sites, bound by
// ADR-0004: it reads only fields that exist and tolerates new ones.
package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
	_ "time/tzdata" // the production image is FROM scratch and has no zoneinfo
)

type Price struct {
	Amount   int    `json:"amount"`
	Currency string `json:"currency"`
}

type Date struct {
	ID       string   `json:"id"`
	Code     string   `json:"code"`
	Start    string   `json:"start"`
	End      string   `json:"end"`
	City     string   `json:"city"`
	Format   string   `json:"format"`
	Language string   `json:"language"`
	Status   string   `json:"status"`
	Trainers []string `json:"trainers"`
	Price    *Price   `json:"price"`
}

type course struct {
	ID         string   `json:"id"`
	ShortTitle string   `json:"short_title"`
	Title      string   `json:"title"`
	Trainers   []string `json:"trainers"`
	Dates      []Date   `json:"dates"`
}

// Entry is one date with the course facts it needs. Trainers is already
// resolved: the date's own list, else the course default.
type Entry struct {
	CourseTitle      string
	CourseShortTitle string
	Date
}

type Result int

const (
	Bookable    Result = iota // open or waitlist, not past
	Closed                    // exists, but full, cancelled or past
	Unknown                   // not in the feed
	Unavailable               // the feed has never been fetched successfully
)

// How long a fetched feed is trusted, and how long to wait after a failed
// fetch before trying again, so an outage of the site does not turn every
// submission into a slow timeout.
const (
	TTL         = 5 * time.Minute
	RetryPause  = 30 * time.Second
	httpTimeout = 5 * time.Second
)

var berlin = mustBerlin()

func mustBerlin() *time.Location {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		panic(err)
	}
	return loc
}

type Feed struct {
	url    string
	client *http.Client
	now    func() time.Time

	mu        sync.Mutex
	entries   map[string]Entry // by booking code
	fetchedAt time.Time
	triedAt   time.Time
}

func New(url string, client *http.Client, now func() time.Time) *Feed {
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}
	return &Feed{url: url, client: client, now: now}
}

// Lookup refreshes the cache when it is stale, then classifies code. A failed
// refresh keeps the last good copy: a GitHub Pages outage must not reject
// real bookings.
func (f *Feed) Lookup(ctx context.Context, code string) (Entry, Result) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := f.now()
	if (f.entries == nil || now.Sub(f.fetchedAt) > TTL) && now.Sub(f.triedAt) > RetryPause {
		f.triedAt = now
		if entries, err := f.fetch(ctx); err == nil {
			f.entries, f.fetchedAt = entries, now
		}
	}
	if f.entries == nil {
		return Entry{}, Unavailable
	}
	e, ok := f.entries[code]
	if !ok {
		return Entry{}, Unknown
	}
	today := now.In(berlin).Format("2006-01-02")
	if (e.Status == "open" || e.Status == "waitlist") && e.End >= today {
		return e, Bookable
	}
	return e, Closed
}

func (f *Feed) fetch(ctx context.Context) (map[string]Entry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("feed: %s", resp.Status)
	}
	var doc struct {
		Courses []course `json:"courses"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("feed: %w", err)
	}
	entries := map[string]Entry{}
	for _, c := range doc.Courses {
		for _, d := range c.Dates {
			if len(d.Trainers) == 0 {
				d.Trainers = c.Trainers
			}
			entries[d.Code] = Entry{CourseTitle: c.Title, CourseShortTitle: c.ShortTitle, Date: d}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("feed: no dates at all, refusing to treat every code as unknown")
	}
	return entries, nil
}
```

- [x] **Step 4: Run to verify it passes**

Run: `cd registration-app && go test ./internal/feed -v`
Expected: PASS, 6 tests.

- [x] **Step 5: Commit**

```bash
git add registration-app/internal/feed
git commit -m "feat(registration): course feed client with Berlin-time bookability"
```

---

### Task 4: Confirmation token

**Files:**
- Create: `registration-app/internal/token/token.go`, `registration-app/internal/token/token_test.go`

**Interfaces:**
- Produces: `token.Claims{ID, Code, Email, LastName, Lang string; Issued int64}`; `token.NewSealer(key []byte, ttl time.Duration, now func() time.Time) (*Sealer, error)`; `(*Sealer).Seal(Claims) (string, error)`; `(*Sealer).Open(string) (Claims, error)`; `token.ErrInvalid`, `token.ErrExpired`; `token.Valid = 5 * 24 * time.Hour`.

- [x] **Step 1: Write the failing test**

`registration-app/internal/token/token_test.go`:

```go
package token

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var key = []byte("0123456789abcdef0123456789abcdef")

func sealer(now *time.Time) *Sealer {
	s, err := NewSealer(key, Valid, func() time.Time { return *now })
	if err != nil {
		panic(err)
	}
	return s
}

func TestRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	s := sealer(&now)
	in := Claims{ID: "R-7F3KQ", Code: "26-12 MSA", Email: "a@example.org", LastName: "Müller", Lang: "de"}
	tok, err := s.Seal(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) > 300 {
		t.Errorf("token is %d chars; the spec promises well under 300", len(tok))
	}
	if strings.ContainsAny(tok, "+/=") {
		t.Errorf("token %q is not URL-safe", tok)
	}
	out, err := s.Open(tok)
	if err != nil {
		t.Fatal(err)
	}
	in.Issued = now.Unix()
	if out != in {
		t.Errorf("Open = %+v, want %+v", out, in)
	}
}

func TestExpiresAfterFiveDays(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	s := sealer(&now)
	tok, _ := s.Seal(Claims{ID: "R-1"})
	now = now.Add(5*24*time.Hour - time.Minute)
	if _, err := s.Open(tok); err != nil {
		t.Fatalf("still valid just before 5 days: %v", err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := s.Open(tok); !errors.Is(err, ErrExpired) {
		t.Fatalf("after 5 days: %v, want ErrExpired", err)
	}
}

func TestTamperingAndGarbageAreInvalid(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	s := sealer(&now)
	tok, _ := s.Seal(Claims{ID: "R-1"})
	flipped := []byte(tok)
	if flipped[20] == 'A' {
		flipped[20] = 'B'
	} else {
		flipped[20] = 'A'
	}
	for name, bad := range map[string]string{
		"one character changed": string(flipped),
		"empty":                 "",
		"not base64":            "%%%",
		"too short":             "AAAA",
		"huge":                  strings.Repeat("A", 5000),
	} {
		if _, err := s.Open(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestAnotherKeyCannotOpen(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	tok, _ := sealer(&now).Seal(Claims{ID: "R-1"})
	other, _ := NewSealer([]byte("ffffffffffffffffffffffffffffffff"), Valid, func() time.Time { return now })
	if _, err := other.Open(tok); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid (rotated key)", err)
	}
}
```

- [x] **Step 2: Run to verify it fails**

Run: `cd registration-app && go test ./internal/token`
Expected: FAIL, `undefined: NewSealer`.

- [x] **Step 3: Implement**

`registration-app/internal/token/token.go`:

```go
// Package token seals the few facts the confirmation step needs into the
// confirm link itself, so the service keeps no state. AES-256-GCM makes the
// token both unreadable and tamper-evident.
package token

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

// Claims is deliberately small: the full registration is already in the
// back office's first mail. A short link survives mail clients that wrap
// long URLs, and link scanners log URLs.
type Claims struct {
	ID       string `json:"i"` // registration id, e.g. R-7F3KQ
	Code     string `json:"c"` // booking code, or "sonstige"
	Email    string `json:"e"`
	LastName string `json:"n"`
	Lang     string `json:"l"`
	Issued   int64  `json:"t"` // unix seconds
}

var (
	ErrInvalid = errors.New("token: invalid")
	ErrExpired = errors.New("token: expired")
)

// Valid is how long a confirm link works. Spec: 5 days, long enough for a
// weekend and the back office's follow-up after 2 to 3 working days.
const Valid = 5 * 24 * time.Hour

const maxLen = 1024

type Sealer struct {
	aead cipher.AEAD
	ttl  time.Duration
	now  func() time.Time
}

func NewSealer(key []byte, ttl time.Duration, now func() time.Time) (*Sealer, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead, ttl: ttl, now: now}, nil
}

func (s *Sealer) Seal(c Claims) (string, error) {
	c.Issued = s.now().Unix()
	plain, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(s.aead.Seal(nonce, nonce, plain, nil)), nil
}

func (s *Sealer) Open(tok string) (Claims, error) {
	if tok == "" || len(tok) > maxLen {
		return Claims{}, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil || len(raw) < s.aead.NonceSize() {
		return Claims{}, ErrInvalid
	}
	plain, err := s.aead.Open(nil, raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():], nil)
	if err != nil {
		return Claims{}, ErrInvalid
	}
	var c Claims
	if err := json.Unmarshal(plain, &c); err != nil {
		return Claims{}, ErrInvalid
	}
	issued := time.Unix(c.Issued, 0)
	now := s.now()
	if issued.After(now.Add(time.Minute)) {
		return Claims{}, ErrInvalid
	}
	if now.Sub(issued) > s.ttl {
		return Claims{}, ErrExpired
	}
	return c, nil
}
```

- [x] **Step 4: Run to verify it passes**

Run: `cd registration-app && go test ./internal/token -v`
Expected: PASS, 4 tests.

- [x] **Step 5: Commit**

```bash
git add registration-app/internal/token
git commit -m "feat(registration): sealed 5-day confirmation token"
```

---

### Task 5: Intake checks, rate limit and hints

**Files:**
- Create: `registration-app/internal/intake/intake.go`, `ratelimit.go`, `hints.go`, `intake_test.go`

**Interfaces:**
- Consumes: `feed.Entry`, `feed.Result` (Task 3).
- Produces: `intake.Registration` (fields `ID, Lang, LastName, FirstName, Email string; Emails []string; Code, ParticipantLast, ParticipantFirst, ParticipantEmail, Billing, Comments, Via, FormSource string`); `intake.Other = "sonstige"`; `intake.Outcome` constants `Accept, Drop, Reject`; `intake.Decision{Outcome; Reason string; Reg Registration; Entry feed.Entry; Found bool; Hints []string}`; `intake.Input{Form url.Values; Origin, IP string}`; `intake.Lookuper` interface; `intake.Checker{Feed Lookuper; Limiter *Limiter; AllowedOrigins []string}` with `Check(ctx, Input) Decision`; `intake.NewLimiter(max int, window time.Duration, now func() time.Time) *Limiter` with `Allow(key string) bool`; `intake.NewID() string`; hint keys `HintGmailDots, HintURLInName, HintOddCase, HintSeveralEmails, HintClosed, HintFeedDown`.

- [x] **Step 1: Write the failing test**

`registration-app/internal/intake/intake_test.go`:

```go
package intake

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"arc42-registration/internal/feed"
)

type fakeFeed map[string]feed.Result

func (f fakeFeed) Lookup(_ context.Context, code string) (feed.Entry, feed.Result) {
	res, ok := f[code]
	if !ok {
		return feed.Entry{}, feed.Unknown
	}
	return feed.Entry{CourseShortTitle: "MSA", Date: feed.Date{Code: code}}, res
}

func checker() *Checker {
	return &Checker{
		Feed:           fakeFeed{"26-12 MSA": feed.Bookable, "26-09 MSA-EN": feed.Closed},
		Limiter:        NewLimiter(5, time.Hour, time.Now),
		AllowedOrigins: []string{"https://trainings.arc42.org"},
	}
}

func germanForm() url.Values {
	return url.Values{
		"Nachname": {"Müller"}, "Vorname": {"Anna"}, "Email": {"anna@example.org"},
		"Kurs": {"26-12 MSA"}, "Rechnungsadresse": {"Firma X\nStraße 1\n12345 Ort"},
		"Bemerkungen": {"PO 4711"}, "language": {"de"}, "via": {"arc42.de"},
		"form_source": {"https://trainings.arc42.org/anmeldung/"},
		"_gotcha":     {""}, "company_website": {""},
	}
}

func TestAcceptsARealGermanRegistration(t *testing.T) {
	d := checker().Check(context.Background(), Input{Form: germanForm(), Origin: "https://trainings.arc42.org", IP: "1.2.3.4"})
	if d.Outcome != Accept {
		t.Fatalf("outcome %v (%s), want Accept", d.Outcome, d.Reason)
	}
	r := d.Reg
	if r.Lang != "de" || r.LastName != "Müller" || r.Emails[0] != "anna@example.org" || r.Via != "arc42.de" {
		t.Errorf("registration = %+v", r)
	}
	if !d.Found || len(d.Hints) != 0 {
		t.Errorf("found=%v hints=%v", d.Found, d.Hints)
	}
}

func TestReadsTheEnglishFieldNames(t *testing.T) {
	f := url.Values{
		"last name": {"Smith"}, "first name": {"Jo"}, "Email": {"jo@example.org"},
		"Kurs": {"26-12 MSA"}, "Billing address": {"ACME"}, "Comments": {"hi"}, "language": {"en"},
	}
	d := checker().Check(context.Background(), Input{Form: f, IP: "1.2.3.4"})
	if d.Outcome != Accept || d.Reg.Lang != "en" || d.Reg.LastName != "Smith" || d.Reg.Billing != "ACME" || d.Reg.Comments != "hi" {
		t.Fatalf("decision = %+v", d)
	}
}

func TestOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(f url.Values, in *Input)
		want   Outcome
		reason string
	}{
		{"foreign origin", func(f url.Values, in *Input) { in.Origin = "https://evil.example" }, Drop, "origin"},
		{"no origin header is fine", func(f url.Values, in *Input) { in.Origin = "" }, Accept, ""},
		{"gotcha honeypot", func(f url.Values, in *Input) { f.Set("_gotcha", "x") }, Drop, "honeypot"},
		{"custom honeypot", func(f url.Values, in *Input) { f.Set("company_website", "http://x") }, Drop, "honeypot"},
		{"unknown code", func(f url.Values, in *Input) { f.Set("Kurs", "99-99 FAKE") }, Drop, "unknown code"},
		{"closed code still goes through", func(f url.Values, in *Input) { f.Set("Kurs", "26-09 MSA-EN") }, Accept, ""},
		{"other", func(f url.Values, in *Input) { f.Set("Kurs", "sonstige") }, Accept, ""},
		{"no last name", func(f url.Values, in *Input) { f.Del("Nachname") }, Reject, "required"},
		{"no billing address", func(f url.Values, in *Input) { f.Set("Rechnungsadresse", "  ") }, Reject, "required"},
		{"bad email", func(f url.Values, in *Input) { f.Set("Email", "anna@localhost") }, Reject, "email"},
		{"display name in email", func(f url.Values, in *Input) { f.Set("Email", "Anna <anna@example.org>") }, Reject, "email"},
		{"comments too long", func(f url.Values, in *Input) { f.Set("Bemerkungen", strings.Repeat("x", 4001)) }, Reject, "too long"},
		{"4000 characters of umlauts are fine", func(f url.Values, in *Input) { f.Set("Bemerkungen", strings.Repeat("ü", 4000)) }, Accept, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := germanForm()
			in := Input{Form: f, Origin: "https://trainings.arc42.org", IP: "1.2.3.4"}
			c.mutate(f, &in)
			d := checker().Check(context.Background(), in)
			if d.Outcome != c.want || !strings.Contains(d.Reason, c.reason) {
				t.Errorf("outcome %v reason %q, want %v containing %q", d.Outcome, d.Reason, c.want, c.reason)
			}
		})
	}
}

func TestClosedAndFeedDownAreFlagged(t *testing.T) {
	c := checker()
	f := germanForm()
	f.Set("Kurs", "26-09 MSA-EN")
	if d := c.Check(context.Background(), Input{Form: f, IP: "a"}); !has(d.Hints, HintClosed) {
		t.Errorf("hints %v lack %s", d.Hints, HintClosed)
	}
	c.Feed = fakeFeed{"26-12 MSA": feed.Unavailable}
	d := c.Check(context.Background(), Input{Form: germanForm(), IP: "b"})
	if d.Outcome != Accept || !has(d.Hints, HintFeedDown) || d.Found {
		t.Errorf("feed down: %+v", d)
	}
}

// Several addresses are allowed (the input has `multiple`), but only the
// first receives the registrant mail, and the back office is told.
func TestSeveralEmails(t *testing.T) {
	f := germanForm()
	f.Set("Email", "anna@example.org, Boss@Example.org")
	d := checker().Check(context.Background(), Input{Form: f, IP: "a"})
	if d.Outcome != Accept || len(d.Reg.Emails) != 2 || d.Reg.Emails[1] != "boss@example.org" || !has(d.Hints, HintSeveralEmails) {
		t.Errorf("decision = %+v", d)
	}
}

func TestRateLimitDropsTheSixthWithinAnHour(t *testing.T) {
	c := checker()
	for i := 0; i < 5; i++ {
		if d := c.Check(context.Background(), Input{Form: germanForm(), IP: "9.9.9.9"}); d.Outcome != Accept {
			t.Fatalf("submission %d: %v %s", i+1, d.Outcome, d.Reason)
		}
	}
	if d := c.Check(context.Background(), Input{Form: germanForm(), IP: "9.9.9.9"}); d.Outcome != Drop {
		t.Errorf("sixth: %v, want Drop", d.Outcome)
	}
	if d := c.Check(context.Background(), Input{Form: germanForm(), IP: "8.8.8.8"}); d.Outcome != Accept {
		t.Errorf("other IP: %v, want Accept", d.Outcome)
	}
}

func TestLimiterWindowSlides(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	l := NewLimiter(1, time.Hour, func() time.Time { return now })
	if !l.Allow("x") || l.Allow("x") {
		t.Fatal("first allowed, second not")
	}
	now = now.Add(time.Hour + time.Second)
	if !l.Allow("x") {
		t.Error("allowed again after the window")
	}
}

func TestHints(t *testing.T) {
	cases := []struct {
		name string
		reg  Registration
		want string
	}{
		{"dotted gmail", Registration{Emails: []string{"a.b.c.d.e1@gmail.com"}}, HintGmailDots},
		{"one dot is normal", Registration{Emails: []string{"firstname.lastname@gmail.com"}}, ""},
		{"url in name", Registration{LastName: "see https://spam.example", Emails: []string{"a@b.de"}}, HintURLInName},
		{"random case", Registration{FirstName: "xKqTvBnM", Emails: []string{"a@b.de"}}, HintOddCase},
		{"McDonald is a name", Registration{LastName: "McDonald", Emails: []string{"a@b.de"}}, ""},
	}
	for _, c := range cases {
		got := hintsFor(c.reg)
		if c.want == "" && len(got) != 0 || c.want != "" && !has(got, c.want) {
			t.Errorf("%s: hints %v, want %q", c.name, got, c.want)
		}
	}
}

func TestNewIDShape(t *testing.T) {
	re := regexp.MustCompile(`^R-[2-9A-HJKMNP-Z]{5}$`)
	for i := 0; i < 200; i++ {
		if id := NewID(); !re.MatchString(id) {
			t.Fatalf("NewID() = %q", id)
		}
	}
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
```

- [x] **Step 2: Run to verify it fails**

Run: `cd registration-app && go test ./internal/intake`
Expected: FAIL, undefined identifiers.

- [x] **Step 3: Implement the rate limiter**

`registration-app/internal/intake/ratelimit.go`:

```go
package intake

import (
	"sync"
	"time"
)

// Limiter allows max hits per key within window. It lives in memory and
// resets when the machine stops; it exists to stop a burst, not to keep
// history (spec 4.1).
type Limiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu   sync.Mutex
	hits map[string][]time.Time
}

// maxKeys bounds memory under a flood of distinct addresses. Past it the map
// is simply reset: forgetting some counts is better than growing without end.
const maxKeys = 10000

func NewLimiter(max int, window time.Duration, now func() time.Time) *Limiter {
	return &Limiter{max: max, window: window, now: now, hits: map[string][]time.Time{}}
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.hits) > maxKeys {
		l.hits = map[string][]time.Time{}
	}
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}
```

- [x] **Step 4: Implement the hints**

`registration-app/internal/intake/hints.go`:

```go
package intake

import (
	"strings"
	"unicode"
)

// Hint keys. The mail package words them per language. Hints never block a
// registration (spec 4.1): each one is a signal a person glances at.
const (
	HintGmailDots     = "gmail-dots"     // a.b.c.d@gmail.com, the probing pattern seen in the spam log
	HintURLInName     = "url-in-name"    // a link where a name belongs
	HintOddCase       = "odd-case"       // xKqTvBnM-style random strings
	HintSeveralEmails = "several-emails" // the Email field held more than one address
	HintClosed        = "closed"         // the date was closed after the page was loaded
	HintFeedDown      = "feed-down"      // the course list could not be read; code not checked
)

func hintsFor(r Registration) []string {
	var hs []string
	for _, e := range r.Emails {
		local, domain, _ := strings.Cut(e, "@")
		if (domain == "gmail.com" || domain == "googlemail.com") && strings.Count(local, ".") >= 3 {
			hs = append(hs, HintGmailDots)
			break
		}
	}
	names := []string{r.LastName, r.FirstName, r.ParticipantLast, r.ParticipantFirst}
	for _, n := range names {
		l := strings.ToLower(n)
		if strings.Contains(l, "http://") || strings.Contains(l, "https://") || strings.Contains(l, "www.") {
			hs = append(hs, HintURLInName)
			break
		}
	}
	for _, n := range names {
		if oddCase(n) {
			hs = append(hs, HintOddCase)
			break
		}
	}
	if len(r.Emails) > 1 {
		hs = append(hs, HintSeveralEmails)
	}
	return hs
}

// oddCase flags a word whose letters switch between upper and lower case four
// or more times. "McDonald" switches three times and passes.
func oddCase(s string) bool {
	for _, word := range strings.Fields(s) {
		switches, prevUpper, seen := 0, false, false
		for _, r := range word {
			if !unicode.IsLetter(r) {
				continue
			}
			up := unicode.IsUpper(r)
			if seen && up != prevUpper {
				switches++
			}
			prevUpper, seen = up, true
		}
		if switches >= 4 {
			return true
		}
	}
	return false
}
```

- [x] **Step 5: Implement the checker**

`registration-app/internal/intake/intake.go`:

```go
// Package intake turns a form submission into a Decision: accept it, drop it
// silently, or reject it back to the person. It knows the form's field names
// in both languages; nothing downstream does.
package intake

import (
	"context"
	"crypto/rand"
	"net/mail"
	"net/url"
	"strings"

	"arc42-registration/internal/feed"
)

type Registration struct {
	ID   string // set by the caller once accepted
	Lang string // "de" or "en"

	LastName  string
	FirstName string
	Email     string   // as typed
	Emails    []string // parsed, lower-cased; Emails[0] receives the registrant mail
	Code      string   // booking code, or Other

	ParticipantLast  string
	ParticipantFirst string
	ParticipantEmail string

	Billing  string
	Comments string

	Via        string
	FormSource string
}

// Other is the value of the form's "Sonstige"/"other" option.
const Other = "sonstige"

type Outcome int

const (
	Accept Outcome = iota
	Drop           // bot signal: no mail, the success page anyway
	Reject         // a person's mistake: the fail page
)

type Decision struct {
	Outcome Outcome
	Reason  string // for the log; never shown
	Reg     Registration
	Entry   feed.Entry // zero for Other or when the feed is down
	Found   bool       // Entry is meaningful
	Hints   []string
}

type Input struct {
	Form   url.Values
	Origin string
	IP     string
}

type Lookuper interface {
	Lookup(ctx context.Context, code string) (feed.Entry, feed.Result)
}

type Checker struct {
	Feed           Lookuper
	Limiter        *Limiter
	AllowedOrigins []string
}

// The form's field names. German and English forms name the visible fields
// differently, and Formspark used those names as labels, so they stay as they
// are; this is the one place that knows both spellings.
var fieldNames = map[string][]string{
	"last":     {"Nachname", "last name"},
	"first":    {"Vorname", "first name"},
	"email":    {"Email"},
	"code":     {"Kurs"},
	"p_last":   {"NachnameTN"},
	"p_first":  {"VornameTN"},
	"p_email":  {"EmailTN"},
	"billing":  {"Rechnungsadresse", "Billing address"},
	"comments": {"Bemerkungen", "Comments"},
}

var maxLen = map[string]int{
	"last": 200, "first": 200, "email": 320, "code": 64,
	"p_last": 200, "p_first": 200, "p_email": 320,
	"billing": 1000, "comments": 4000,
}

func get(f url.Values, key string) string {
	for _, name := range fieldNames[key] {
		if v := strings.TrimSpace(f.Get(name)); v != "" {
			return v
		}
	}
	return ""
}

func (c *Checker) Check(ctx context.Context, in Input) Decision {
	f := in.Form
	r := Registration{
		Lang:             "de",
		LastName:         get(f, "last"),
		FirstName:        get(f, "first"),
		Email:            get(f, "email"),
		Code:             get(f, "code"),
		ParticipantLast:  get(f, "p_last"),
		ParticipantFirst: get(f, "p_first"),
		ParticipantEmail: get(f, "p_email"),
		Billing:          get(f, "billing"),
		Comments:         get(f, "comments"),
		Via:              clip(f.Get("via"), 100),
		FormSource:       clip(f.Get("form_source"), 300),
	}
	if f.Get("language") == "en" {
		r.Lang = "en"
	}
	d := Decision{Reg: r}
	drop := func(why string) Decision { d.Outcome, d.Reason = Drop, why; return d }
	reject := func(why string) Decision { d.Outcome, d.Reason = Reject, why; return d }

	for key, max := range maxLen {
		if len([]rune(get(f, key))) > max {
			return reject("too long: " + key)
		}
	}
	if in.Origin != "" && !contains(c.AllowedOrigins, strings.ToLower(in.Origin)) {
		return drop("origin " + in.Origin)
	}
	if f.Get("_gotcha") != "" || f.Get("company_website") != "" {
		return drop("honeypot")
	}
	if c.Limiter != nil && !c.Limiter.Allow(in.IP) {
		return drop("rate limit")
	}
	if r.LastName == "" || r.Email == "" || r.Code == "" || r.Billing == "" {
		return reject("required field missing")
	}
	emails, ok := parseEmails(r.Email)
	if !ok {
		return reject("email syntax")
	}
	d.Reg.Emails = emails

	if r.Code != Other {
		entry, res := c.Feed.Lookup(ctx, r.Code)
		switch res {
		case feed.Unknown:
			return drop("unknown code " + r.Code)
		case feed.Closed:
			d.Hints = append(d.Hints, HintClosed)
		case feed.Unavailable:
			d.Hints = append(d.Hints, HintFeedDown)
		}
		d.Entry, d.Found = entry, res != feed.Unavailable
	}
	d.Hints = append(d.Hints, hintsFor(d.Reg)...)
	d.Outcome = Accept
	return d
}

// parseEmails accepts one or more comma-separated bare addresses (the form's
// email input has the `multiple` attribute). A display name, a missing dot in
// the domain, or anything net/mail cannot parse fails the whole field.
func parseEmails(s string) ([]string, bool) {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		a, err := mail.ParseAddress(part)
		if err != nil || a.Name != "" || a.Address != part {
			return nil, false
		}
		_, domain, _ := strings.Cut(a.Address, "@")
		if !strings.Contains(domain, ".") || strings.HasSuffix(domain, ".") {
			return nil, false
		}
		out = append(out, strings.ToLower(a.Address))
	}
	return out, len(out) > 0
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// idAlphabet leaves out 0/O, 1/I/L: the id is read aloud on the phone.
const idAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// NewID returns a registration id such as "R-7F3KQ". 31^5 is about 29
// million; at a few registrations a week a collision is not a concern, and
// the id only has to tell two mails in one inbox apart.
func NewID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = idAlphabet[int(b[i])%len(idAlphabet)]
	}
	return "R-" + string(b)
}
```

- [x] **Step 6: Run to verify it passes**

Run: `cd registration-app && go test ./internal/intake -v`
Expected: PASS.

- [x] **Step 7: Commit**

```bash
git add registration-app/internal/intake
git commit -m "feat(registration): intake checks, rate limit and spam hints"
```

---

### Task 6: Mail rendering

**Files:**
- Create: `registration-app/internal/mail/mail.go`, `mail_test.go`, `templates/registrant_de.txt`, `templates/registrant_en.txt`, `templates/registrant.html`, `templates/backoffice_de.txt`, `templates/backoffice_en.txt`, `templates/confirmed_de.txt`, `templates/confirmed_en.txt`, and the golden files under `testdata/`

**Interfaces:**
- Consumes: `feed.Entry` (Task 3), `intake.Registration` and hint keys (Task 5), `token.Claims` (Task 4), `labels` (Task 2).
- Produces: `mail.Facts{Title, ShortTitle, Code, Dates, Where, Trainers, Price string}`; `mail.FactsFor(feed.Entry, lang string) *Facts`; `mail.Rendered{Subject, Text, HTML string}`; `mail.Registrant(lang string, facts *Facts, confirmURL string) (Rendered, error)` (facts nil for "Sonstige"); `mail.Backoffice(reg intake.Registration, facts *Facts, hints []string) (Rendered, error)`; `mail.Confirmed(token.Claims) (Rendered, error)`.

- [x] **Step 1: Write the test and the expected output**

`registration-app/internal/mail/mail_test.go`:

```go
package mail

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arc42-registration/internal/feed"
	"arc42-registration/internal/intake"
	"arc42-registration/internal/token"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/mail -update, then read the file)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the golden file.\n--- got ---\n%s", name, got)
	}
}

var dez = feed.Entry{
	CourseTitle: "Mastering Software Architectures", CourseShortTitle: "MSA",
	Date: feed.Date{Code: "26-12 MSA", Start: "2026-12-01", End: "2026-12-04", City: "München",
		Format: "public", Trainers: []string{"Peter Hruschka", "Gernot Starke"},
		Price: &feed.Price{Amount: 2890, Currency: "EUR"}},
}

const confirmURL = "https://register.arc42.org/confirm?t=TOKEN"

func TestRegistrantMails(t *testing.T) {
	for _, l := range []string{"de", "en"} {
		r, err := Registrant(l, FactsFor(dez, l), confirmURL)
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "registrant_"+l+".txt", r.Subject+"\n\n"+r.Text)
		golden(t, "registrant_"+l+".html", r.HTML)
		other, err := Registrant(l, nil, confirmURL)
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "registrant_other_"+l+".txt", other.Subject+"\n\n"+other.Text)
	}
}

func TestBackofficeMails(t *testing.T) {
	reg := intake.Registration{
		ID: "R-7F3KQ", LastName: "Müller", FirstName: "Anna", Email: "anna@example.org",
		Code: "26-12 MSA", Billing: "Firma X\nStraße 1\n12345 Ort", Comments: "",
		Via: "arc42.de", FormSource: "https://trainings.arc42.org/anmeldung/",
	}
	for _, l := range []string{"de", "en"} {
		reg.Lang = l
		r, err := Backoffice(reg, FactsFor(dez, l), []string{intake.HintGmailDots, intake.HintClosed})
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "backoffice_"+l+".txt", r.Subject+"\n\n"+r.Text)
		c, err := Confirmed(token.Claims{ID: "R-7F3KQ", Code: "26-12 MSA", Email: "anna@example.org", LastName: "Müller", Lang: l})
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "confirmed_"+l+".txt", c.Subject+"\n\n"+c.Text)
		if strings.TrimSuffix(r.Subject, "(UNBESTÄTIGT)") != strings.TrimSuffix(c.Subject, "(BESTÄTIGT)") &&
			strings.TrimSuffix(r.Subject, "(UNCONFIRMED)") != strings.TrimSuffix(c.Subject, "(CONFIRMED)") {
			t.Errorf("subjects must differ only in the tag, so they thread:\n%s\n%s", r.Subject, c.Subject)
		}
	}
}

// The contract that keeps the registrant mail from being a spam relay: no
// text the registrant typed may appear in it, in either part, in either
// language.
func TestRegistrantMailEchoesNothingTyped(t *testing.T) {
	for _, l := range []string{"de", "en"} {
		r, _ := Registrant(l, FactsFor(dez, l), confirmURL)
		for _, typed := range []string{"Müller", "Anna", "Firma X", "Straße 1"} {
			if strings.Contains(r.Text, typed) || strings.Contains(r.HTML, typed) || strings.Contains(r.Subject, typed) {
				t.Errorf("%s registrant mail contains typed text %q", l, typed)
			}
		}
	}
}

func TestNoEarlyBirdOrAlumniInTheRegistrantMail(t *testing.T) {
	for _, l := range []string{"de", "en"} {
		r, _ := Registrant(l, FactsFor(dez, l), confirmURL)
		low := strings.ToLower(r.Text + r.HTML)
		for _, banned := range []string{"früh", "early", "alumni", "2.690", "2,690"} {
			if strings.Contains(low, banned) {
				t.Errorf("%s mail mentions %q", l, banned)
			}
		}
	}
}

func TestOnlineDateSaysOnline(t *testing.T) {
	e := dez
	e.Format, e.City = "online", ""
	if f := FactsFor(e, "en"); f.Where != "Online" {
		t.Errorf("Where = %q", f.Where)
	}
}

func TestHTMLEscapesFacts(t *testing.T) {
	e := dez
	e.CourseTitle = `<script>alert(1)</script>`
	r, _ := Registrant("en", FactsFor(e, "en"), confirmURL)
	if strings.Contains(r.HTML, "<script>") {
		t.Error("HTML part did not escape a course title")
	}
}
```

The golden files are the expected mails. Create them exactly as below (each file ends with a single newline after the last line, as written by the renderer).

`testdata/registrant_de.txt`:

```text
Bitte bestätigen Sie Ihre Anmeldung: MSA, 1.-4. Dezember 2026

Guten Tag,

vielen Dank für Ihre Anmeldung bei arc42. Sie ist erst vollständig, wenn Sie sie bestätigen:

https://register.arc42.org/confirm?t=TOKEN

Der Link ist 5 Tage gültig.

Ihre Auswahl
  Kurs:          Mastering Software Architectures
  Termin:        1.-4. Dezember 2026
  Ort:           München
  Trainer:       Peter Hruschka und Gernot Starke
  Preis:         2.890 €
  Buchungscode:  26-12 MSA

Wir bearbeiten Anmeldungen von Hand und melden uns persönlich, meist innerhalb von ein bis zwei Werktagen.

Sie haben sich nicht angemeldet? Dann ignorieren Sie diese Mail einfach. Ohne Bestätigung geschieht nichts.

Fragen? Antworten Sie einfach auf diese Mail.

arc42 Trainings
https://trainings.arc42.org
```

`testdata/registrant_en.txt`:

```text
Please confirm your registration: MSA, December 1-4, 2026

Hello,

thank you for your registration with arc42. It is only complete once you confirm it:

https://register.arc42.org/confirm?t=TOKEN

The link is valid for 5 days.

Your selection
  Course:        Mastering Software Architectures
  Dates:         December 1-4, 2026
  Location:      München
  Trainers:      Peter Hruschka and Gernot Starke
  Price:         €2,890
  Booking code:  26-12 MSA

We process registrations by hand and will get back to you personally, usually within one or two business days.

You did not register? Then simply ignore this mail. Nothing happens without confirmation.

Questions? Just reply to this mail.

arc42 Trainings
https://trainings.arc42.org
```

`testdata/registrant_other_de.txt`:

```text
Bitte bestätigen Sie Ihre Anfrage

Guten Tag,

vielen Dank für Ihre Anfrage bei arc42. Sie ist erst vollständig, wenn Sie sie bestätigen:

https://register.arc42.org/confirm?t=TOKEN

Der Link ist 5 Tage gültig.

Sie haben "Sonstige" gewählt. Wir melden uns persönlich bei Ihnen.

Wir bearbeiten Anmeldungen von Hand und melden uns persönlich, meist innerhalb von ein bis zwei Werktagen.

Sie haben sich nicht angemeldet? Dann ignorieren Sie diese Mail einfach. Ohne Bestätigung geschieht nichts.

Fragen? Antworten Sie einfach auf diese Mail.

arc42 Trainings
https://trainings.arc42.org
```

`testdata/registrant_other_en.txt`:

```text
Please confirm your request

Hello,

thank you for your request with arc42. It is only complete once you confirm it:

https://register.arc42.org/confirm?t=TOKEN

The link is valid for 5 days.

You chose "other". We will get in touch with you personally.

We process registrations by hand and will get back to you personally, usually within one or two business days.

You did not register? Then simply ignore this mail. Nothing happens without confirmation.

Questions? Just reply to this mail.

arc42 Trainings
https://trainings.arc42.org
```

`testdata/backoffice_de.txt`:

```text
[trainings.arc42.org] ANMELDUNG R-7F3KQ 26-12 MSA (UNBESTÄTIGT)

Anmeldung R-7F3KQ (UNBESTÄTIGT)

Kurs:              26-12 MSA
                   Mastering Software Architectures, 1.-4. Dezember 2026, München
Nachname:          Müller
Vorname:           Anna
E-Mail:            anna@example.org
Nachname TN:       
Vorname TN:        
E-Mail TN:         
Rechnungsadresse:
  Firma X
  Straße 1
  12345 Ort
Bemerkungen:
  -

Sprache:           de
via:               arc42.de
form_source:       https://trainings.arc42.org/anmeldung/

Hinweise:
  - Gmail-Adresse mit vielen Punkten (Muster aus Spam-Tests)
  - Termin war beim Absenden nicht mehr offen (ausgebucht, abgesagt oder vorbei)

Gebucht wird erst auf die Mail "(BESTÄTIGT)" mit derselben Nummer R-7F3KQ.
```

`testdata/backoffice_en.txt`:

```text
[trainings.arc42.org] REGISTRATION R-7F3KQ 26-12 MSA (UNCONFIRMED)

Registration R-7F3KQ (UNCONFIRMED)

Course:                   26-12 MSA
                          Mastering Software Architectures, December 1-4, 2026, München
Last name:                Müller
First name:               Anna
E-mail:                   anna@example.org
Participant last name:    
Participant first name:   
Participant e-mail:       
Billing address:
  Firma X
  Straße 1
  12345 Ort
Comments:
  -

Language:                 en
via:                      arc42.de
form_source:              https://trainings.arc42.org/anmeldung/

Notes:
  - Gmail address with many dots (a spam-probing pattern)
  - Date was no longer open when submitted (full, cancelled or past)

Book only on the "(CONFIRMED)" mail with the same number R-7F3KQ.
```

`testdata/confirmed_de.txt`:

```text
[trainings.arc42.org] ANMELDUNG R-7F3KQ 26-12 MSA (BESTÄTIGT)

Anmeldung R-7F3KQ wurde vom Anmeldenden bestätigt.

Kurs:       26-12 MSA
Nachname:   Müller
E-Mail:     anna@example.org

Alle Angaben stehen in der Mail "ANMELDUNG R-7F3KQ ... (UNBESTÄTIGT)".
```

`testdata/confirmed_en.txt`:

```text
[trainings.arc42.org] REGISTRATION R-7F3KQ 26-12 MSA (CONFIRMED)

Registration R-7F3KQ was confirmed by the registrant.

Course:     26-12 MSA
Last name:  Müller
E-mail:     anna@example.org

All details are in the mail "REGISTRATION R-7F3KQ ... (UNCONFIRMED)".
```

The two HTML goldens (`registrant_de.html`, `registrant_en.html`) are generated in Step 4 with `-update` and then read by a person, because hand-typing escaped HTML is where typos hide.

- [x] **Step 2: Run to verify it fails**

Run: `cd registration-app && go test ./internal/mail`
Expected: FAIL, undefined identifiers.

- [x] **Step 3: Implement the templates and the renderer**

`templates/registrant_de.txt`:

```text
Guten Tag,

vielen Dank für Ihre {{if .Facts}}Anmeldung{{else}}Anfrage{{end}} bei arc42. Sie ist erst vollständig, wenn Sie sie bestätigen:

{{.ConfirmURL}}

Der Link ist 5 Tage gültig.
{{with .Facts}}
Ihre Auswahl
  Kurs:          {{.Title}}
  Termin:        {{.Dates}}
  Ort:           {{.Where}}
{{- if .Trainers}}
  Trainer:       {{.Trainers}}{{end}}
{{- if .Price}}
  Preis:         {{.Price}}{{end}}
  Buchungscode:  {{.Code}}
{{else}}
Sie haben "Sonstige" gewählt. Wir melden uns persönlich bei Ihnen.
{{end}}
Wir bearbeiten Anmeldungen von Hand und melden uns persönlich, meist innerhalb von ein bis zwei Werktagen.

Sie haben sich nicht angemeldet? Dann ignorieren Sie diese Mail einfach. Ohne Bestätigung geschieht nichts.

Fragen? Antworten Sie einfach auf diese Mail.

arc42 Trainings
https://trainings.arc42.org
```

`templates/registrant_en.txt`:

```text
Hello,

thank you for your {{if .Facts}}registration{{else}}request{{end}} with arc42. It is only complete once you confirm it:

{{.ConfirmURL}}

The link is valid for 5 days.
{{with .Facts}}
Your selection
  Course:        {{.Title}}
  Dates:         {{.Dates}}
  Location:      {{.Where}}
{{- if .Trainers}}
  Trainers:      {{.Trainers}}{{end}}
{{- if .Price}}
  Price:         {{.Price}}{{end}}
  Booking code:  {{.Code}}
{{else}}
You chose "other". We will get in touch with you personally.
{{end}}
We process registrations by hand and will get back to you personally, usually within one or two business days.

You did not register? Then simply ignore this mail. Nothing happens without confirmation.

Questions? Just reply to this mail.

arc42 Trainings
https://trainings.arc42.org
```

`templates/registrant.html`:

```html
<!DOCTYPE html>
<html lang="{{.Lang}}"><head><meta charset="utf-8"><title>{{.T.Title}}</title></head>
<body style="font-family:Arial,Helvetica,sans-serif;font-size:15px;line-height:1.5;color:#222;max-width:560px">
<p>{{.T.Hello}}</p>
<p>{{if .Facts}}{{.T.Intro}}{{else}}{{.T.IntroOther}}{{end}}</p>
<p><a href="{{.ConfirmURL}}" style="display:inline-block;padding:10px 18px;background:#1f4e79;color:#ffffff;text-decoration:none;border-radius:4px">{{.T.Button}}</a></p>
<p style="font-size:13px;color:#555">{{.T.Valid}}<br>{{.ConfirmURL}}</p>
{{with .Facts}}
<table style="border-collapse:collapse;font-size:15px">
<tr><td style="padding:2px 12px 2px 0;color:#555">{{$.T.Course}}</td><td>{{.Title}}</td></tr>
<tr><td style="padding:2px 12px 2px 0;color:#555">{{$.T.Dates}}</td><td>{{.Dates}}</td></tr>
<tr><td style="padding:2px 12px 2px 0;color:#555">{{$.T.Where}}</td><td>{{.Where}}</td></tr>
{{if .Trainers}}<tr><td style="padding:2px 12px 2px 0;color:#555">{{$.T.Trainers}}</td><td>{{.Trainers}}</td></tr>{{end}}
{{if .Price}}<tr><td style="padding:2px 12px 2px 0;color:#555">{{$.T.Price}}</td><td>{{.Price}}</td></tr>{{end}}
<tr><td style="padding:2px 12px 2px 0;color:#555">{{$.T.Code}}</td><td>{{.Code}}</td></tr>
</table>
{{else}}<p>{{.T.Other}}</p>{{end}}
<p>{{.T.ByHand}}</p>
<p>{{.T.NotYou}}</p>
<p>{{.T.Questions}}</p>
<p>arc42 Trainings<br><a href="https://trainings.arc42.org">trainings.arc42.org</a></p>
</body></html>
```

`templates/backoffice_de.txt`:

```text
Anmeldung {{.Reg.ID}} ({{.Status}})

Kurs:              {{.Reg.Code}}
{{- with .Facts}}
                   {{.Title}}, {{.Dates}}, {{.Where}}{{end}}
Nachname:          {{.Reg.LastName}}
Vorname:           {{.Reg.FirstName}}
E-Mail:            {{.Reg.Email}}
Nachname TN:       {{.Reg.ParticipantLast}}
Vorname TN:        {{.Reg.ParticipantFirst}}
E-Mail TN:         {{.Reg.ParticipantEmail}}
Rechnungsadresse:
{{indent .Reg.Billing}}
Bemerkungen:
{{indent .Reg.Comments}}

Sprache:           {{.Reg.Lang}}
via:               {{.Reg.Via}}
form_source:       {{.Reg.FormSource}}
{{- if .Hints}}

Hinweise:
{{- range .Hints}}
  - {{.}}{{end}}{{end}}

Gebucht wird erst auf die Mail "(BESTÄTIGT)" mit derselben Nummer {{.Reg.ID}}.
```

`templates/backoffice_en.txt`:

```text
Registration {{.Reg.ID}} ({{.Status}})

Course:                   {{.Reg.Code}}
{{- with .Facts}}
                          {{.Title}}, {{.Dates}}, {{.Where}}{{end}}
Last name:                {{.Reg.LastName}}
First name:               {{.Reg.FirstName}}
E-mail:                   {{.Reg.Email}}
Participant last name:    {{.Reg.ParticipantLast}}
Participant first name:   {{.Reg.ParticipantFirst}}
Participant e-mail:       {{.Reg.ParticipantEmail}}
Billing address:
{{indent .Reg.Billing}}
Comments:
{{indent .Reg.Comments}}

Language:                 {{.Reg.Lang}}
via:                      {{.Reg.Via}}
form_source:              {{.Reg.FormSource}}
{{- if .Hints}}

Notes:
{{- range .Hints}}
  - {{.}}{{end}}{{end}}

Book only on the "(CONFIRMED)" mail with the same number {{.Reg.ID}}.
```

`templates/confirmed_de.txt`:

```text
Anmeldung {{.ID}} wurde vom Anmeldenden bestätigt.

Kurs:       {{.Code}}
Nachname:   {{.LastName}}
E-Mail:     {{.Email}}

Alle Angaben stehen in der Mail "ANMELDUNG {{.ID}} ... (UNBESTÄTIGT)".
```

`templates/confirmed_en.txt`:

```text
Registration {{.ID}} was confirmed by the registrant.

Course:     {{.Code}}
Last name:  {{.LastName}}
E-mail:     {{.Email}}

All details are in the mail "REGISTRATION {{.ID}} ... (UNCONFIRMED)".
```

`mail.go`:

```go
// Package mail renders the three mails: the registrant's confirmation request
// and the back office's UNBESTÄTIGT/BESTÄTIGT pair. Every mail is in the
// language of the registration.
//
// The registrant mail never contains text the registrant typed: not the name,
// not the comments, not the billing address. The address it goes to was typed
// by a stranger, and anything echoed would be their text delivered from
// arc42's domain (spec 4.2). Course facts come from the feed only.
package mail

import (
	"bytes"
	"embed"
	htmltemplate "html/template"
	"strings"
	"text/template"

	"arc42-registration/internal/feed"
	"arc42-registration/internal/intake"
	"arc42-registration/internal/labels"
	"arc42-registration/internal/token"
)

//go:embed templates/*
var files embed.FS

var funcs = template.FuncMap{"indent": indent}

var (
	textT = template.Must(template.New("").Funcs(funcs).ParseFS(files, "templates/*.txt"))
	htmlT = htmltemplate.Must(htmltemplate.ParseFS(files, "templates/registrant.html"))
)

type Rendered struct {
	Subject string
	Text    string
	HTML    string // empty for the back-office mails
}

// Facts are the course facts as the registrant sees them, already worded for
// one language. Price is the regular amount only: early bird and alumni
// prices are applied by hand (spec 2).
type Facts struct {
	Title, ShortTitle, Code, Dates, Where, Trainers, Price string
}

func FactsFor(e feed.Entry, lang string) *Facts {
	f := &Facts{
		Title:      e.CourseTitle,
		ShortTitle: e.CourseShortTitle,
		Code:       e.Code,
		Dates:      labels.DateRange(e.Start, e.End, lang),
		Where:      e.City,
	}
	if f.Title == "" {
		f.Title = e.CourseShortTitle
	}
	if e.Format == "online" {
		f.Where = "Online"
	}
	join := " and "
	if lang == "de" {
		join = " und "
	}
	f.Trainers = strings.Join(e.Trainers, join)
	if e.Price != nil && e.Price.Amount > 0 {
		f.Price = labels.Money(e.Price.Amount, e.Price.Currency, lang)
	}
	return f
}

var text = map[string]map[string]string{
	"de": {
		"Title": "Bitte bestätigen Sie Ihre Anmeldung", "Hello": "Guten Tag,",
		"Intro":      "vielen Dank für Ihre Anmeldung bei arc42. Sie ist erst vollständig, wenn Sie sie bestätigen:",
		"IntroOther": "vielen Dank für Ihre Anfrage bei arc42. Sie ist erst vollständig, wenn Sie sie bestätigen:",
		"Button":     "Anmeldung bestätigen", "Valid": "Der Link ist 5 Tage gültig.",
		"Course": "Kurs", "Dates": "Termin", "Where": "Ort", "Trainers": "Trainer", "Price": "Preis", "Code": "Buchungscode",
		"Other":     `Sie haben "Sonstige" gewählt. Wir melden uns persönlich bei Ihnen.`,
		"ByHand":    "Wir bearbeiten Anmeldungen von Hand und melden uns persönlich, meist innerhalb von ein bis zwei Werktagen.",
		"NotYou":    "Sie haben sich nicht angemeldet? Dann ignorieren Sie diese Mail einfach. Ohne Bestätigung geschieht nichts.",
		"Questions": "Fragen? Antworten Sie einfach auf diese Mail.",
	},
	"en": {
		"Title": "Please confirm your registration", "Hello": "Hello,",
		"Intro":      "thank you for your registration with arc42. It is only complete once you confirm it:",
		"IntroOther": "thank you for your request with arc42. It is only complete once you confirm it:",
		"Button":     "Confirm registration", "Valid": "The link is valid for 5 days.",
		"Course": "Course", "Dates": "Dates", "Where": "Location", "Trainers": "Trainers", "Price": "Price", "Code": "Booking code",
		"Other":     `You chose "other". We will get in touch with you personally.`,
		"ByHand":    "We process registrations by hand and will get back to you personally, usually within one or two business days.",
		"NotYou":    "You did not register? Then simply ignore this mail. Nothing happens without confirmation.",
		"Questions": "Questions? Just reply to this mail.",
	},
}

var hintText = map[string]map[string]string{
	"de": {
		intake.HintGmailDots:     "Gmail-Adresse mit vielen Punkten (Muster aus Spam-Tests)",
		intake.HintURLInName:     "Link in einem Namensfeld",
		intake.HintOddCase:       "Zufällig wirkende Groß-/Kleinschreibung in einem Namen",
		intake.HintSeveralEmails: "Mehrere E-Mail-Adressen; die Bestätigung ging nur an die erste",
		intake.HintClosed:        "Termin war beim Absenden nicht mehr offen (ausgebucht, abgesagt oder vorbei)",
		intake.HintFeedDown:      "Kursliste nicht verfügbar, Buchungscode ungeprüft",
	},
	"en": {
		intake.HintGmailDots:     "Gmail address with many dots (a spam-probing pattern)",
		intake.HintURLInName:     "Link in a name field",
		intake.HintOddCase:       "Random-looking upper/lower case in a name",
		intake.HintSeveralEmails: "Several e-mail addresses; only the first received the confirmation request",
		intake.HintClosed:        "Date was no longer open when submitted (full, cancelled or past)",
		intake.HintFeedDown:      "Course list unavailable, booking code not checked",
	},
}

func lang(l string) string {
	if l == "en" {
		return "en"
	}
	return "de"
}

// Registrant renders the confirmation request. facts is nil for "Sonstige".
func Registrant(l string, facts *Facts, confirmURL string) (Rendered, error) {
	l = lang(l)
	data := map[string]any{"Lang": l, "Facts": facts, "ConfirmURL": confirmURL, "T": text[l]}
	var t, h bytes.Buffer
	if err := textT.ExecuteTemplate(&t, "registrant_"+l+".txt", data); err != nil {
		return Rendered{}, err
	}
	if err := htmlT.ExecuteTemplate(&h, "registrant.html", data); err != nil {
		return Rendered{}, err
	}
	subject := text[l]["Title"]
	if facts == nil {
		subject = map[string]string{"de": "Bitte bestätigen Sie Ihre Anfrage", "en": "Please confirm your request"}[l]
	} else {
		subject += ": " + facts.ShortTitle + ", " + facts.Dates
	}
	return Rendered{Subject: subject, Text: t.String(), HTML: h.String()}, nil
}

// Backoffice renders the first mail, sent at once. facts may be nil.
func Backoffice(reg intake.Registration, facts *Facts, hints []string) (Rendered, error) {
	l := lang(reg.Lang)
	status := map[string]string{"de": "UNBESTÄTIGT", "en": "UNCONFIRMED"}[l]
	var worded []string
	for _, h := range hints {
		worded = append(worded, hintText[l][h])
	}
	data := map[string]any{"Reg": reg, "Facts": facts, "Hints": worded, "Status": status}
	var t bytes.Buffer
	if err := textT.ExecuteTemplate(&t, "backoffice_"+l+".txt", data); err != nil {
		return Rendered{}, err
	}
	return Rendered{Subject: backofficeSubject(l, reg.ID, reg.Code, status), Text: t.String()}, nil
}

// Confirmed renders the second back-office mail from the token alone.
func Confirmed(c token.Claims) (Rendered, error) {
	l := lang(c.Lang)
	status := map[string]string{"de": "BESTÄTIGT", "en": "CONFIRMED"}[l]
	var t bytes.Buffer
	if err := textT.ExecuteTemplate(&t, "confirmed_"+l+".txt", c); err != nil {
		return Rendered{}, err
	}
	return Rendered{Subject: backofficeSubject(l, c.ID, c.Code, status), Text: t.String()}, nil
}

// Both back-office subjects differ only in the status tag, so mail clients
// thread them and a filter can match the tag.
func backofficeSubject(l, id, code, status string) string {
	word := map[string]string{"de": "ANMELDUNG", "en": "REGISTRATION"}[l]
	return "[trainings.arc42.org] " + word + " " + id + " " + code + " (" + status + ")"
}

func indent(s string) string {
	if strings.TrimSpace(s) == "" {
		return "  -"
	}
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}
```

- [x] **Step 4: Generate the HTML goldens and read them**

Run: `cd registration-app && go test ./internal/mail -run TestRegistrantMails -update && go test ./internal/mail -v`
Expected: PASS. Then open `internal/mail/testdata/registrant_de.html` in a browser and check: one blue button "Anmeldung bestätigen", the confirm URL printed under it, the table with Kurs, Termin, Ort, Trainer, Preis `2.890 €`, Buchungscode, and no name or address anywhere. `git diff --stat internal/mail/testdata` must show only the two `.html` files as new; if a `.txt` golden changed, the templates differ from this plan.

- [x] **Step 5: Commit**

```bash
git add registration-app/internal/mail
git commit -m "feat(registration): mails in the registration's language, nothing echoed to the registrant"
```

---

### Task 7: Sending (Mailjet, test-mode allow-list, log)

**Files:**
- Create: `registration-app/internal/send/send.go`, `mailjet.go`, `send_test.go`

**Interfaces:**
- Produces: `send.Message{To []string; ReplyTo, Subject, Text, HTML, CustomID string}`; `send.Sender` interface `Send(ctx, Message) error`; `send.Mailjet{Public, Private, From, FromName string; Sandbox bool; Client *http.Client; Endpoint string}`; `send.NewAllowList(next Sender, allowed []string, logger *log.Logger) *AllowList`; `send.LogSender{W io.Writer}`.

- [x] **Step 1: Write the failing test**

`registration-app/internal/send/send_test.go`:

```go
package send

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

type recorder struct{ got []Message }

func (r *recorder) Send(_ context.Context, m Message) error { r.got = append(r.got, m); return nil }

func TestAllowListKeepsTestDeploymentsHarmless(t *testing.T) {
	rec := &recorder{}
	var logs bytes.Buffer
	a := NewAllowList(rec, []string{"Me@Example.org"}, log.New(&logs, "", 0))

	_ = a.Send(context.Background(), Message{To: []string{"stranger@example.com"}, Subject: "x", CustomID: "R-1"})
	if len(rec.got) != 0 {
		t.Fatalf("a stranger was mailed: %+v", rec.got)
	}
	if !strings.Contains(logs.String(), "R-1") {
		t.Errorf("drop not logged: %q", logs.String())
	}
	_ = a.Send(context.Background(), Message{To: []string{"me@example.org"}, Subject: "Hello"})
	if len(rec.got) != 1 || rec.got[0].Subject != "[TEST] Hello" {
		t.Errorf("allowed mail = %+v", rec.got)
	}
}

func TestMailjetRequestShape(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "pub" || p != "priv" {
			t.Errorf("basic auth = %q %q %v", u, p, ok)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"Messages":[{"Status":"success"}]}`))
	}))
	defer srv.Close()
	m := &Mailjet{Public: "pub", Private: "priv", From: "trainings@arc42.org", FromName: "arc42 Trainings", Endpoint: srv.URL}
	err := m.Send(context.Background(), Message{To: []string{"a@example.org"}, ReplyTo: "info@arc42.de", Subject: "S", Text: "T", CustomID: "R-1"})
	if err != nil {
		t.Fatal(err)
	}
	msg := body["Messages"].([]any)[0].(map[string]any)
	if msg["TrackClicks"] != "disabled" || msg["TrackOpens"] != "disabled" {
		t.Errorf("tracking not disabled: %v %v", msg["TrackClicks"], msg["TrackOpens"])
	}
	if msg["From"].(map[string]any)["Email"] != "trainings@arc42.org" || msg["ReplyTo"].(map[string]any)["Email"] != "info@arc42.de" {
		t.Errorf("from/reply = %v %v", msg["From"], msg["ReplyTo"])
	}
	if _, ok := body["SandboxMode"]; ok {
		t.Error("SandboxMode sent although off")
	}
}

func TestMailjetRetriesOnceOn5xxButNotOn4xx(t *testing.T) {
	var calls atomic.Int32
	status := http.StatusBadGateway
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	m := &Mailjet{Public: "p", Private: "q", Endpoint: srv.URL}
	if err := m.Send(context.Background(), Message{To: []string{"a@b.de"}}); err == nil || calls.Load() != 2 {
		t.Errorf("5xx: err=%v calls=%d, want an error after 2 calls", err, calls.Load())
	}
	calls.Store(0)
	status = http.StatusUnauthorized
	if err := m.Send(context.Background(), Message{To: []string{"a@b.de"}}); err == nil || calls.Load() != 1 {
		t.Errorf("4xx: err=%v calls=%d, want an error after 1 call", err, calls.Load())
	}
}

// Runs against the real Mailjet API in sandbox mode (nothing is delivered)
// when MJ_APIKEY_PUBLIC/PRIVATE are set in the environment. Skipped in CI.
func TestMailjetSandboxAgainstTheRealAPI(t *testing.T) {
	pub, priv := os.Getenv("MJ_APIKEY_PUBLIC"), os.Getenv("MJ_APIKEY_PRIVATE")
	if pub == "" || priv == "" {
		t.Skip("MJ_APIKEY_PUBLIC/PRIVATE not set")
	}
	m := &Mailjet{Public: pub, Private: priv, From: "trainings@arc42.org", FromName: "arc42 Trainings", Sandbox: true}
	if err := m.Send(context.Background(), Message{To: []string{"sandbox@example.org"}, Subject: "sandbox", Text: "sandbox"}); err != nil {
		t.Fatal(err)
	}
}
```

- [x] **Step 2: Run to verify it fails**

Run: `cd registration-app && go test ./internal/send`
Expected: FAIL, undefined identifiers.

- [x] **Step 3: Implement**

`registration-app/internal/send/send.go`:

```go
// Package send delivers a rendered mail. Mailjet does it in production; the
// allow-list wrapper keeps a test deployment from mailing anyone else; the log
// sender prints mails for local runs.
package send

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
)

type Message struct {
	To       []string
	ReplyTo  string
	Subject  string
	Text     string
	HTML     string // optional
	CustomID string // registration id, shows up in Mailjet's message log
}

type Sender interface {
	Send(ctx context.Context, m Message) error
}

// AllowList passes a message on only to allowed recipients and marks every
// subject with [TEST]. A message left with no recipient is dropped and logged,
// not an error: the registrant mail to a stranger's address is the expected
// case in test mode.
type AllowList struct {
	Next    Sender
	Allowed map[string]bool
	Log     *log.Logger
}

func NewAllowList(next Sender, allowed []string, logger *log.Logger) *AllowList {
	m := map[string]bool{}
	for _, a := range allowed {
		m[strings.ToLower(a)] = true
	}
	return &AllowList{Next: next, Allowed: m, Log: logger}
}

func (a *AllowList) Send(ctx context.Context, m Message) error {
	var kept []string
	for _, to := range m.To {
		if a.Allowed[strings.ToLower(to)] {
			kept = append(kept, to)
		} else {
			a.Log.Printf("test mode: not sending %s %q to a recipient outside TEST_RECIPIENTS", m.CustomID, m.Subject)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	m.To = kept
	m.Subject = "[TEST] " + m.Subject
	return a.Next.Send(ctx, m)
}

// LogSender writes mails to W instead of sending them (MAILER=log).
type LogSender struct {
	mu sync.Mutex
	W  io.Writer
}

func (l *LogSender) Send(_ context.Context, m Message) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err := fmt.Fprintf(l.W, "----- mail %s\nTo: %s\nReply-To: %s\nSubject: %s\n\n%s\n", m.CustomID, strings.Join(m.To, ", "), m.ReplyTo, m.Subject, m.Text)
	return err
}
```

`registration-app/internal/send/mailjet.go`:

```go
package send

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const mailjetEndpoint = "https://api.mailjet.com/v3.1/send"

// Mailjet sends through the Send API v3.1 with basic auth. Tracking is off
// per message: click tracking would rewrite the confirm link into a Mailjet
// redirect (a foreign domain, likelier to be flagged, and a tracker on every
// click), and open tracking adds a pixel for nothing.
type Mailjet struct {
	Public, Private string
	From, FromName  string
	Sandbox         bool // validate without delivering
	Client          *http.Client
	Endpoint        string // tests point this at httptest
}

type mjAddress struct {
	Email string `json:"Email"`
	Name  string `json:"Name,omitempty"`
}

type mjMessage struct {
	From        mjAddress   `json:"From"`
	To          []mjAddress `json:"To"`
	ReplyTo     *mjAddress  `json:"ReplyTo,omitempty"`
	Subject     string      `json:"Subject"`
	TextPart    string      `json:"TextPart"`
	HTMLPart    string      `json:"HTMLPart,omitempty"`
	CustomID    string      `json:"CustomID,omitempty"`
	TrackClicks string      `json:"TrackClicks"`
	TrackOpens  string      `json:"TrackOpens"`
}

func (m *Mailjet) Send(ctx context.Context, msg Message) error {
	body := struct {
		Messages    []mjMessage `json:"Messages"`
		SandboxMode bool        `json:"SandboxMode,omitempty"`
	}{SandboxMode: m.Sandbox}
	mm := mjMessage{
		From: mjAddress{Email: m.From, Name: m.FromName}, Subject: msg.Subject,
		TextPart: msg.Text, HTMLPart: msg.HTML, CustomID: msg.CustomID,
		TrackClicks: "disabled", TrackOpens: "disabled",
	}
	for _, to := range msg.To {
		mm.To = append(mm.To, mjAddress{Email: to})
	}
	if msg.ReplyTo != "" {
		mm.ReplyTo = &mjAddress{Email: msg.ReplyTo}
	}
	body.Messages = []mjMessage{mm}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	// One retry, only for a server-side or network failure. A 4xx is our
	// mistake (bad key, unvalidated sender) and will not improve.
	err = m.post(ctx, payload)
	var retry retryable
	if errors.As(err, &retry) {
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
		err = m.post(ctx, payload)
	}
	return err
}

type retryable struct{ error }

func (m *Mailjet) post(ctx context.Context, payload []byte) error {
	endpoint := m.Endpoint
	if endpoint == "" {
		endpoint = mailjetEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.SetBasicAuth(m.Public, m.Private)
	req.Header.Set("Content-Type", "application/json")
	client := m.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return retryable{fmt.Errorf("mailjet: %w", err)}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 500 {
		return retryable{fmt.Errorf("mailjet: %s: %s", resp.Status, raw)}
	}
	var out struct {
		Messages []struct {
			Status string `json:"Status"`
		} `json:"Messages"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &out) != nil || len(out.Messages) == 0 || out.Messages[0].Status != "success" {
		return fmt.Errorf("mailjet: %s: %s", resp.Status, raw)
	}
	return nil
}
```

- [x] **Step 4: Run to verify it passes**

Run: `cd registration-app && go test ./internal/send -v`
Expected: PASS; `TestMailjetSandboxAgainstTheRealAPI` is SKIPPED unless the Mailjet keys are in the environment. When Gernot has finished the Mailjet Todoist tasks, run it once with the keys exported: it must PASS, which proves the key pair and the sender `trainings@arc42.org` are accepted without delivering anything.

- [x] **Step 5: Commit**

```bash
git add registration-app/internal/send
git commit -m "feat(registration): Mailjet sender with tracking off, test-mode allow-list"
```

---

### Task 8: HTTP handlers and pages

**Files:**
- Create: `registration-app/internal/web/server.go`, `submit.go`, `confirm.go`, `web_test.go`, `pages/confirm.html`, `pages/error.html`

**Interfaces:**
- Consumes: everything from Tasks 1 to 7.
- Produces: `web.Deps{Cfg config.Config; Checker *intake.Checker; Feed intake.Lookuper; Sealer *token.Sealer; Sender send.Sender; NewID func() string; Log *log.Logger}`; `web.New(Deps) *Server`; `(*Server).Routes() http.Handler` with `GET /healthz`, `POST /submit`, `GET /confirm`, `POST /confirm`.

- [x] **Step 1: Write the failing test**

`registration-app/internal/web/web_test.go`:

```go
package web

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"arc42-registration/internal/config"
	"arc42-registration/internal/feed"
	"arc42-registration/internal/intake"
	"arc42-registration/internal/send"
	"arc42-registration/internal/token"
)

type fakeFeed map[string]feed.Result

func (f fakeFeed) Lookup(_ context.Context, code string) (feed.Entry, feed.Result) {
	res, ok := f[code]
	if !ok {
		return feed.Entry{}, feed.Unknown
	}
	return feed.Entry{CourseTitle: "Mastering Software Architectures", CourseShortTitle: "MSA",
		Date: feed.Date{Code: code, Start: "2026-12-01", End: "2026-12-04", City: "München", Format: "public",
			Price: &feed.Price{Amount: 2890, Currency: "EUR"}}}, res
}

type fakeSender struct {
	got  []send.Message
	fail func(send.Message) error
}

func (f *fakeSender) Send(_ context.Context, m send.Message) error {
	if f.fail != nil {
		if err := f.fail(m); err != nil {
			return err
		}
	}
	f.got = append(f.got, m)
	return nil
}

type env struct {
	srv    http.Handler
	sender *fakeSender
	now    *time.Time
	logs   *bytes.Buffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	e := &env{sender: &fakeSender{}, now: &now, logs: &bytes.Buffer{}}
	sealer, _ := token.NewSealer([]byte("0123456789abcdef0123456789abcdef"), token.Valid, func() time.Time { return *e.now })
	ff := fakeFeed{"26-12 MSA": feed.Bookable}
	e.srv = New(Deps{
		Cfg: config.Config{BackofficeTo: "office@example.org", ReplyTo: "info@arc42.de",
			SiteURL: "https://trainings.arc42.org", PublicURL: "https://register.example"},
		Checker: &intake.Checker{Feed: ff, Limiter: intake.NewLimiter(100, time.Hour, time.Now),
			AllowedOrigins: []string{"https://trainings.arc42.org"}},
		Feed: ff, Sealer: sealer, Sender: e.sender,
		NewID: func() string { return "R-TEST1" },
		Log:   log.New(e.logs, "", 0),
	}).Routes()
	return e
}

func form(lang string) url.Values {
	v := url.Values{"Email": {"anna@example.org"}, "Kurs": {"26-12 MSA"}, "language": {lang}}
	if lang == "de" {
		v.Set("Nachname", "Müller")
		v.Set("Rechnungsadresse", "Firma X")
	} else {
		v.Set("last name", "Smith")
		v.Set("Billing address", "ACME")
	}
	return v
}

func (e *env) post(path string, v url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://trainings.arc42.org")
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

func (e *env) get(path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

var linkRe = regexp.MustCompile(`https://register\.example(/confirm\?t=[A-Za-z0-9_%-]+)`)

func TestTheWholeFlowInGerman(t *testing.T) {
	e := newEnv(t)
	rec := e.post("/submit", form("de"))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "https://trainings.arc42.org/anmeldung-erfolg/" {
		t.Fatalf("submit: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if len(e.sender.got) != 2 {
		t.Fatalf("sent %d mails, want back office + registrant", len(e.sender.got))
	}
	bo, reg := e.sender.got[0], e.sender.got[1]
	if bo.To[0] != "office@example.org" || !strings.HasSuffix(bo.Subject, "(UNBESTÄTIGT)") || bo.ReplyTo != "anna@example.org" {
		t.Errorf("back-office mail = %+v", bo)
	}
	if reg.To[0] != "anna@example.org" || reg.ReplyTo != "info@arc42.de" || !strings.Contains(reg.Text, "2.890 €") {
		t.Errorf("registrant mail = %+v", reg)
	}

	link := linkRe.FindStringSubmatch(reg.Text)
	if link == nil {
		t.Fatalf("no confirm link in:\n%s", reg.Text)
	}
	page := e.get(link[1])
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Anmeldung bestätigen") ||
		page.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("confirm page: %d %s", page.Code, page.Body.String())
	}
	if len(e.sender.got) != 2 {
		t.Fatal("opening the link sent a mail: a link scanner would confirm fakes")
	}

	tok, _ := url.QueryUnescape(strings.TrimPrefix(link[1], "/confirm?t="))
	done := e.post("/confirm", url.Values{"t": {tok}})
	if done.Code != http.StatusSeeOther || done.Header().Get("Location") != "https://trainings.arc42.org/anmeldung-bestaetigt/" {
		t.Fatalf("confirm: %d %q", done.Code, done.Header().Get("Location"))
	}
	conf := e.sender.got[2]
	if !strings.HasSuffix(conf.Subject, "(BESTÄTIGT)") || !strings.Contains(conf.Subject, "R-TEST1") {
		t.Errorf("confirmed mail subject = %q", conf.Subject)
	}

	// Pressed twice: a second mail, accepted (spec 4.4).
	e.post("/confirm", url.Values{"t": {tok}})
	if len(e.sender.got) != 4 {
		t.Errorf("double confirm sent %d mails in total, want 4", len(e.sender.got))
	}
}

func TestEnglishRedirectsAndSubjects(t *testing.T) {
	e := newEnv(t)
	rec := e.post("/submit", form("en"))
	if rec.Header().Get("Location") != "https://trainings.arc42.org/registration-success/" {
		t.Errorf("Location = %q", rec.Header().Get("Location"))
	}
	if !strings.HasSuffix(e.sender.got[0].Subject, "(UNCONFIRMED)") || !strings.Contains(e.sender.got[1].Text, "€2,890") {
		t.Errorf("mails = %+v", e.sender.got)
	}
}

func TestDropsLookLikeSuccessAndSendNothing(t *testing.T) {
	e := newEnv(t)
	f := form("de")
	f.Set("_gotcha", "bot")
	rec := e.post("/submit", f)
	if rec.Header().Get("Location") != "https://trainings.arc42.org/anmeldung-erfolg/" || len(e.sender.got) != 0 {
		t.Errorf("honeypot: %q, %d mails", rec.Header().Get("Location"), len(e.sender.got))
	}
	if !strings.Contains(e.logs.String(), "honeypot") {
		t.Error("drop reason not logged")
	}
}

func TestRejectsGoToTheFailPage(t *testing.T) {
	e := newEnv(t)
	f := form("en")
	f.Del("last name")
	if rec := e.post("/submit", f); rec.Header().Get("Location") != "https://trainings.arc42.org/registration-fail/" || len(e.sender.got) != 0 {
		t.Errorf("missing name: %q, %d mails", rec.Header().Get("Location"), len(e.sender.got))
	}
}

func TestBackOfficeFailureIsAFailure(t *testing.T) {
	e := newEnv(t)
	e.sender.fail = func(m send.Message) error { return errors.New("mailjet down") }
	if rec := e.post("/submit", form("de")); rec.Header().Get("Location") != "https://trainings.arc42.org/anmeldung-fail/" {
		t.Errorf("Location = %q, want the fail page", rec.Header().Get("Location"))
	}
}

func TestRegistrantFailureIsStillSuccess(t *testing.T) {
	e := newEnv(t)
	e.sender.fail = func(m send.Message) error {
		if m.To[0] == "anna@example.org" {
			return errors.New("bounced")
		}
		return nil
	}
	rec := e.post("/submit", form("de"))
	if rec.Header().Get("Location") != "https://trainings.arc42.org/anmeldung-erfolg/" || len(e.sender.got) != 1 {
		t.Errorf("Location = %q, mails = %d", rec.Header().Get("Location"), len(e.sender.got))
	}
}

func TestOversizedBodyIsRefused(t *testing.T) {
	e := newEnv(t)
	f := form("de")
	f.Set("Bemerkungen", strings.Repeat("x", maxBody))
	if rec := e.post("/submit", f); rec.Code != http.StatusRequestEntityTooLarge || len(e.sender.got) != 0 {
		t.Errorf("status %d, %d mails", rec.Code, len(e.sender.got))
	}
}

func TestBadTokens(t *testing.T) {
	e := newEnv(t)
	e.post("/submit", form("de"))
	tok, _ := url.QueryUnescape(strings.TrimPrefix(linkRe.FindStringSubmatch(e.sender.got[1].Text)[1], "/confirm?t="))

	if rec := e.get("/confirm?t=garbage"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "info@arc42.de") {
		t.Errorf("garbage: %d", rec.Code)
	}
	*e.now = e.now.Add(token.Valid + time.Hour)
	rec := e.post("/confirm", url.Values{"t": {tok}})
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusGone || !strings.Contains(string(body), "5") {
		t.Errorf("expired: %d %s", rec.Code, body)
	}
	if len(e.sender.got) != 2 {
		t.Error("an expired token sent a confirmation")
	}
}
```

- [x] **Step 2: Run to verify it fails**

Run: `cd registration-app && go test ./internal/web`
Expected: FAIL, undefined identifiers.

- [x] **Step 3: Implement the pages**

`pages/confirm.html`:

```html
<!DOCTYPE html>
<html lang="{{.Lang}}"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>{{.T.Title}}</title>
<style>
body{font-family:Arial,Helvetica,sans-serif;font-size:16px;line-height:1.5;color:#222;background:#fff;max-width:560px;margin:40px auto;padding:0 16px}
dt{color:#555;float:left;clear:left;width:9em}dd{margin:0 0 4px 9em}
button{font-size:17px;padding:10px 20px;background:#1f4e79;color:#fff;border:0;border-radius:4px;cursor:pointer}
</style></head>
<body>
<h1>{{.T.Title}}</h1>
{{with .Facts}}
<dl>
<dt>{{$.T.Course}}</dt><dd>{{.Title}}</dd>
<dt>{{$.T.Dates}}</dt><dd>{{.Dates}}</dd>
<dt>{{$.T.Where}}</dt><dd>{{.Where}}</dd>
<dt>{{$.T.Code}}</dt><dd>{{.Code}}</dd>
</dl>
{{end}}
<p>{{.T.Explain}}</p>
<form method="post" action="/confirm">
<input type="hidden" name="t" value="{{.Token}}">
<button type="submit">{{.T.Button}}</button>
</form>
</body></html>
```

`pages/error.html`:

```html
<!DOCTYPE html>
<html lang="de"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>arc42 Trainings</title>
<style>body{font-family:Arial,Helvetica,sans-serif;font-size:16px;line-height:1.5;color:#222;background:#fff;max-width:560px;margin:40px auto;padding:0 16px}</style>
</head>
<body>
<h1 lang="de">Dieser Link funktioniert nicht</h1>
<p lang="de">{{.DE}} Schreiben Sie uns bitte an <a href="mailto:info@arc42.de?subject=Anmeldung%20bestätigen">info@arc42.de</a>, wir kümmern uns darum.</p>
<h1 lang="en">This link does not work</h1>
<p lang="en">{{.EN}} Please write to <a href="mailto:info@arc42.de?subject=Confirm%20registration">info@arc42.de</a> and we will take care of it.</p>
</body></html>
```

- [x] **Step 4: Implement the server, submit and confirm handlers**

`server.go`:

```go
// Package web is the HTTP surface: POST /submit takes the form, GET /confirm
// shows a page with a button, POST /confirm confirms. Nothing here keeps
// state between requests.
package web

import (
	"context"
	"embed"
	"html/template"
	"log"
	"net"
	"net/http"
	"time"

	"arc42-registration/internal/config"
	"arc42-registration/internal/intake"
	"arc42-registration/internal/send"
	"arc42-registration/internal/token"
)

//go:embed pages/*.html
var pageFiles embed.FS

var pages = template.Must(template.ParseFS(pageFiles, "pages/*.html"))

type Deps struct {
	Cfg     config.Config
	Checker *intake.Checker
	Feed    intake.Lookuper
	Sealer  *token.Sealer
	Sender  send.Sender
	NewID   func() string
	Log     *log.Logger
}

type Server struct{ d Deps }

func New(d Deps) *Server {
	if d.NewID == nil {
		d.NewID = intake.NewID
	}
	return &Server{d: d}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("POST /submit", s.handleSubmit)
	mux.HandleFunc("GET /confirm", s.handleConfirmPage)
	mux.HandleFunc("POST /confirm", s.handleConfirm)
	return mux
}

// sendTimeout bounds one mail send including its retry, so a Mailjet outage
// turns into the fail page within seconds rather than a hanging browser.
const sendTimeout = 15 * time.Second

func (s *Server) send(ctx context.Context, m send.Message) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	return s.d.Sender.Send(ctx, m)
}

// Pages on the site the service redirects to, per language.
var sitePages = map[string]map[string]string{
	"success":   {"de": "/anmeldung-erfolg/", "en": "/registration-success/"},
	"fail":      {"de": "/anmeldung-fail/", "en": "/registration-fail/"},
	"confirmed": {"de": "/anmeldung-bestaetigt/", "en": "/registration-confirmed/"},
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request, page, lang string) {
	if lang != "en" {
		lang = "de"
	}
	http.Redirect(w, r, s.d.Cfg.SiteURL+sitePages[page][lang], http.StatusSeeOther)
}

// clientIP prefers Fly-Client-IP, which fly's proxy sets to the real client
// address. It is only trustworthy behind fly; locally RemoteAddr is used.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("Fly-Client-IP"); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// noStore keeps the confirm pages out of caches and, via no-referrer, keeps
// the token in their URL from leaking to any page they link to.
func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}
```

`submit.go`:

```go
package web

import (
	"net/http"
	"net/url"

	"arc42-registration/internal/intake"
	"arc42-registration/internal/mail"
	"arc42-registration/internal/send"
	"arc42-registration/internal/token"
)

const maxBody = 32 << 10

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseForm(); err != nil {
		s.d.Log.Printf("submit: unreadable body: %v", err)
		http.Error(w, "request too large or malformed", http.StatusRequestEntityTooLarge)
		return
	}
	dec := s.d.Checker.Check(r.Context(), intake.Input{Form: r.PostForm, Origin: r.Header.Get("Origin"), IP: clientIP(r)})
	reg := dec.Reg
	switch dec.Outcome {
	case intake.Drop:
		// A bot learns nothing about which check it tripped.
		s.d.Log.Printf("submit: dropped (%s)", dec.Reason)
		s.redirect(w, r, "success", reg.Lang)
		return
	case intake.Reject:
		s.d.Log.Printf("submit: rejected (%s)", dec.Reason)
		s.redirect(w, r, "fail", reg.Lang)
		return
	}

	reg.ID = s.d.NewID()
	var facts *mail.Facts
	if dec.Found {
		facts = mail.FactsFor(dec.Entry, reg.Lang)
	}

	// Back office first: if this fails, the registration reached nobody and
	// the person has to know.
	bo, err := mail.Backoffice(reg, facts, dec.Hints)
	if err == nil {
		err = s.send(r.Context(), send.Message{To: []string{s.d.Cfg.BackofficeTo}, ReplyTo: reg.Emails[0], Subject: bo.Subject, Text: bo.Text, CustomID: reg.ID})
	}
	if err != nil {
		s.d.Log.Printf("submit %s: back-office mail failed: %v", reg.ID, err)
		s.redirect(w, r, "fail", reg.Lang)
		return
	}

	// The registrant mail. If it fails the back office already has the
	// registration and follows up, so the person still sees success.
	tok, err := s.d.Sealer.Seal(token.Claims{ID: reg.ID, Code: reg.Code, Email: reg.Emails[0], LastName: reg.LastName, Lang: reg.Lang})
	if err == nil {
		confirmURL := s.d.Cfg.PublicURL + "/confirm?t=" + url.QueryEscape(tok)
		var rm mail.Rendered
		if rm, err = mail.Registrant(reg.Lang, facts, confirmURL); err == nil {
			err = s.send(r.Context(), send.Message{To: []string{reg.Emails[0]}, ReplyTo: s.d.Cfg.ReplyTo, Subject: rm.Subject, Text: rm.Text, HTML: rm.HTML, CustomID: reg.ID})
		}
	}
	if err != nil {
		s.d.Log.Printf("submit %s: registrant mail failed: %v", reg.ID, err)
	} else {
		s.d.Log.Printf("submit %s: accepted %s (%s) hints=%v", reg.ID, reg.Code, reg.Lang, dec.Hints)
	}
	s.redirect(w, r, "success", reg.Lang)
}
```

`confirm.go`:

```go
package web

import (
	"errors"
	"net/http"

	"arc42-registration/internal/mail"
	"arc42-registration/internal/send"
	"arc42-registration/internal/token"
)

var confirmText = map[string]map[string]string{
	"de": {"Title": "Anmeldung bestätigen", "Course": "Kurs", "Dates": "Termin", "Where": "Ort", "Code": "Buchungscode",
		"Explain": "Mit dem Knopf bestätigen Sie diese Anmeldung. Wir melden uns danach persönlich bei Ihnen.",
		"Button":  "Anmeldung bestätigen"},
	"en": {"Title": "Confirm registration", "Course": "Course", "Dates": "Dates", "Where": "Location", "Code": "Booking code",
		"Explain": "This button confirms the registration. We will then get in touch with you personally.",
		"Button":  "Confirm registration"},
}

// handleConfirmPage shows the course and a button. It confirms NOTHING:
// corporate link scanners (Safe Links, Mimecast, Proofpoint) open every link
// in incoming mail, and if a GET confirmed, every fake registration sent to a
// company address would confirm itself.
func (s *Server) handleConfirmPage(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("t")
	c, err := s.d.Sealer.Open(tok)
	if err != nil {
		s.errorPage(w, err)
		return
	}
	var facts *mail.Facts
	if c.Code != "" && c.Code != "sonstige" {
		if e, _ := s.d.Feed.Lookup(r.Context(), c.Code); e.Code != "" {
			facts = mail.FactsFor(e, c.Lang)
		}
	}
	lang := c.Lang
	if lang != "en" {
		lang = "de"
	}
	noStore(w)
	_ = pages.ExecuteTemplate(w, "confirm.html", map[string]any{"Lang": lang, "T": confirmText[lang], "Facts": facts, "Token": tok})
}

func (s *Server) handleConfirm(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		s.errorPage(w, token.ErrInvalid)
		return
	}
	c, err := s.d.Sealer.Open(r.PostForm.Get("t"))
	if err != nil {
		s.errorPage(w, err)
		return
	}
	m, err := mail.Confirmed(c)
	if err == nil {
		err = s.send(r.Context(), send.Message{To: []string{s.d.Cfg.BackofficeTo}, ReplyTo: c.Email, Subject: m.Subject, Text: m.Text, CustomID: c.ID})
	}
	if err != nil {
		s.d.Log.Printf("confirm %s: mail failed: %v", c.ID, err)
		s.errorPage(w, err)
		return
	}
	s.d.Log.Printf("confirm %s: confirmed %s", c.ID, c.Code)
	s.redirect(w, r, "confirmed", c.Lang)
}

// errorPage is bilingual: an unreadable token carries no language.
func (s *Server) errorPage(w http.ResponseWriter, err error) {
	de, en := "Der Link ist ungültig.", "The link is invalid."
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, token.ErrExpired):
		de, en = "Der Link ist abgelaufen (er gilt 5 Tage).", "The link has expired (it is valid for 5 days)."
		status = http.StatusGone
	case !errors.Is(err, token.ErrInvalid):
		de, en = "Die Bestätigung konnte gerade nicht verschickt werden.", "The confirmation could not be sent just now."
		status = http.StatusBadGateway
	}
	noStore(w)
	w.WriteHeader(status)
	_ = pages.ExecuteTemplate(w, "error.html", map[string]string{"DE": de, "EN": en})
}
```

- [x] **Step 5: Run to verify it passes**

Run: `make reg-check`
Expected: `registration service: tests, vet and gofmt are clean`.

- [x] **Step 6: Commit**

```bash
git add registration-app/internal/web
git commit -m "feat(registration): submit, two-step confirm, bilingual error page"
```

---

### Task 9: Main, container, fly app, deploy plumbing, README

**Files:**
- Create: `registration-app/main.go`, `registration-app/Dockerfile`, `registration-app/fly.toml`, `registration-app/README.md`
- Modify: `Makefile` (deploy targets), `.github/workflows/registration-app.yml` (deploy job)

- [x] **Step 1: Write `main.go`**

```go
// Command arc42-registration receives the trainings.arc42.org registration
// form, mails the back office and the registrant, and confirms registrations
// from a sealed link. See docs/superpowers/specs/2026-09-25-registration-service-design.md.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"arc42-registration/internal/config"
	"arc42-registration/internal/feed"
	"arc42-registration/internal/intake"
	"arc42-registration/internal/send"
	"arc42-registration/internal/token"
	"arc42-registration/internal/web"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("config: %v", err)
	}

	var sender send.Sender
	if cfg.Mailer == "log" {
		sender = &send.LogSender{W: os.Stdout}
	} else {
		sender = &send.Mailjet{Public: cfg.MailjetPublic, Private: cfg.MailjetPrivate, From: cfg.MailFrom, FromName: cfg.MailFromName}
	}
	if cfg.TestMode() {
		// The back office address is always allowed, so test mode still
		// shows both back-office mails.
		sender = send.NewAllowList(sender, append(cfg.TestRecipients, cfg.BackofficeTo), logger)
		logger.Printf("test mode: mail only to %v and %s", cfg.TestRecipients, cfg.BackofficeTo)
	}

	sealer, err := token.NewSealer(cfg.TokenKey, token.Valid, time.Now)
	if err != nil {
		logger.Fatalf("token: %v", err)
	}
	courses := feed.New(cfg.FeedURL, nil, time.Now)
	srv := web.New(web.Deps{
		Cfg: cfg,
		Checker: &intake.Checker{
			Feed:           courses,
			Limiter:        intake.NewLimiter(5, time.Hour, time.Now),
			AllowedOrigins: cfg.AllowedOrigins,
		},
		Feed:   courses,
		Sealer: sealer,
		Sender: sender,
		Log:    logger,
	})

	hs := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      45 * time.Second, // two sends with retries, bounded by sendTimeout
	}
	logger.Printf("listening on %s, confirm links under %s", cfg.Addr, cfg.PublicURL)
	logger.Fatal(hs.ListenAndServe())
}
```

- [x] **Step 2: Local smoke run against the live feed**

Run in one terminal: `make reg-run`. In another:

```bash
code=$(curl -s https://trainings.arc42.org/api/trainings.json | python3 -c "import json,sys;d=json.load(sys.stdin);print([x['code'] for c in d['courses'] for x in c['dates'] if x['status']=='open'][0])")
curl -s -o /dev/null -w "%{http_code} %{redirect_url}\n" -X POST http://localhost:8099/submit \
  -H 'Origin: http://localhost:4260' --data-urlencode "Nachname=Test" --data-urlencode "Email=test@example.org" \
  --data-urlencode "Kurs=$code" --data-urlencode "Rechnungsadresse=X" --data-urlencode "language=de"
curl -s -o /dev/null -w "%{http_code} %{redirect_url}\n" -X POST http://localhost:8099/submit \
  --data-urlencode "Nachname=Bot" --data-urlencode "Email=b@example.org" --data-urlencode "Kurs=99 FAKE" --data-urlencode "Rechnungsadresse=X"
```

Expected: both print `303 https://trainings.arc42.org/anmeldung-erfolg/`. The first terminal prints two mails (`(UNBESTÄTIGT)` to `office@example.invalid`, the registrant mail with a `http://localhost:8099/confirm?t=...` link) and, for the second request, `submit: dropped (unknown code 99 FAKE)`. Open the confirm link in a browser: a page with the course and one button; pressing it prints the `(BESTÄTIGT)` mail and redirects to `/anmeldung-bestaetigt/` (a 404 until Task 10 is deployed; that is expected here).

- [x] **Step 3: Container and fly config**

`registration-app/Dockerfile`:

```dockerfile
# Production image for the registration service. Same shape as
# admin-app/Dockerfile: built with registration-app/ as the context, one static
# binary FROM scratch. time/tzdata is compiled in (internal/feed), because
# scratch has no zoneinfo and "is this date past" is decided in Berlin time.
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/registration .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/registration /registration
EXPOSE 8080
ENTRYPOINT ["/registration"]
```

`registration-app/fly.toml`:

```toml
# The registration service. Stateless like the admin app: no [mounts], ever.
# Pending registrations live inside the confirm link (internal/token), not here.
app = "arc42-registration"
primary_region = "ams"

[build]

[http_service]
  internal_port = 8080
  force_https = true
  # Scale to zero. A cold start costs about 1.6 s on the first submit after an
  # idle spell (measured on the admin app, 2026-09-25); fly's proxy holds the
  # request meanwhile, so nothing is lost. "suspend" would be faster if needed.
  auto_stop_machines = "stop"
  auto_start_machines = true
  min_machines_running = 0

[env]
  # TEST MODE until the switch-over PR (spec 7.2, stage 5). Without
  # ENVIRONMENT=PRODUCTION the app refuses to start unless TEST_RECIPIENTS is
  # set, so this deployment cannot mail anyone outside the list.
  ENVIRONMENT = "TEST"
  PUBLIC_URL = "https://arc42-registration.fly.dev"
  SITE_URL = "https://trainings.arc42.org"
  FEED_URL = "https://trainings.arc42.org/api/trainings.json"
  ALLOWED_ORIGINS = "https://trainings.arc42.org,http://localhost:4260"
  # Set with `fly secrets import`, never here: MJ_APIKEY_PUBLIC,
  # MJ_APIKEY_PRIVATE and TOKEN_KEY are secret; BACKOFFICE_TO and
  # TEST_RECIPIENTS are personal mail addresses, and this repository is public.

[[http_service.checks]]
  interval = "30s"
  timeout = "5s"
  grace_period = "5s"
  method = "GET"
  path = "/healthz"
```

- [x] **Step 4: Prove the image works, including the time zone**

```bash
cd registration-app && docker build -t arc42-registration:local . && \
docker run --rm -p 8099:8080 -e MAILER=log -e PUBLIC_URL=http://localhost:8099 \
  -e BACKOFFICE_TO=office@example.invalid -e TOKEN_KEY=$(openssl rand -base64 32) arc42-registration:local
```

Expected: `listening on :8080`, no panic (a missing zone database would panic at start in `feed.mustBerlin`). `curl -s localhost:8099/healthz` prints `ok`. Stop with Ctrl-C.

- [x] **Step 5: Deploy targets and CI deploy job**

Add to the registration section of the `Makefile`:

```make
reg-deploy: check-flyctl reg-check ## Deploy the registration service in the CURRENT working tree to fly.io
	@printf "==> Deploying $(REG_DIR)/ to fly app $(REG_APP)\n"
	@if [ "$(YES)" != "1" ]; then \
		printf "==> Type 'deploy' to continue (or YES=1 make reg-deploy): "; \
		read -r answer; [ "$$answer" = "deploy" ] || { printf "==> aborted\n"; exit 1; }; \
	fi
	cd $(REG_DIR) && flyctl deploy --remote-only -a $(REG_APP)

reg-status: check-flyctl ## Show the registration service's machines and health checks
	cd $(REG_DIR) && flyctl status -a $(REG_APP)

reg-logs: check-flyctl ## Tail the registration service's logs (drops, rejects, sends)
	cd $(REG_DIR) && flyctl logs -a $(REG_APP)
```

Append to `.github/workflows/registration-app.yml`:

```yaml
  deploy:
    needs: test
    if: github.event_name != 'pull_request'
    runs-on: ubuntu-latest
    concurrency:
      group: deploy-registration-app
      cancel-in-progress: true
    steps:
      - uses: actions/checkout@v4
      - uses: superfly/flyctl-actions/setup-flyctl@master
      - name: Deploy
        working-directory: registration-app
        # A deploy token is scoped to one fly app, so this is a separate
        # secret from the admin app's FLY_API_TOKEN. Skips cleanly until it
        # exists, like deploy-admin-app.yml.
        run: |
          if [ -z "$FLY_API_TOKEN" ]; then
            echo "::notice title=Deploy skipped::FLY_REGISTRATION_API_TOKEN is not set."
            exit 0
          fi
          flyctl deploy --remote-only
        env:
          FLY_API_TOKEN: ${{ secrets.FLY_REGISTRATION_API_TOKEN }}
```

- [x] **Step 6: README**

`registration-app/README.md`, covering, in this order and briefly: what it does (one paragraph, link to the spec and to `docs/registration-flow/registration-flow.svg`); the four endpoints; the configuration table from spec section 6 plus `PUBLIC_URL` and `MAILER`, marking which are fly secrets and why (`BACKOFFICE_TO` and `TEST_RECIPIENTS` are personal addresses, the repo is public); test mode and the start-up refusal; `make reg-run` / `reg-check` / `reg-deploy` / `reg-logs`; what the log lines mean (`dropped (reason)`, `rejected (reason)`, `accepted`, `confirmed`); rotating `TOKEN_KEY` invalidates open links.

- [x] **Step 7: Commit**

```bash
git add registration-app Makefile .github/workflows/registration-app.yml
git commit -m "feat(registration): main, container, fly app and deploy plumbing"
```

---

### Task 10: Site pages, the test page and the endpoint parameter

**Files:**
- Modify: `_includes/registration-form.html` (the `<form>` tag), `_includes/head.html` (noindex)
- Create: `_pages/anmeldung-bestaetigt.md`, `_pages/registration-confirmed.md`, `_pages/anmeldung-test-8r4tqz.md`, `_pages/registration-test-8r4tqz.md`

- [ ] **Step 1: The endpoint parameter**

In `_includes/registration-form.html`, replace

```liquid
<form action="https://submit-form.com/{{ formspark_id }}"
      data-botpoison-public-key="{{ botpoison_key }}"
```

with

```liquid
{%- comment -%}
  include.endpoint: where the form posts when it is NOT Formspark. Set only by
  the hidden test pages while the registration service runs in test mode
  (spec 2026-09-25, section 7.2 stage 4); the go-live PR makes it the default.
  With an endpoint there is no Botpoison attribute: the service does its own
  checks, and the Botpoison script would otherwise try to attach a challenge
  the service never verifies. rf_ prefix: includes share the caller's scope.
{%- endcomment -%}
{%- if include.endpoint -%}
  {%- assign rf_action = include.endpoint -%}
{%- else -%}
  {%- capture rf_action -%}https://submit-form.com/{{ formspark_id }}{%- endcapture -%}
{%- endif -%}
<form action="{{ rf_action }}"
      {% if include.endpoint %}method="post"{% else %}data-botpoison-public-key="{{ botpoison_key }}"{% endif %}
```

(`method="post"` only with an endpoint: the service accepts POST only, and a form without `method` submits as GET. The production Formspark form keeps its exact current attributes, so this task changes nothing for real registrants.)

- [ ] **Step 2: noindex support**

In `_includes/head.html`, directly after `{% include seo.html %}`:

```liquid
{%- comment -%}
  noindex: front-matter flag for pages that must be reachable but not found,
  such as the registration service's test pages. sitemap: false keeps them out
  of sitemap.xml; this keeps them out of search results.
{%- endcomment -%}
{% if page.noindex %}<meta name="robots" content="noindex, nofollow">{% endif %}
```

- [ ] **Step 3: The confirmed pages**

`_pages/anmeldung-bestaetigt.md`:

```markdown
---
title: "Bestätigt"
layout: page
permalink: /anmeldung-bestaetigt/
lang: de
# og:locale comes from page.locale via the vendored _includes/seo.html (no jekyll-seo-tag plugin)
locale: de_DE
translation_url: /registration-confirmed/
sitemap: false
---

<div class="form-outcome form-outcome--success" markdown="1">
### Danke, Ihre Anmeldung ist bestätigt.

Wir bearbeiten Anmeldungen _von Hand_ und melden uns persönlich bei Ihnen, meist innerhalb von ein bis zwei Werktagen.
</div>

### und nun...

<a class="btn btn--info" href="/de/#training-dates">Zu den<br>Terminen</a>
<a class="btn btn--inverse" href="/de/">Zur<br>Startseite</a>
```

`_pages/registration-confirmed.md`:

```markdown
---
title: "Confirmed"
layout: page
permalink: /registration-confirmed/
lang: en
translation_url: /anmeldung-bestaetigt/
sitemap: false
---

<div class="form-outcome form-outcome--success" markdown="1">
### Thank you, your registration is confirmed.

We process registrations _by hand_ and will get back to you personally, usually within one or two business days.
</div>

### and now...

<a class="btn btn--info" href="/#training-dates">Back to<br>training dates</a>
<a class="btn btn--inverse" href="/">Take me<br>home</a>
```

- [ ] **Step 4: The test pages**

`_pages/anmeldung-test-8r4tqz.md`:

```markdown
---
title: "Anmeldung (Test)"
layout: page
permalink: /anmeldung-test-8r4tqz/
lang: de
locale: de_DE
translation_url: /registration-test-8r4tqz/
noindex: true
sitemap: false
---

{%- comment -%}
  Test page for the registration service in test mode (spec 2026-09-25,
  section 7.2 stage 4). The path is not secret, this repository is public;
  what makes the page harmless is the service's TEST_RECIPIENTS allow-list.
  Deleted in the go-live PR.
{%- endcomment -%}

<div class="form-outcome form-outcome--fail" markdown="1">
**Testseite.** Diese Anmeldung geht an den neuen Anmeldedienst im Testmodus. Nichts davon ist eine echte Buchung.
</div>

{% include registration-form.html lang="de" endpoint="https://arc42-registration.fly.dev/submit" %}
```

`_pages/registration-test-8r4tqz.md`: the same with `title: "Registration (test)"`, `permalink: /registration-test-8r4tqz/`, `lang: en`, no `locale` line, `translation_url: /anmeldung-test-8r4tqz/`, the notice text `**Test page.** This registration goes to the new registration service in test mode. Nothing here is a real booking.`, and `lang="en"` in the include.

- [ ] **Step 5: Verify the built pages**

```bash
make site
grep -o '<form action="[^"]*"' _site/anmeldung-test-8r4tqz/index.html _site/registration-test-8r4tqz/index.html _site/anmeldung/index.html _site/registration/index.html
grep -c 'data-botpoison-public-key' _site/anmeldung-test-8r4tqz/index.html _site/anmeldung/index.html
grep -c 'noindex' _site/anmeldung-test-8r4tqz/index.html _site/anmeldung/index.html
grep -c 'test-8r4tqz\|bestaetigt\|registration-confirmed' _site/sitemap.xml
test -f _site/anmeldung-bestaetigt/index.html && test -f _site/registration-confirmed/index.html && echo confirmed-pages
make check-links
```

Expected: the test pages post to `https://arc42-registration.fly.dev/submit`, the production pages still to `https://submit-form.com/AIKiYyJP` and `.../Tq1M7LqmX`; botpoison counts `0` for the test page and `1` for `/anmeldung/`; noindex counts `1` for the test page and `0` for `/anmeldung/`; the sitemap count is `0`; `confirmed-pages`; the link check passes.

- [ ] **Step 6: Commit**

```bash
git add _includes/registration-form.html _includes/head.html _pages/anmeldung-bestaetigt.md _pages/registration-confirmed.md _pages/anmeldung-test-8r4tqz.md _pages/registration-test-8r4tqz.md
git commit -m "feat: confirmed pages, hidden test pages and an endpoint option for the form"
```

---

### Task 11: Deploy in test mode and smoke-test it

Prerequisites (Gernot, Todoist section "Registration service"): the Mailjet domain, key and curl tasks are done; the fly app exists and its secrets are imported, including `BACKOFFICE_TO` and `TEST_RECIPIENTS`.

- [ ] **Step 1: Deploy**

Run: `make reg-deploy` (type `deploy`), then `make reg-status`.
Expected: one machine, check passing. `curl -s https://arc42-registration.fly.dev/healthz` prints `ok`.

- [ ] **Step 2: Automated cases against the deployed service**

```bash
B=https://arc42-registration.fly.dev
curl -s -o /dev/null -w "honeypot %{http_code} %{redirect_url}\n" -X POST $B/submit -H 'Origin: https://trainings.arc42.org' \
  --data-urlencode "Nachname=Bot" --data-urlencode "Email=b@example.org" --data-urlencode "Kurs=sonstige" \
  --data-urlencode "Rechnungsadresse=X" --data-urlencode "_gotcha=x"
curl -s -o /dev/null -w "unknown %{http_code} %{redirect_url}\n" -X POST $B/submit -H 'Origin: https://trainings.arc42.org' \
  --data-urlencode "Nachname=Bot" --data-urlencode "Email=b@example.org" --data-urlencode "Kurs=99 FAKE" --data-urlencode "Rechnungsadresse=X"
curl -s -o /dev/null -w "origin %{http_code} %{redirect_url}\n" -X POST $B/submit -H 'Origin: https://evil.example' \
  --data-urlencode "Nachname=Bot" --data-urlencode "Email=b@example.org" --data-urlencode "Kurs=sonstige" --data-urlencode "Rechnungsadresse=X"
curl -s -o /dev/null -w "missing %{http_code} %{redirect_url}\n" -X POST $B/submit -H 'Origin: https://trainings.arc42.org' \
  --data-urlencode "Email=b@example.org" --data-urlencode "Kurs=sonstige" --data-urlencode "language=en"
curl -s -o /dev/null -w "badtoken %{http_code}\n" "$B/confirm?t=garbage"
```

Expected: the first three redirect to `.../anmeldung-erfolg/`, `missing` to `.../registration-fail/`, `badtoken 400`. `make reg-logs` shows `dropped (honeypot)`, `dropped (unknown code 99 FAKE)`, `dropped (origin https://evil.example)`, `rejected (required field missing)`, and no send.

- [ ] **Step 3: Rate limit**

Send the `unknown` request six more times from one machine within a minute. Expected: the log shows `dropped (rate limit)` at the latest from the sixth request on. The limiter counts every submission that got past the origin and honeypot checks, so the `unknown` and `missing` requests from Step 2 already count.

- [ ] **Step 4: Hand over**

Tell Gernot the test pages are live at `https://trainings.arc42.org/anmeldung-test-8r4tqz/` and `/registration-test-8r4tqz/`, and that the Todoist task "end-to-end test in test mode" is ready. Record the cold-start time he measures in the PR description of the go-live plan.

---

### Task 12: Documentation of the third program

**Files:**
- Modify: `CLAUDE.md` ("What is here", "Building and checking", "Contracts that break silently"), `README.md` (overview and local development)

- [ ] **Step 1: CLAUDE.md**

- "What is here": "Two programs" becomes three: add **the registration service** (Go, own fly app `arc42-registration`, source under `registration-app/`, see `registration-app/README.md`).
- "Building and checking": add `make reg-check` next to `make app-check`.
- "Contracts that break silently", add one bullet: *The registration service reads `/api/trainings.json`* to decide which booking codes it accepts; a code that disappears from the feed is dropped silently, and an empty feed is treated as unavailable. It is a feed consumer under ADR-0004 like the other sites. Also: `registration-app` in `exclude:` is load-bearing, like `admin-app`.
- Do **not** touch the Formspark bullets yet: production still posts to Formspark until the go-live plan.

- [ ] **Step 2: README.md**

Add a short "Registration service" subsection under the overview, linking `registration-app/README.md`, the spec, and `docs/registration-flow/`.

- [ ] **Step 3: Commit and open the PR**

```bash
git add CLAUDE.md README.md
git commit -m "docs: the registration service as the repository's third program"
```

Remove the reference copy now that `registration-app/` exists, so two copies cannot drift:

```bash
diff -r docs/superpowers/plans/registration-service-reference registration-app -x README.md -x '*.golden' | grep -v '^Only in registration-app' ; git rm -r -q docs/superpowers/plans/registration-service-reference
git commit -m "docs: drop the registration service reference copy, registration-app/ is the source now"
```

The `diff` must print nothing except files that exist only in `registration-app/` (README, HTML goldens).

Open a PR titled `Registration service in test mode`. The description names the five Review Focus items, the test pages, and states plainly that production forms still post to Formspark.
