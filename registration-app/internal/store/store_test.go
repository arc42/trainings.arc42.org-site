package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"arc42-registration/internal/intake"
)

var t0 = time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)

// contract is what every Store must do, whatever keeps the data.
func contract(t *testing.T, s Store, id string) {
	ctx := context.Background()
	if ok, err := s.Confirmed(ctx, id); err != nil || ok {
		t.Fatalf("fresh id: confirmed=%v err=%v", ok, err)
	}
	if first, err := s.Confirm(ctx, id, t0); err != nil || !first {
		t.Fatalf("first confirm: first=%v err=%v", first, err)
	}
	if first, err := s.Confirm(ctx, id, t0.Add(time.Minute)); err != nil || first {
		t.Fatalf("second confirm must not be first: first=%v err=%v", first, err)
	}
	if ok, _ := s.Confirmed(ctx, id); !ok {
		t.Error("not confirmed after Confirm")
	}
	// Unconfirm undoes a confirmation whose mail could not be sent, so the
	// registrant's retry goes through.
	if err := s.Unconfirm(ctx, id); err != nil {
		t.Fatal(err)
	}
	if first, _ := s.Confirm(ctx, id, t0); !first {
		t.Error("confirm after Unconfirm is not first")
	}

	for want := 1; want <= 3; want++ {
		if n, err := s.Failure(ctx, id, t0); err != nil || n != want {
			t.Fatalf("failure %d: n=%d err=%v", want, n, err)
		}
	}
	if n, _ := s.Failures(ctx, id); n != 3 {
		t.Errorf("Failures = %d, want 3", n)
	}
	if n, _ := s.Failures(ctx, id+"-other"); n != 0 {
		t.Errorf("another id has %d failures", n)
	}

	// Links live 5 days; Prune drops what is older than that.
	if err := s.Prune(ctx, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.Confirmed(ctx, id); ok {
		t.Error("confirmation survived Prune")
	}
	if n, _ := s.Failures(ctx, id); n != 0 {
		t.Errorf("failures survived Prune: %d", n)
	}
	registrations(t, s, id)
}

// registrations is the part of the contract added by the confirm-gated
// redesign: what submit stores is what confirm reads back.
func registrations(t *testing.T, s Store, id string) {
	ctx := context.Background()
	if _, ok, err := s.Registration(ctx, id); err != nil || ok {
		t.Fatalf("fresh id: ok=%v err=%v", ok, err)
	}
	in := Record{
		Reg: intake.Registration{ID: id, Lang: "de", LastName: "Müller", FirstName: "Anna",
			Email: "Anna@Example.org, boss@example.org", Emails: []string{"anna@example.org", "boss@example.org"},
			Code: "26-12 MSA", Participants: []intake.Participant{{Salutation: "Frau", Last: "Meier", First: "Eva", Email: "eva@example.org"}, {Last: "Kurz"}},
			Company: "Firma X", Billing: "Firma X\nStraße 1", Comments: "PO 4711",
			HeardVia: "Buch", Via: "arc42.de", FormSource: "https://trainings.arc42.org/anmeldung/"},
		Hints: []string{"several-emails", "no-script"}, Status: StatusPending, CreatedAt: t0, Price: 2690,
	}
	if err := s.Save(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, in); err == nil {
		t.Error("a second Save of the same id succeeded")
	}
	got, ok, err := s.Registration(ctx, id)
	if err != nil || !ok {
		t.Fatalf("Registration: ok=%v err=%v", ok, err)
	}
	if !got.CreatedAt.Equal(t0) || !got.ConfirmedAt.IsZero() || got.MailFailed || !got.SheetAt.IsZero() {
		t.Errorf("times/flags = %v %v %v %v", got.CreatedAt, got.ConfirmedAt, got.MailFailed, got.SheetAt)
	}
	got.CreatedAt = in.CreatedAt // time.Time location differs between stores
	if fmt.Sprint(got) != fmt.Sprint(in) {
		t.Errorf("read back\n%+v\nwant\n%+v", got, in)
	}

	// Brevo's delivery events: a delivery clears only a bounce from before
	// it, and a correction clears any.
	bounce := func(at time.Time, reason string) {
		t.Helper()
		if err := s.Bounced(ctx, id, at, reason); err != nil {
			t.Fatal(err)
		}
	}
	bounce(t0.Add(time.Minute), "soft_bounce: 554 5.7.1 blocked by RBL")
	if got, _, _ := s.Registration(ctx, id); !got.BouncedAt.Equal(t0.Add(time.Minute)) || got.BounceReason != "soft_bounce: 554 5.7.1 blocked by RBL" {
		t.Errorf("after Bounced: %v %q", got.BouncedAt, got.BounceReason)
	}
	if err := s.Delivered(ctx, id, t0); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.Registration(ctx, id); got.BouncedAt.IsZero() {
		t.Error("a delivery from before the bounce cleared it")
	}
	if err := s.Delivered(ctx, id, t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.Registration(ctx, id); !got.BouncedAt.IsZero() || got.BounceReason != "" {
		t.Errorf("a later delivery left %v %q", got.BouncedAt, got.BounceReason)
	}
	bounce(t0.Add(3*time.Minute), "hard_bounce: user unknown")

	if err := s.MailFailed(ctx, id); err != nil {
		t.Fatal(err)
	}
	for _, e := range []string{"anna@example.com", "anna@example.net"} {
		if err := s.Corrected(ctx, id, e); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.MarkConfirmed(ctx, id, t0.Add(time.Hour), "code"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkConfirmed(ctx, id, t0.Add(2*time.Hour), "link"); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.Registration(ctx, id)
	if !got.MailFailed || got.Reg.Email != "anna@example.net" || len(got.Reg.Emails) != 1 ||
		got.CorrectedFrom != "Anna@Example.org, boss@example.org" {
		t.Errorf("after mail failure and two corrections: %+v", got)
	}
	if !got.BouncedAt.IsZero() || got.BounceReason != "" {
		t.Errorf("a correction left the bounce of the old address: %v %q", got.BouncedAt, got.BounceReason)
	}
	if got.Status != StatusConfirmed || got.ConfirmedVia != "code" || !got.ConfirmedAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("confirmed twice: status %q via %q at %v; the first one counts", got.Status, got.ConfirmedVia, got.ConfirmedAt)
	}
	if err := s.SheetDone(ctx, id, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, _, _ = s.Registration(ctx, id); !got.SheetAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("SheetAt = %v", got.SheetAt)
	}
	// Updates to an id that was never stored are no error: confirm calls
	// them for registrations from before the redesign.
	if err := s.MarkConfirmed(ctx, id+"-none", t0, "link"); err != nil {
		t.Error(err)
	}
	if err := s.Prune(ctx, t0.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Registration(ctx, id); !ok {
		t.Error("Prune deleted a registration; that is the summary run's job")
	}
	summaries(t, s, id)
}

// summaries is the part the weekly summary needs: listing, reconciling,
// marking, deleting, and the week claim.
func summaries(t *testing.T, s Store, id string) {
	ctx := context.Background()
	// A second registration, confirmed in the confirmed table only: the
	// row update failed after the mail went out.
	other := Record{Reg: intake.Registration{ID: id + "b", Lang: "en", Code: "26-12 MSA", Email: "jo@example.org"},
		Status: StatusPending, CreatedAt: t0.Add(time.Minute)}
	if err := s.Save(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Confirm(ctx, other.Reg.ID, t0.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Reconcile(ctx); err != nil || n != 1 {
		t.Errorf("Reconcile = %d, %v; want the one pending row with a confirmation", n, err)
	}
	if n, _ := s.Reconcile(ctx); n != 0 {
		t.Errorf("a second Reconcile marked %d", n)
	}
	all, err := s.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var mine []Record
	for _, r := range all {
		if strings.HasPrefix(r.Reg.ID, id) {
			mine = append(mine, r)
		}
	}
	if len(mine) != 2 || mine[0].Reg.ID != id || mine[1].Reg.ID != other.Reg.ID {
		t.Fatalf("All = %+v, want both, oldest first", mine)
	}
	if r := mine[1]; r.Status != StatusConfirmed || !r.ConfirmedAt.Equal(t0.Add(3*time.Hour)) || r.ConfirmedVia != "" {
		t.Errorf("reconciled row = %q at %v via %q", r.Status, r.ConfirmedAt, r.ConfirmedVia)
	}
	at := t0.Add(7 * 24 * time.Hour)
	if err := s.Reported(ctx, []string{id}, at); err != nil {
		t.Fatal(err)
	}
	if r, _, _ := s.Registration(ctx, id); !r.ReportedAt.Equal(at) {
		t.Errorf("ReportedAt = %v", r.ReportedAt)
	}
	if err := s.Delete(ctx, []string{id, other.Reg.ID}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Registration(ctx, id); ok {
		t.Error("Delete left the row")
	}
	if err := s.Reported(ctx, nil, at); err != nil {
		t.Error(err)
	}

	week := "test-" + id
	if ok, err := s.Claim(ctx, week, at, at.Add(-15*time.Minute)); err != nil || !ok {
		t.Fatalf("first claim: %v %v", ok, err)
	}
	if ok, _ := s.Claim(ctx, week, at.Add(time.Minute), at.Add(-14*time.Minute)); ok {
		t.Error("a fresh claim was taken over")
	}
	if ok, _ := s.Claim(ctx, week, at.Add(20*time.Minute), at.Add(5*time.Minute)); !ok {
		t.Error("a stale, unsent claim was not taken over")
	}
	if err := s.Release(ctx, week); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.Claim(ctx, week, at.Add(21*time.Minute), at.Add(6*time.Minute)); !ok {
		t.Error("a released week could not be claimed again")
	}
	// to lies in the past: run against a real database, this row must
	// never look like a newer summary than the real ones.
	to := at
	if err := s.SummarySent(ctx, week, at, t0, to, 2); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := s.LastSummary(ctx); err != nil || !ok || got.Before(to) {
		t.Errorf("LastSummary = %v %v %v, want %v or later", got, ok, err, to)
	}
	if ok, _ := s.Claim(ctx, week, to.Add(time.Hour), to); ok {
		t.Error("a sent week was claimed again")
	}
	_ = s.Release(ctx, week) // no effect on a sent week
	if ok, _ := s.Claim(ctx, week, to.Add(time.Hour), to); ok {
		t.Error("Release removed a sent week")
	}
}

func TestMemoryStore(t *testing.T) {
	contract(t, NewMemory(), "R-MEM01")
}

// Runs the contract against a real Turso database when TURSO_DATABASE_URL and
// TURSO_AUTH_TOKEN are set. Skipped in CI.
func TestTursoAgainstTheRealDatabase(t *testing.T) {
	url, tok := os.Getenv("TURSO_DATABASE_URL"), os.Getenv("TURSO_AUTH_TOKEN")
	if url == "" || tok == "" {
		t.Skip("TURSO_DATABASE_URL/TURSO_AUTH_TOKEN not set")
	}
	s, err := NewTurso(url, tok, nil)
	if err != nil {
		t.Fatal(err)
	}
	contract(t, s, "R-TEST-"+time.Now().Format("150405.000000"))
}

// The HTTP side of Turso: the URL scheme is rewritten, the token goes in
// as Bearer, statements are parameterised (never string-built), integers
// travel as strings as the protocol wants, and the affected-row count decides
// whether a confirmation is the first.
func TestTursoSpeaksThePipelineProtocol(t *testing.T) {
	var got []map[string]any
	affected := 1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/pipeline" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("request %s %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Requests []map[string]any `json:"requests"`
		}
		_ = json.Unmarshal(raw, &body)
		var results []string
		for _, req := range body.Requests {
			if req["type"] == "execute" {
				got = append(got, req)
				results = append(results, `{"type":"ok","response":{"type":"execute","result":{"cols":[],"rows":[],"affected_row_count":`+itoa(affected)+`}}}`)
			} else {
				results = append(results, `{"type":"ok","response":{"type":"close"}}`)
			}
		}
		_, _ = w.Write([]byte(`{"results":[` + strings.Join(results, ",") + `]}`))
	}))
	defer srv.Close()

	s, err := NewTurso(strings.Replace(srv.URL, "http://", "libsql://", 1), "tok", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	s.base = srv.URL // httptest speaks http, not https
	first, err := s.Confirm(context.Background(), "R-ABC12", t0)
	if err != nil || !first {
		t.Fatalf("first=%v err=%v", first, err)
	}
	last := got[len(got)-1]["stmt"].(map[string]any)
	if !strings.Contains(last["sql"].(string), "INSERT INTO confirmed") || strings.Contains(last["sql"].(string), "R-ABC12") {
		t.Errorf("sql = %q (the id must be an argument, not in the SQL)", last["sql"])
	}
	args := last["args"].([]any)
	if a := args[0].(map[string]any); a["type"] != "text" || a["value"] != "R-ABC12" {
		t.Errorf("arg 0 = %v", a)
	}
	if a := args[1].(map[string]any); a["type"] != "integer" || a["value"] != itoa(int(t0.Unix())) {
		t.Errorf("arg 1 = %v, want the time as an integer string", a)
	}
	affected = 0
	if first, _ := s.Confirm(context.Background(), "R-ABC12", t0); first {
		t.Error("affected_row_count 0 was taken as a first confirmation")
	}

	if _, err := NewTurso("https://example.org", "", nil); err == nil {
		t.Error("an empty token was accepted")
	}
}

// fakeTurso answers the pipeline protocol: the column check with every
// column there, a registrations SELECT with row, anything else with nothing.
func fakeTurso(t *testing.T, row string) *Turso {
	var cols []string
	for _, c := range strings.Split(regColumns, ",") {
		cols = append(cols, `[{"type":"text","value":"`+strings.TrimSpace(c)+`"}]`)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Requests []struct {
				Type string
				Stmt struct{ SQL string }
			}
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		var results []string
		for _, req := range body.Requests {
			rows := ""
			switch {
			case req.Type != "execute":
				results = append(results, `{"type":"ok","response":{"type":"close"}}`)
				continue
			case strings.Contains(req.Stmt.SQL, "pragma_table_info"):
				rows = strings.Join(cols, ",")
			case strings.HasPrefix(req.Stmt.SQL, "SELECT") && strings.Contains(req.Stmt.SQL, "FROM registrations"):
				rows = row
			}
			results = append(results, `{"type":"ok","response":{"type":"execute","result":{"rows":[`+rows+`],"affected_row_count":0}}}`)
		}
		_, _ = w.Write([]byte(`{"results":[` + strings.Join(results, ",") + `]}`))
	}))
	t.Cleanup(srv.Close)
	s, _ := NewTurso("libsql://x", "tok", srv.Client())
	s.base = srv.URL
	return s
}

// A row comes back as typed values; NULL has no value at all. This one was
// stored before the participants, company and price columns existed: they
// are NULL, and the one participant is in p_last, p_first, p_email.
func TestTursoReadsARegistrationRow(t *testing.T) {
	row := `[{"type":"text","value":"R-ABC12"},{"type":"integer","value":"1790000000"},{"type":"text","value":"confirmed"},` +
		`{"type":"integer","value":"1790003600"},{"type":"text","value":"link"},{"type":"text","value":""},{"type":"integer","value":"0"},` +
		`{"type":"text","value":"en"},{"type":"text","value":"26-12 MSA"},{"type":"text","value":"Smith"},{"type":"text","value":"Jo"},` +
		`{"type":"text","value":"jo@example.org"},{"type":"text","value":"jo@example.org"},{"type":"null"},` +
		`{"type":"text","value":"Doe"},{"type":"text","value":"Ann"},{"type":"text","value":""},{"type":"text","value":"ACME Ltd\nLondon"},` +
		`{"type":"text","value":""},{"type":"null"},{"type":"null"},{"type":"text","value":"trainings.arc42.org"},` +
		`{"type":"text","value":""},{"type":"text","value":"no-script"},{"type":"null"},` +
		`{"type":"null"},{"type":"null"},{"type":"null"},{"type":"null"},{"type":"null"},{"type":"null"}]`
	r, ok, err := fakeTurso(t, row).Registration(context.Background(), "R-ABC12")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if r.Reg.LastName != "Smith" || r.Reg.Billing != "ACME Ltd\nLondon" || r.CorrectedFrom != "" || r.Reg.HeardVia != "" ||
		r.ConfirmedAt.Unix() != 1790003600 || r.ConfirmedVia != "link" || len(r.Hints) != 1 || r.MailFailed || !r.ReportedAt.IsZero() {
		t.Errorf("record = %+v", r)
	}
	if fmt.Sprint(r.Reg.Participants) != fmt.Sprint([]intake.Participant{{Last: "Doe", First: "Ann"}}) ||
		r.Reg.Company != "" || r.Price != 0 || !r.SheetAt.IsZero() || !r.BouncedAt.IsZero() {
		t.Errorf("old row: participants %+v company %q price %d sheet %v", r.Reg.Participants, r.Reg.Company, r.Price, r.SheetAt)
	}
}

// The live table predates participants, company, price and the bounce
// columns. ensure adds
// exactly the missing columns, once per process.
func TestTursoAddsMissingColumns(t *testing.T) {
	var alters []string
	pipelines := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pipelines++
		var body struct {
			Requests []struct {
				Type string
				Stmt struct{ SQL string }
			}
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		var results []string
		for _, req := range body.Requests {
			if req.Type != "execute" {
				results = append(results, `{"type":"ok","response":{"type":"close"}}`)
				continue
			}
			rows := ""
			if strings.Contains(req.Stmt.SQL, "pragma_table_info") {
				rows = `[{"type":"text","value":"id"}],[{"type":"text","value":"company"}]`
			}
			if strings.HasPrefix(req.Stmt.SQL, "ALTER") {
				alters = append(alters, req.Stmt.SQL)
			}
			results = append(results, `{"type":"ok","response":{"type":"execute","result":{"rows":[`+rows+`],"affected_row_count":1}}}`)
		}
		_, _ = w.Write([]byte(`{"results":[` + strings.Join(results, ",") + `]}`))
	}))
	defer srv.Close()
	s, _ := NewTurso("libsql://x", "tok", srv.Client())
	s.base = srv.URL
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := s.Confirm(ctx, "R-ABC12", t0); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"ALTER TABLE registrations ADD COLUMN participants TEXT", "ALTER TABLE registrations ADD COLUMN price INTEGER",
		"ALTER TABLE registrations ADD COLUMN bounced_at INTEGER", "ALTER TABLE registrations ADD COLUMN bounce_reason TEXT"}
	if fmt.Sprint(alters) != fmt.Sprint(want) {
		t.Errorf("alters = %q, want %q", alters, want)
	}
	if pipelines != 4 { // schema+check, alters, confirm, confirm
		t.Errorf("%d pipelines, want 4", pipelines)
	}
}
