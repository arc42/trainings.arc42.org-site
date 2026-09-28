package web

import (
	"errors"
	"net/http"
	"time"

	"arc42-registration/internal/mail"
	"arc42-registration/internal/send"
	"arc42-registration/internal/token"
)

var confirmText = map[string]map[string]string{
	"de": {"Title": "Anmeldung bestätigen", "Course": "Kurs", "Dates": "Termin", "Where": "Ort", "Code": "Buchungscode",
		"Explain": "Mit dem Knopf bestätigen Sie diese Anmeldung. Wir melden uns danach persönlich bei Ihnen.",
		"Button":  "Anmeldung bestätigen"},
	"en": {"Title": "Confirm registration", "Course": "Course", "Dates": "Dates", "Where": "Location", "Code": "Booking code",
		"Explain": "This button confirms the registration. We will then get in touch with you personally.",
		"Button":  "Confirm registration"},
}

// handleConfirmPage shows the course and a button. It confirms NOTHING:
// corporate link scanners (Safe Links, Mimecast, Proofpoint) open every link
// in incoming mail, and if a GET confirmed, every fake registration sent to a
// company address would confirm itself.
func (s *Server) handleConfirmPage(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("t")
	c, err := s.d.Sealer.Open(tok)
	if err == nil && c.Purpose != token.PurposeConfirm {
		err = token.ErrInvalid
	}
	if err != nil {
		s.errorPage(w, err)
		return
	}
	// A used link goes straight to the confirmed page. Reading the store is
	// harmless for link scanners; only the POST below changes anything.
	if s.d.Store != nil {
		if done, err := s.d.Store.Confirmed(r.Context(), c.ID); err == nil && done {
			s.redirect(w, r, "confirmed", c.Lang)
			return
		}
	}
	var facts *mail.Facts
	if c.Code != "" && c.Code != "sonstige" {
		if e, _ := s.d.Feed.Lookup(r.Context(), c.Code); e.Code != "" {
			facts = mail.FactsFor(e, c.Lang)
		}
	}
	lang := c.Lang
	if lang != "en" {
		lang = "de"
	}
	noStore(w)
	_ = pages.ExecuteTemplate(w, "confirm.html", s.frame(lang, confirmText[lang]["Title"], map[string]any{"T": confirmText[lang], "Facts": facts, "Token": tok}))
}

func (s *Server) handleConfirm(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		s.errorPage(w, token.ErrInvalid)
		return
	}
	c, err := s.d.Sealer.Open(r.PostForm.Get("t"))
	if err == nil && c.Purpose != token.PurposeConfirm {
		err = token.ErrInvalid
	}
	if err != nil {
		s.errorPage(w, err)
		return
	}
	s.confirm(w, r, c, "link")
}

// confirm is the one way a registration becomes confirmed, by link or by
// code. It works once per registration: the store records the id first,
// and only a first record sends BESTÄTIGT (Gernot, 28 Sep 2026: one mail per
// click was one too many for the back office). If the mail fails, the
// record is taken back so a retry goes through. If the store is down, the
// link confirms anyway: a possible duplicate mail is better than a
// registration that cannot be confirmed.
func (s *Server) confirm(w http.ResponseWriter, r *http.Request, c token.Claims, via string) {
	ctx := r.Context()
	first := true
	if s.d.Store != nil {
		f, err := s.d.Store.Confirm(ctx, c.ID, s.d.Now())
		if err != nil {
			s.d.Log.Printf("confirm %s: store unavailable, confirming without it: %v", c.ID, err)
		} else {
			first = f
		}
	}
	if !first {
		s.d.Log.Printf("confirm %s: already confirmed (%s), no mail", c.ID, via)
		s.redirect(w, r, "confirmed", c.Lang)
		return
	}
	m, err := mail.Confirmed(c)
	if err == nil {
		err = s.send(ctx, send.Message{To: s.d.Cfg.BackofficeTo, ReplyTo: c.Email, Subject: m.Subject, Text: m.Text, CustomID: c.ID})
	}
	if err != nil {
		s.d.Log.Printf("confirm %s: mail failed: %v", c.ID, err)
		if s.d.Store != nil {
			if uerr := s.d.Store.Unconfirm(ctx, c.ID); uerr != nil {
				s.d.Log.Printf("confirm %s: could not take the confirmation back: %v", c.ID, uerr)
			}
		}
		s.errorPage(w, err)
		return
	}
	s.d.Log.Printf("confirm %s: confirmed %s (%s)", c.ID, c.Code, via)
	if s.d.Store != nil {
		// Housekeeping on the rare write path: nothing older than a link's
		// lifetime (plus a day) is worth keeping.
		if err := s.d.Store.Prune(ctx, s.d.Now().Add(-token.Valid-24*time.Hour)); err != nil {
			s.d.Log.Printf("store prune: %v", err)
		}
	}
	s.redirect(w, r, "confirmed", c.Lang)
}

// errorPage is bilingual: an unreadable token carries no language.
func (s *Server) errorPage(w http.ResponseWriter, err error) {
	de, en := "Der Link ist ungültig.", "The link is invalid."
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, token.ErrExpired):
		de, en = "Der Link ist abgelaufen (er gilt 5 Tage).", "The link has expired (it is valid for 5 days)."
		status = http.StatusGone
	case !errors.Is(err, token.ErrInvalid):
		de, en = "Die Bestätigung konnte gerade nicht verschickt werden.", "The confirmation could not be sent just now."
		status = http.StatusBadGateway
	}
	noStore(w)
	w.WriteHeader(status)
	_ = pages.ExecuteTemplate(w, "error.html", s.frame("de", "Dieser Link funktioniert nicht / This link does not work", map[string]any{"DE": de, "EN": en}))
}
