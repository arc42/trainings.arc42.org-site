// Package send delivers a rendered mail. Brevo does it in production; the
// allow-list wrapper keeps a test deployment from mailing anyone else; the log
// sender prints mails for local runs.
package send

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"
)

type Message struct {
	To       []string
	ReplyTo  string
	Subject  string
	Text     string
	HTML     string // optional
	CustomID string // registration id, a tag in Brevo's log
}

type Sender interface {
	Send(ctx context.Context, m Message) error
}

// AllowList passes a message on only to allowed recipients and marks every
// subject with [TEST]. A message left with no recipient is dropped and logged,
// not an error: the registrant mail to a stranger's address is the expected
// case in test mode.
type AllowList struct {
	Next    Sender
	Allowed map[string]bool
	Log     *log.Logger
}

func NewAllowList(next Sender, allowed []string, logger *log.Logger) *AllowList {
	m := map[string]bool{}
	for _, a := range allowed {
		m[strings.ToLower(a)] = true
	}
	return &AllowList{Next: next, Allowed: m, Log: logger}
}

func (a *AllowList) Send(ctx context.Context, m Message) error {
	var kept []string
	for _, to := range m.To {
		if a.Allowed[strings.ToLower(to)] {
			kept = append(kept, to)
		} else {
			a.Log.Printf("test mode: not sending %s %q to a recipient outside TEST_RECIPIENTS", m.CustomID, m.Subject)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	m.To = kept
	m.Subject = "[TEST] " + m.Subject
	return a.Next.Send(ctx, m)
}

// LogSender writes mails to W instead of sending them (MAILER=log).
type LogSender struct {
	mu sync.Mutex
	W  io.Writer
}

func (l *LogSender) Send(_ context.Context, m Message) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err := fmt.Fprintf(l.W, "----- mail %s\nTo: %s\nReply-To: %s\nSubject: %s\n\n%s\n", m.CustomID, strings.Join(m.To, ", "), m.ReplyTo, m.Subject, m.Text)
	return err
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
