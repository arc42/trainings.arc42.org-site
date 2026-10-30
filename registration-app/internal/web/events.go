package web

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"arc42-registration/internal/sheet"
)

// Brevo's delivery events (spec 2026-10-30). Brevo accepts a mail with 201
// and only learns afterwards whether the receiving server took it. On 30
// Oct 2026 mailbox.org refused a confirmation mail because Brevo's shared
// sending IP was on the SpamCop blocklist; the service had logged
// "accepted" and the person waited for nothing. The webhook brings that
// news back: the registration row is marked, and a row goes to the
// "Zustellung" tab, whose Google notification reaches the back office
// without passing through the pool that just failed.

// Deliveries takes a delivery failure as a row of the sheet's Zustellung
// tab (package sheet). nil: no sheet; the store and the log still have it.
type Deliveries interface {
	AppendDelivery(ctx context.Context, d sheet.Delivery) error
}

// brevoEvent is the part of Brevo's transactional webhook body the service
// reads (developers.brevo.com/docs/transactional-webhooks). Tags arrive as
// "tags", an array, in most events; some carry "tag", once a string that
// holds a JSON array. Both are read leniently: a field Brevo changes must
// not turn every event into a 400 that Brevo then retries.
type brevoEvent struct {
	Event     string          `json:"event"`
	Email     string          `json:"email"`
	Tags      json.RawMessage `json:"tags"`
	Tag       json.RawMessage `json:"tag"`
	TsEvent   int64           `json:"ts_event"`
	Reason    string          `json:"reason"`
	MessageID string          `json:"message-id"`
}

// failed are the events that mean the mail did not arrive. "deferred" is
// not among them: Brevo is still trying, and a later event settles it.
var failed = map[string]bool{
	"hard_bounce": true, "soft_bounce": true, "blocked": true, "invalid_email": true, "error": true,
}

// maxEventBody: one event is well under 1 KB; a batch, if Brevo is ever
// set to send them, a few dozen.
const maxEventBody = 64 << 10

func (s *Server) handleBrevoEvents(w http.ResponseWriter, r *http.Request) {
	// The token is in the URL because Brevo's webhook settings do not
	// document basic auth; a query parameter works in every form of them.
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("t")), []byte(s.d.Cfg.WebhookToken)) != 1 {
		s.d.Log.Printf("brevo events: wrong or missing token")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxEventBody))
	if err != nil {
		http.Error(w, "too large", http.StatusRequestEntityTooLarge)
		return
	}
	events, err := parseEvents(raw)
	if err != nil {
		s.d.Log.Printf("brevo events: unreadable body: %v", err)
		http.Error(w, "unreadable", http.StatusBadRequest)
		return
	}
	for _, ev := range events {
		// A store failure is the one answer worth a retry from Brevo: the
		// row is the record the summary reads.
		if err := s.deliveryEvent(r.Context(), ev); err != nil {
			s.d.Log.Printf("brevo events: %s for %s: %v", ev.Event, ev.MessageID, err)
			http.Error(w, "try again", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

// parseEvents takes one event or an array of them.
func parseEvents(raw []byte) ([]brevoEvent, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '[' {
		var evs []brevoEvent
		err := json.Unmarshal(raw, &evs)
		return evs, err
	}
	var ev brevoEvent
	err := json.Unmarshal(raw, &ev)
	return []brevoEvent{ev}, err
}

// registrationID is the first tag that looks like a registration id. The
// service tags every mail with its CustomID: the id for the registrant
// mail and the BESTÄTIGT mail, "summary-<week>" or "sheet-alert" for the
// back office's others.
func (ev brevoEvent) registrationID() string {
	for _, f := range []json.RawMessage{ev.Tags, ev.Tag} {
		for _, t := range tags(f) {
			if strings.HasPrefix(t, "R-") {
				return t
			}
		}
	}
	return ""
}

// tags reads an array, a string, or a string holding an array.
func tags(f json.RawMessage) []string {
	var list []string
	if json.Unmarshal(f, &list) == nil {
		return list
	}
	var one string
	if json.Unmarshal(f, &one) != nil {
		return nil
	}
	if json.Unmarshal([]byte(one), &list) == nil {
		return list
	}
	return []string{one}
}

func (s *Server) deliveryEvent(ctx context.Context, ev brevoEvent) error {
	if !failed[ev.Event] && ev.Event != "delivered" {
		return nil // not subscribed; harmless if someone ticks more boxes
	}
	at := s.d.Now()
	if ev.TsEvent > 0 {
		at = time.Unix(ev.TsEvent, 0)
	}
	id := ev.registrationID()
	reason := ev.Event
	if ev.Reason != "" {
		reason += ": " + ev.Reason
	}
	d := sheet.Delivery{At: at, ID: id, Email: ev.Email, Event: ev.Event, Reason: ev.Reason}

	if s.backoffice(ev.Email) {
		if !failed[ev.Event] {
			return nil
		}
		// Not stored: there is no row for the summary or a sheet alert,
		// and the sheet is this case's record. The registration, if the
		// mail had one, only fills in the row.
		d.Kind = sheet.KindBackoffice
		if id != "" && s.d.Store != nil {
			if rec, ok, err := s.d.Store.Registration(ctx, id); err == nil && ok {
				d.Code, d.Last, d.First = rec.Reg.Code, rec.Reg.LastName, rec.Reg.FirstName
			}
		}
		s.d.Log.Printf("brevo events: back-office mail %s to %s not delivered: %s", orTag(id, ev), ev.Email, reason)
		s.toDeliveries(ctx, d)
		return nil
	}

	if id == "" {
		s.d.Log.Printf("brevo events: %s for a mail without a registration id, ignored", ev.Event)
		return nil
	}
	if s.d.Store == nil {
		return errors.New("no store")
	}
	rec, ok, err := s.d.Store.Registration(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		// Reported and deleted already, or from before the redesign.
		s.d.Log.Printf("brevo events: %s for %s, which the store no longer holds, ignored", ev.Event, id)
		return nil
	}
	// Only the address the next mail would go to counts. A bounce for an
	// address the person has corrected since is old news.
	current := rec.Reg.Email
	if len(rec.Reg.Emails) > 0 {
		current = rec.Reg.Emails[0]
	}
	if !strings.EqualFold(strings.TrimSpace(current), strings.TrimSpace(ev.Email)) {
		s.d.Log.Printf("brevo events: %s for %s at a former address, ignored", ev.Event, id)
		return nil
	}
	if !failed[ev.Event] {
		return s.d.Store.Delivered(ctx, id, at)
	}
	if err := s.d.Store.Bounced(ctx, id, at, reason); err != nil {
		return err
	}
	s.d.Log.Printf("brevo events: registrant mail %s not delivered: %s", id, reason)
	d.Kind, d.Code, d.Last, d.First = sheet.KindRegistrant, rec.Reg.Code, rec.Reg.LastName, rec.Reg.FirstName
	s.toDeliveries(ctx, d)
	return nil
}

func (s *Server) backoffice(addr string) bool {
	for _, b := range s.d.Cfg.BackofficeTo {
		if strings.EqualFold(b, strings.TrimSpace(addr)) {
			return true
		}
	}
	return false
}

// orTag names a mail in the log by its registration id, else its message id.
func orTag(id string, ev brevoEvent) string {
	if id != "" {
		return id
	}
	return ev.MessageID
}

// toDeliveries appends the row in the background, as toSheet does: Brevo
// gets its answer once the store has the news, and Google being slow or
// down costs a log line. The weekly summary lists a bounced registration
// either way.
func (s *Server) toDeliveries(ctx context.Context, d sheet.Delivery) {
	if s.d.Deliveries == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		defer func() {
			if p := recover(); p != nil {
				s.d.Log.Printf("brevo events: sheet panicked, contained: %v", p)
			}
		}()
		actx, cancel := context.WithTimeout(ctx, sheetTimeout)
		defer cancel()
		if err := s.d.Deliveries.AppendDelivery(actx, d); err != nil {
			s.d.Log.Printf("brevo events: Zustellung row for %s failed: %v", d.Email, err)
		}
	}()
}
