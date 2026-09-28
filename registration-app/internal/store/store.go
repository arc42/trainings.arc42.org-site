// Package store is the service's only state: which registrations are
// confirmed, and how many wrong codes were entered for each. Nothing
// personal, only registration ids (R-XXXXX) and times, kept for as long as a
// confirm link lives (5 days) and then pruned.
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
	"sync"
	"time"
)

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
	// Prune drops everything recorded before cutoff.
	Prune(ctx context.Context, cutoff time.Time) error
}

// Memory keeps the state in the process. It forgets on every restart and
// on scale-to-zero, so it is for local runs, tests and a test deployment
// without Turso; config refuses it in production.
type Memory struct {
	mu        sync.Mutex
	confirmed map[string]time.Time
	failures  map[string]memFailure
}

type memFailure struct {
	n  int
	at time.Time
}

func NewMemory() *Memory {
	return &Memory{confirmed: map[string]time.Time{}, failures: map[string]memFailure{}}
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
