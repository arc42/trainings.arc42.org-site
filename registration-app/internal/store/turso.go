package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"arc42-registration/internal/intake"
)

// Turso keeps the state in a Turso (libSQL) database, spoken to over its
// plain HTTP API (POST /v2/pipeline, docs.turso.tech/sdk/http/reference)
// rather than a client library: the service depends on nothing but the Go
// standard library, and three statements do not need a driver.
type Turso struct {
	base   string // https://<db>-<org>.turso.io
	token  string
	client *http.Client

	mu    sync.Mutex
	ready bool // tables and columns exist; checked once per process
}

// schema is run once per process before the first statement. CREATE TABLE
// IF NOT EXISTS never adds a column to a table that exists, so registrations
// carried the columns of the later steps of spec 2026-10-25 (sheet_at,
// reported_at) from its first deploy on; what came later is in addedColumns.
var schema = []string{
	"CREATE TABLE IF NOT EXISTS confirmed (id TEXT PRIMARY KEY, at INTEGER NOT NULL)",
	"CREATE TABLE IF NOT EXISTS code_failures (id TEXT PRIMARY KEY, n INTEGER NOT NULL, at INTEGER NOT NULL)",
	`CREATE TABLE IF NOT EXISTS registrations (
		id TEXT PRIMARY KEY, created_at INTEGER NOT NULL, status TEXT NOT NULL,
		confirmed_at INTEGER, confirmed_via TEXT, drop_reason TEXT,
		mail_failed INTEGER NOT NULL DEFAULT 0,
		lang TEXT NOT NULL, code TEXT NOT NULL,
		last_name TEXT, first_name TEXT, email TEXT, emails TEXT, corrected_from TEXT,
		p_last TEXT, p_first TEXT, p_email TEXT,
		billing TEXT, comments TEXT, heard TEXT, heard_detail TEXT,
		via TEXT, form_source TEXT, hints TEXT,
		sheet_at INTEGER, reported_at INTEGER,
		participants TEXT, company TEXT, price INTEGER)`,
	// One row per summary week. The primary key is the claim that keeps two
	// machines from both sending; sent_at NULL means claimed, not yet sent.
	`CREATE TABLE IF NOT EXISTS summaries (
		week TEXT PRIMARY KEY, claimed_at INTEGER NOT NULL,
		sent_at INTEGER, from_at INTEGER, to_at INTEGER, rows INTEGER)`,
}

// addedColumns arrived after the registrations table was live (the sheet,
// Oct 2026). SQLite has no ADD COLUMN IF NOT EXISTS, so ensure reads the
// table's columns and adds what is missing. A new column goes here AND into
// the CREATE TABLE above, which is what a fresh database gets.
var addedColumns = []struct{ name, def string }{
	// JSON list of the participant blocks. p_last, p_first and p_email keep
	// the first one, so a revert of the sheet PR still shows it.
	{"participants", "participants TEXT"},
	{"company", "company TEXT"},
	{"price", "price INTEGER"},
}

// NewTurso takes the database URL as `turso db show --url` prints it
// (libsql://...) and a token from `turso db tokens create`.
func NewTurso(url, token string, client *http.Client) (*Turso, error) {
	if token == "" {
		return nil, errors.New("turso: auth token missing")
	}
	base := url
	for _, scheme := range []string{"libsql://", "turso://"} {
		base = strings.Replace(base, scheme, "https://", 1)
	}
	if !strings.HasPrefix(base, "https://") {
		return nil, fmt.Errorf("turso: database URL %q is not libsql:// or https://", url)
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &Turso{base: strings.TrimRight(base, "/"), token: token, client: client}, nil
}

type arg struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func text(s string) arg   { return arg{"text", s} }
func integer(n int64) arg { return arg{"integer", strconv.FormatInt(n, 10)} }
func itoa(n int) string   { return strconv.Itoa(n) }
func stmt(sql string, a ...arg) map[string]any {
	if a == nil {
		a = []arg{}
	}
	return map[string]any{"type": "execute", "stmt": map[string]any{"sql": sql, "args": a}}
}

type execResult struct {
	Rows     [][]arg `json:"rows"`
	Affected int     `json:"affected_row_count"`
}

// run executes the statements in one pipeline and returns the result of the
// last one. The first call of a process makes sure the schema is there.
func (t *Turso) run(ctx context.Context, stmts ...map[string]any) (execResult, error) {
	if err := t.ensure(ctx); err != nil {
		return execResult{}, err
	}
	return t.pipeline(ctx, stmts...)
}

// ensure creates the tables and adds missing columns, once per process. It
// costs one round trip more on the first request, two when a column is
// added. Two machines adding the same column during a deploy: one of them
// fails with "duplicate column", and its next call finds the column there.
func (t *Turso) ensure(ctx context.Context) error {
	t.mu.Lock()
	ready := t.ready
	t.mu.Unlock()
	if ready {
		return nil
	}
	var reqs []map[string]any
	for _, s := range schema {
		reqs = append(reqs, stmt(s))
	}
	reqs = append(reqs, stmt("SELECT name FROM pragma_table_info('registrations')"))
	cols, err := t.pipeline(ctx, reqs...)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, row := range cols.Rows {
		if len(row) > 0 {
			have[row[0].Value] = true
		}
	}
	var alter []map[string]any
	for _, c := range addedColumns {
		if !have[c.name] {
			alter = append(alter, stmt("ALTER TABLE registrations ADD COLUMN "+c.def))
		}
	}
	if len(alter) > 0 {
		if _, err := t.pipeline(ctx, alter...); err != nil {
			return err
		}
	}
	t.mu.Lock()
	t.ready = true
	t.mu.Unlock()
	return nil
}

// pipeline sends the statements as they are and returns the result of the
// last one.
func (t *Turso) pipeline(ctx context.Context, stmts ...map[string]any) (execResult, error) {
	reqs := append(append([]map[string]any(nil), stmts...), map[string]any{"type": "close"})
	payload, err := json.Marshal(map[string]any{"requests": reqs})
	if err != nil {
		return execResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.base+"/v2/pipeline", bytes.NewReader(payload))
	if err != nil {
		return execResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+t.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return execResult{}, fmt.Errorf("turso: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return execResult{}, fmt.Errorf("turso: %s: %.200s", resp.Status, raw)
	}
	var out struct {
		Results []struct {
			Type     string                    `json:"type"`
			Error    *struct{ Message string } `json:"error"`
			Response struct {
				Type   string     `json:"type"`
				Result execResult `json:"result"`
			} `json:"response"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return execResult{}, fmt.Errorf("turso: unreadable response: %w", err)
	}
	var last execResult
	for _, r := range out.Results {
		if r.Type != "ok" {
			msg := "unknown error"
			if r.Error != nil {
				msg = r.Error.Message
			}
			return execResult{}, fmt.Errorf("turso: %s", msg)
		}
		if r.Response.Type == "execute" {
			last = r.Response.Result
		}
	}
	return last, nil
}

func (t *Turso) Confirm(ctx context.Context, id string, at time.Time) (bool, error) {
	r, err := t.run(ctx, stmt("INSERT INTO confirmed (id, at) VALUES (?, ?) ON CONFLICT (id) DO NOTHING", text(id), integer(at.Unix())))
	return err == nil && r.Affected == 1, err
}

func (t *Turso) Unconfirm(ctx context.Context, id string) error {
	_, err := t.run(ctx, stmt("DELETE FROM confirmed WHERE id = ?", text(id)))
	return err
}

func (t *Turso) Confirmed(ctx context.Context, id string) (bool, error) {
	r, err := t.run(ctx, stmt("SELECT 1 FROM confirmed WHERE id = ?", text(id)))
	return err == nil && len(r.Rows) > 0, err
}

func (t *Turso) Failure(ctx context.Context, id string, at time.Time) (int, error) {
	r, err := t.run(ctx, stmt("INSERT INTO code_failures (id, n, at) VALUES (?, 1, ?) ON CONFLICT (id) DO UPDATE SET n = n + 1, at = excluded.at RETURNING n", text(id), integer(at.Unix())))
	if err != nil {
		return 0, err
	}
	return firstInt(r)
}

func (t *Turso) Failures(ctx context.Context, id string) (int, error) {
	r, err := t.run(ctx, stmt("SELECT n FROM code_failures WHERE id = ?", text(id)))
	if err != nil || len(r.Rows) == 0 {
		return 0, err
	}
	return firstInt(r)
}

func (t *Turso) Prune(ctx context.Context, cutoff time.Time) error {
	c := integer(cutoff.Unix())
	_, err := t.run(ctx, stmt("DELETE FROM confirmed WHERE at < ?", c), stmt("DELETE FROM code_failures WHERE at < ?", c))
	return err
}

// regColumns is the order Save writes and Registration reads.
const regColumns = "id, created_at, status, confirmed_at, confirmed_via, drop_reason, mail_failed, " +
	"lang, code, last_name, first_name, email, emails, corrected_from, " +
	"p_last, p_first, p_email, billing, comments, heard, heard_detail, via, form_source, hints, reported_at, " +
	"participants, company, price, sheet_at"

func (t *Turso) Save(ctx context.Context, r Record) error {
	g := r.Reg
	var first intake.Participant
	if len(g.Participants) > 0 {
		first = g.Participants[0]
	}
	ps, err := encodeParticipants(g.Participants)
	if err != nil {
		return err
	}
	_, err = t.run(ctx, stmt("INSERT INTO registrations ("+regColumns+") VALUES (?, ?, ?, NULL, NULL, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, NULL)",
		text(g.ID), integer(r.CreatedAt.Unix()), text(r.Status), text(r.DropReason), flag(r.MailFailed),
		text(g.Lang), text(g.Code), text(g.LastName), text(g.FirstName), text(g.Email), text(strings.Join(g.Emails, ",")),
		text(first.Last), text(first.First), text(first.Email), text(g.Billing), text(g.Comments),
		text(g.HeardVia), text(g.HeardViaDetail), text(g.Via), text(g.FormSource), text(strings.Join(r.Hints, ",")),
		text(ps), text(g.Company), integer(int64(r.Price))))
	return err
}

// storedParticipant is the JSON shape of one participant in the
// participants column. Short keys: the column is read by nothing but this
// file and `make show-registrations`.
type storedParticipant struct {
	Salutation string `json:"s,omitempty"`
	Last       string `json:"l,omitempty"`
	First      string `json:"f,omitempty"`
	Email      string `json:"e,omitempty"`
}

func encodeParticipants(ps []intake.Participant) (string, error) {
	if len(ps) == 0 {
		return "", nil
	}
	out := make([]storedParticipant, len(ps))
	for i, p := range ps {
		out[i] = storedParticipant(p)
	}
	b, err := json.Marshal(out)
	return string(b), err
}

// decodeParticipants reads the participants column. A row stored before the
// column existed has it NULL and its one participant in p_last, p_first,
// p_email.
func decodeParticipants(js, last, first, email string) ([]intake.Participant, error) {
	if js == "" {
		if last == "" && first == "" && email == "" {
			return nil, nil
		}
		return []intake.Participant{{Last: last, First: first, Email: email}}, nil
	}
	var in []storedParticipant
	if err := json.Unmarshal([]byte(js), &in); err != nil {
		return nil, fmt.Errorf("turso: participants column: %w", err)
	}
	out := make([]intake.Participant, len(in))
	for i, p := range in {
		out[i] = intake.Participant(p)
	}
	return out, nil
}

func (t *Turso) Registration(ctx context.Context, id string) (Record, bool, error) {
	res, err := t.run(ctx, stmt("SELECT "+regColumns+" FROM registrations WHERE id = ?", text(id)))
	if err != nil || len(res.Rows) == 0 {
		return Record{}, false, err
	}
	r, err := scan(res.Rows[0])
	return r, err == nil, err
}

func (t *Turso) All(ctx context.Context) ([]Record, error) {
	res, err := t.run(ctx, stmt("SELECT "+regColumns+" FROM registrations ORDER BY created_at, id"))
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(res.Rows))
	for _, v := range res.Rows {
		r, err := scan(v)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// scan reads one row in regColumns order.
func scan(v []arg) (Record, error) {
	if len(v) != strings.Count(regColumns, ",")+1 {
		return Record{}, fmt.Errorf("turso: registrations row has %d columns", len(v))
	}
	ps, err := decodeParticipants(v[25].Value, v[14].Value, v[15].Value, v[16].Value)
	if err != nil {
		return Record{}, err
	}
	price, _ := strconv.Atoi(v[27].Value) // NULL in rows from before the column: unknown
	return Record{
		Reg: intake.Registration{
			ID: v[0].Value, Lang: v[7].Value, Code: v[8].Value,
			LastName: v[9].Value, FirstName: v[10].Value, Email: v[11].Value, Emails: split(v[12].Value),
			Participants: ps, Company: v[26].Value,
			Billing: v[17].Value, Comments: v[18].Value, HeardVia: v[19].Value, HeardViaDetail: v[20].Value,
			Via: v[21].Value, FormSource: v[22].Value,
		},
		Status: v[2].Value, ConfirmedVia: v[4].Value, DropReason: v[5].Value, MailFailed: v[6].Value == "1",
		CorrectedFrom: v[13].Value, Hints: split(v[23].Value),
		CreatedAt: unix(v[1]), ConfirmedAt: unix(v[3]), ReportedAt: unix(v[24]),
		Price: price, SheetAt: unix(v[28]),
	}, nil
}

func (t *Turso) MailFailed(ctx context.Context, id string) error {
	_, err := t.run(ctx, stmt("UPDATE registrations SET mail_failed = 1 WHERE id = ?", text(id)))
	return err
}

// Corrected keeps only the first address in corrected_from: that is the one
// the back office might already know from somewhere, and a second correction
// is rare enough not to need a history.
func (t *Turso) Corrected(ctx context.Context, id, email string) error {
	_, err := t.run(ctx, stmt("UPDATE registrations SET corrected_from = COALESCE(corrected_from, email), email = ?, emails = ? WHERE id = ?",
		text(email), text(email), text(id)))
	return err
}

func (t *Turso) MarkConfirmed(ctx context.Context, id string, at time.Time, via string) error {
	_, err := t.run(ctx, stmt("UPDATE registrations SET status = ?, confirmed_at = ?, confirmed_via = ? WHERE id = ? AND status = ?",
		text(StatusConfirmed), integer(at.Unix()), text(via), text(id), text(StatusPending)))
	return err
}

func (t *Turso) SheetDone(ctx context.Context, id string, at time.Time) error {
	_, err := t.run(ctx, stmt("UPDATE registrations SET sheet_at = ? WHERE id = ?", integer(at.Unix()), text(id)))
	return err
}

// Reconcile takes the time from the confirmed table; how the registrant
// confirmed is not recorded there, so confirmed_via stays NULL.
func (t *Turso) Reconcile(ctx context.Context) (int, error) {
	r, err := t.run(ctx, stmt(`UPDATE registrations
		SET status = ?, confirmed_at = (SELECT at FROM confirmed WHERE confirmed.id = registrations.id)
		WHERE status = ? AND id IN (SELECT id FROM confirmed)`, text(StatusConfirmed), text(StatusPending)))
	return r.Affected, err
}

// Reported and Delete send one statement per id in a single pipeline: a
// week holds dozens of rows, and this keeps every id a parameter.
func (t *Turso) Reported(ctx context.Context, ids []string, at time.Time) error {
	return t.each(ctx, ids, "UPDATE registrations SET reported_at = ? WHERE id = ?", integer(at.Unix()))
}

func (t *Turso) Delete(ctx context.Context, ids []string) error {
	return t.each(ctx, ids, "DELETE FROM registrations WHERE id = ?")
}

// each runs sql once per id, with the id as the last argument.
func (t *Turso) each(ctx context.Context, ids []string, sql string, a ...arg) error {
	if len(ids) == 0 {
		return nil
	}
	stmts := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		stmts = append(stmts, stmt(sql, append(append([]arg(nil), a...), text(id))...))
	}
	_, err := t.run(ctx, stmts...)
	return err
}

func (t *Turso) LastSummary(ctx context.Context) (time.Time, bool, error) {
	r, err := t.run(ctx, stmt("SELECT to_at FROM summaries WHERE sent_at IS NOT NULL ORDER BY to_at DESC LIMIT 1"))
	if err != nil || len(r.Rows) == 0 || len(r.Rows[0]) == 0 {
		return time.Time{}, false, err
	}
	return unix(r.Rows[0][0]), true, nil
}

// Claim inserts the week's row; the upsert takes over only a claim that is
// stale and was never sent. SQLite counts an upsert whose WHERE fails as no
// change, so the affected-row count says who won.
func (t *Turso) Claim(ctx context.Context, week string, at, stale time.Time) (bool, error) {
	r, err := t.run(ctx, stmt(`INSERT INTO summaries (week, claimed_at) VALUES (?, ?)
		ON CONFLICT (week) DO UPDATE SET claimed_at = excluded.claimed_at
		WHERE summaries.sent_at IS NULL AND summaries.claimed_at < ?`,
		text(week), integer(at.Unix()), integer(stale.Unix())))
	return err == nil && r.Affected == 1, err
}

func (t *Turso) Release(ctx context.Context, week string) error {
	_, err := t.run(ctx, stmt("DELETE FROM summaries WHERE week = ? AND sent_at IS NULL", text(week)))
	return err
}

func (t *Turso) SummarySent(ctx context.Context, week string, at, from, to time.Time, rows int) error {
	_, err := t.run(ctx, stmt("UPDATE summaries SET sent_at = ?, from_at = ?, to_at = ?, rows = ? WHERE week = ?",
		integer(at.Unix()), integer(from.Unix()), integer(to.Unix()), integer(int64(rows)), text(week)))
	return err
}

func flag(b bool) arg {
	if b {
		return integer(1)
	}
	return integer(0)
}

// unix reads an INTEGER column holding unix seconds; NULL is the zero time.
func unix(a arg) time.Time {
	n, err := strconv.ParseInt(a.Value, 10, 64)
	if a.Type != "integer" || err != nil {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

func split(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func firstInt(r execResult) (int, error) {
	if len(r.Rows) == 0 || len(r.Rows[0]) == 0 {
		return 0, errors.New("turso: no row returned")
	}
	return strconv.Atoi(r.Rows[0][0].Value)
}
