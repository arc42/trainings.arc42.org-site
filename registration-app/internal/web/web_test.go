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
		RecipientLimiter: intake.NewLimiter(3, 24*time.Hour, time.Now),
		NewID:            func() string { return "R-TEST1" },
		Log:              log.New(e.logs, "", 0),
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
	sentPage(t, rec, "anna@example.org", "Almost done")
	if !strings.HasSuffix(e.sender.got[0].Subject, "(UNCONFIRMED)") || !strings.Contains(e.sender.got[1].Text, "€2,890") {
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
	if len(toNew) != 1 || !strings.Contains(toNew[0].Text, "2.890 €") {
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
