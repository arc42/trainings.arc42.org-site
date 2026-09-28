package web

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"arc42-registration/internal/intake"
	"arc42-registration/internal/mail"
	"arc42-registration/internal/send"
	"arc42-registration/internal/token"
)

// maxCorrections: two chances to fix a typo. After that the page points to
// info@arc42.de; a person who needs a third try is better helped by a human,
// and a bot gets no further address to mail.
const maxCorrections = 2

// correctionWindow: corrections are for the moment right after submitting.
// The page's token stops correcting this long after the submission, whatever
// happens in between, which bounds what a replayed token can do.
const correctionWindow = 30 * time.Minute

var sentText = map[string]map[string]string{
	"de": {
		"Title":     "Fast geschafft: bitte bestätigen Sie Ihre Anmeldung",
		"Sent":      "Wir haben eine E-Mail an",
		"SentTail":  " geschickt. Ihre Anmeldung ist erst vollständig, wenn Sie sie bestätigen: mit dem Code aus der E-Mail hier unten, oder mit dem Link darin. Beides gilt 5 Tage.",
		"NoMail":    "Keine Mail da? Sehen Sie bitte auch im Spam-Ordner nach.",
		"Wrong":     "Adresse falsch geschrieben? Tragen Sie die richtige ein, wir schicken die E-Mail dann dorthin:",
		"Button":    "An diese Adresse schicken",
		"Invalid":   "Das sieht nicht nach einer gültigen E-Mail-Adresse aus.",
		"Exhausted": "Die Adresse lässt sich hier nicht mehr ändern. Schreiben Sie uns bitte an",
		"Back":      "Zurück zu den Terminen",
		"CodeLabel": "Bestätigungscode aus der E-Mail", "CodeButton": "Anmeldung bestätigen",
		"CodeWrong":       "Der Code stimmt nicht. Bitte prüfen Sie ihn in der E-Mail.",
		"CodeLocked":      "Zu viele falsche Versuche. Bitte bestätigen Sie mit dem Link in der E-Mail.",
		"CodeUnavailable": "Die Code-Eingabe ist gerade nicht verfügbar. Bitte bestätigen Sie mit dem Link in der E-Mail.",
	},
	"en": {
		"Title":     "Almost done: please confirm your registration",
		"Sent":      "We have sent an e-mail to",
		"SentTail":  ". Your registration is only complete once you confirm it: with the code from the e-mail below, or with the link in it. Both are valid for 5 days.",
		"NoMail":    "No mail? Please also check your spam folder.",
		"Wrong":     "Address mistyped? Enter the correct one and we will send the e-mail there:",
		"Button":    "Send to this address",
		"Invalid":   "That does not look like a valid e-mail address.",
		"Exhausted": "The address cannot be changed here any more. Please write to us at",
		"Back":      "Back to the training dates",
		"CodeLabel": "Confirmation code from the e-mail", "CodeButton": "Confirm registration",
		"CodeWrong":       "That code is not right. Please check it in the e-mail.",
		"CodeLocked":      "Too many wrong tries. Please confirm with the link in the e-mail.",
		"CodeUnavailable": "Code entry is not available just now. Please confirm with the link in the e-mail.",
	},
}

// sentPage is shown after every accepted AND every dropped submission, so a
// bot cannot tell the two apart. c is the correction token's content.
func (s *Server) sentPage(w http.ResponseWriter, c token.Claims, email string, invalid bool) {
	s.sentPageNote(w, c, email, invalid, "")
}

// sentPageNote is sentPage with a note on the code field: "wrong", "locked"
// or "unavailable" (store down), from handleCode.
func (s *Server) sentPageNote(w http.ResponseWriter, c token.Claims, email string, invalid bool, codeNote string) {
	l := c.Lang
	if l != "en" {
		l = "de"
	}
	c.Purpose = token.PurposeCorrect
	tok, err := s.d.Sealer.Seal(c)
	if err != nil {
		s.errorPage(w, err)
		return
	}
	back := s.d.Cfg.SiteURL + map[string]string{"de": "/de/#training-dates", "en": "/#training-dates"}[l]
	noStore(w)
	_ = pages.ExecuteTemplate(w, "sent.html", s.frame(l, sentText[l]["Title"], map[string]any{
		"T": sentText[l], "Email": email, "Token": tok, "Back": back,
		"Invalid": invalid, "Exhausted": c.Corrections >= maxCorrections,
		"CodeNote": codeNote, "CodeOpen": codeNote != "locked" && codeNote != "unavailable",
	}))
}

func (s *Server) handleCorrect(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		s.errorPage(w, token.ErrInvalid)
		return
	}
	c, err := s.d.Sealer.Open(r.PostForm.Get("t"))
	if err == nil && c.Purpose != token.PurposeCorrect {
		err = token.ErrInvalid
	}
	if err != nil {
		s.errorPage(w, err)
		return
	}
	if c.Corrections >= maxCorrections || s.d.Sealer.Age(c) > correctionWindow {
		c.Corrections = maxCorrections // shows the "write to us" page, no form
		s.sentPage(w, c, c.Email, false)
		return
	}
	newEmail, ok := intake.ParseAddress(r.PostForm.Get("email"))
	if !ok {
		s.sentPage(w, c, c.Email, true)
		return
	}
	old := c.Email
	c.Email, c.Corrections = newEmail, c.Corrections+1

	// A dropped submission (no id) and a limited client get the same page and
	// no mail, so neither learns anything.
	if c.Dropped == 1 {
		s.sentPage(w, c, newEmail, false)
		return
	}
	if s.d.CorrectionLimiter != nil && !s.d.CorrectionLimiter.Allow(c.ID) {
		s.d.Log.Printf("correct %s: dropped (more than %d corrections)", c.ID, maxCorrections)
		c.Corrections = maxCorrections
		s.sentPage(w, c, newEmail, false)
		return
	}
	if s.d.Checker != nil && s.d.Checker.Limiter != nil && !s.d.Checker.Limiter.Allow(intake.IPKey(clientIP(r))) {
		s.d.Log.Printf("correct %s: dropped (rate limit)", c.ID)
		s.sentPage(w, c, newEmail, false)
		return
	}
	if s.d.RecipientLimiter != nil && !s.d.RecipientLimiter.Allow(newEmail) {
		s.d.Log.Printf("correct %s: registrant mail withheld (per-address cap)", c.ID)
		s.sentPage(w, c, newEmail, false)
		return
	}

	var facts *mail.Facts
	if c.Code != intake.Other {
		if e, _ := s.d.Feed.Lookup(r.Context(), c.Code); e.Code != "" {
			facts = mail.FactsFor(e, c.Lang)
		}
	}
	confirm := c
	confirm.Purpose = token.PurposeConfirm
	confirm.Issued = 0 // the new confirm link gets its full 5 days
	tok, err := s.d.Sealer.Seal(confirm)
	if err == nil {
		var rm mail.Rendered
		if rm, err = mail.Registrant(c.Lang, facts, c.Code == intake.Other, s.d.Cfg.PublicURL+"/confirm?t="+url.QueryEscape(tok), s.d.Sealer.Code(c.ID, newEmail)); err == nil {
			err = s.send(r.Context(), send.Message{To: []string{newEmail}, ReplyTo: s.d.Cfg.ReplyTo, Subject: rm.Subject, Text: rm.Text, HTML: rm.HTML, CustomID: c.ID})
		}
	}
	if err != nil {
		s.d.Log.Printf("correct %s: registrant mail failed: %v", c.ID, err)
	}
	if m, err := mail.Corrected(c, old); err == nil {
		if err := s.send(r.Context(), send.Message{To: s.d.Cfg.BackofficeTo, ReplyTo: newEmail, Subject: m.Subject, Text: m.Text, CustomID: c.ID}); err != nil {
			s.d.Log.Printf("correct %s: back-office notice failed: %v", c.ID, err)
		}
	}
	s.d.Log.Printf("correct %s: address corrected (%d of %d)", c.ID, c.Corrections, maxCorrections)
	s.sentPage(w, c, newEmail, false)
}

// firstTyped is what the person typed into the email field, first address
// only, for the page shown after a dropped submission.
func firstTyped(s string) string {
	first, _, _ := strings.Cut(s, ",")
	return strings.TrimSpace(first)
}
