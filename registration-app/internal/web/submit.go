package web

import (
	"net/http"
	"net/url"
	"strings"

	"arc42-registration/internal/intake"
	"arc42-registration/internal/mail"
	"arc42-registration/internal/send"
	"arc42-registration/internal/token"
)

const maxBody = 32 << 10

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseForm(); err != nil {
		s.d.Log.Printf("submit: unreadable body: %v", err)
		http.Error(w, "request too large or malformed", http.StatusRequestEntityTooLarge)
		return
	}
	dec := s.d.Checker.Check(r.Context(), intake.Input{Form: r.PostForm, Origin: r.Header.Get("Origin"), IP: clientIP(r)})
	reg := dec.Reg
	switch dec.Outcome {
	case intake.Drop:
		// A bot learns nothing about which check it tripped: it gets the same
		// page a person gets, with a correction form that sends nothing.
		s.d.Log.Printf("submit: dropped (%s)", dec.Reason)
		addr := strings.ToLower(firstTyped(reg.Email))
		s.sentPage(w, token.Claims{ID: s.d.NewID(), Code: reg.Code, Email: addr, LastName: reg.LastName, Lang: reg.Lang, Dropped: 1}, addr, false)
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

	// Back office first: if this fails, the registration reached nobody and
	// the person has to know.
	bo, err := mail.Backoffice(reg, facts, dec.Hints)
	if err == nil {
		err = s.send(r.Context(), send.Message{To: []string{s.d.Cfg.BackofficeTo}, ReplyTo: reg.Emails[0], Subject: bo.Subject, Text: bo.Text, CustomID: reg.ID})
	}
	if err != nil {
		s.d.Log.Printf("submit %s: back-office mail failed: %v", reg.ID, err)
		s.redirect(w, r, "fail", reg.Lang)
		return
	}

	// The registrant mail. If it fails the back office already has the
	// registration and follows up, so the person still sees success.
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
		if rm, err = mail.Registrant(reg.Lang, facts, reg.Code == intake.Other, confirmURL); err == nil {
			err = s.send(r.Context(), send.Message{To: []string{reg.Emails[0]}, ReplyTo: s.d.Cfg.ReplyTo, Subject: rm.Subject, Text: rm.Text, HTML: rm.HTML, CustomID: reg.ID})
		}
	}
	if err != nil {
		s.d.Log.Printf("submit %s: registrant mail failed: %v", reg.ID, err)
	} else {
		s.d.Log.Printf("submit %s: accepted %s (%s) hints=%v", reg.ID, reg.Code, reg.Lang, dec.Hints)
	}
	s.sentPage(w, claims, reg.Emails[0], false)
}
