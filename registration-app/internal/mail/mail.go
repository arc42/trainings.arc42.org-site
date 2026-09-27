// Package mail renders the three mails: the registrant's confirmation request
// and the back office's UNBESTÄTIGT/BESTÄTIGT pair. Every mail is in the
// language of the registration.
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

	"arc42-registration/internal/feed"
	"arc42-registration/internal/intake"
	"arc42-registration/internal/labels"
	"arc42-registration/internal/token"
)

//go:embed templates/*
var files embed.FS

var funcs = template.FuncMap{"indent": indent}

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
// one language. Price is the regular amount only: early bird and alumni
// prices are applied by hand (spec 2).
type Facts struct {
	Title, ShortTitle, Code, Dates, Where, Trainers, Price string
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
	if e.Price != nil && e.Price.Amount > 0 {
		f.Price = labels.Money(e.Price.Amount, e.Price.Currency, lang)
	}
	return f
}

var text = map[string]map[string]string{
	"de": {
		"Title": "Bitte bestätigen Sie Ihre Anmeldung", "Hello": "Guten Tag,",
		"Intro":      "vielen Dank für Ihre Anmeldung bei arc42. Sie ist erst vollständig, wenn Sie sie bestätigen:",
		"IntroOther": "vielen Dank für Ihre Anfrage bei arc42. Sie ist erst vollständig, wenn Sie sie bestätigen:",
		"Button":     "Anmeldung bestätigen", "Valid": "Der Link ist 5 Tage gültig.",
		"Course": "Kurs", "Dates": "Termin", "Where": "Ort", "Trainers": "Trainer", "Price": "Preis", "Code": "Buchungscode",
		"Other":     `Sie haben "Sonstige" gewählt. Wir melden uns persönlich bei Ihnen.`,
		"NoFacts":   "Die Einzelheiten zu Ihrem Termin bestätigen wir Ihnen persönlich.",
		"Waitlist":  "Hinweis: Dieser Termin ist ausgebucht. Mit Ihrer Anmeldung kommen Sie auf die Warteliste; wir melden uns, sobald ein Platz frei wird.",
		"ByHand":    "Wir bearbeiten Anmeldungen von Hand und melden uns persönlich, meist innerhalb von ein bis zwei Werktagen.",
		"NotYou":    "Sie haben sich nicht angemeldet? Dann ignorieren Sie diese Mail einfach. Ohne Bestätigung geschieht nichts.",
		"Questions": "Fragen? Antworten Sie einfach auf diese Mail.",
	},
	"en": {
		"Title": "Please confirm your registration", "Hello": "Hello,",
		"Intro":      "thank you for your registration with arc42. It is only complete once you confirm it:",
		"IntroOther": "thank you for your request with arc42. It is only complete once you confirm it:",
		"Button":     "Confirm registration", "Valid": "The link is valid for 5 days.",
		"Course": "Course", "Dates": "Dates", "Where": "Location", "Trainers": "Trainers", "Price": "Price", "Code": "Booking code",
		"Other":     `You chose "other". We will get in touch with you personally.`,
		"NoFacts":   "We will confirm the details of your date personally.",
		"Waitlist":  "Please note: this date is fully booked. Your registration puts you on the waiting list; we will get in touch as soon as a seat becomes available.",
		"ByHand":    "We process registrations by hand and will get back to you personally, usually within one or two business days.",
		"NotYou":    "You did not register? Then simply ignore this mail. Nothing happens without confirmation.",
		"Questions": "Questions? Just reply to this mail.",
	},
}

var hintText = map[string]map[string]string{
	"de": {
		intake.HintGmailDots:     "Gmail-Adresse mit vielen Punkten (Muster aus Spam-Tests)",
		intake.HintURLInName:     "Link in einem Namensfeld",
		intake.HintOddCase:       "Zufällig wirkende Groß-/Kleinschreibung in einem Namen",
		intake.HintSeveralEmails: "Mehrere E-Mail-Adressen; die Bestätigung ging nur an die erste",
		intake.HintClosed:        "Termin war beim Absenden nicht mehr offen (ausgebucht, abgesagt oder vorbei)",
		intake.HintFeedDown:      "Kursliste nicht verfügbar, Buchungscode ungeprüft",
		intake.HintWaitlist:      "WARTELISTE: der Termin ist ausgebucht, der Anmeldende wurde darauf hingewiesen",
		intake.HintRecipientCap:  "Bestätigungsmail an den Anmeldenden nicht verschickt: zu viele Anmeldungen an diese Adresse in 24 Stunden",
	},
	"en": {
		intake.HintGmailDots:     "Gmail address with many dots (a spam-probing pattern)",
		intake.HintURLInName:     "Link in a name field",
		intake.HintOddCase:       "Random-looking upper/lower case in a name",
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
func Registrant(l string, facts *Facts, other bool, confirmURL string) (Rendered, error) {
	l = lang(l)
	data := map[string]any{"Lang": l, "Facts": facts, "Other": other, "ConfirmURL": confirmURL, "T": text[l]}
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
		subject = map[string]string{"de": "Bitte bestätigen Sie Ihre Anfrage", "en": "Please confirm your request"}[l]
	case facts == nil:
	default:
		subject += ": " + facts.ShortTitle + ", " + facts.Dates
	}
	return Rendered{Subject: subject, Text: t.String(), HTML: h.String()}, nil
}

// Backoffice renders the first mail, sent at once. facts may be nil.
func Backoffice(reg intake.Registration, facts *Facts, hints []string) (Rendered, error) {
	l := lang(reg.Lang)
	status := map[string]string{"de": "UNBESTÄTIGT", "en": "UNCONFIRMED"}[l]
	var worded []string
	for _, h := range hints {
		worded = append(worded, hintText[l][h])
	}
	data := map[string]any{"Reg": reg, "Facts": facts, "Hints": worded, "Status": status}
	var t bytes.Buffer
	if err := textT.ExecuteTemplate(&t, "backoffice_"+l+".txt", data); err != nil {
		return Rendered{}, err
	}
	return Rendered{Subject: backofficeSubject(l, reg.ID, reg.Code, status), Text: t.String()}, nil
}

// Confirmed renders the second back-office mail from the token alone.
func Confirmed(c token.Claims) (Rendered, error) {
	l := lang(c.Lang)
	status := map[string]string{"de": "BESTÄTIGT", "en": "CONFIRMED"}[l]
	var t bytes.Buffer
	if err := textT.ExecuteTemplate(&t, "confirmed_"+l+".txt", c); err != nil {
		return Rendered{}, err
	}
	return Rendered{Subject: backofficeSubject(l, c.ID, c.Code, status), Text: t.String()}, nil
}

// Corrected tells the back office that the registrant fixed their address on
// the page after submitting. The confirmation went to the new address.
func Corrected(c token.Claims, oldEmail string) (Rendered, error) {
	l := lang(c.Lang)
	status := map[string]string{"de": "E-MAIL KORRIGIERT", "en": "EMAIL CORRECTED"}[l]
	var t bytes.Buffer
	if err := textT.ExecuteTemplate(&t, "corrected_"+l+".txt", map[string]any{"C": c, "Old": oldEmail}); err != nil {
		return Rendered{}, err
	}
	return Rendered{Subject: backofficeSubject(l, c.ID, c.Code, status), Text: t.String()}, nil
}

// Both back-office subjects differ only in the status tag, so mail clients
// thread them and a filter can match the tag.
func backofficeSubject(l, id, code, status string) string {
	word := map[string]string{"de": "ANMELDUNG", "en": "REGISTRATION"}[l]
	return "[trainings.arc42.org] " + word + " " + id + " " + code + " (" + status + ")"
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
