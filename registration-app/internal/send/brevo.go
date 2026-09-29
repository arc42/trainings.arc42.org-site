package send

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const brevoEndpoint = "https://api.brevo.com/v3/smtp/email"

// Brevo sends through the transactional email API (developers.brevo.com,
// "Send a transactional email"). It is the only provider: Mailjet, the
// first choice, was removed on 29 Sep 2026 after its account review stalled
// (spec, section 5).
//
// The API has no per-message switch for open and click tracking. Both must be off in the Brevo account (transactional settings):
// click tracking would rewrite the confirm link into a Brevo redirect.
type Brevo struct {
	APIKey         string
	From, FromName string
	Sandbox        bool // X-Sib-Sandbox: drop, accepted but not delivered
	Client         *http.Client
	Endpoint       string // tests point this at httptest
}

type brevoAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type brevoMessage struct {
	Sender      brevoAddress      `json:"sender"`
	To          []brevoAddress    `json:"to"`
	ReplyTo     *brevoAddress     `json:"replyTo,omitempty"`
	Subject     string            `json:"subject"`
	TextContent string            `json:"textContent"`
	HTMLContent string            `json:"htmlContent,omitempty"`
	Tags        []string          `json:"tags,omitempty"` // the R- id, to find a mail in Brevo's log
	Headers     map[string]string `json:"headers,omitempty"`
}

func (b *Brevo) Send(ctx context.Context, msg Message) error {
	bm := brevoMessage{
		Sender:  brevoAddress{Email: b.From, Name: b.FromName},
		Subject: msg.Subject, TextContent: msg.Text, HTMLContent: msg.HTML,
	}
	for _, to := range msg.To {
		bm.To = append(bm.To, brevoAddress{Email: to})
	}
	if msg.ReplyTo != "" {
		bm.ReplyTo = &brevoAddress{Email: msg.ReplyTo}
	}
	if msg.CustomID != "" {
		bm.Tags = []string{msg.CustomID}
	}
	if b.Sandbox {
		bm.Headers = map[string]string{"X-Sib-Sandbox": "drop"}
	}
	payload, err := json.Marshal(bm)
	if err != nil {
		return err
	}
	return retryOnce(ctx, func() error { return b.post(ctx, payload) })
}

func (b *Brevo) post(ctx context.Context, payload []byte) error {
	endpoint := b.Endpoint
	if endpoint == "" {
		endpoint = brevoEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("api-key", b.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := b.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return retryable{fmt.Errorf("brevo: %w", err)}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 500 {
		return retryable{fmt.Errorf("brevo: %s: %s", resp.Status, raw)}
	}
	// 201 with a messageId is success; everything else (400 bad request, 401
	// bad key, sender not validated) is our mistake and will not improve.
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("brevo: %s: %s", resp.Status, raw)
	}
	return nil
}
