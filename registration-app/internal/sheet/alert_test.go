package sheet

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"arc42-registration/internal/send"
)

type fakeSender struct {
	got  []send.Message
	fail error
}

func (f *fakeSender) Send(_ context.Context, m send.Message) error {
	if f.fail != nil {
		return f.fail
	}
	f.got = append(f.got, m)
	return nil
}

func alerting(t *testing.T) (*Alerting, *fakeGoogle, *fakeSender, *time.Time) {
	s, f := setup(t)
	now := t0
	s.Now = func() time.Time { return now }
	snd := &fakeSender{}
	return &Alerting{Sheet: s, Sender: snd, To: []string{"office@example.org"}, Log: log.New(&bytes.Buffer{}, "", 0), Every: 24 * time.Hour}, f, snd, &now
}

// The first refusal mails at once; the next ones within a day only log,
// so a dead key does not mail once per confirmation.
func TestARefusalIsMailedAtOnceAndThenDaily(t *testing.T) {
	a, f, snd, now := alerting(t)
	f.tokenStatus = http.StatusBadRequest
	ctx := context.Background()
	if err := a.Append(ctx, record()); err == nil {
		t.Fatal("a refusal was swallowed: the summary must still retry")
	}
	if len(snd.got) != 1 {
		t.Fatalf("%d mails after the first refusal, want 1", len(snd.got))
	}
	m := snd.got[0]
	if m.To[0] != "office@example.org" || !strings.Contains(m.Subject, "Schlüssel ungültig") ||
		!strings.Contains(m.Text, "R-7F3KQ") || !strings.Contains(m.Text, "GOOGLE_SA_KEY") {
		t.Errorf("alert %q:\n%s", m.Subject, m.Text)
	}
	*now = now.Add(23 * time.Hour)
	_ = a.Append(ctx, record())
	if len(snd.got) != 1 {
		t.Errorf("%d mails within a day, want 1", len(snd.got))
	}
	*now = now.Add(2 * time.Hour)
	_ = a.Append(ctx, record())
	if len(snd.got) != 2 {
		t.Errorf("%d mails after a day, want 2", len(snd.got))
	}
}

// A refusal on the delivery tab names that tab: the likely cause is that
// nobody has created it yet.
func TestADeliveryRefusalNamesItsTab(t *testing.T) {
	a, f, snd, _ := alerting(t)
	f.status = http.StatusBadRequest
	if err := a.AppendDelivery(context.Background(), Delivery{At: t0, Kind: KindBackoffice, Email: "office@example.org", Event: "blocked"}); err == nil {
		t.Fatal("a refusal was swallowed")
	}
	if len(snd.got) != 1 || !strings.Contains(snd.got[0].Text, `"Zustellung"`) || strings.Contains(snd.got[0].Text, "SHEET_TAB") {
		t.Errorf("alert: %+v", snd.got)
	}
}

func TestWeatherIsNotMailed(t *testing.T) {
	a, f, snd, _ := alerting(t)
	f.status = http.StatusServiceUnavailable
	_ = a.Append(context.Background(), record())
	if len(snd.got) != 0 {
		t.Errorf("a 503 was mailed: %+v", snd.got)
	}
}

// An alert that could not be sent does not start the pause.
func TestAFailedAlertIsTriedAgain(t *testing.T) {
	a, f, snd, _ := alerting(t)
	f.status = http.StatusForbidden
	snd.fail = errors.New("brevo down")
	_ = a.Append(context.Background(), record())
	snd.fail = nil
	_ = a.Append(context.Background(), record())
	if len(snd.got) != 1 || !strings.Contains(snd.got[0].Subject, "Zugriff verweigert") {
		t.Errorf("mails %+v", snd.got)
	}
}

func TestAnUnreadableKeyIsMailed(t *testing.T) {
	snd := &fakeSender{}
	key := []byte(`{"type":"service_account","client_email":"x@proj-1.iam.gserviceaccount.com","private_key":"cut off`)
	_, cause := New(key, "id", "Tabelle1")
	MailUnreadableKey(context.Background(), snd, []string{"office@example.org"}, key, "id", cause, log.New(&bytes.Buffer{}, "", 0))
	if len(snd.got) != 1 || !strings.Contains(snd.got[0].Subject, "Schlüssel unlesbar") {
		t.Fatalf("mails %+v", snd.got)
	}
	// Not JSON at all, so no account; the remedy still reads.
	if text := snd.got[0].Text; !strings.Contains(text, "Das Konto des Anmeldedienstes") || strings.Contains(text, "?project=") {
		t.Errorf("alert:\n%s", text)
	}
}

func TestContainTurnsAPanicIntoAnError(t *testing.T) {
	err := func() (err error) {
		defer Contain(&err)
		var s *Sheet
		return s.Append(context.Background(), record()) // nil receiver: panics
	}()
	if err == nil || !strings.Contains(err.Error(), "panic contained") {
		t.Errorf("err = %v", err)
	}
}
