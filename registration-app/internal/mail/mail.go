// Package mail renders the mails: the registrant's confirmation request, the
// back office's one BESTÄTIGT mail, sent once the registrant confirmed (spec
// 2026-10-25), and the weekly summary (summary.go). Every mail about one
// registration is in its language; the summary is German.
//
// The registrant mail never contains text the registrant typed: not the name,
// not the comments, not the billing address. The address it goes to was typed
// by a stranger, and anything echoed would be their text delivered from
// arc42's domain (spec 4.2). Course facts come from the feed only.
package mail

import (
	"bytes"
	"embed"
	htmltemplate "html/template"
	"strings"
	"text/template"
	"time"

	"arc42-registration/internal/feed"
	"arc42-registration/internal/intake"
	"arc42-registration/internal/labels"
	"arc42-registration/internal/token"
)

//go:embed templates/*
var files embed.FS

var funcs = template.FuncMap{"indent": indent, "person": person, "inc": func(i int) int { return i + 1 }}

var (
	textT = template.Must(template.New("").Funcs(funcs).ParseFS(files, "templates/*.txt"))
	htmlT = htmltemplate.Must(htmltemplate.ParseFS(files, "templates/registrant.html"))
)

type Rendered struct {
	Subject string
	Text    string
	HTML    string // empty for the back-office mails
}

// Facts are the course facts as the registrant sees them, already worded for
// one language. Deliberately no price: many clients have special agreements,
// and a price in writing from us reads as a quote. Prices are settled with
// the invoice (Gernot, 28 Sep 2026).
type Facts struct {
	Title, ShortTitle, Code, Dates, Where, Trainers string
	// Waitlist: the date is fully booked. There is no flow of its own, only
	// a note in the registrant mail (spec section 8).
	Waitlist bool
}

func FactsFor(e feed.Entry, lang string) *Facts {
	f := &Facts{
		Title:      e.CourseTitle,
		ShortTitle: e.CourseShortTitle,
		Code:       e.Code,
		Dates:      labels.DateRange(e.Start, e.End, lang),
		Where:      e.City,
		Waitlist:   e.Status == "waitlist",
	}
	if f.Title == "" {
		f.Title = e.CourseShortTitle
	}
	if e.Format == "online" {
		f.Where = "Online"
	}
	join := " and "
	if lang == "de" {
		join = " und "
	}
	f.Trainers = strings.Join(e.Trainers, join)
	return f
}

var text = map[string]map[string]string{
	"de": {
		"Title": "Bitte bestätigen Sie Ihre E-Mail-Adresse", "Hello": "Guten Tag,",
		"Intro":     "vielen Dank für Ihre Anmeldung bei arc42. Bitte bestätigen Sie noch Ihre E-Mail-Adresse, dann ist Ihre Anmeldung vollständig.",
		"CodeLabel": "Ihr Bestätigungscode:", "CodeHint": "Geben Sie ihn auf der Seite ein, die sich nach dem Absenden geöffnet hat.",
		"OrLink":     "Oder bestätigen Sie Ihre Adresse mit diesem Link:",
		"IntroOther": "vielen Dank für Ihre Anfrage bei arc42. Bitte bestätigen Sie noch Ihre E-Mail-Adresse, dann ist Ihre Anfrage vollständig.",
		"Button":     "E-Mail-Adresse bestätigen", "Valid": "Code und Link sind 5 Tage gültig.",
		"Course": "Kurs", "Dates": "Termin", "Where": "Ort", "Trainers": "Trainer", "Code": "Buchungscode",
		"Other":     `Sie haben "Sonstige" gewählt. Wir melden uns persönlich bei Ihnen.`,
		"NoFacts":   "Die Einzelheiten zu Ihrem Termin bestätigen wir Ihnen persönlich.",
		"Waitlist":  "Hinweis: Dieser Termin ist ausgebucht. Mit Ihrer Anmeldung kommen Sie auf die Warteliste; wir melden uns, um das weitere Vorgehen zu klären.",
		"ByHand":    "Wir bearbeiten Anmeldungen von Hand und melden uns persönlich, meist innerhalb von ein bis zwei Werktagen.",
		"NotYou":    "Sie haben sich nicht angemeldet? Dann ignorieren Sie diese Mail einfach. Ohne Bestätigung geschieht nichts.",
		"Questions": "Fragen? Antworten Sie einfach auf diese Mail.",
	},
	"en": {
		"Title": "Please confirm your e-mail address", "Hello": "Hello,",
		"Intro":     "thank you for your registration with arc42. Please confirm your e-mail address to complete it.",
		"CodeLabel": "Your confirmation code:", "CodeHint": "Enter it on the page that opened after you submitted the form.",
		"OrLink":     "Or confirm your address with this link:",
		"IntroOther": "thank you for your request with arc42. Please confirm your e-mail address to complete it.",
		"Button":     "Confirm e-mail address", "Valid": "The code and the link are valid for 5 days.",
		"Course": "Course", "Dates": "Dates", "Where": "Location", "Trainers": "Trainers", "Code": "Booking code",
		"Other":     `You chose "other". We will get in touch with you personally.`,
		"NoFacts":   "We will confirm the details of your date personally.",
		"Waitlist":  "Please note: this date is fully booked. Your registration puts you on the waiting list; we will get in touch to agree on the next steps.",
		"ByHand":    "We process registrations by hand and will get back to you personally, usually within one or two business days.",
		"NotYou":    "You did not register? Then simply ignore this mail. Nothing happens without confirmation.",
		"Questions": "Questions? Just reply to this mail.",
	},
}

var hintText = map[string]map[string]string{
	"de": {
		intake.HintGmailDots:     "Gmail-Adresse mit vielen Punkten (Muster aus Spam-Tests)",
		intake.HintOddCase:       "Zufällig wirkende Groß-/Kleinschreibung in einem Namen",
		intake.HintSameName:      "Vorname gleich Nachname",
		intake.HintSameNames:     "Alle vier Namensfelder gleich",
		intake.HintShortBilling:  "Rechnungsadresse sehr kurz oder nur ein Wort",
		intake.HintDoubleTLD:     "E-Mail-Domain der Form x.com.de (Muster aus dem Spam)",
		intake.HintNoScript:      "Formular ohne JavaScript abgeschickt",
		intake.HintSeveralEmails: "Mehrere E-Mail-Adressen; die Bestätigung ging nur an die erste",
		intake.HintClosed:        "Termin war beim Absenden nicht mehr offen (ausgebucht, abgesagt oder vorbei)",
		intake.HintFeedDown:      "Kursliste nicht verfügbar, Buchungscode ungeprüft",
		intake.HintWaitlist:      "WARTELISTE: der Termin ist ausgebucht, der Anmeldende wurde darauf hingewiesen",
		intake.HintRecipientCap:  "Bestätigungsmail an den Anmeldenden nicht verschickt: zu viele Anmeldungen an diese Adresse in 24 Stunden",
	},
	"en": {
		intake.HintGmailDots:     "Gmail address with many dots (a spam-probing pattern)",
		intake.HintOddCase:       "Random-looking upper/lower case in a name",
		intake.HintSameName:      "First name equals last name",
		intake.HintSameNames:     "All four name fields identical",
		intake.HintShortBilling:  "Billing address very short or a single word",
		intake.HintDoubleTLD:     "E-mail domain shaped like x.com.de (a spam pattern)",
		intake.HintNoScript:      "Form sent without JavaScript",
		intake.HintSeveralEmails: "Several e-mail addresses; only the first received the confirmation request",
		intake.HintClosed:        "Date was no longer open when submitted (full, cancelled or past)",
		intake.HintFeedDown:      "Course list unavailable, booking code not checked",
		intake.HintWaitlist:      "WAITING LIST: the date is fully booked, the registrant was told so",
		intake.HintRecipientCap:  "Confirmation mail to the registrant not sent: too many registrations to this address within 24 hours",
	},
}

func lang(l string) string {
	if l == "en" {
		return "en"
	}
	return "de"
}

// Registrant renders the confirmation request. other says the registrant
// chose "Sonstige"/"other"; facts is nil then, and also when the course list
// could not be read, which must not read like "other".
// code is the 6-digit confirmation code (token.Sealer.Code); it is shown
// grouped 3+3 and leads the mail, the link follows as the fallback.
func Registrant(l string, facts *Facts, other bool, confirmURL, code string) (Rendered, error) {
	l = lang(l)
	if len(code) == 6 {
		code = code[:3] + " " + code[3:]
	}
	data := map[string]any{"Lang": l, "Facts": facts, "Other": other, "ConfirmURL": confirmURL, "ConfirmCode": code, "T": text[l]}
	var t, h bytes.Buffer
	if err := textT.ExecuteTemplate(&t, "registrant_"+l+".txt", data); err != nil {
		return Rendered{}, err
	}
	if err := htmlT.ExecuteTemplate(&h, "registrant.html", data); err != nil {
		return Rendered{}, err
	}
	subject := text[l]["Title"]
	switch {
	case other:
		subject = map[string]string{"de": "Bitte bestätigen Sie Ihre E-Mail-Adresse", "en": "Please confirm your e-mail address"}[l]
	case facts == nil:
	default:
		subject += ": " + facts.ShortTitle + ", " + facts.Dates
	}
	return Rendered{Subject: subject, Text: t.String(), HTML: h.String()}, nil
}

// Confirmation is what the back office learns on confirmation. Reg is nil
// when the store could not supply the registration; Missing says why.
type Confirmation struct {
	Reg           *intake.Registration
	Missing       string // "down": store unreachable; "absent": stored before the redesign
	Facts         *Facts
	Hints         []string
	CorrectedFrom string
	At            time.Time
	Via           string // "code" or "link"
}

// berlin is the back office's clock. The tz database is compiled in by
// package feed (the image is FROM scratch).
var berlin = func() *time.Location {
	if loc, err := time.LoadLocation("Europe/Berlin"); err == nil {
		return loc
	}
	return time.UTC
}()

// Confirmed renders the one back-office mail. Its body is the complete
// registration, one "Label: value" line per field, so it can be copied into
// the back-office spreadsheet; c (the confirm token) only fills in for a
// registration the store could not supply.
func Confirmed(c token.Claims, k Confirmation) (Rendered, error) {
	l := lang(c.Lang)
	status := map[string]string{"de": "BESTÄTIGT", "en": "CONFIRMED"}[l]
	var worded []string
	for _, h := range k.Hints {
		if w := hintText[l][h]; w != "" {
			worded = append(worded, w)
		}
	}
	at := k.At.In(berlin)
	how := map[string]map[string]string{
		"de": {"code": "per Code", "link": "per Link"},
		"en": {"code": "by code", "link": "by link"},
	}[l][k.Via]
	when := at.Format("02.01.2006, 15:04 Uhr") + ", " + how
	if l == "en" {
		when = at.Format("2006-01-02 15:04") + " (Berlin), " + how
	}
	data := map[string]any{
		"ID": c.ID, "Code": c.Code, "LastName": c.LastName, "Email": c.Email,
		"Reg": k.Reg, "Missing": k.Missing, "Facts": k.Facts, "Hints": worded,
		"CorrectedFrom": k.CorrectedFrom, "At": when,
	}
	var t bytes.Buffer
	if err := textT.ExecuteTemplate(&t, "confirmed_"+l+".txt", data); err != nil {
		return Rendered{}, err
	}
	return Rendered{Subject: backofficeSubject(l, c.ID, c.Code, status), Text: t.String()}, nil
}

// The subject is the one the BESTÄTIGT mail had in the two-mail flow, so
// existing filters keep matching.
func backofficeSubject(l, id, code, status string) string {
	word := map[string]string{"de": "ANMELDUNG", "en": "REGISTRATION"}[l]
	return "[trainings.arc42.org] " + word + " " + id + " " + code + " (" + status + ")"
}

// person is one participant on one line: "Frau Eva Meier <eva@example.org>".
// The salutation is the stored value, German in both languages.
func person(p intake.Participant) string {
	var parts []string
	for _, s := range []string{p.Salutation, p.First, p.Last} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	s := strings.Join(parts, " ")
	if p.Email != "" {
		s = strings.TrimSpace(s + " <" + p.Email + ">")
	}
	return s
}

func indent(s string) string {
	if strings.TrimSpace(s) == "" {
		return "  -"
	}
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}
