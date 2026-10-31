package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"arc42-registration/internal/sheet"
	"arc42-registration/internal/store"
)

const hookToken = "0123456789abcdef0123456789abcdef"

type fakeDeliveries struct {
	mu  sync.Mutex
	got []sheet.Delivery
	err error
}

func (f *fakeDeliveries) AppendDelivery(_ context.Context, d sheet.Delivery) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.got = append(f.got, d)
	return nil
}

// hookEnv is an env with the webhook on and one submitted registration,
// R-TEST1 for anna@example.org, whose registrant mail went out at ts 1000.
func hookEnv(t *testing.T) (*env, *fakeDeliveries) {
	t.Helper()
	e := newEnv(t)
	f := &fakeDeliveries{}
	e.rebuildWith(func(d *Deps) { d.Cfg.WebhookToken = hookToken; d.Deliveries = f })
	e.post("/submit", form("de"))
	return e, f
}

func (e *env) event(tok, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/brevo/events?t="+url.QueryEscape(tok), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	e.server.Wait()
	return rec
}

// brevo builds an event as Brevo's documentation shows it
// (developers.brevo.com/docs/transactional-webhooks), tagged as the
// service tags its mails.
func brevo(event, email, tag string, ts int) string {
	return fmt.Sprintf(`{"event":%q,"email":%q,"id":123,"date":"2026-10-30 18:55:00","ts":%d,`+
		`"message-id":"<202610301755.76784136735@smtp-relay.mailin.fr>","ts_event":%d,"subject":"Bitte bestätigen",`+
		`"sending_ip":"77.32.148.25","tags":[%q],"reason":"554 5.7.1 Service unavailable; Client host [77.32.148.25] blocked by RBL"}`,
		event, email, ts, ts, tag)
}

// The case of 30 Oct 2026: the row is marked and the back office gets a
// line in Zustellung.
func TestABounceMarksTheRegistrationAndTellsTheSheet(t *testing.T) {
	e, f := hookEnv(t)
	if rec := e.event(hookToken, brevo("soft_bounce", "Anna@Example.org", "R-TEST1", 2000)); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	r, _, _ := e.store.Registration(context.Background(), "R-TEST1")
	if r.BouncedAt.Unix() != 2000 || !strings.HasPrefix(r.BounceReason, "soft_bounce: 554 5.7.1") {
		t.Errorf("stored: %v %q", r.BouncedAt, r.BounceReason)
	}
	if len(f.got) != 1 {
		t.Fatalf("%d rows, want 1", len(f.got))
	}
	if d := f.got[0]; d.Kind != sheet.KindRegistrant || d.ID != "R-TEST1" || d.Code != "26-12 MSA" || d.Last != "Müller" ||
		d.Event != "soft_bounce" || !strings.Contains(d.Reason, "blocked by RBL") {
		t.Errorf("row %+v", d)
	}
}

// Brevo's events can arrive out of order: only a delivery after the
// bounce settles it.
func TestOnlyALaterDeliveryClearsABounce(t *testing.T) {
	e, f := hookEnv(t)
	e.event(hookToken, brevo("soft_bounce", "anna@example.org", "R-TEST1", 2000))
	e.event(hookToken, brevo("delivered", "anna@example.org", "R-TEST1", 1500))
	if r, _, _ := e.store.Registration(context.Background(), "R-TEST1"); r.BouncedAt.IsZero() {
		t.Error("an earlier delivery cleared the bounce")
	}
	e.event(hookToken, brevo("delivered", "anna@example.org", "R-TEST1", 2500))
	if r, _, _ := e.store.Registration(context.Background(), "R-TEST1"); !r.BouncedAt.IsZero() {
		t.Error("a later delivery left the bounce")
	}
	if len(f.got) != 1 {
		t.Errorf("%d rows; a delivery adds none", len(f.got))
	}
}

// The person corrected the address; the bounce of the old one is old news.
func TestABounceForACorrectedAddressIsIgnored(t *testing.T) {
	e, f := hookEnv(t)
	if err := e.store.Corrected(context.Background(), "R-TEST1", "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	e.event(hookToken, brevo("hard_bounce", "anna@example.org", "R-TEST1", 2000))
	if r, _, _ := e.store.Registration(context.Background(), "R-TEST1"); !r.BouncedAt.IsZero() || len(f.got) != 0 {
		t.Errorf("stored %v, rows %+v", r.BouncedAt, f.got)
	}
}

// The back office sits behind the same kind of filter. Its failures go to
// the sheet only; the registration row is not touched.
func TestABackOfficeBounceGoesToTheSheetOnly(t *testing.T) {
	e, f := hookEnv(t)
	e.event(hookToken, brevo("soft_bounce", "Office@Example.org", "R-TEST1", 2000))
	e.event(hookToken, brevo("blocked", "office@example.org", "summary-2026-W45", 3000))
	e.event(hookToken, brevo("delivered", "office@example.org", "summary-2026-W46", 4000))
	if r, _, _ := e.store.Registration(context.Background(), "R-TEST1"); !r.BouncedAt.IsZero() {
		t.Error("a back-office bounce marked the registration")
	}
	if len(f.got) != 2 {
		t.Fatalf("%d rows, want 2: %+v", len(f.got), f.got)
	}
	if d := f.got[0]; d.Kind != sheet.KindBackoffice || d.ID != "R-TEST1" || d.Code != "26-12 MSA" {
		t.Errorf("BESTÄTIGT bounce: %+v", d)
	}
	if d := f.got[1]; d.Kind != sheet.KindBackoffice || d.ID != "" || d.Event != "blocked" {
		t.Errorf("summary bounce: %+v", d)
	}
}

func TestEventsWithoutAKnownRegistrationAreIgnored(t *testing.T) {
	e, f := hookEnv(t)
	for _, body := range []string{
		brevo("hard_bounce", "x@example.org", "R-GONE1", 2000),               // reported and deleted
		brevo("hard_bounce", "x@example.org", "newsletter", 2000),            // another sender on the account
		brevo("opened", "anna@example.org", "R-TEST1", 2000),                 // not subscribed
		`{"event":"deferred","email":"anna@example.org","tags":["R-TEST1"]}`, // still trying
	} {
		if rec := e.event(hookToken, body); rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200 so Brevo does not retry", body, rec.Code)
		}
	}
	if r, _, _ := e.store.Registration(context.Background(), "R-TEST1"); !r.BouncedAt.IsZero() || len(f.got) != 0 {
		t.Errorf("stored %v, rows %+v", r.BouncedAt, f.got)
	}
}

// Brevo's "tag" field once held a JSON array as a string, and a batched
// webhook sends an array of events. Both are read.
func TestOtherShapesOfTheBody(t *testing.T) {
	e, _ := hookEnv(t)
	body := `[{"event":"delivered","email":"x@example.org","tags":["R-NONE"]},` +
		`{"event":"hard_bounce","email":"anna@example.org","tag":"[\"R-TEST1\"]","ts_event":2000,"reason":"user unknown"}]`
	if rec := e.event(hookToken, body); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if r, _, _ := e.store.Registration(context.Background(), "R-TEST1"); r.BounceReason != "hard_bounce: user unknown" {
		t.Errorf("stored %q", r.BounceReason)
	}
	if rec := e.event(hookToken, `{"event":`); rec.Code != http.StatusBadRequest {
		t.Errorf("broken JSON: status %d", rec.Code)
	}
}

func TestTheWebhookNeedsItsToken(t *testing.T) {
	e, f := hookEnv(t)
	for _, tok := range []string{"", "wrong", hookToken + "x"} {
		if rec := e.event(tok, brevo("hard_bounce", "anna@example.org", "R-TEST1", 2000)); rec.Code != http.StatusUnauthorized {
			t.Errorf("token %q: status %d", tok, rec.Code)
		}
	}
	if r, _, _ := e.store.Registration(context.Background(), "R-TEST1"); !r.BouncedAt.IsZero() || len(f.got) != 0 {
		t.Error("an unauthorized event changed something")
	}
	// Without a configured token the route does not exist at all.
	off := newEnv(t)
	if rec := off.event("", "{}"); rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("webhook off: status %d", rec.Code)
	}
}

// The store is the record: if it cannot take the news, Brevo is asked to
// try again. The sheet is not: Google down still answers 200.
func TestStoreFailureAsksBrevoToRetrySheetFailureDoesNot(t *testing.T) {
	e := newEnv(t)
	b := &brokenStore{Store: store.NewMemory()}
	f := &fakeDeliveries{err: errors.New("503 from Google")}
	e.store = b
	e.rebuildWith(func(d *Deps) { d.Cfg.WebhookToken = hookToken; d.Deliveries = f })
	e.post("/submit", form("de"))

	if rec := e.event(hookToken, brevo("hard_bounce", "anna@example.org", "R-TEST1", 2000)); rec.Code != http.StatusOK {
		t.Errorf("sheet down: status %d, want 200", rec.Code)
	}
	if r, _, _ := b.Store.Registration(context.Background(), "R-TEST1"); r.BouncedAt.IsZero() {
		t.Error("sheet down: the store missed the bounce")
	}
	if !strings.Contains(e.logs.String(), "Zustellung row") {
		t.Errorf("log:\n%s", e.logs)
	}
	b.get = errors.New("turso down")
	if rec := e.event(hookToken, brevo("hard_bounce", "anna@example.org", "R-TEST1", 3000)); rec.Code != http.StatusInternalServerError {
		t.Errorf("store down: status %d, want 500", rec.Code)
	}
}

// Brevo's form refuses a URL with ?t= and sends its "Token" field as a
// header instead. A header, when there is one, is the token.
func TestTheTokenCanComeAsAHeader(t *testing.T) {
	e, _ := hookEnv(t)
	for h, want := range map[string]int{
		"Bearer " + hookToken: http.StatusOK,
		"bearer " + hookToken: http.StatusOK,
		hookToken:             http.StatusOK,
		"Bearer wrong":        http.StatusUnauthorized,
	} {
		req := httptest.NewRequest(http.MethodPost, "/brevo/events", strings.NewReader(brevo("delivered", "anna@example.org", "R-TEST1", 2000)))
		req.Header.Set("Authorization", h)
		rec := httptest.NewRecorder()
		e.srv.ServeHTTP(rec, req)
		e.server.Wait()
		if rec.Code != want {
			t.Errorf("Authorization %q: status %d, want %d", h, rec.Code, want)
		}
	}
}
