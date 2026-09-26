// Package labels formats money and date ranges exactly as the site does in
// _includes/money.html and _includes/training-date-label.html ("long" style).
// The mail must say what the card and the form said; the golden cases in the
// test are copied from the built site.
package labels

import (
	"strconv"
	"time"
)

var monthsDE = []string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"}
var monthsEN = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}

// Money: de "2.890 €", en "€2,890"; other currencies "2.890 CHF" / "2,890 CHF".
func Money(amount int, currency, lang string) string {
	sep := ","
	if lang == "de" {
		sep = "."
	}
	num := group(amount, sep)
	if currency == "" || currency == "EUR" {
		if lang == "de" {
			return num + " €"
		}
		return "€" + num
	}
	return num + " " + currency
}

func group(n int, sep string) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + group(-n, sep)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + sep + s[i:]
	}
	return s
}

// DateRange renders ISO dates in the site's long style:
// de "1.-4. Dezember 2026", en "December 1-4, 2026", with month and year
// spans and single days handled as in training-date-label.html. Unparsable
// input comes back verbatim rather than as an error: a mail with a raw date
// is better than no mail.
func DateRange(start, end, lang string) string {
	s, err1 := time.Parse("2006-01-02", start)
	if end == "" {
		end = start
	}
	e, err2 := time.Parse("2006-01-02", end)
	if err1 != nil || err2 != nil {
		if end != start {
			return start + " - " + end
		}
		return start
	}
	sd, ed := strconv.Itoa(s.Day()), strconv.Itoa(e.Day())
	sy, ey := strconv.Itoa(s.Year()), strconv.Itoa(e.Year())
	if lang == "de" {
		sm, em := monthsDE[s.Month()-1], monthsDE[e.Month()-1]
		switch {
		case start == end:
			return sd + ". " + sm + " " + sy
		case sy == ey && s.Month() == e.Month():
			return sd + ".-" + ed + ". " + sm + " " + sy
		case sy == ey:
			return sd + ". " + sm + " - " + ed + ". " + em + " " + sy
		default:
			return sd + ". " + sm + " " + sy + " - " + ed + ". " + em + " " + ey
		}
	}
	sm, em := monthsEN[s.Month()-1], monthsEN[e.Month()-1]
	switch {
	case start == end:
		return sm + " " + sd + ", " + sy
	case sy == ey && s.Month() == e.Month():
		return sm + " " + sd + "-" + ed + ", " + sy
	case sy == ey:
		return sm + " " + sd + " - " + em + " " + ed + ", " + sy
	default:
		return sm + " " + sd + ", " + sy + " - " + em + " " + ed + ", " + ey
	}
}
