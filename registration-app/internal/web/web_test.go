package web

import (
	"bytes"
	"context"
	"errors"
	"html"
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
	"arc42-registration/internal/store"
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
	store  store.Store
}

func newEnv(t *testing.T) *env {
	t.Helper()
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	e := &env{sender: &fakeSender{}, now: &now, logs: &bytes.Buffer{}, store: store.NewMemory()}
	return rebuild(e)
}

// rebuild (re)creates the server from the env, e.g. after swapping its store.
func rebuild(e *env) *env {
	sealer, _ := token.NewSealer([]byte("0123456789abcdef0123456789abcdef"), token.Valid, func() time.Time { return *e.now })
	ff := fakeFeed{"26-12 MSA": feed.Bookable}
	e.srv = New(Deps{
		Cfg: config.Config{BackofficeTo: []string{"office@example.org"}, ReplyTo: "info@arc42.de",
			SiteURL: "https://trainings.arc42.org", PublicURL: "https://register.example"},
		Checker: &intake.Checker{Feed: ff, Limiter: intake.NewLimiter(100, time.Hour, time.Now),
			AllowedOrigins: []string{"https://trainings.arc42.org"}},
		Feed: ff, Sealer: sealer, Sender: e.sender, Store: e.store,
		RecipientLimiter:  intake.NewLimiter(3, 24*time.Hour, time.Now),
		CorrectionLimiter: intake.NewLimiter(maxCorrections, correctionWindow, func() time.Time { return *e.now }),
		NewID:             func() string { return "R-TEST1" },
		Log:               log.New(e.logs, "", 0),
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
	sentPage(t, rec, "anna@example.org", "Fast geschafft")
	if len(e.sender.got) != 2 {
		t.Fatalf("sent %d mails, want back office + registrant", len(e.sender.got))
	}
	bo, reg := e.sender.got[0], e.sender.got[1]
	if bo.To[0] != "office@example.org" || !strings.HasSuffix(bo.Subject, "(UNBESTÄTIGT)") || bo.ReplyTo != "anna@example.org" {
		t.Errorf("back-office mail = %+v", bo)
	}
	if reg.To[0] != "anna@example.org" || reg.ReplyTo != "info@arc42.de" || !strings.Contains(reg.Text, "26-12 MSA") {
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

	// Pressed twice: the link works once. The second press lands on the
	// same confirmed page and sends nothing (changed 28 Sep 2026: the back
	// office got one BESTÄTIGT mail per click).
	again := e.post("/confirm", url.Values{"t": {tok}})
	if len(e.sender.got) != 3 || again.Header().Get("Location") != "https://trainings.arc42.org/anmeldung-bestaetigt/" {
		t.Errorf("second confirm: %d mails in total (want 3), location %q", len(e.sender.got), again.Header().Get("Location"))
	}
	// Opening the link again goes straight to the confirmed page.
	if page := e.get(link[1]); page.Code != http.StatusSeeOther || page.Header().Get("Location") != "https://trainings.arc42.org/anmeldung-bestaetigt/" {
		t.Errorf("opening a used link: %d %q", page.Code, page.Header().Get("Location"))
	}
}

func TestEnglishRedirectsAndSubjects(t *testing.T) {
	e := newEnv(t)
	rec := e.post("/submit", form("en"))
	sentPage(t, rec, "anna@example.org", "Almost done")
	if !strings.HasSuffix(e.sender.got[0].Subject, "(UNCONFIRMED)") || !strings.Contains(e.sender.got[1].Text, "26-12 MSA") {
		t.Errorf("mails = %+v", e.sender.got)
	}
}

func TestDropsLookLikeSuccessAndSendNothing(t *testing.T) {
	e := newEnv(t)
	f := form("de")
	f.Set("_gotcha", "bot")
	rec := e.post("/submit", f)
	// A bot gets exactly the page a person gets, correction form included.
	sentPage(t, rec, "anna@example.org", "Fast geschafft")
	if len(e.sender.got) != 0 {
		t.Errorf("honeypot: %d mails", len(e.sender.got))
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
	sentPage(t, rec, "anna@example.org", "Fast geschafft")
	if len(e.sender.got) != 1 {
		t.Errorf("mails = %d", len(e.sender.got))
	}
}

// Chinese or emoji text is 9 to 12 bytes per character once form-encoded, so
// a comment well inside its 4000-character limit used to hit the old fixed
// 32 KB cap and get a bare 413 instead of the page.
func TestALongNonLatinCommentIsNotRefusedAsTooLarge(t *testing.T) {
	e := newEnv(t)
	f := form("de")
	f.Set("Bemerkungen", strings.Repeat("会", 3900))
	rec := e.post("/submit", f)
	if rec.Code == http.StatusRequestEntityTooLarge {
		t.Fatal("a comment within its limit was refused as too large")
	}
	sentPage(t, rec, "anna@example.org", "Fast geschafft")
}

func TestOversizedBodyIsRefused(t *testing.T) {
	e := newEnv(t)
	f := form("de")
	f.Set("Bemerkungen", strings.Repeat("x", int(intake.MaxFormBytes())))
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

// The registrant mail goes to an address a stranger typed. However many IPs a
// script uses, one address gets at most three confirmation requests a day;
// the back office still sees every submission, marked.
func TestConfirmationMailsPerAddressAreCapped(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 4; i++ {
		e.post("/submit", form("de"))
	}
	var toOffice, toRegistrant []send.Message
	for _, m := range e.sender.got {
		if m.To[0] == "office@example.org" {
			toOffice = append(toOffice, m)
		} else {
			toRegistrant = append(toRegistrant, m)
		}
	}
	if len(toOffice) != 4 || len(toRegistrant) != 3 {
		t.Fatalf("office %d, registrant %d; want 4 and 3", len(toOffice), len(toRegistrant))
	}
	if !strings.Contains(toOffice[3].Text, "nicht verschickt") {
		t.Errorf("the fourth back-office mail does not say the confirmation was withheld:\n%s", toOffice[3].Text)
	}
}

// Opening the service's address in a browser should explain itself rather
// than answer 404, which looks like a failure.
func TestRootSaysWhatThisIs(t *testing.T) {
	e := newEnv(t)
	rec := e.get("/")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "arc42 registration service") {
		t.Errorf("GET / = %d %q", rec.Code, rec.Body.String())
	}
	if rec := e.get("/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /nope = %d, want 404", rec.Code)
	}
}

// sentPage asserts the page shown after a submission: the address the person
// typed, and a form to correct it.
func sentPage(t *testing.T, rec *httptest.ResponseRecorder, email, title string) string {
	t.Helper()
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, title) || !strings.Contains(body, email) || !strings.Contains(body, `action="/correct"`) {
		t.Fatalf("sent page: %d, want %q with %q and a correction form:\n%s", rec.Code, title, email, body)
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("sent page carries a token and must not be cached or leak it")
	}
	return body
}

var correctionTokenRe = regexp.MustCompile(`action="/correct"[\s\S]*?name="t" value="([^"]+)"`)

func correctionToken(t *testing.T, body string) string {
	t.Helper()
	m := correctionTokenRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no correction token in:\n%s", body)
	}
	return html.UnescapeString(m[1])
}

func mailsTo(e *env, addr string) []send.Message {
	var out []send.Message
	for _, m := range e.sender.got {
		if m.To[0] == addr {
			out = append(out, m)
		}
	}
	return out
}

// Typo in the address: the person corrects it on the page, the confirmation
// goes to the new address, the back office learns about it, and the
// confirmed mail carries the corrected address.
func TestCorrectingTheAddressResendsTheConfirmation(t *testing.T) {
	e := newEnv(t)
	body := sentPage(t, e.post("/submit", form("de")), "anna@example.org", "Fast geschafft")
	rec := e.post("/correct", url.Values{"t": {correctionToken(t, body)}, "email": {"anna@example.com"}})
	sentPage(t, rec, "anna@example.com", "Fast geschafft")

	toNew := mailsTo(e, "anna@example.com")
	if len(toNew) != 1 || !strings.Contains(toNew[0].Text, "26-12 MSA") {
		t.Fatalf("mails to the corrected address: %+v", toNew)
	}
	office := mailsTo(e, "office@example.org")
	last := office[len(office)-1]
	if !strings.Contains(last.Subject, "R-TEST1") || !strings.HasSuffix(last.Subject, "(E-MAIL KORRIGIERT)") ||
		!strings.Contains(last.Text, "anna@example.org") || !strings.Contains(last.Text, "anna@example.com") {
		t.Errorf("back-office correction notice: %q\n%s", last.Subject, last.Text)
	}

	link := linkRe.FindStringSubmatch(toNew[0].Text)
	tok, _ := url.QueryUnescape(strings.TrimPrefix(link[1], "/confirm?t="))
	e.post("/confirm", url.Values{"t": {tok}})
	conf := e.sender.got[len(e.sender.got)-1]
	if !strings.HasSuffix(conf.Subject, "(BESTÄTIGT)") || !strings.Contains(conf.Text, "anna@example.com") {
		t.Errorf("confirmed mail does not carry the corrected address:\n%s", conf.Text)
	}
}

// The token on the page is for correcting only. If it could confirm, a bot
// would confirm fakes without ever seeing the confirmation mail.
func TestTheCorrectionTokenCannotConfirm(t *testing.T) {
	e := newEnv(t)
	body := sentPage(t, e.post("/submit", form("de")), "anna@example.org", "Fast geschafft")
	before := len(e.sender.got)
	rec := e.post("/confirm", url.Values{"t": {correctionToken(t, body)}})
	if rec.Code != http.StatusBadRequest || len(e.sender.got) != before {
		t.Errorf("confirm with a correction token: %d, %d new mails", rec.Code, len(e.sender.got)-before)
	}
	// And the other way round.
	link := linkRe.FindStringSubmatch(mailsTo(e, "anna@example.org")[0].Text)
	tok, _ := url.QueryUnescape(strings.TrimPrefix(link[1], "/confirm?t="))
	if rec := e.post("/correct", url.Values{"t": {tok}, "email": {"x@example.com"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("correct with a confirm token: %d", rec.Code)
	}
}

func TestAtMostTwoCorrections(t *testing.T) {
	e := newEnv(t)
	body := sentPage(t, e.post("/submit", form("de")), "anna@example.org", "Fast geschafft")
	body = sentPage(t, e.post("/correct", url.Values{"t": {correctionToken(t, body)}, "email": {"a1@example.com"}}), "a1@example.com", "Fast geschafft")
	rec := e.post("/correct", url.Values{"t": {correctionToken(t, body)}, "email": {"a2@example.com"}})
	// After the second correction the page names the address and offers no
	// further form, only a human.
	last := rec.Body.String()
	if !strings.Contains(last, "a2@example.com") || strings.Contains(last, `action="/correct"`) || !strings.Contains(last, "info@arc42.de") {
		t.Errorf("after two corrections:\n%s", last)
	}
	if len(mailsTo(e, "a1@example.com")) != 1 || len(mailsTo(e, "a2@example.com")) != 1 {
		t.Errorf("each corrected address should get exactly one mail")
	}
}

func TestAnInvalidCorrectionIsShownNotSent(t *testing.T) {
	e := newEnv(t)
	body := sentPage(t, e.post("/submit", form("en")), "anna@example.org", "Almost done")
	before := len(e.sender.got)
	rec := e.post("/correct", url.Values{"t": {correctionToken(t, body)}, "email": {"anna@localhost"}})
	if len(e.sender.got) != before || !strings.Contains(rec.Body.String(), "does not look like") {
		t.Errorf("invalid correction: %d new mails, body:\n%s", len(e.sender.got)-before, rec.Body.String())
	}
}

// A dropped submission gets the same page and a working-looking correction;
// correcting it sends nothing, and the bot cannot tell.
func TestCorrectingADroppedSubmissionSendsNothing(t *testing.T) {
	e := newEnv(t)
	f := form("de")
	f.Set("_gotcha", "bot")
	body := sentPage(t, e.post("/submit", f), "anna@example.org", "Fast geschafft")
	sentPage(t, e.post("/correct", url.Values{"t": {correctionToken(t, body)}, "email": {"victim@example.com"}}), "victim@example.com", "Fast geschafft")
	if len(e.sender.got) != 0 {
		t.Errorf("a dropped submission sent %d mails after correction", len(e.sender.got))
	}
}

// A bot must not tell a drop from an accept by the page: same address
// spelling, same token length.
// The service's pages live on another host than the site, so they cannot use
// its Jekyll layout. They carry a copy of the site's frame (masthead with the
// arc42 logo, footer with imprint and privacy) so a registrant does not
// suddenly land on a bare page from an unknown domain in the middle of
// registering, and a progress bar that says where they are: form done, mail
// to confirm, registered.
func TestPagesCarryTheSiteFrameAndTheProgress(t *testing.T) {
	e := newEnv(t)
	sent := e.post("/submit", form("de")).Body.String()
	reg := e.sender.got[1]
	link := linkRe.FindStringSubmatch(reg.Text)
	confirm := e.get(link[1]).Body.String()
	sentEN := e.post("/submit", form("en")).Body.String()
	bad := e.get("/confirm?t=garbage").Body.String()

	frame := []string{`class="masthead"`, `aria-label="arc42"`, `href="https://trainings.arc42.org/imprint/"`, "Supported by INNOQ"}
	for name, body := range map[string]string{"sent": sent, "confirm": confirm, "sent-en": sentEN, "error": bad} {
		for _, want := range frame {
			if !strings.Contains(body, want) {
				t.Errorf("%s page lacks %q", name, want)
			}
		}
	}
	for name, c := range map[string]struct{ body, home, step string }{
		"sent":    {sent, `href="https://trainings.arc42.org/de/"`, `aria-current="step">E-Mail bestätigen`},
		"confirm": {confirm, `href="https://trainings.arc42.org/de/"`, `aria-current="step">E-Mail bestätigen`},
		"sent-en": {sentEN, `href="https://trainings.arc42.org/"`, `aria-current="step">Confirm e-mail`},
	} {
		if !strings.Contains(c.body, c.home) {
			t.Errorf("%s page: logo does not link to %s", name, c.home)
		}
		if !strings.Contains(stepText(c.body), c.step) {
			t.Errorf("%s page: current step is not %q", name, c.step)
		}
	}
}

// stepText collapses the markup inside a step so the test does not depend on
// the icon's SVG.
func stepText(body string) string {
	return regexp.MustCompile(`aria-current="step"[^>]*>(?:\s|<[^>]*>)*`).ReplaceAllString(body, `aria-current="step">`)
}

func TestDroppedAndAcceptedPagesLookAlike(t *testing.T) {
	e := newEnv(t)
	f := form("de")
	f.Set("Email", "Anna@Example.org")
	accepted := sentPage(t, e.post("/submit", f), "anna@example.org", "Fast geschafft")
	f.Set("_gotcha", "bot")
	dropped := sentPage(t, e.post("/submit", f), "anna@example.org", "Fast geschafft")
	if a, d := len(correctionToken(t, accepted)), len(correctionToken(t, dropped)); a != d {
		t.Errorf("token length accepted %d, dropped %d", a, d)
	}
	if strings.Contains(dropped, "Anna@Example.org") {
		t.Error("the dropped page shows the address as typed, the accepted one lower-cased")
	}
}

// The token is stateless and can be posted again. However often, one
// registration gets at most two corrections.
func TestReplayingACorrectionTokenIsBounded(t *testing.T) {
	e := newEnv(t)
	tok := correctionToken(t, sentPage(t, e.post("/submit", form("de")), "anna@example.org", "Fast geschafft"))
	for _, addr := range []string{"r1@example.com", "r2@example.com", "r3@example.com", "r4@example.com"} {
		e.post("/correct", url.Values{"t": {tok}, "email": {addr}})
	}
	sent := 0
	for _, addr := range []string{"r1@example.com", "r2@example.com", "r3@example.com", "r4@example.com"} {
		sent += len(mailsTo(e, addr))
	}
	if sent != maxCorrections {
		t.Errorf("replayed token sent %d corrections, want %d", sent, maxCorrections)
	}
}

// Corrections are for the moment right after submitting.
func TestTheCorrectionWindowCloses(t *testing.T) {
	e := newEnv(t)
	tok := correctionToken(t, sentPage(t, e.post("/submit", form("de")), "anna@example.org", "Fast geschafft"))
	*e.now = e.now.Add(correctionWindow + time.Minute)
	rec := e.post("/correct", url.Values{"t": {tok}, "email": {"late@example.com"}})
	if len(mailsTo(e, "late@example.com")) != 0 || !strings.Contains(rec.Body.String(), "info@arc42.de") {
		t.Errorf("late correction sent mail or offered a form:\n%s", rec.Body.String())
	}
}

// A failed attempt re-issues the page's token; that must not restart the
// window, or one token could be kept alive for ever.
func TestAnInvalidCorrectionDoesNotExtendTheWindow(t *testing.T) {
	e := newEnv(t)
	tok := correctionToken(t, sentPage(t, e.post("/submit", form("de")), "anna@example.org", "Fast geschafft"))
	*e.now = e.now.Add(correctionWindow - 5*time.Minute)
	tok = correctionToken(t, e.post("/correct", url.Values{"t": {tok}, "email": {"not-an-address"}}).Body.String())
	*e.now = e.now.Add(10 * time.Minute)
	e.post("/correct", url.Values{"t": {tok}, "email": {"late@example.com"}})
	if len(mailsTo(e, "late@example.com")) != 0 {
		t.Error("an invalid correction restarted the correction window")
	}
}

// A confirmation whose back-office mail failed is taken back, so the
// registrant's retry is a first confirmation and the mail goes out then.
func TestAFailedConfirmationCanBeRetried(t *testing.T) {
	e := newEnv(t)
	e.post("/submit", form("de"))
	link := linkRe.FindStringSubmatch(e.sender.got[1].Text)
	tok, _ := url.QueryUnescape(strings.TrimPrefix(link[1], "/confirm?t="))
	e.sender.fail = func(m send.Message) error {
		if strings.Contains(m.Subject, "BESTÄTIGT") {
			return errors.New("provider down")
		}
		return nil
	}
	if rec := e.post("/confirm", url.Values{"t": {tok}}); rec.Code != http.StatusBadGateway {
		t.Fatalf("failed mail: status %d, want 502", rec.Code)
	}
	e.sender.fail = nil
	e.post("/confirm", url.Values{"t": {tok}})
	if n := len(e.sender.got); n != 3 || !strings.HasSuffix(e.sender.got[2].Subject, "(BESTÄTIGT)") {
		t.Errorf("retry after a failed mail: %d mails, last %q", n, e.sender.got[n-1].Subject)
	}
}

var codeRe = regexp.MustCompile(`Bestätigungscode:\s+(\d{3} \d{3})`)

// The code from the mail, typed on the page that stayed open, confirms like
// the link: one BESTÄTIGT mail, then the site's confirmed page. Typing it
// again sends nothing.
func TestTheCodeFromTheMailConfirms(t *testing.T) {
	e := newEnv(t)
	page := e.post("/submit", form("de")).Body.String()
	if !strings.Contains(page, `action="/code"`) || !strings.Contains(page, `autocomplete="one-time-code"`) {
		t.Fatalf("the sent page has no code form:\n%s", page)
	}
	code := codeRe.FindStringSubmatch(e.sender.got[1].Text)[1]
	pageTok := correctionTokenRe.FindStringSubmatch(page)[1]
	rec := e.post("/code", url.Values{"t": {pageTok}, "code": {code}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "https://trainings.arc42.org/anmeldung-bestaetigt/" {
		t.Fatalf("code: %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if n := len(e.sender.got); n != 3 || !strings.HasSuffix(e.sender.got[2].Subject, "(BESTÄTIGT)") {
		t.Fatalf("after the code: %d mails", n)
	}
	e.post("/code", url.Values{"t": {pageTok}, "code": {code}})
	if len(e.sender.got) != 3 {
		t.Error("the code confirmed twice")
	}
}

// Six digits are guessable only by trying, so wrong tries are counted per
// registration and the field locks after five: from then on only the link
// in the mail confirms, even with the right code.
func TestWrongCodesLockAfterFive(t *testing.T) {
	e := newEnv(t)
	page := e.post("/submit", form("de")).Body.String()
	code := strings.ReplaceAll(codeRe.FindStringSubmatch(e.sender.got[1].Text)[1], " ", "")
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	pageTok := correctionTokenRe.FindStringSubmatch(page)[1]
	for i := 1; i <= 5; i++ {
		body := e.post("/code", url.Values{"t": {pageTok}, "code": {wrong}}).Body.String()
		if i < 5 && !strings.Contains(body, "Der Code stimmt nicht") {
			t.Fatalf("wrong try %d: no wrong-code message", i)
		}
	}
	body := e.post("/code", url.Values{"t": {pageTok}, "code": {code}}).Body.String()
	if !strings.Contains(body, "Zu viele falsche Versuche") || strings.Contains(body, `action="/code"`) || len(e.sender.got) != 2 {
		t.Errorf("after five wrong tries the right code still confirmed (%d mails) or the form is still there", len(e.sender.got))
	}
}

type downStore struct{ store.Store }

func (downStore) Failures(context.Context, string) (int, error) { return 0, errors.New("down") }

// Without the store the tries cannot be counted, so the code is refused and
// the page points to the link, which works without counting.
func TestCodeEntryNeedsTheStore(t *testing.T) {
	e := newEnv(t)
	e.store = downStore{store.NewMemory()}
	e = rebuild(e)
	page := e.post("/submit", form("de")).Body.String()
	code := codeRe.FindStringSubmatch(e.sender.got[1].Text)[1]
	body := e.post("/code", url.Values{"t": {correctionTokenRe.FindStringSubmatch(page)[1]}, "code": {code}}).Body.String()
	if !strings.Contains(body, "Link in der E-Mail") || len(e.sender.got) != 2 {
		t.Errorf("code accepted without a store (%d mails):\n%s", len(e.sender.got), body)
	}
}

// A dropped submission gets the same page, code form included, and no code
// ever confirms it: a bot learns nothing and confirms nothing.
func TestADroppedSubmissionNeverConfirms(t *testing.T) {
	e := newEnv(t)
	f := form("de")
	f.Set("_gotcha", "bot")
	page := e.post("/submit", f).Body.String()
	if !strings.Contains(page, `action="/code"`) {
		t.Fatal("the dropped page lacks the code form")
	}
	pageTok := correctionTokenRe.FindStringSubmatch(page)[1]
	for _, c := range []string{"000000", "123456", "999999"} {
		e.post("/code", url.Values{"t": {pageTok}, "code": {c}})
	}
	if len(e.sender.got) != 0 {
		t.Errorf("a dropped submission sent %d mails", len(e.sender.got))
	}
}
