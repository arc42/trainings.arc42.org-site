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

func TestDateIDFollowsTheCourseMonthYearConvention(t *testing.T) {
	cases := []struct{ name, courseID, start, language, want string }{
		{"course, month and year", "msa", "2027-02-23", "de", "msa-feb-2027"},
		{"december is dec, not the german dez", "improve", "2027-12-01", "de", "improve-dec-2027"},
		{"a course spanning new year takes its start month", "adoc", "2027-11-30", "de", "adoc-nov-2027"},
		{"an english date carries the -en suffix", "msa", "2027-09-28", "en", "msa-sep-2027-en"},
		{"without a start there is nothing to derive", "msa", "", "de", ""},
		{"a malformed start derives nothing", "msa", "2027-2-3", "de", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DateID(c.courseID, c.start, c.language); got != c.want {
				t.Errorf("DateID(%q, %q, %q) = %q, want %q", c.courseID, c.start, c.language, got, c.want)
			}
		})
	}
}

// A German and an English run in the same month is a real schedule (MSA,
// September 2027). Their derived ids must differ, or the second is rejected.
func TestDateIDSeparatesLanguagesInTheSameMonth(t *testing.T) {
	de, en := DateID("msa", "2027-09-14", "de"), DateID("msa", "2027-09-28", "en")
	if de == en {
		t.Errorf("both runs derive %q", de)
	}
}
