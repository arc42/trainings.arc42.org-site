package model

import "testing"

// Every availability the form offers needs words. One without a label would
// render as a bare token in the dropdown, which is exactly what this replaces.
func TestEveryAvailabilityHasALabel(t *testing.T) {
	for _, a := range Availabilities {
		if AvailabilityLabel(a) == a || AvailabilityLabel(a) == "" {
			t.Errorf("availability %q has no label", a)
		}
	}
	if got := AvailabilityLabel("waitlist"); got != "fully booked, waiting list available" {
		t.Errorf("waitlist label = %q", got)
	}
	if got := AvailabilityLabel("bogus"); got != "bogus" {
		t.Errorf("unknown availability must fall back to itself, got %q", got)
	}
}

// The form offers one choice; the file keeps two fields, because the feed
// publishes both and its consumers read them (ADR-0004). Each choice must map
// to exactly one stored pair, and read back as itself.
func TestAvailabilityIsStoredAsStatusAndSeatsLimited(t *testing.T) {
	for _, c := range []struct {
		availability string
		status       string
		seatsLimited bool
	}{
		{"open", "open", false},
		{"few seats", "open", true},
		{"waitlist", "waitlist", false},
		{"full", "full", false},
		{"cancelled", "cancelled", false},
	} {
		d := Date{Status: "full", SeatsLimited: true}
		d.SetAvailability(c.availability)
		if d.Status != c.status || d.SeatsLimited != c.seatsLimited {
			t.Errorf("%q stored as status=%q seats_limited=%v, want %q/%v",
				c.availability, d.Status, d.SeatsLimited, c.status, c.seatsLimited)
		}
		if got := d.Availability(); got != c.availability {
			t.Errorf("%q read back as %q", c.availability, got)
		}
	}
}

// "Fully booked" and "few seats left" contradict each other. Such a pair can
// only come from a hand edit or from before the form had one choice; it reads
// as the status, and the next save drops the seat flag.
func TestAContradictoryPairReadsAsItsStatusAndHealsOnSave(t *testing.T) {
	d := Date{Status: "waitlist", SeatsLimited: true}
	if got := d.Availability(); got != "waitlist" {
		t.Fatalf("availability = %q, want waitlist", got)
	}
	d.SetAvailability(d.Availability())
	if d.SeatsLimited {
		t.Error("saving a waitlist date kept seats_limited")
	}
}
