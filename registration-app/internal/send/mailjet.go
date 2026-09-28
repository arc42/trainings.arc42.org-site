package send

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const mailjetEndpoint = "https://api.mailjet.com/v3.1/send"

// Mailjet sends through the Send API v3.1 with basic auth. Tracking is off
// per message: click tracking would rewrite the confirm link into a Mailjet
// redirect (a foreign domain, likelier to be flagged, and a tracker on every
// click), and open tracking adds a pixel for nothing.
type Mailjet struct {
	Public, Private string
	From, FromName  string
	Sandbox         bool // validate without delivering
	Client          *http.Client
	Endpoint        string // tests point this at httptest
}

type mjAddress struct {
	Email string `json:"Email"`
	Name  string `json:"Name,omitempty"`
}

type mjMessage struct {
	From        mjAddress   `json:"From"`
	To          []mjAddress `json:"To"`
	ReplyTo     *mjAddress  `json:"ReplyTo,omitempty"`
	Subject     string      `json:"Subject"`
	TextPart    string      `json:"TextPart"`
	HTMLPart    string      `json:"HTMLPart,omitempty"`
	CustomID    string      `json:"CustomID,omitempty"`
	TrackClicks string      `json:"TrackClicks"`
	TrackOpens  string      `json:"TrackOpens"`
}

func (m *Mailjet) Send(ctx context.Context, msg Message) error {
	body := struct {
		Messages    []mjMessage `json:"Messages"`
		SandboxMode bool        `json:"SandboxMode,omitempty"`
	}{SandboxMode: m.Sandbox}
	mm := mjMessage{
		From: mjAddress{Email: m.From, Name: m.FromName}, Subject: msg.Subject,
		TextPart: msg.Text, HTMLPart: msg.HTML, CustomID: msg.CustomID,
		TrackClicks: "disabled", TrackOpens: "disabled",
	}
	for _, to := range msg.To {
		mm.To = append(mm.To, mjAddress{Email: to})
	}
	if msg.ReplyTo != "" {
		mm.ReplyTo = &mjAddress{Email: msg.ReplyTo}
	}
	body.Messages = []mjMessage{mm}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	return retryOnce(ctx, func() error { return m.post(ctx, payload) })
}

type retryable struct{ error }

// retryOnce is the retry rule both providers share: one more try, only for
// a server-side or network failure. A 4xx is our mistake (bad key,
// unvalidated sender) and will not improve.
func retryOnce(ctx context.Context, post func() error) error {
	err := post()
	var retry retryable
	if errors.As(err, &retry) {
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
		err = post()
	}
	return err
}

func (m *Mailjet) post(ctx context.Context, payload []byte) error {
	endpoint := m.Endpoint
	if endpoint == "" {
		endpoint = mailjetEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.SetBasicAuth(m.Public, m.Private)
	req.Header.Set("Content-Type", "application/json")
	client := m.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return retryable{fmt.Errorf("mailjet: %w", err)}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 500 {
		return retryable{fmt.Errorf("mailjet: %s: %s", resp.Status, raw)}
	}
	var out struct {
		Messages []struct {
			Status string `json:"Status"`
		} `json:"Messages"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &out) != nil || len(out.Messages) == 0 || out.Messages[0].Status != "success" {
		return fmt.Errorf("mailjet: %s: %s", resp.Status, raw)
	}
	return nil
}
