package validate

import (
	"strings"
	"testing"

	"arc42-trainings-admin/internal/model"
)

// fields returns the Field of every warning, so a test can assert which rules
// fired without pinning the exact wording.
func fields(ws []Warning) string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Field)
	}
	return strings.Join(out, ",")
}

func TestDateWarningsFireOnImplausibleEntries(t *testing.T) {
	// A date that should raise nothing at all, used as the base for each case.
	ok := model.Date{
		ID: "msa-feb-2027", Code: "27-02 MSA-EN", Start: "2027-02-23",
		End: "2027-02-25", Language: "en", Format: "online", Status: "open",
	}

	cases := []struct {
		name   string
		mutate func(d *model.Date)
		want   string // the Field expected to appear, "" for none
	}{
		{"a clean date warns about nothing", func(d *model.Date) {}, ""},
		{"a start date in the past", func(d *model.Date) { d.Start, d.End = "2020-01-01", "2020-01-03" }, "start"},
		{"a course that starts and ends the same day", func(d *model.Date) { d.End = d.Start }, "end"},
		{"a course running longer than a working week", func(d *model.Date) { d.End = "2027-03-08" }, "end"},
		{"an online date carrying a city", func(d *model.Date) { d.City = "München" }, "city"},
		{"a status that hides the date from booking", func(d *model.Date) { d.Status = "waitlist" }, "status"},
		{"a booking code off the house convention", func(d *model.Date) { d.Code = "27-02-MSA-online" }, "code"},
		{"an id off the naming convention", func(d *model.Date) { d.ID = "msa-27-02-online" }, "id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := ok
			c.mutate(&d)
			got := fields(DateWarnings(d, "msa", "2026-08-16", true))
			if c.want == "" {
				if got != "" {
					t.Errorf("expected no warnings, got %q", got)
				}
				return
			}
			if !strings.Contains(got, c.want) {
				t.Errorf("expected a warning on %q, got %q", c.want, got)
			}
		})
	}
}

// A non-online date needs a country, but the city is already a blocking rule,
// so the two must not both fire for the same omission.
func TestPublicDateWithoutCountryWarns(t *testing.T) {
	d := model.Date{
		ID: "msa-feb-2027", Code: "27-02 MSA", Start: "2027-02-23", End: "2027-02-25",
		Language: "de", Format: "public", City: "München", Status: "open",
	}
	if got := fields(DateWarnings(d, "msa", "2026-08-16", true)); !strings.Contains(got, "country") {
		t.Errorf("expected a country warning, got %q", got)
	}
}

// The combination that shipped yesterday: seats advertised on a date nobody
// can book.
func TestFewSeatsOnAnUnbookableDateWarns(t *testing.T) {
	d := model.Date{
		ID: "msa-feb-2027", Code: "27-02 MSA-EN", Start: "2027-02-23", End: "2027-02-25",
		Language: "en", Format: "online", Status: "full", SeatsLimited: true,
	}
	if got := fields(DateWarnings(d, "msa", "2026-08-16", true)); !strings.Contains(got, "seats_limited") {
		t.Errorf("expected a seats_limited warning, got %q", got)
	}
}

// Existing dates predate the naming convention, so editing one must not nag
// about an id that can never be changed anyway.
func TestIDConventionIsOnlyCheckedForNewDates(t *testing.T) {
	d := model.Date{
		ID: "msa-27-02-online", Code: "27-02 MSA-EN", Start: "2027-02-23",
		End: "2027-02-25", Language: "en", Format: "online", Status: "open",
	}
	if got := fields(DateWarnings(d, "msa", "2026-08-16", false)); strings.Contains(got, "id") {
		t.Errorf("an existing date must not be nagged about its id, got %q", got)
	}
}

func TestCourseWarnings(t *testing.T) {
	ok := model.Course{
		ID: "msa", ShortTitle: "Mastering SW Architectures",
		Title: "Mastering Software Architectures", URL: "https://www.arc42.de/info-msa/",
		Blurb: "Two expert trainers at all times, highly practical and pragmatic.",
	}
	if got := fields(CourseWarnings(ok)); got != "" {
		t.Errorf("a clean course warns about nothing, got %q", got)
	}

	long := ok
	long.ShortTitle = "Mastering Software Architectures the Complete Edition"
	if got := fields(CourseWarnings(long)); !strings.Contains(got, "short_title") {
		t.Errorf("expected a short_title warning, got %q", got)
	}

	insecure := ok
	insecure.URL = "http://www.arc42.de/info-msa/"
	if got := fields(CourseWarnings(insecure)); !strings.Contains(got, "url") {
		t.Errorf("expected an http url warning, got %q", got)
	}

	bare := ok
	bare.Blurb = ""
	if got := fields(CourseWarnings(bare)); !strings.Contains(got, "blurb") {
		t.Errorf("expected an empty blurb warning, got %q", got)
	}
}

// The whole point of the severity split: a warning must never reach the gate
// that blocks a pull request.
func TestWarningsAreNotProblems(t *testing.T) {
	t7 := model.Trainings{Courses: []model.Course{{
		ID: "msa", Dates: []model.Date{{
			ID: "msa-27-02-online", Code: "27-02-MSA-online", Start: "2020-01-01",
			End: "2020-01-03", Language: "en", Format: "online", Status: "waitlist",
		}},
	}}}
	if problems := Rules(t7); len(problems) > 0 {
		t.Errorf("advisory conditions must not block: %v", problems)
	}
}

// Editing a date that has already happened is routine — marking it cancelled,
// fixing a trainer name. Only a date being created can sensibly be warned
// about starting in the past.
func TestPastStartIsOnlyWarnedForNewDates(t *testing.T) {
	d := model.Date{
		ID: "msa-jan-2026", Code: "26-01 MSA", Start: "2026-01-01", End: "2026-01-02",
		City: "München", Country: "DE", Language: "de", Format: "public", Status: "open",
	}
	if got := fields(DateWarnings(d, "msa", "2026-08-16", true)); !strings.Contains(got, "start") {
		t.Errorf("a new date in the past must warn, got %q", got)
	}
	if got := fields(DateWarnings(d, "msa", "2026-08-16", false)); strings.Contains(got, "start") {
		t.Errorf("an existing past date must not be nagged, got %q", got)
	}
}

// url_en arrived after the warning rules were written, so it was the one URL
// on either form that could be plain http without anyone saying so.
func TestEnglishCourseURLIsHeldToTheSameRuleAsTheMainOne(t *testing.T) {
	c := model.Course{
		ID: "msa", ShortTitle: "Mastering SW Architectures",
		Title: "Mastering Software Architectures", URL: "https://www.arc42.de/info-msa/",
		Blurb: "Two expert trainers at all times, highly practical and pragmatic.",
		URLEn: "http://trainings.arc42.org/courses/msa/",
	}
	if got := fields(CourseWarnings(c)); !strings.Contains(got, "url_en") {
		t.Errorf("expected an http url_en warning, got %q", got)
	}
	// Empty is the normal case — most courses have no English page.
	c.URLEn = ""
	if got := fields(CourseWarnings(c)); strings.Contains(got, "url_en") {
		t.Errorf("an absent English page must not warn, got %q", got)
	}
}
