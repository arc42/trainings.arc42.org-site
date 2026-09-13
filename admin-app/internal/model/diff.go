package model

import (
	"strconv"
	"strings"
)

// FieldChange is one field that differs between two versions of a date or a
// course, already rendered for a human reader.
//
// It exists because the review screen used to show a unified YAML diff and
// nothing else. A diff is the right artefact for CI and the wrong one for the
// person deciding whether to publish: marking one run as "few seats left" came
// out as four lines of context around one changed key, and the operator had to
// re-derive their own edit from the file format.
//
// Key is a stable identifier, not a label: the pull request title picks a
// wording per field, and switching on prose would break the moment a label is
// reworded.
type FieldChange struct {
	Key    string
	Label  string
	Before string
	After  string
}

// NoValue is how an absent value is written in a report. An empty cell reads
// as "unknown"; this reads as "nothing there", which is what it means.
const NoValue = "—"

// DiffDates returns the fields whose value differs, in file order.
//
// Both directions of "one side is missing" fall out of passing the zero Date:
// DiffDates(Date{}, d) describes an addition and DiffDates(d, Date{}) a
// removal, so those need no separate function and cannot drift from this one.
func DiffDates(before, after Date) []FieldChange {
	return changed([]FieldChange{
		{"id", "Id", before.ID, after.ID},
		{"code", "Booking code", before.Code, after.Code},
		{"start", "Starts", before.Start, after.Start},
		{"end", "Ends", before.End, after.End},
		{"city", "City", before.City, after.City},
		{"country", "Country", before.Country, after.Country},
		{"language", "Held in", languageName(before.Language), languageName(after.Language)},
		{"format", "Format", before.Format, after.Format},
		{"trainers", "Trainers", strings.Join(before.Trainers, ", "), strings.Join(after.Trainers, ", ")},
		{"price", "Price", FormatPrice(before.Price), FormatPrice(after.Price)},
		{"seats_limited", "Few seats left", yesNo(before.SeatsLimited), yesNo(after.SeatsLimited)},
		{"url", "Registration link", before.URL, after.URL},
		{"status", "Status", before.Status, after.Status},
	})
}

// DiffCourses does the same for a course's own fields. Dates are left out on
// purpose: each one is its own entry in the change list, and repeating them
// here would report the same edit twice.
func DiffCourses(before, after Course) []FieldChange {
	return changed([]FieldChange{
		{"id", "Id", before.ID, after.ID},
		{"short_title", "Short title", before.ShortTitle, after.ShortTitle},
		{"title", "Title", before.Title, after.Title},
		{"blurb", "Blurb", before.Blurb, after.Blurb},
		{"certification", "Certification", before.Certification, after.Certification},
		{"credit_points", "iSAQB credit points", FormatCredits(before.CreditPoints), FormatCredits(after.CreditPoints)},
		{"url", "German page", before.URL, after.URL},
		{"url_en", "English page", before.URLEn, after.URLEn},
		{"trainers", "Trainers", strings.Join(before.Trainers, ", "), strings.Join(after.Trainers, ", ")},
	})
}

// changed drops the fields that stayed the same and fills in the placeholder
// for the ones that were or became empty. The comparison is on the rendered
// strings, so two prices that print identically are one price — which is the
// question the reader is actually asking.
func changed(all []FieldChange) []FieldChange {
	var out []FieldChange
	for _, fc := range all {
		if fc.Before == fc.After {
			continue
		}
		if fc.Before == "" {
			fc.Before = NoValue
		}
		if fc.After == "" {
			fc.After = NoValue
		}
		out = append(out, fc)
	}
	return out
}

// Span renders a date's range for a report header. A one-day run and a run
// with no end recorded both print as the single date rather than "x to x".
func (d Date) Span() string {
	if d.End == "" || d.End == d.Start {
		return d.Start
	}
	return d.Start + " to " + d.End
}

// FormatPrice renders a price the way the report and the pull request body
// want it. A nil price and a zero amount are both "no published price", which
// is the normal case for three of the four courses.
func FormatPrice(p *Price) string {
	if p == nil || p.Amount == 0 {
		return ""
	}
	parts := []string{formatMoney(p.Amount, p.Currency)}
	if p.Alumni > 0 {
		parts = append(parts, "alumni "+formatMoney(p.Alumni, p.Currency))
	}
	// An expired early bird is still shown here. This is a report on what the
	// file says, not on what the site would render today, and hiding a lapsed
	// offer would make an edit that removes it look like no edit at all.
	if eb := p.EarlyBird; eb != nil && eb.Amount > 0 {
		parts = append(parts, "early bird "+formatMoney(eb.Amount, p.Currency)+" until "+eb.Until)
	}
	return strings.Join(parts, ", ")
}

// FormatCredits renders credit points as the sentence the site builds from
// them, minus the language: "20 methodical, 10 technical".
func FormatCredits(c *CreditPoints) string {
	if c.Empty() {
		return ""
	}
	var parts []string
	for _, cat := range []struct {
		n     int
		label string
	}{
		{c.Methodical, "methodical"},
		{c.Technical, "technical"},
		{c.Communication, "communication"},
	} {
		if cat.n > 0 {
			parts = append(parts, strconv.Itoa(cat.n)+" "+cat.label)
		}
	}
	return strings.Join(parts, ", ")
}

func formatMoney(amount int, currency string) string {
	unit := "€"
	if currency != "" && currency != "EUR" {
		unit = currency
	}
	return unit + " " + group(amount)
}

// group inserts thousands separators. The admin app's interface is English
// throughout, so the separator is a comma here; the site renders the same
// integer per language and is unaffected by this.
func group(n int) string {
	s := strconv.Itoa(n)
	var b strings.Builder
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// languageName spells out the delivery language. The stored value is the
// two-letter code; an unrecognised one is passed through rather than blanked,
// because a report must never invent or hide what the file says.
func languageName(code string) string {
	switch code {
	case "de":
		return "German"
	case "en":
		return "English"
	}
	return code
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
