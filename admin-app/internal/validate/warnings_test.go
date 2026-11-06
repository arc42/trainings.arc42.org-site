package validate

import (
	"slices"
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
		ID: "msa-feb-2027-en", Code: "27-02 MSA-EN", Start: "2027-02-23",
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
		{"a full date cannot be registered for", func(d *model.Date) { d.Status = "full" }, "availability"},
		{"a waitlist date is still registrable and warns about nothing", func(d *model.Date) { d.Status = "waitlist" }, ""},
		{"a booking code off the house convention", func(d *model.Date) { d.Code = "27-02-MSA-online" }, "code"},
		{"an id off the naming convention", func(d *model.Date) { d.ID = "msa-27-02-online" }, "id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := ok
			c.mutate(&d)
			got := fields(DateWarnings(d, "msa", "2026-08-16", true, model.Taken{}))
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
	if got := fields(DateWarnings(d, "msa", "2026-08-16", true, model.Taken{})); !strings.Contains(got, "country") {
		t.Errorf("expected a country warning, got %q", got)
	}
}

// Existing dates predate the naming convention, so editing one must not nag
// about an id that can never be changed anyway.
func TestIDConventionIsOnlyCheckedForNewDates(t *testing.T) {
	d := model.Date{
		ID: "msa-27-02-online", Code: "27-02 MSA-EN", Start: "2027-02-23",
		End: "2027-02-25", Language: "en", Format: "online", Status: "open",
	}
	if got := fields(DateWarnings(d, "msa", "2026-08-16", false, model.Taken{})); strings.Contains(got, "id") {
		t.Errorf("an existing date must not be nagged about its id, got %q", got)
	}
}

func TestCourseWarnings(t *testing.T) {
	ok := model.Course{
		ID: "msa", ShortTitle: "MSA",
		Title: "Mastering Software Architectures", URL: "https://www.arc42.de/info-msa/",
		BlurbEN: "Two expert trainers at all times, highly practical and pragmatic.",
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

	// msa has a hand-made card that ignores the descriptions, so the empty
	// case is checked on a course that renders through the standard card.
	bare := ok
	bare.ID, bare.BlurbEN = "mas", ""
	if got := fields(CourseWarnings(bare)); !strings.Contains(got, "blurb") {
		t.Errorf("expected an empty description warning, got %q", got)
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
	if got := fields(DateWarnings(d, "msa", "2026-08-16", true, model.Taken{})); !strings.Contains(got, "start") {
		t.Errorf("a new date in the past must warn, got %q", got)
	}
	if got := fields(DateWarnings(d, "msa", "2026-08-16", false, model.Taken{})); strings.Contains(got, "start") {
		t.Errorf("an existing past date must not be nagged, got %q", got)
	}
}

// url_en arrived after the warning rules were written, so it was the one URL
// on either form that could be plain http without anyone saying so.
func TestEnglishCourseURLIsHeldToTheSameRuleAsTheMainOne(t *testing.T) {
	c := model.Course{
		ID: "msa", ShortTitle: "MSA",
		Title: "Mastering Software Architectures", URL: "https://www.arc42.de/info-msa/",
		BlurbEN: "Two expert trainers at all times, highly practical and pragmatic.",
		URLEn:   "http://trainings.arc42.org/courses/msa/",
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

// The old message named arc42.de's form, which no longer takes registrations.
func TestStatusWarningNamesTheRightSite(t *testing.T) {
	d := model.Date{ID: "msa-feb-2027-en", Code: "27-02 MSA-EN", Start: "2027-02-23",
		End: "2027-02-25", Language: "en", Format: "online", Status: "full"}
	for _, w := range DateWarnings(d, "msa", "2026-08-16", false, model.Taken{}) {
		if w.Field == "availability" && strings.Contains(w.Message, "arc42.de") {
			t.Errorf("status warning still mentions arc42.de: %q", w.Message)
		}
	}
}

// A course the site has no hand-made card for, with nothing else wrong: the
// base for the description and label tests below.
func masCourse() model.Course {
	return model.Course{
		ID: "mas", ShortTitle: "MAS", Title: "Methodische Architektur-Skills",
		URL: "https://www.arc42.de/info-mas/", Trainers: []string{"Dr. Peter Hruschka"},
	}
}

func masDate() model.Date {
	return model.Date{
		ID: "mas-apr-2027", Code: "27-04 MAS", Start: "2027-04-13", End: "2027-04-15",
		Language: "de", Format: "online", Status: "open",
	}
}

// The site renders any course through the standard card now, so a new course
// has nothing to warn about regarding templates — and the operator could not
// have acted on such a warning anyway.
func TestNoCardTemplateWarningForANewCourse(t *testing.T) {
	c := masCourse()
	c.BlurbDE = "Methodik für Architekten."
	var ws []Warning
	ws = append(ws, CourseWarnings(c)...)
	for _, f := range model.Formats {
		d := masDate()
		d.Format = f
		if f != "online" {
			d.City, d.Country = "München", "DE"
		}
		ws = append(ws, DateWarnings(d, "mas", "2026-11-06", true, model.Taken{})...)
	}
	for _, w := range ws {
		for _, leak := range []string{"card", "template", "_includes"} {
			if strings.Contains(w.Message, leak) {
				t.Errorf("warning mentions %q: %s", leak, w)
			}
		}
	}
}

func TestCourseWithoutAnyDescriptionWarns(t *testing.T) {
	var got []Warning
	for _, w := range CourseWarnings(masCourse()) {
		if w.Label == "Description" {
			got = append(got, w)
		}
	}
	if len(got) != 1 {
		t.Fatalf("want one Description warning, got %v", got)
	}
	if want := "The home page cards for this course will show no description."; got[0].Message != want {
		t.Errorf("message = %q, want %q", got[0].Message, want)
	}
}

// A hand-made card ignores the descriptions in the data, so warning about a
// missing one would be untrue. The operator gets an info line instead.
func TestHandMadeCardCourseGetsNoteNotWarning(t *testing.T) {
	c := masCourse()
	c.ID = "msa"
	for _, w := range CourseWarnings(c) {
		if strings.HasPrefix(w.Field, "blurb") || strings.HasPrefix(w.Label, "Description") {
			t.Errorf("a hand-made card course got a description warning: %s", w)
		}
	}
	want := "This course still has its own hand-made card on the home pages. Changes to its descriptions show up there once it is switched to the standard card."
	if notes := CourseNotes(c); !slices.Contains(notes, want) {
		t.Errorf("notes = %q, want them to contain %q", notes, want)
	}
}

func TestOnlyGermanDescriptionNote(t *testing.T) {
	de := masCourse()
	de.BlurbDE = "Methodik für Architekten."
	if want := "Only a German description: the English home page shows it in German too."; !slices.Contains(CourseNotes(de), want) {
		t.Errorf("notes = %q, want %q", CourseNotes(de), want)
	}
	en := masCourse()
	en.BlurbEN = "Methods for architects."
	if want := "Only an English description: the German home page shows it in English too."; !slices.Contains(CourseNotes(en), want) {
		t.Errorf("notes = %q, want %q", CourseNotes(en), want)
	}
	both := masCourse()
	both.BlurbDE, both.BlurbEN = "Methodik für Architekten.", "Methods for architects."
	if notes := CourseNotes(both); len(notes) != 0 {
		t.Errorf("a course with both descriptions needs no note, got %q", notes)
	}
	// Neither description is a warning, not a note: it must not say both.
	if notes := CourseNotes(masCourse()); len(notes) != 0 {
		t.Errorf("a course without descriptions gets a warning, not a note: %q", notes)
	}
}

// Every rule fires here at least once, so every warning the operator can see
// is checked for a form label and for words that only mean something to a
// developer.
func TestWarningsCarryFormLabels(t *testing.T) {
	c := masCourse()
	c.ShortTitle = "Methodische Architektur-Skills für alle"
	c.URL, c.URLEn = "http://www.arc42.de/info-mas/", "http://trainings.arc42.org/courses/mas/"
	ws := CourseWarnings(c)
	c.BlurbEN = strings.Repeat("x", maxBlurb+1)
	ws = append(ws, CourseWarnings(c)...)

	dates := []model.Date{}
	past := masDate()
	past.Start, past.End = "2020-01-01", "2020-01-01" // past, and one day
	dates = append(dates, past)
	long := masDate()
	long.End = "2027-04-30"
	dates = append(dates, long)
	city := masDate()
	city.City = "München"
	dates = append(dates, city)
	nocountry := masDate()
	nocountry.Format, nocountry.City = "public", "München"
	dates = append(dates, nocountry)
	for _, st := range []string{"full", "cancelled"} {
		d := masDate()
		d.Status = st
		dates = append(dates, d)
	}
	off := masDate()
	off.Code, off.ID = "27-04-MAS", "mas-27-04"
	dates = append(dates, off)
	for _, d := range dates {
		ws = append(ws, DateWarnings(d, "mas", "2026-11-06", true, model.Taken{})...)
	}

	seen := map[string]bool{}
	for _, w := range ws {
		seen[w.Field] = true
		if w.Label == "" {
			t.Errorf("warning without a label: %s", w)
		}
		for _, leak := range []string{"schema", "_includes", ".html", "feed"} {
			if strings.Contains(w.Message, leak) {
				t.Errorf("warning mentions %q: %s", leak, w)
			}
		}
	}
	for _, f := range []string{"short_title", "url", "url_en", "blurb_en", "start", "end",
		"city", "country", "availability", "code", "id"} {
		if !seen[f] {
			t.Errorf("no warning on %q fired, so its label went unchecked", f)
		}
	}
}

func TestHTTPWarningSaysWhatToType(t *testing.T) {
	c := masCourse()
	c.BlurbDE = "Methodik für Architekten."
	c.URL = "http://www.arc42.de/info-mas/"
	for _, w := range CourseWarnings(c) {
		if w.Field == "url" {
			if w.Message != "links must start with https://" {
				t.Errorf("message = %q", w.Message)
			}
			return
		}
	}
	t.Error("no warning for a plain http course URL")
}

// A second MAS run in January 2027 is "27-01 MAS-2", because "27-01 MAS" is
// taken. The convention warnings must measure against that free variant: they
// used to tell the operator to use the very value the duplicate rule refused.
func TestConventionWarningsAdviseTheFreeVariant(t *testing.T) {
	tr := model.Trainings{Courses: []model.Course{{ID: "mas", Dates: []model.Date{
		{ID: "mas-jan-2027", Code: "27-01 MAS", Start: "2027-01-13", Language: "de"},
	}}}}
	taken := tr.TakenExcept("")
	d := model.Date{
		ID: "mas-jan-2027-2", Code: "27-01 MAS-2", Start: "2027-01-27", End: "2027-01-28",
		Language: "de", Format: "online", Status: "open",
	}
	if ws := DateWarnings(d, "mas", "2026-11-06", true, taken); len(ws) != 0 {
		t.Errorf("the free variant draws warnings: %v", ws)
	}

	d.ID, d.Code = "mas-jan-2027-x", "27-01 MAS-X"
	for _, w := range DateWarnings(d, "mas", "2026-11-06", true, taken) {
		for _, bad := range []string{`"27-01 MAS"`, `"mas-jan-2027"`} {
			if strings.Contains(w.Message, bad) {
				t.Errorf("a warning advises the taken %s: %s", bad, w)
			}
		}
		if w.Field == "code" && !strings.Contains(w.Message, `"27-01 MAS-2"`) {
			t.Errorf("code warning does not advise 27-01 MAS-2: %s", w)
		}
		if w.Field == "id" && !strings.Contains(w.Message, `"mas-jan-2027-2"`) {
			t.Errorf("id warning does not advise mas-jan-2027-2: %s", w)
		}
	}
}
