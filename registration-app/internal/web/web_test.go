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
