package sheet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"arc42-registration/internal/mail"
	"arc42-registration/internal/send"
	"arc42-registration/internal/store"
)

// Alerting mails the back office the moment Google refuses (Refused): a key
// that is no longer valid does not heal, and the weekly summary would tell
// only on Monday. Transient failures send nothing; the summary run retries
// them. One mail at the first refusal, then at most one per Every, so a
// dead key does not mail once per confirmation. The pause lives in memory:
// a restart may send one more, which is the cheaper failure.
type Alerting struct {
	*Sheet
	Sender send.Sender
	To     []string // BACKOFFICE_TO, as for the summary
	Log    *log.Logger
	Every  time.Duration

	mu   sync.Mutex
	last time.Time
}

// alertTimeout bounds the alert mail. It is not tied to the append's
// context, which the refusal may have used up.
const alertTimeout = 15 * time.Second

func (a *Alerting) Append(ctx context.Context, rec store.Record) (err error) {
	defer Contain(&err)
	err = a.Sheet.Append(ctx, rec)
	var r *Refused
	if errors.As(err, &r) {
		a.alert(ctx, mail.SheetProblem{ID: rec.Reg.ID, Tab: a.Tab}, r)
	}
	return err
}

// AppendDelivery is Append for DeliveryTab. A refusal there is most likely
// the tab missing or renamed, and the alert names that tab.
func (a *Alerting) AppendDelivery(ctx context.Context, d Delivery) (err error) {
	defer Contain(&err)
	err = a.Sheet.AppendDelivery(ctx, d)
	var r *Refused
	if errors.As(err, &r) {
		a.alert(ctx, mail.SheetProblem{ID: d.ID, Tab: DeliveryTab, Delivery: true}, r)
	}
	return err
}

// alert mails p, completed with the refusal and the account, unless one
// went out within Every.
func (a *Alerting) alert(ctx context.Context, p mail.SheetProblem, r *Refused) {
	now := a.Now()
	a.mu.Lock()
	if !a.last.IsZero() && now.Sub(a.last) < a.Every {
		a.mu.Unlock()
		a.Log.Printf("sheet: %v (alert already mailed at %s)", r, a.last.Format(time.RFC3339))
		return
	}
	a.last = now // taken before sending, so two failing appends send one mail
	a.mu.Unlock()

	p.At, p.Stage, p.Status, p.Detail, p.Account, p.SheetID = now, r.Stage, r.Status, r.Detail, a.Email(), a.ID
	m, err := mail.SheetAlert(p)
	if err == nil {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), alertTimeout)
		err = a.Sender.Send(sctx, send.Message{To: a.To, Subject: m.Subject, Text: m.Text, CustomID: "sheet-alert"})
		cancel()
	}
	if err != nil {
		a.mu.Lock()
		a.last = time.Time{} // the next refusal tries again
		a.mu.Unlock()
		a.Log.Printf("sheet: %v; the alert mail failed too: %v", r, err)
		return
	}
	a.Log.Printf("sheet: %v; alert mailed to the back office", r)
}

// MailUnreadableKey reports a GOOGLE_SA_KEY that New could not read: the
// service started without the sheet. Once per start; fly restarts the
// service when the secret is set again.
func MailUnreadableKey(ctx context.Context, sender send.Sender, to []string, key []byte, sheetID string, cause error, logger *log.Logger) {
	var err error
	defer func() {
		if Contain(&err); err != nil {
			logger.Printf("%v", err)
		}
	}()
	// The account's address, if the JSON is readable that far; it makes
	// the remedy's links point at the right account.
	var k struct {
		ClientEmail string `json:"client_email"`
	}
	_ = json.Unmarshal(key, &k)
	m, err := mail.SheetAlert(mail.SheetProblem{At: time.Now(), Stage: StageKey, Detail: cause.Error(),
		Account: k.ClientEmail, SheetID: sheetID})
	if err == nil {
		sctx, cancel := context.WithTimeout(ctx, alertTimeout)
		err = sender.Send(sctx, send.Message{To: to, Subject: m.Subject, Text: m.Text, CustomID: "sheet-alert"})
		cancel()
	}
	if err != nil {
		logger.Printf("sheet: the alert about the unreadable key failed: %v", err)
		return
	}
	logger.Printf("sheet: alert about the unreadable key mailed to the back office")
}

// Contain turns a panic into an error. The sheet is never worth the
// process: a panic in a goroutine ends the whole service, registrations
// included. Every path into the sheet defers it, so the caller sees an
// ordinary failure, logs it and leaves the row to the next retry.
func Contain(err *error) {
	if p := recover(); p != nil {
		*err = fmt.Errorf("sheet: panic contained: %v", p)
	}
}
