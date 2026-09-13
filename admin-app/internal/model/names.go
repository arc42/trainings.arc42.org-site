package model

import (
	"strings"
	"unicode"
)

// Surname reduces a full name to the part that tells two rows apart:
// "Wolfgang Reimesch" → "Reimesch". A view with one line per date — the dates
// list — carries up to three trainers in a single cell, and the given names
// are the half that never distinguishes anything.
//
// The rule is "the last whitespace-separated token, plus any lowercase
// particles immediately in front of it", so "Ruth van der Linden" is
// "van der Linden" and not "Linden": the particle belongs to the name, and
// dropping it renames the person. A leading title needs no rule of its own
// ("Dr. Peter Hruschka" → "Hruschka"), and a single-word value is returned
// unchanged rather than blanked — the roster is free text (see KnownTrainers),
// so a mononym or a guest trainer entered as one word is legitimate, and
// showing it beats showing nothing.
func Surname(full string) string {
	f := strings.Fields(full)
	if len(f) == 0 {
		return ""
	}
	i := len(f) - 1
	// i > 1 leaves at least one token in front of the surname, so a value that
	// begins with a lowercase word cannot be swallowed whole.
	for i > 1 && startsLower(f[i-1]) {
		i--
	}
	return strings.Join(f[i:], " ")
}

// Surnames maps Surname over a roster, dropping entries that reduce to
// nothing so a blank name in the file cannot render as a stray separator.
func Surnames(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if s := Surname(n); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func startsLower(token string) bool {
	for _, r := range token {
		return unicode.IsLower(r)
	}
	return false
}
