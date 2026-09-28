package store

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
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
