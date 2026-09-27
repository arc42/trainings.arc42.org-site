package web

import (
	"errors"
	"net/http"

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
	_ = pages.ExecuteTemplate(w, "confirm.html", map[string]any{"Lang": lang, "T": confirmText[lang], "Facts": facts, "Token": tok})
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
	m, err := mail.Confirmed(c)
	if err == nil {
		err = s.send(r.Context(), send.Message{To: s.d.Cfg.BackofficeTo, ReplyTo: c.Email, Subject: m.Subject, Text: m.Text, CustomID: c.ID})
	}
	if err != nil {
		s.d.Log.Printf("confirm %s: mail failed: %v", c.ID, err)
		s.errorPage(w, err)
		return
	}
	s.d.Log.Printf("confirm %s: confirmed %s", c.ID, c.Code)
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
	_ = pages.ExecuteTemplate(w, "error.html", map[string]string{"DE": de, "EN": en})
}
