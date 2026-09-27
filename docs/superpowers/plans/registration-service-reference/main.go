// Command arc42-registration receives the trainings.arc42.org registration
// form, mails the back office and the registrant, and confirms registrations
// from a sealed link. See docs/superpowers/specs/2026-09-25-registration-service-design.md.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"arc42-registration/internal/config"
	"arc42-registration/internal/feed"
	"arc42-registration/internal/intake"
	"arc42-registration/internal/send"
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
	if cfg.Mailer == "log" {
		sender = &send.LogSender{W: os.Stdout}
	} else {
		sender = &send.Mailjet{Public: cfg.MailjetPublic, Private: cfg.MailjetPrivate, From: cfg.MailFrom, FromName: cfg.MailFromName}
	}
	if cfg.TestMode() {
		// The back office address is always allowed, so test mode still
		// shows both back-office mails.
		sender = send.NewAllowList(sender, append(cfg.TestRecipients, cfg.BackofficeTo), logger)
		logger.Printf("test mode: mail only to %v and %s", cfg.TestRecipients, cfg.BackofficeTo)
	}

	sealer, err := token.NewSealer(cfg.TokenKey, token.Valid, time.Now)
	if err != nil {
		logger.Fatalf("token: %v", err)
	}
	courses := feed.New(cfg.FeedURL, nil, time.Now)
	srv := web.New(web.Deps{
		Cfg: cfg,
		Checker: &intake.Checker{
			Feed:           courses,
			Limiter:        intake.NewLimiter(5, time.Hour, time.Now),
			AllowedOrigins: cfg.AllowedOrigins,
		},
		Feed:   courses,
		Sealer: sealer,
		Sender: sender,
		Log:    logger,
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
