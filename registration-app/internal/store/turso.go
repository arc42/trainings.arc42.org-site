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
	ready bool // tables exist; checked once per process
}

var schema = []string{
	"CREATE TABLE IF NOT EXISTS confirmed (id TEXT PRIMARY KEY, at INTEGER NOT NULL)",
	"CREATE TABLE IF NOT EXISTS code_failures (id TEXT PRIMARY KEY, n INTEGER NOT NULL, at INTEGER NOT NULL)",
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
// last one. The first call of a process creates the tables in front.
func (t *Turso) run(ctx context.Context, stmts ...map[string]any) (execResult, error) {
	t.mu.Lock()
	ready := t.ready
	t.mu.Unlock()
	var reqs []map[string]any
	if !ready {
		for _, s := range schema {
			reqs = append(reqs, stmt(s))
		}
	}
	reqs = append(reqs, stmts...)
	reqs = append(reqs, map[string]any{"type": "close"})
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
	if !ready {
		t.mu.Lock()
		t.ready = true
		t.mu.Unlock()
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

func firstInt(r execResult) (int, error) {
	if len(r.Rows) == 0 || len(r.Rows[0]) == 0 {
		return 0, errors.New("turso: no row returned")
	}
	return strconv.Atoi(r.Rows[0][0].Value)
}
