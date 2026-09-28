package labels

import "testing"

func TestDateRangeMatchesTheSite(t *testing.T) {
	cases := []struct{ start, end, lang, want string }{
		{"2026-12-01", "2026-12-04", "de", "1.-4. Dezember 2026"},
		{"2026-12-01", "2026-12-04", "en", "December 1-4, 2026"},
		{"2026-09-29", "2026-10-01", "de", "29. September - 1. Oktober 2026"},
		{"2026-09-29", "2026-10-01", "en", "September 29 - October 1, 2026"},
		{"2026-12-30", "2027-01-02", "de", "30. Dezember 2026 - 2. Januar 2027"},
		{"2026-12-30", "2027-01-02", "en", "December 30, 2026 - January 2, 2027"},
		{"2026-11-02", "2026-11-02", "de", "2. November 2026"},
		{"2026-11-02", "", "en", "November 2, 2026"},
		{"2027-03-02", "2027-03-05", "de", "2.-5. März 2027"},
		{"garbage", "", "de", "garbage"},
	}
	for _, c := range cases {
		if got := DateRange(c.start, c.end, c.lang); got != c.want {
			t.Errorf("DateRange(%q,%q,%q) = %q, want %q", c.start, c.end, c.lang, got, c.want)
		}
	}
}
