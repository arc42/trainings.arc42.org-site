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
	ConfirmedVia  string // "code" or "link"
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
}

type memFailure struct {
	n  int
	at time.Time
}

func NewMemory() *Memory {
	return &Memory{confirmed: map[string]time.Time{}, failures: map[string]memFailure{}, regs: map[string]Record{}}
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
