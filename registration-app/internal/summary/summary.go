// Package summary sends the weekly overview of every registration attempt
// to the back office and then forgets what it reported (spec 2026-10-25,
// sections 4.2 and 7).
//
// There is no scheduler. Every request asks Kick, and the run goes out on
// the first request after Monday 07:00 Berlin time. The machine sleeps when
// idle, so a cron-job.org job GETs /healthz on Monday at 07:00 to make sure
// there is such a request; if it does not come, the next real request sends
// the summary late, marked as late. Whether a summary is due is decided from
// the store, never from process memory, so a restart cannot send twice; the
// week claim keeps two machines (a deploy in progress) from both sending.
package summary

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"arc42-registration/internal/mail"
	"arc42-registration/internal/send"
	"arc42-registration/internal/store"
	"arc42-registration/internal/token"
)

const (
	// staleClaim: a claim this old that was never sent belongs to a process
	// that died mid-send, and may be taken over.
	staleClaim = 15 * time.Minute
	// lateAfter: a summary sent this long after it was due says so, which
	// tells the back office the Monday ping did not come.
	lateAfter = time.Hour
	// checkEvery throttles Kick: fly's health checks hit /healthz every 30
	// seconds while the machine is awake, and a store that is down should
	// not be asked twice a minute.
	checkEvery = time.Minute
	// runTimeout bounds one run. It is not tied to the request that kicked
	// it, which has long been answered.
	runTimeout = time.Minute
)

type Runner struct {
	Store  store.Store
	Sender send.Sender
	To     []string // BACKOFFICE_TO
	Log    *log.Logger
	Now    func() time.Time
	// Sheet is the back-office sheet; the run appends what the confirm
	// step could not. nil: no sheet, and confirmed rows go after one report.
	Sheet interface {
		Append(ctx context.Context, rec store.Record) error
	}

	mu      sync.Mutex
	running bool
	next    time.Time // no check before this
	done    time.Time // the Monday whose summary is known to be out
	wg      sync.WaitGroup
}

// Kick starts the due-check in the background, unless one is running or
// one ran less than a minute ago. It costs a mutex, so every request can
// call it.
func (r *Runner) Kick() {
	now := r.Now()
	r.mu.Lock()
	if r.running || now.Before(r.next) || r.done.Equal(due(now)) {
		r.mu.Unlock()
		return
	}
	r.running, r.next = true, now.Add(checkEvery)
	r.wg.Add(1)
	r.mu.Unlock()
	go func() {
		defer r.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
		defer cancel()
		if err := r.Run(ctx); err != nil {
			r.Log.Printf("summary: %v", err)
		}
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()
}

// Wait blocks until a kicked run has finished. For tests and shutdown.
func (r *Runner) Wait() { r.wg.Wait() }

// Run sends the summary if one is due. Nothing happens when it is not.
func (r *Runner) Run(ctx context.Context) error {
	now := r.Now()
	monday := due(now)
	last, ok, err := r.Store.LastSummary(ctx)
	if err != nil {
		return fmt.Errorf("due-check: %w", err)
	}
	if ok && !last.Before(monday) {
		r.remember(monday)
		return nil
	}
	year, week := monday.ISOWeek()
	// The first summary ever (right after the deploy) is not late: there
	// was no Monday it could have been sent on.
	err = r.send(ctx, fmt.Sprintf("%d-W%02d", year, week), mail.Week{
		Number: week, From: from(last, ok), To: now, Due: monday, Late: ok && now.Sub(monday) > lateAfter,
	})
	if err == nil {
		r.remember(monday)
	}
	return err
}

// Force sends a summary now, whatever the week (SUMMARY_NOW, test
// deployments). It covers the time since the last summary as a regular run
// would, and the next regular one is due the Monday after.
func (r *Runner) Force(ctx context.Context) error {
	now := r.Now()
	last, ok, err := r.Store.LastSummary(ctx)
	if err != nil {
		return err
	}
	_, week := now.In(berlin).ISOWeek()
	return r.send(ctx, "now-"+now.UTC().Format("20060102T150405"), mail.Week{Number: week, From: from(last, ok), To: now, Due: now})
}

// send is one summary run once it is due (spec 7.2): claim the week,
// repair, send, and only after the mail went out record and forget.
func (r *Runner) send(ctx context.Context, key string, w mail.Week) error {
	claimed, err := r.Store.Claim(ctx, key, w.To, w.To.Add(-staleClaim))
	if err != nil {
		return fmt.Errorf("claim %s: %w", key, err)
	}
	if !claimed {
		r.Log.Printf("summary %s: claimed by another machine", key)
		return nil
	}
	// A failure before the mail is out gives the claim back and changes
	// nothing else, so the next due-check tries again in a minute.
	release := func(err error) error {
		if rerr := r.Store.Release(ctx, key); rerr != nil {
			r.Log.Printf("summary %s: could not release the claim, retry after %v: %v", key, staleClaim, rerr)
		}
		return fmt.Errorf("summary %s: %w", key, err)
	}
	reconciled, err := r.Store.Reconcile(ctx)
	if err != nil {
		return release(err)
	}
	if w.Rows, err = r.Store.All(ctx); err != nil {
		return release(err)
	}
	synced := r.toSheet(ctx, key, w.Rows)
	w.NoSheet = r.owed(w.Rows, w.To)
	m, err := mail.Summary(w)
	if err == nil {
		err = r.Sender.Send(ctx, send.Message{To: r.To, Subject: m.Subject, Text: m.Text, CustomID: "summary-" + key})
	}
	if err != nil {
		return release(err)
	}

	// The mail is out. A failure from here on repeats some rows next week:
	// a duplicate line is the cheaper failure (spec 7.2, step 5).
	if err := r.Store.SummarySent(ctx, key, r.Now(), w.From, w.To, len(w.Rows)); err != nil {
		r.Log.Printf("summary %s: sent, but not recorded: %v", key, err)
	}
	reported, gone := r.retain(w.Rows, w.To)
	if err := r.Store.Reported(ctx, reported, w.To); err != nil {
		r.Log.Printf("summary %s: marking reported: %v", key, err)
	}
	if err := r.Store.Delete(ctx, gone); err != nil {
		r.Log.Printf("summary %s: deleting: %v", key, err)
		gone = nil
	}
	// The exactly-once guard and the wrong-code counts are needed while a
	// link lives; a day on top covers a confirmation in its last minute.
	if err := r.Store.Prune(ctx, w.To.Add(-token.Valid-24*time.Hour)); err != nil {
		r.Log.Printf("summary %s: prune: %v", key, err)
	}
	r.Log.Printf("summary %s: sent, %d rows reported, %d kept, %d deleted, %d reconciled, %d appended to the sheet",
		key, len(w.Rows), len(reported), len(gone), reconciled, synced)
	return nil
}

// sheetTimeout bounds one append; runTimeout bounds the whole run.
const sheetTimeout = 10 * time.Second

// toSheet appends the confirmed registrations whose rows are not in the
// sheet yet: Google was down at confirm time, or the store was, and
// reconciliation has just marked the row. It stops at the first failure,
// since Google being down makes every further try another timeout; what is
// left is retried next week. rows is updated in place.
func (r *Runner) toSheet(ctx context.Context, key string, rows []store.Record) int {
	if r.Sheet == nil {
		return 0
	}
	n := 0
	for i := range rows {
		rec := &rows[i]
		if rec.Status != store.StatusConfirmed || !rec.SheetAt.IsZero() {
			continue
		}
		actx, cancel := context.WithTimeout(ctx, sheetTimeout)
		err := r.appendContained(actx, *rec)
		cancel()
		if err != nil {
			r.Log.Printf("summary %s: sheet append %s failed, stopping here: %v", key, rec.Reg.ID, err)
			return n
		}
		n++
		rec.SheetAt = r.Now()
		if err := r.Store.SheetDone(ctx, rec.Reg.ID, rec.SheetAt); err != nil {
			r.Log.Printf("summary %s: %s in the sheet, but not recorded: %v", key, rec.Reg.ID, err)
		}
	}
	return n
}

// appendContained keeps a panic in the sheet from ending the run, and with
// it the process: the summary mail matters more than the sheet.
func (r *Runner) appendContained(ctx context.Context, rec store.Record) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("sheet panicked: %v", p)
		}
	}()
	return r.Sheet.Append(ctx, rec)
}

// owed lists the confirmed registrations still missing from the sheet
// after toSheet: "retry" for one kept for another week, "manual" for one
// this run deletes, whose rows the back office must type in from its mail.
func (r *Runner) owed(rows []store.Record, now time.Time) map[string]string {
	if r.Sheet == nil {
		return nil
	}
	out := map[string]string{}
	for _, rec := range rows {
		switch {
		case rec.Status != store.StatusConfirmed || !rec.SheetAt.IsZero():
		case r.keepForSheet(rec):
			out[rec.Reg.ID] = "retry"
		default:
			out[rec.Reg.ID] = "manual"
		}
	}
	return out
}

// keepForSheet: a confirmed registration missing from the sheet stays one
// more week, so the next run retries it. Once it has been reported as
// confirmed, it goes: the back office has the mail, and Turso keeps
// personal data no longer than about two weeks (spec 4.2). A row reported
// last week while still pending is reported as confirmed for the first
// time now.
func (r *Runner) keepForSheet(rec store.Record) bool {
	return r.Sheet != nil && rec.Status == store.StatusConfirmed && rec.SheetAt.IsZero() &&
		(rec.ReportedAt.IsZero() || rec.ConfirmedAt.After(rec.ReportedAt))
}

// retain applies the retention table of spec 4.2: what was reported and is
// final is deleted; a pending registration whose link still works is kept
// and reported again next week with its final status, and so is a
// confirmed one whose rows are not in the sheet yet (keepForSheet).
func (r *Runner) retain(rows []store.Record, now time.Time) (kept, gone []string) {
	for _, rec := range rows {
		if rec.Status == store.StatusPending && rec.CreatedAt.Add(token.Valid).After(now) || r.keepForSheet(rec) {
			kept = append(kept, rec.Reg.ID)
		} else {
			gone = append(gone, rec.Reg.ID)
		}
	}
	return kept, gone
}

func (r *Runner) remember(monday time.Time) {
	r.mu.Lock()
	r.done = monday
	r.mu.Unlock()
}

// berlin is the back office's clock; the tz database is compiled in by
// package feed (the image is FROM scratch).
var berlin = func() *time.Location {
	if loc, err := time.LoadLocation("Europe/Berlin"); err == nil {
		return loc
	}
	return time.UTC
}()

// due is the most recent Monday 07:00 Berlin time at or before now.
func due(now time.Time) time.Time {
	b := now.In(berlin)
	days := (int(b.Weekday()) + 6) % 7 // Monday 0
	m := time.Date(b.Year(), b.Month(), b.Day()-days, 7, 0, 0, 0, berlin)
	if m.After(now) {
		m = time.Date(b.Year(), b.Month(), b.Day()-days-7, 7, 0, 0, 0, berlin)
	}
	return m
}

func from(last time.Time, ok bool) time.Time {
	if !ok {
		return time.Time{}
	}
	return last
}
