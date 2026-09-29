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

func TestBrevoRequestShape(t *testing.T) {
	var body map[string]any
	var sandbox bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") != "xkeysib-test" {
			t.Errorf("api-key header = %q", r.Header.Get("api-key"))
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		if h, ok := body["headers"].(map[string]any); ok && h["X-Sib-Sandbox"] == "drop" {
			sandbox = true
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"messageId":"<1@relay>"}`))
	}))
	defer srv.Close()
	b := &Brevo{APIKey: "xkeysib-test", From: "trainings@arc42.org", FromName: "arc42 Trainings", Endpoint: srv.URL}
	err := b.Send(context.Background(), Message{To: []string{"a@example.org", "b@example.org"}, ReplyTo: "info@arc42.de", Subject: "S", Text: "T", CustomID: "R-1"})
	if err != nil {
		t.Fatal(err)
	}
	if s := body["sender"].(map[string]any); s["email"] != "trainings@arc42.org" || s["name"] != "arc42 Trainings" {
		t.Errorf("sender = %v", s)
	}
	if to := body["to"].([]any); len(to) != 2 || to[1].(map[string]any)["email"] != "b@example.org" {
		t.Errorf("to = %v", to)
	}
	if body["replyTo"].(map[string]any)["email"] != "info@arc42.de" || body["subject"] != "S" || body["textContent"] != "T" {
		t.Errorf("replyTo/subject/text = %v %v %v", body["replyTo"], body["subject"], body["textContent"])
	}
	if _, ok := body["htmlContent"]; ok {
		t.Error("empty htmlContent was sent; Brevo would send an empty HTML part")
	}
	if tags := body["tags"].([]any); len(tags) != 1 || tags[0] != "R-1" {
		t.Errorf("tags = %v, want the registration id for Brevo's log", tags)
	}
	if sandbox {
		t.Error("sandbox header sent although off")
	}

	b.Sandbox = true
	if err := b.Send(context.Background(), Message{To: []string{"a@example.org"}, Subject: "S", Text: "T"}); err != nil || !sandbox {
		t.Errorf("sandbox: err=%v, drop header sent=%v", err, sandbox)
	}
}

func TestBrevoRetriesOnceOn5xxButNotOn4xx(t *testing.T) {
	var calls atomic.Int32
	status := http.StatusBadGateway
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	b := &Brevo{APIKey: "k", Endpoint: srv.URL}
	if err := b.Send(context.Background(), Message{To: []string{"a@b.de"}}); err == nil || calls.Load() != 2 {
		t.Errorf("5xx: err=%v calls=%d, want an error after 2 calls", err, calls.Load())
	}
	calls.Store(0)
	status = http.StatusUnauthorized
	if err := b.Send(context.Background(), Message{To: []string{"a@b.de"}}); err == nil || calls.Load() != 1 {
		t.Errorf("4xx: err=%v calls=%d, want an error after 1 call", err, calls.Load())
	}
}

// Runs against the real Brevo API in sandbox mode (X-Sib-Sandbox: drop,
// nothing is delivered) when BREVO_API_KEY is set. Skipped in CI.
func TestBrevoSandboxAgainstTheRealAPI(t *testing.T) {
	key := os.Getenv("BREVO_API_KEY")
	if key == "" {
		t.Skip("BREVO_API_KEY not set")
	}
	b := &Brevo{APIKey: key, From: "trainings@arc42.org", FromName: "arc42 Trainings", Sandbox: true}
	if err := b.Send(context.Background(), Message{To: []string{"sandbox@example.org"}, Subject: "sandbox", Text: "sandbox", CustomID: "R-SANDBOX"}); err != nil {
		t.Fatal(err)
	}
}
