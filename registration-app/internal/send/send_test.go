package send

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

type recorder struct{ got []Message }

func (r *recorder) Send(_ context.Context, m Message) error { r.got = append(r.got, m); return nil }

func TestAllowListKeepsTestDeploymentsHarmless(t *testing.T) {
	rec := &recorder{}
	var logs bytes.Buffer
	a := NewAllowList(rec, []string{"Me@Example.org"}, log.New(&logs, "", 0))

	_ = a.Send(context.Background(), Message{To: []string{"stranger@example.com"}, Subject: "x", CustomID: "R-1"})
	if len(rec.got) != 0 {
		t.Fatalf("a stranger was mailed: %+v", rec.got)
	}
	if !strings.Contains(logs.String(), "R-1") {
		t.Errorf("drop not logged: %q", logs.String())
	}
	_ = a.Send(context.Background(), Message{To: []string{"me@example.org"}, Subject: "Hello"})
	if len(rec.got) != 1 || rec.got[0].Subject != "[TEST] Hello" {
		t.Errorf("allowed mail = %+v", rec.got)
	}
}

func TestMailjetRequestShape(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "pub" || p != "priv" {
			t.Errorf("basic auth = %q %q %v", u, p, ok)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"Messages":[{"Status":"success"}]}`))
	}))
	defer srv.Close()
	m := &Mailjet{Public: "pub", Private: "priv", From: "trainings@arc42.org", FromName: "arc42 Trainings", Endpoint: srv.URL}
	err := m.Send(context.Background(), Message{To: []string{"a@example.org"}, ReplyTo: "info@arc42.de", Subject: "S", Text: "T", CustomID: "R-1"})
	if err != nil {
		t.Fatal(err)
	}
	msg := body["Messages"].([]any)[0].(map[string]any)
	if msg["TrackClicks"] != "disabled" || msg["TrackOpens"] != "disabled" {
		t.Errorf("tracking not disabled: %v %v", msg["TrackClicks"], msg["TrackOpens"])
	}
	if msg["From"].(map[string]any)["Email"] != "trainings@arc42.org" || msg["ReplyTo"].(map[string]any)["Email"] != "info@arc42.de" {
		t.Errorf("from/reply = %v %v", msg["From"], msg["ReplyTo"])
	}
	if _, ok := body["SandboxMode"]; ok {
		t.Error("SandboxMode sent although off")
	}
}

func TestMailjetRetriesOnceOn5xxButNotOn4xx(t *testing.T) {
	var calls atomic.Int32
	status := http.StatusBadGateway
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	m := &Mailjet{Public: "p", Private: "q", Endpoint: srv.URL}
	if err := m.Send(context.Background(), Message{To: []string{"a@b.de"}}); err == nil || calls.Load() != 2 {
		t.Errorf("5xx: err=%v calls=%d, want an error after 2 calls", err, calls.Load())
	}
	calls.Store(0)
	status = http.StatusUnauthorized
	if err := m.Send(context.Background(), Message{To: []string{"a@b.de"}}); err == nil || calls.Load() != 1 {
		t.Errorf("4xx: err=%v calls=%d, want an error after 1 call", err, calls.Load())
	}
}

// Runs against the real Mailjet API in sandbox mode (nothing is delivered)
// when MJ_APIKEY_PUBLIC/PRIVATE are set in the environment. Skipped in CI.
func TestMailjetSandboxAgainstTheRealAPI(t *testing.T) {
	pub, priv := os.Getenv("MJ_APIKEY_PUBLIC"), os.Getenv("MJ_APIKEY_PRIVATE")
	if pub == "" || priv == "" {
		t.Skip("MJ_APIKEY_PUBLIC/PRIVATE not set")
	}
	m := &Mailjet{Public: pub, Private: priv, From: "trainings@arc42.org", FromName: "arc42 Trainings", Sandbox: true}
	if err := m.Send(context.Background(), Message{To: []string{"sandbox@example.org"}, Subject: "sandbox", Text: "sandbox"}); err != nil {
		t.Fatal(err)
	}
}
