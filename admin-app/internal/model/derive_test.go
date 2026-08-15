package model

import "testing"

func TestBookingCodeFollowsTheHouseConvention(t *testing.T) {
	cases := []struct {
		name     string
		courseID string
		start    string
		language string
		want     string
	}{
		{"german date is year-month plus the course token", "msa", "2026-12-01", "de", "26-12 MSA"},
		{"an english date carries the -EN suffix", "msa", "2026-09-29", "en", "26-09 MSA-EN"},
		{"a mixed-case token is not upper-cased", "req4arc", "2026-09-14", "de", "26-09 Req4Arc"},
		{"a course without a token falls back to its upper-case id", "newthing", "2027-04-01", "de", "27-04 NEWTHING"},
		{"without a start date there is nothing to derive", "msa", "", "de", ""},
		{"without a course there is nothing to derive", "", "2026-12-01", "de", ""},
		{"a malformed start date derives nothing rather than nonsense", "msa", "2026-12", "de", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := BookingCode(c.courseID, c.start, c.language); got != c.want {
				t.Errorf("BookingCode(%q, %q, %q) = %q, want %q",
					c.courseID, c.start, c.language, got, c.want)
			}
		})
	}
}

func TestRegistrationURLIsTheTermineAnchor(t *testing.T) {
	if got := RegistrationURL("msa-dez-2026"); got != "https://www.arc42.de/termine#msa-dez-2026" {
		t.Errorf("RegistrationURL = %q", got)
	}
	if got := RegistrationURL(""); got != "" {
		t.Errorf("an empty id must derive an empty url, got %q", got)
	}
}

func TestCodeTokenIsExposedForTheForm(t *testing.T) {
	if got := CodeToken("req4arc"); got != "Req4Arc" {
		t.Errorf("CodeToken(req4arc) = %q, want Req4Arc", got)
	}
	if got := CodeToken("improve"); got != "IMPROVE" {
		t.Errorf("CodeToken(improve) = %q, want IMPROVE", got)
	}
}
