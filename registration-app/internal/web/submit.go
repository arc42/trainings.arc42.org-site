package web

import (
	"net/http"
	"net/url"
	"strings"

	"arc42-registration/internal/feed"
	"arc42-registration/internal/intake"
	"arc42-registration/internal/mail"
	"arc42-registration/internal/send"
	"arc42-registration/internal/store"
	"arc42-registration/internal/token"
)

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, intake.MaxFormBytes())
	if err := r.ParseForm(); err != nil {
		s.d.Log.Printf("submit: unreadable body: %v", err)
		http.Error(w, "request too large or malformed", http.StatusRequestEntityTooLarge)
		return
	}
	dec := s.d.Checker.Check(r.Context(), intake.Input{Form: r.PostForm, Origin: r.Header.Get("Origin"), IP: clientIP(r)})
	reg := dec.Reg
	ctx := r.Context()
	switch dec.Outcome {
	case intake.Drop:
		// A bot learns nothing about which check it tripped: it gets the same
		// page a person gets, with a correction form that sends nothing.
		s.d.Log.Printf("submit: dropped (%s)", dec.Reason)
		addr := strings.ToLower(firstTyped(reg.Email))
		reg.ID = s.d.NewID()
		// Kept for the weekly summary, so a rule that drops a real person
		// shows up there with an address to write to. Not the rate-limit
		// drops: one script hammering from one address would fill the table
		// with copies, and the first five of its submissions are there.
		if dec.Reason != "rate limit" && s.d.Store != nil {
			rec := store.Record{Reg: reg, Hints: dec.Hints, Status: store.StatusDropped, DropReason: dec.Reason, CreatedAt: s.d.Now()}
			if err := s.d.Store.Save(ctx, rec); err != nil {
				s.d.Log.Printf("submit %s: storing the drop failed: %v", reg.ID, err)
			}
		}
		s.sentPage(w, token.Claims{ID: reg.ID, Code: reg.Code, Email: addr, LastName: reg.LastName, Lang: reg.Lang, Dropped: 1}, addr, false)
		return
	case intake.Reject:
		s.d.Log.Printf("submit: rejected (%s)", dec.Reason)
		s.redirect(w, r, "fail", reg.Lang)
		return
	}

	reg.ID = s.d.NewID()
	var facts *mail.Facts
	if dec.Found {
		facts = mail.FactsFor(dec.Entry, reg.Lang)
	}

	mailRegistrant := s.d.RecipientLimiter == nil || s.d.RecipientLimiter.Allow(reg.Emails[0])
	if !mailRegistrant {
		dec.Hints = append(dec.Hints, intake.HintRecipientCap)
	}

	// The store first: until the registrant confirms, it is the only place
	// the registration exists (spec 2026-10-25, 3.1). If it cannot be
	// written, the person has to know.
	if s.d.Store == nil {
		s.d.Log.Printf("submit %s: no store, registration not kept", reg.ID)
		s.redirect(w, r, "fail", reg.Lang)
		return
	}
	rec := store.Record{Reg: reg, Hints: dec.Hints, Status: store.StatusPending, CreatedAt: s.d.Now()}
	if dec.Found {
		// For the sheet only; no mail states a price (spec 2026-09-25, 2).
		rec.Price = dec.Entry.PriceOn(feed.Day(rec.CreatedAt))
	}
	if err := s.d.Store.Save(ctx, rec); err != nil {
		s.d.Log.Printf("submit %s: store write failed: %v", reg.ID, err)
		s.redirect(w, r, "fail", reg.Lang)
		return
	}

	claims := token.Claims{ID: reg.ID, Code: reg.Code, Email: reg.Emails[0], LastName: reg.LastName, Lang: reg.Lang}
	if !mailRegistrant {
		s.d.Log.Printf("submit %s: accepted %s (%s), registrant mail withheld (per-address cap) hints=%v", reg.ID, reg.Code, reg.Lang, dec.Hints)
		s.sentPage(w, claims, reg.Emails[0], false)
		return
	}
	confirm := claims
	confirm.Purpose = token.PurposeConfirm
	tok, err := s.d.Sealer.Seal(confirm)
	if err == nil {
		confirmURL := s.d.Cfg.PublicURL + "/confirm?t=" + url.QueryEscape(tok)
		var rm mail.Rendered
		if rm, err = mail.Registrant(reg.Lang, facts, reg.Code == intake.Other, confirmURL, s.d.Sealer.Code(reg.ID, reg.Emails[0])); err == nil {
			err = s.send(ctx, send.Message{To: []string{reg.Emails[0]}, ReplyTo: s.d.Cfg.ReplyTo, Subject: rm.Subject, Text: rm.Text, HTML: rm.HTML, CustomID: reg.ID})
		}
	}
	// Without the registrant mail nobody can confirm, and the back office
	// hears of the registration only in the weekly summary: so the fail
	// page, and the row is marked for that summary.
	if err != nil {
		s.d.Log.Printf("submit %s: registrant mail failed: %v", reg.ID, err)
		if merr := s.d.Store.MailFailed(ctx, reg.ID); merr != nil {
			s.d.Log.Printf("submit %s: could not mark the failed mail: %v", reg.ID, merr)
		}
		s.redirect(w, r, "fail", reg.Lang)
		return
	}
	s.d.Log.Printf("submit %s: accepted %s (%s) hints=%v", reg.ID, reg.Code, reg.Lang, dec.Hints)
	s.sentPage(w, claims, reg.Emails[0], false)
}
