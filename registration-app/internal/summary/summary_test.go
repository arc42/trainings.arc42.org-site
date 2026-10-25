package summary

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"arc42-registration/internal/intake"
	"arc42-registration/internal/send"
	"arc42-registration/internal/store"
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

// berlinAt is a wall-clock time in Berlin.
func berlinAt(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, berlin)
}

type rig struct {
	st     store.Store
	sender *fakeSender
	now    time.Time
	logs   *bytes.Buffer
}

func newRig(now time.Time) *rig {
	return &rig{st: store.NewMemory(), sender: &fakeSender{}, now: now, logs: &bytes.Buffer{}}
}

// runner is a fresh process on the rig's store: what a restart looks like.
func (g *rig) runner() *Runner {
	return &Runner{Store: g.st, Sender: g.sender, To: []string{"office@example.org"},
		Log: log.New(g.logs, "", 0), Now: func() time.Time { return g.now }}
}

func (g *rig) run(t *testing.T, r *Runner) {
	t.Helper()
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (g *rig) save(t *testing.T, id, status string, created time.Time) {
	t.Helper()
	err := g.st.Save(context.Background(), store.Record{
		Reg:    intake.Registration{ID: id, Lang: "de", Code: "26-12 MSA", LastName: "Müller", Email: "a@example.org"},
		Status: status, CreatedAt: created})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDueIsTheLastMondaySevenInBerlin(t *testing.T) {
	for _, c := range []struct{ now, want time.Time }{
		{berlinAt(2026, 10, 26, 7, 0), berlinAt(2026, 10, 26, 7, 0)},
		{berlinAt(2026, 10, 26, 6, 59), berlinAt(2026, 10, 19, 7, 0)},
		{berlinAt(2026, 10, 25, 23, 0), berlinAt(2026, 10, 19, 7, 0)}, // Sunday, the night summer time ended
		{berlinAt(2026, 11, 1, 12, 0), berlinAt(2026, 10, 26, 7, 0)},
		{berlinAt(2027, 3, 29, 7, 30), berlinAt(2027, 3, 29, 7, 0)},         // Monday after summer time began
		{berlinAt(2026, 10, 26, 7, 30).UTC(), berlinAt(2026, 10, 26, 7, 0)}, // 06:30 UTC, 07:30 in Berlin
	} {
		if got := due(c.now); !got.Equal(c.want) {
			t.Errorf("due(%v) = %v, want %v", c.now, got, c.want)
		}
	}
}

func TestOneSummaryPerWeekWhoeverAsks(t *testing.T) {
	g := newRig(berlinAt(2026, 10, 26, 7, 0))
	r := g.runner()
	g.run(t, r)
	if len(g.sender.got) != 1 {
		t.Fatalf("first Monday: %d mails, want 1", len(g.sender.got))
	}
	if s := g.sender.got[0].Subject; !strings.Contains(s, "KW 44 (0 bestätigt, 0 offen, 0 verworfen)") {
		t.Errorf("subject %q", s)
	}
	// The first one after a deploy on a Wednesday is due at once, and not late.
	fresh := newRig(berlinAt(2026, 10, 28, 12, 0))
	fresh.run(t, fresh.runner())
	if len(fresh.sender.got) != 1 || strings.Contains(fresh.sender.got[0].Text, "Verspätet") {
		t.Fatalf("first summary on a Wednesday: %d mails", len(fresh.sender.got))
	}
	// The rest of the week: the same process, then a restarted one.
	for _, at := range []time.Time{berlinAt(2026, 10, 26, 7, 1), berlinAt(2026, 10, 28, 12, 0), berlinAt(2026, 11, 2, 6, 59)} {
		g.now = at
		g.run(t, r)
		g.run(t, g.runner())
	}
	if len(g.sender.got) != 1 {
		t.Fatalf("%d mails in one week, want 1", len(g.sender.got))
	}
	g.now = berlinAt(2026, 11, 2, 7, 0)
	g.run(t, g.runner())
	if len(g.sender.got) != 2 || !strings.Contains(g.sender.got[1].Subject, "KW 45") {
		t.Fatalf("next Monday: %d mails, last %q", len(g.sender.got), g.sender.got[len(g.sender.got)-1].Subject)
	}
	if strings.Contains(g.sender.got[1].Text, "Verspätet") || !strings.Contains(g.sender.got[1].Text, "26.10.2026, 07:00 Uhr bis 02.11.2026, 07:00 Uhr") {
		t.Errorf("second summary:\n%s", g.sender.got[1].Text)
	}
}

func TestASummaryWithoutAMondayRequestIsLateNotLost(t *testing.T) {
	g := newRig(berlinAt(2026, 10, 19, 7, 0))
	g.run(t, g.runner())
	g.now = berlinAt(2026, 10, 27, 15, 20) // no ping on Monday; a registration on Tuesday
	g.run(t, g.runner())
	if len(g.sender.got) != 2 || !strings.Contains(g.sender.got[1].Text, "Verspätet: fällig war 26.10.2026, 07:00 Uhr.") {
		t.Fatalf("late summary: %d mails\n%s", len(g.sender.got), g.sender.got[len(g.sender.got)-1].Text)
	}
}

func TestAFailedSendChangesNothing(t *testing.T) {
	g := newRig(berlinAt(2026, 10, 26, 7, 0))
	g.save(t, "R-DROP1", store.StatusDropped, berlinAt(2026, 10, 24, 3, 0))
	g.sender.fail = errors.New("brevo: 503")
	r := g.runner()
	if err := r.Run(context.Background()); err == nil {
		t.Fatal("a failed send was not reported")
	}
	if all, _ := g.st.All(context.Background()); len(all) != 1 || !all[0].ReportedAt.IsZero() {
		t.Fatalf("after a failed send: %+v", all)
	}
	// The claim was given back: a minute later, Brevo is up again.
	g.sender.fail = nil
	g.now = g.now.Add(time.Minute)
	g.run(t, r)
	if len(g.sender.got) != 1 || !strings.Contains(g.sender.got[0].Text, "R-DROP1") {
		t.Fatalf("retry: %d mails", len(g.sender.got))
	}
	if all, _ := g.st.All(context.Background()); len(all) != 0 {
		t.Errorf("reported drop not deleted: %+v", all)
	}
}

func TestRetention(t *testing.T) {
	mon := berlinAt(2026, 10, 26, 7, 0)
	g := newRig(mon)
	ctx := context.Background()
	g.save(t, "R-DROP1", store.StatusDropped, mon.Add(-50*time.Hour))
	g.save(t, "R-CONF1", store.StatusConfirmed, mon.Add(-100*time.Hour))
	g.save(t, "R-OLDP1", store.StatusPending, mon.Add(-6*24*time.Hour)) // link expired: final
	g.save(t, "R-NEWP1", store.StatusPending, mon.Add(-24*time.Hour))   // link valid four more days
	g.save(t, "R-NEWP2", store.StatusPending, mon.Add(-2*time.Hour))    // will be confirmed during the week
	g.run(t, g.runner())
	all, _ := g.st.All(ctx)
	if len(all) != 2 || all[0].Reg.ID != "R-NEWP1" || all[1].Reg.ID != "R-NEWP2" || all[0].ReportedAt.IsZero() {
		t.Fatalf("kept after the first summary: %+v", all)
	}

	// During the week: one confirms (the row update failed, so only the
	// confirmed table knows), the other never does.
	if _, err := g.st.Confirm(ctx, "R-NEWP2", mon.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	g.now = mon.Add(7 * 24 * time.Hour)
	g.run(t, g.runner())
	text := g.sender.got[1].Text
	if !strings.Contains(g.sender.got[1].Subject, "(1 bestätigt, 1 offen, 0 verworfen)") ||
		!strings.Contains(text, "letzte Woche noch offen") || !strings.Contains(text, "abgelaufen, nicht bestätigt") {
		t.Errorf("second summary %q:\n%s", g.sender.got[1].Subject, text)
	}
	if all, _ := g.st.All(ctx); len(all) != 0 {
		t.Errorf("something outlived two summaries: %+v", all)
	}
	// The exactly-once guard was pruned with the rows.
	if ok, _ := g.st.Confirmed(ctx, "R-NEWP2"); ok {
		t.Error("confirmation from last week not pruned")
	}
	if !strings.Contains(g.logs.String(), "1 reconciled") {
		t.Errorf("log: %s", g.logs)
	}
}

func TestTwoMachinesSendOneSummary(t *testing.T) {
	g := newRig(berlinAt(2026, 10, 26, 7, 0))
	ctx := context.Background()
	// The other machine claimed the week a minute ago and is sending.
	if ok, _ := g.st.Claim(ctx, "2026-W44", g.now.Add(-time.Minute), g.now.Add(-time.Hour)); !ok {
		t.Fatal("setup claim failed")
	}
	g.run(t, g.runner())
	if len(g.sender.got) != 0 {
		t.Fatal("sent although the week was claimed")
	}
	// It died mid-send: after 15 minutes the claim is stale and taken over.
	g.now = g.now.Add(16 * time.Minute)
	g.run(t, g.runner())
	if len(g.sender.got) != 1 {
		t.Fatalf("stale claim not taken over: %d mails", len(g.sender.got))
	}
}

func TestSummaryNowDoesNotReplaceMonday(t *testing.T) {
	g := newRig(berlinAt(2026, 10, 27, 10, 0)) // a Tuesday
	r := g.runner()
	if err := r.Force(context.Background()); err != nil {
		t.Fatal(err)
	}
	g.run(t, r) // this week's Monday is covered by the forced one
	g.now = berlinAt(2026, 11, 2, 7, 5)
	g.run(t, r)
	if len(g.sender.got) != 2 || !strings.Contains(g.sender.got[1].Text, "27.10.2026, 10:00 Uhr bis") {
		t.Fatalf("%d mails; last:\n%s", len(g.sender.got), g.sender.got[len(g.sender.got)-1].Text)
	}
}

func TestKickRunsInTheBackgroundAtMostOnceAMinute(t *testing.T) {
	g := newRig(berlinAt(2026, 10, 26, 7, 0))
	broken := &countingStore{Store: g.st, err: errors.New("turso down")}
	r := g.runner()
	r.Store = broken
	r.Kick()
	r.Wait()
	r.Kick() // same minute: not asked again
	r.Wait()
	if broken.asked != 1 {
		t.Fatalf("store asked %d times within a minute", broken.asked)
	}
	broken.err = nil
	g.now = g.now.Add(time.Minute)
	r.Kick()
	r.Wait()
	if len(g.sender.got) != 1 {
		t.Fatalf("%d mails after Turso came back", len(g.sender.got))
	}
	// Sent: the rest of the week costs no store call at all.
	g.now = g.now.Add(time.Hour)
	r.Kick()
	r.Wait()
	if broken.asked != 2 {
		t.Errorf("store asked %d times, want 2", broken.asked)
	}
}

type countingStore struct {
	store.Store
	asked int
	err   error
}

func (c *countingStore) LastSummary(ctx context.Context) (time.Time, bool, error) {
	c.asked++
	if c.err != nil {
		return time.Time{}, false, c.err
	}
	return c.Store.LastSummary(ctx)
}
