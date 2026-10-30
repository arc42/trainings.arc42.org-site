// Package store is the service's only state: which registrations are
// confirmed, how many wrong codes were entered for each, and, since the
// confirm-gated redesign (spec 2026-10-25), the registrations themselves.
//
// The registrations table is personal data: everything the person typed,
// from submit until the weekly summary has reported it (about two weeks at
// most, spec 4.2). The back office gets its one mail only after the
// registrant confirmed, so until then the data exists only here.
//
// The service was stateless until 28 Sep 2026: everything about a
// registration travels inside its sealed links. Two wishes needed memory
// that survives restarts and scale-to-zero: a confirm link that works once
// (the back office got one BESTÄTIGT mail per click), and a 6-digit code on
// the "please confirm" page whose wrong tries must be counted. See the
// spec's "State" section.
package store

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"arc42-registration/internal/intake"
)

// Status of a stored registration.
const (
	StatusPending   = "pending"   // accepted, waiting for the registrant to confirm
	StatusConfirmed = "confirmed" // confirmed; the back office has its mail
	StatusDropped   = "dropped"   // a bot signal; kept for one summary, never mailed
)

// Record is one submission as stored: the registration as typed plus what
// happened to it.
type Record struct {
	Reg           intake.Registration // ID, Lang and every field as typed
	Hints         []string            // hint keys (intake.Hint*)
	Status        string
	DropReason    string
	MailFailed    bool   // the registrant mail at submit failed
	CorrectedFrom string // the address before the first correction
	CreatedAt     time.Time
	ConfirmedAt   time.Time
	ConfirmedVia  string    // "code" or "link"; empty when reconciled (spec 4.3)
	ReportedAt    time.Time // zero until a summary listed it
	// Price is what the date cost on the day of the submit (feed.PriceOn),
	// 0 when unknown ("Sonstige", feed down). Kept from the submit, so a
	// sheet row retried a week later shows what the registrant saw.
	Price   int
	SheetAt time.Time // zero until its rows are in the back-office sheet
}

type Store interface {
	// Confirm records id as confirmed. first is true only for the call that
	// recorded it; every later call for the same id returns false.
	Confirm(ctx context.Context, id string, at time.Time) (first bool, err error)
	// Unconfirm undoes Confirm, for a confirmation whose mail failed, so the
	// registrant's retry is a first confirmation again.
	Unconfirm(ctx context.Context, id string) error
	Confirmed(ctx context.Context, id string) (bool, error)
	// Failure counts one wrong code for id and returns the new total.
	Failure(ctx context.Context, id string, at time.Time) (int, error)
	Failures(ctx context.Context, id string) (int, error)
	// Prune drops confirmations and wrong-code counts recorded before
	// cutoff. Registrations are not touched: the summary run deletes them.
	Prune(ctx context.Context, cutoff time.Time) error

	// Save stores a new registration. An id that exists is an error.
	Save(ctx context.Context, r Record) error
	// Registration returns the stored registration; ok is false if there is
	// none (stored before the redesign, or the store was down at submit).
	Registration(ctx context.Context, id string) (r Record, ok bool, err error)
	// MailFailed marks that the registrant mail at submit failed.
	MailFailed(ctx context.Context, id string) error
	// Corrected replaces the address after a correction on the page. The
	// address before the first correction is kept in CorrectedFrom.
	Corrected(ctx context.Context, id, email string) error
	// MarkConfirmed moves a pending registration to confirmed.
	MarkConfirmed(ctx context.Context, id string, at time.Time, via string) error
	// SheetDone records that the registration's rows are in the sheet.
	SheetDone(ctx context.Context, id string, at time.Time) error

	// The weekly summary (package summary, spec 7).

	// All returns every stored registration, oldest first.
	All(ctx context.Context) ([]Record, error)
	// Reconcile marks pending registrations confirmed whose id is in the
	// confirmed table: the mail went out, the row update did not (spec
	// 4.3). It returns how many it marked.
	Reconcile(ctx context.Context) (int, error)
	// Reported records that a summary listed these registrations.
	Reported(ctx context.Context, ids []string, at time.Time) error
	// Delete removes registrations for good.
	Delete(ctx context.Context, ids []string) error
	// LastSummary returns the end of the period the last sent summary
	// covered; ok is false before the first one.
	LastSummary(ctx context.Context) (to time.Time, ok bool, err error)
	// Claim takes the right to send the summary for week. It succeeds once
	// per week, and again only for a claim made before stale that was never
	// sent (the process died mid-send). Two machines during a deploy cannot
	// both send.
	Claim(ctx context.Context, week string, at, stale time.Time) (bool, error)
	// Release gives up an unsent claim, so the next due-check tries again.
	Release(ctx context.Context, week string) error
	// SummarySent closes the claim: the summary covering from..to, with
	// rows registrations, went out at at.
	SummarySent(ctx context.Context, week string, at, from, to time.Time, rows int) error
}

// Memory keeps the state in the process. It forgets on every restart and
// on scale-to-zero, so it is for local runs, tests and a test deployment
// without Turso; config refuses it in production.
var errExists = errors.New("store: registration id exists")

type Memory struct {
	mu        sync.Mutex
	confirmed map[string]time.Time
	failures  map[string]memFailure
	regs      map[string]Record
	summaries map[string]memSummary
}

type memFailure struct {
	n  int
	at time.Time
}

type memSummary struct{ claimed, sent, from, to time.Time }

func NewMemory() *Memory {
	return &Memory{confirmed: map[string]time.Time{}, failures: map[string]memFailure{},
		regs: map[string]Record{}, summaries: map[string]memSummary{}}
}

func (m *Memory) Confirm(_ context.Context, id string, at time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.confirmed[id]; ok {
		return false, nil
	}
	m.confirmed[id] = at
	return true, nil
}

func (m *Memory) Unconfirm(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.confirmed, id)
	return nil
}

func (m *Memory) Confirmed(_ context.Context, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.confirmed[id]
	return ok, nil
}

func (m *Memory) Failure(_ context.Context, id string, at time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.failures[id]
	f.n++
	f.at = at
	m.failures[id] = f
	return f.n, nil
}

func (m *Memory) Failures(_ context.Context, id string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.failures[id].n, nil
}

func (m *Memory) Prune(_ context.Context, cutoff time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, at := range m.confirmed {
		if at.Before(cutoff) {
			delete(m.confirmed, id)
		}
	}
	for id, f := range m.failures {
		if f.at.Before(cutoff) {
			delete(m.failures, id)
		}
	}
	return nil
}

func (m *Memory) Save(_ context.Context, r Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.regs[r.Reg.ID]; ok {
		return errExists
	}
	r.Reg.Emails = append([]string(nil), r.Reg.Emails...)
	r.Reg.Participants = append([]intake.Participant(nil), r.Reg.Participants...)
	r.Hints = append([]string(nil), r.Hints...)
	m.regs[r.Reg.ID] = r
	return nil
}

func (m *Memory) Registration(_ context.Context, id string) (Record, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.regs[id]
	return r, ok, nil
}

func (m *Memory) MailFailed(_ context.Context, id string) error {
	return m.change(id, func(r *Record) { r.MailFailed = true })
}

func (m *Memory) Corrected(_ context.Context, id, email string) error {
	return m.change(id, func(r *Record) {
		if r.CorrectedFrom == "" {
			r.CorrectedFrom = r.Reg.Email
		}
		r.Reg.Email, r.Reg.Emails = email, []string{email}
	})
}

func (m *Memory) MarkConfirmed(_ context.Context, id string, at time.Time, via string) error {
	return m.change(id, func(r *Record) {
		if r.Status == StatusPending {
			r.Status, r.ConfirmedAt, r.ConfirmedVia = StatusConfirmed, at, via
		}
	})
}

func (m *Memory) SheetDone(_ context.Context, id string, at time.Time) error {
	return m.change(id, func(r *Record) { r.SheetAt = at })
}

// change applies f to a stored registration. A missing id is no error, as
// an UPDATE that matches no row is none in Turso.
func (m *Memory) change(id string, f func(*Record)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.regs[id]; ok {
		f(&r)
		m.regs[id] = r
	}
	return nil
}

func (m *Memory) All(_ context.Context) ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Record, 0, len(m.regs))
	for _, r := range m.regs {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].Reg.ID < out[j].Reg.ID
	})
	return out, nil
}

func (m *Memory) Reconcile(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id, r := range m.regs {
		if at, ok := m.confirmed[id]; ok && r.Status == StatusPending {
			r.Status, r.ConfirmedAt = StatusConfirmed, at
			m.regs[id] = r
			n++
		}
	}
	return n, nil
}

func (m *Memory) Reported(_ context.Context, ids []string, at time.Time) error {
	for _, id := range ids {
		_ = m.change(id, func(r *Record) { r.ReportedAt = at })
	}
	return nil
}

func (m *Memory) Delete(_ context.Context, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		delete(m.regs, id)
	}
	return nil
}

func (m *Memory) LastSummary(_ context.Context) (time.Time, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var to time.Time
	ok := false
	for _, s := range m.summaries {
		if !s.sent.IsZero() && (!ok || s.to.After(to)) {
			to, ok = s.to, true
		}
	}
	return to, ok, nil
}

func (m *Memory) Claim(_ context.Context, week string, at, stale time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.summaries[week]; ok && (!s.sent.IsZero() || !s.claimed.Before(stale)) {
		return false, nil
	}
	m.summaries[week] = memSummary{claimed: at}
	return true, nil
}

func (m *Memory) Release(_ context.Context, week string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.summaries[week]; ok && s.sent.IsZero() {
		delete(m.summaries, week)
	}
	return nil
}

func (m *Memory) SummarySent(_ context.Context, week string, at, from, to time.Time, _ int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.summaries[week]
	s.sent, s.from, s.to = at, from, to
	m.summaries[week] = s
	return nil
}
