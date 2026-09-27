package model

import "testing"

// Every status the form offers needs words. A status without a label would
// render as a bare token in the dropdown, which is exactly what this replaces.
func TestEveryStatusHasALabel(t *testing.T) {
	for _, s := range Statuses {
		if StatusLabel(s) == s || StatusLabel(s) == "" {
			t.Errorf("status %q has no label", s)
		}
	}
	if got := StatusLabel("waitlist"); got != "fully booked, waiting list available" {
		t.Errorf("waitlist label = %q", got)
	}
	if got := StatusLabel("bogus"); got != "bogus" {
		t.Errorf("unknown status must fall back to itself, got %q", got)
	}
}
