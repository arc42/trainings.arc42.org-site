package model

import "testing"

func TestBookingCodeFollowsTheHouseConvention(t *testing.T) {
	msa := Course{ID: "msa", ShortTitle: "MSA"}
	cases := []struct {
		name     string
		course   Course
		start    string
		language string
		want     string
	}{
		{"german date is year-month plus the course token", msa, "2026-12-01", "de", "26-12 MSA"},
		{"an english date carries the -EN suffix", msa, "2026-09-29", "en", "26-09 MSA-EN"},
		{"a mixed-case short title is the token as spelled", Course{ID: "req4arc", ShortTitle: "Req4Arc"}, "2026-09-14", "de", "26-09 Req4Arc"},
		{"a course without a short title falls back to its upper-case id", Course{ID: "newthing"}, "2027-04-01", "de", "27-04 NEWTHING"},
		{"without a start date there is nothing to derive", msa, "", "de", ""},
		{"without a course there is nothing to derive", Course{}, "2026-12-01", "de", ""},
		{"a malformed start date derives nothing rather than nonsense", msa, "2026-12", "de", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := BookingCode(c.course, c.start, c.language); got != c.want {
				t.Errorf("BookingCode(%+v, %q, %q) = %q, want %q",
					c.course, c.start, c.language, got, c.want)
			}
		})
	}
}

func TestRegistrationURLIsTheTrainingsAnchor(t *testing.T) {
	if got := RegistrationURL("msa-dez-2026"); got != "https://trainings.arc42.org/#msa-dez-2026" {
		t.Errorf("RegistrationURL = %q", got)
	}
	if got := RegistrationURL(""); got != "" {
		t.Errorf("an empty id must derive an empty url, got %q", got)
	}
}

// No course is a special case: the token is the short title as spelled, when
// it spells the id, and the upper-cased id otherwise.
func TestCodeTokenComesFromTheShortTitle(t *testing.T) {
	for _, c := range []struct{ id, short, want string }{
		{"msa", "MSA", "MSA"},
		{"improve", "IMPROVE", "IMPROVE"},
		{"req4arc", "Req4Arc", "Req4Arc"},
		{"adoc", "ADOC", "ADOC"},
		{"maispec", "MAISPEC", "MAISPEC"},
		{"mai-spec", "MAI-Spec", "MAI-Spec"},
		// A space cannot stand in a code: it separates month and course.
		{"mai-spec", "MAI Spec", "MAI-SPEC"},
		// A short title edited after the id was fixed: codes keep the id.
		{"msa", "MSA Plus", "MSA"},
		{"pruefung", "Prüfung", "PRUEFUNG"},
		{"x", "", "X"},
	} {
		if got := CodeToken(Course{ID: c.id, ShortTitle: c.short}); got != c.want {
			t.Errorf("CodeToken(%s, %q) = %q, want %q", c.id, c.short, got, c.want)
		}
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

// Two runs of one course in the same month and language derive the same id
// and code; the second gets the first free "-2", "-3" variant, the way an
// English run gets "-EN".
func TestNextFreeAppendsTheFirstFreeSuffix(t *testing.T) {
	cases := []struct {
		name  string
		taken []string
		want  string
	}{
		{"nothing taken: the base itself", nil, "mas-jan-2027"},
		{"base taken: -2", []string{"mas-jan-2027"}, "mas-jan-2027-2"},
		{"base and -2 taken: -3", []string{"mas-jan-2027", "mas-jan-2027-2"}, "mas-jan-2027-3"},
		{"only -2 taken: the base is still free", []string{"mas-jan-2027-2"}, "mas-jan-2027"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			taken := map[string]bool{}
			for _, v := range c.taken {
				taken[v] = true
			}
			if got := NextFree("mas-jan-2027", taken); got != c.want {
				t.Errorf("NextFree = %q, want %q", got, c.want)
			}
		})
	}
	if got := NextFree("", map[string]bool{"": true}); got != "" {
		t.Errorf("an empty base derives %q, want nothing", got)
	}
}

func TestTakenDerivesTheFreeIDAndCode(t *testing.T) {
	tr := Trainings{Courses: []Course{{ID: "mas", Dates: []Date{
		{ID: "mas-jan-2027", Code: "27-01 MAS", Start: "2027-01-13", Language: "de"},
	}}}}

	tk := tr.TakenExcept("")
	if got := tk.DateID("mas", "2027-01-27", "de"); got != "mas-jan-2027-2" {
		t.Errorf("DateID = %q, want mas-jan-2027-2", got)
	}
	if got := tk.BookingCode("mas", "2027-01-27", "de"); got != "27-01 MAS-2" {
		t.Errorf("BookingCode = %q, want 27-01 MAS-2", got)
	}
	// Another language in the same month never collided in the first place.
	if got := tk.BookingCode("mas", "2027-01-27", "en"); got != "27-01 MAS-EN" {
		t.Errorf("BookingCode(en) = %q, want 27-01 MAS-EN", got)
	}

	// The course's own spelling, through the id alone.
	mixed := Trainings{Courses: []Course{{ID: "req4arc", ShortTitle: "Req4Arc"}}}.TakenExcept("")
	if got := mixed.BookingCode("req4arc", "2027-03-01", "de"); got != "27-03 Req4Arc" {
		t.Errorf("BookingCode(req4arc) = %q, want 27-03 Req4Arc", got)
	}
	if got := mixed.CodeMask("req4arc"); got != "YY-MM Req4Arc" {
		t.Errorf("CodeMask(req4arc) = %q, want YY-MM Req4Arc", got)
	}

	// The date being edited does not block its own values, or editing it
	// would suggest it move to "-2".
	own := tr.TakenExcept("mas-jan-2027")
	if got := own.BookingCode("mas", "2027-01-13", "de"); got != "27-01 MAS" {
		t.Errorf("own code = %q, want 27-01 MAS", got)
	}
	if got := own.DateID("mas", "2027-01-13", "de"); got != "mas-jan-2027" {
		t.Errorf("own id = %q, want mas-jan-2027", got)
	}
}
