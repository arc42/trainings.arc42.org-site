// Command arc42-registration receives the trainings.arc42.org registration
// form, mails the registrant, confirms registrations from a sealed link or a
// code, mails the back office once per confirmed registration, and sends the
// back office a weekly summary of every attempt. See
// docs/superpowers/specs/2026-09-25-registration-service-design.md and
// 2026-10-25-confirm-gated-registration-design.md.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"arc42-registration/internal/config"
	"arc42-registration/internal/feed"
	"arc42-registration/internal/intake"
	"arc42-registration/internal/send"
	"arc42-registration/internal/store"
	"arc42-registration/internal/summary"
	"arc42-registration/internal/token"
	"arc42-registration/internal/web"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("config: %v", err)
	}

	var sender send.Sender
	switch cfg.Mailer {
	case "log":
		sender = &send.LogSender{W: os.Stdout}
	default: // "brevo"; config has refused anything else
		sender = &send.Brevo{APIKey: cfg.BrevoKey, From: cfg.MailFrom, FromName: cfg.MailFromName}
	}
	if cfg.TestMode() {
		// The back office address is always allowed, so test mode still
		// shows the back-office mail.
		sender = send.NewAllowList(sender, append(cfg.TestRecipients, cfg.BackofficeTo...), logger)
		logger.Printf("test mode: mail only to %v and %v", cfg.TestRecipients, cfg.BackofficeTo)
	}

	sealer, err := token.NewSealer(cfg.TokenKey, token.Valid, time.Now)
	if err != nil {
		logger.Fatalf("token: %v", err)
	}
	courses := feed.New(cfg.FeedURL, nil, time.Now)
	// The only state: registrations until reported, confirmations and
	// wrong-code counts. Config has already refused production without Turso.
	var st store.Store
	if cfg.TursoURL != "" {
		t, err := store.NewTurso(cfg.TursoURL, cfg.TursoToken, nil)
		if err != nil {
			logger.Fatalf("store: %v", err)
		}
		st = t
		logger.Printf("store: turso")
	} else {
		st = store.NewMemory()
		logger.Printf("store: in memory - forgets on restart, test deployments only")
	}

	// The weekly summary goes out from inside the first request after
	// Monday 07:00 (package summary); cron-job.org makes sure there is one.
	weekly := &summary.Runner{Store: st, Sender: sender, To: cfg.BackofficeTo, Log: logger, Now: time.Now}
	if cfg.SummaryNow {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			if err := weekly.Force(ctx); err != nil {
				logger.Printf("summary (SUMMARY_NOW): %v", err)
			}
		}()
	}

	srv := web.New(web.Deps{
		Cfg: cfg,
		Checker: &intake.Checker{
			Feed:           courses,
			Limiter:        intake.NewLimiter(5, time.Hour, time.Now),
			AllowedOrigins: cfg.AllowedOrigins,
		},
		Feed:    courses,
		Sealer:  sealer,
		Sender:  sender,
		Log:     logger,
		Store:   st,
		Summary: weekly,
		// Three confirmation requests per address and day are plenty for a
		// person who mistyped twice; more is someone using us as a mailer.
		RecipientLimiter: intake.NewLimiter(3, 24*time.Hour, time.Now),
		// Two corrections per registration within the half hour the page's
		// correction token works (internal/web/correct.go).
		CorrectionLimiter: intake.NewLimiter(2, 30*time.Minute, time.Now),
	})

	hs := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      45 * time.Second, // two sends with retries, bounded by sendTimeout
	}
	logger.Printf("listening on %s, confirm links under %s", cfg.Addr, cfg.PublicURL)
	logger.Fatal(hs.ListenAndServe())
}
