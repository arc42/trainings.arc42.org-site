package model

import (
	"strings"
	"testing"
)

// The cases that actually occur in _data/trainings.yml, plus the three edges
// that decide what the rule has to be rather than what it happens to do.
func TestSurnameKeepsWhatIdentifiesAPerson(t *testing.T) {
	cases := []struct{ full, want string }{
		{"Wolfgang Reimesch", "Reimesch"},
		{"Peter Hruschka", "Hruschka"},
		// The roster spells the same people with titles; a title must not
		// become the surname, which is why the rule reads from the right.
		{"Dr. Gernot Starke", "Starke"},
		{"Dr. Peter Hruschka", "Hruschka"},
		// A particle is part of the surname. "Linden" would be a different
		// person's name, so the lowercase run in front of the last token is
		// kept.
		{"Ruth van der Linden", "van der Linden"},
		{"Jan de Vries", "de Vries"},
		// One word is returned whole: the trainer field is free text, so this
		// is a real value (a mononym, or a guest typed with one name) and
		// blanking it would hide an assignment that exists.
		{"Prince", "Prince"},
		// Nothing in, nothing out — the caller renders the absent-value dash,
		// which must not be decided here.
		{"", ""},
		{"   ", ""},
		// Stray whitespace comes from hand-edited YAML and is not a name.
		{"  Wolfgang   Reimesch  ", "Reimesch"},
		// Two tokens starting lowercase still leave the first one alone: with
		// nothing in front of it, "van" is the only thing that could be the
		// given name.
		{"van Linden", "Linden"},
	}
	for _, c := range cases {
		if got := Surname(c.full); got != c.want {
			t.Errorf("Surname(%q) = %q, want %q", c.full, got, c.want)
		}
	}
}

// The cell joins a roster, so an empty entry must not leave ", " behind.
func TestSurnamesDropsEmptyEntries(t *testing.T) {
	got := strings.Join(Surnames([]string{"Peter Hruschka", "", "Dr. Gernot Starke"}), ", ")
	if got != "Hruschka, Starke" {
		t.Errorf("Surnames joined = %q, want %q", got, "Hruschka, Starke")
	}
	if got := Surnames(nil); len(got) != 0 {
		t.Errorf("Surnames(nil) = %v, want empty", got)
	}
}
