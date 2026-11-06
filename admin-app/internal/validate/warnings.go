package validate

import (
	"fmt"
	"strings"
	"time"

	"arc42-trainings-admin/internal/model"
)

// A Warning is advisory. It never blocks a save and never reaches the gate in
// Rules that stops a pull request — the operator sees it, decides, and saves
// anyway if they meant it. Every rule here encodes a house convention or a
// combination that has actually gone wrong, not a law: the December run that
// starts on 30 November is off-convention on purpose, and must stay possible.
//
// Label is what the operator sees: the visible label of the form control the
// warning is about, never a YAML key. Field stays the control's name, for
// tests and for String().
type Warning struct {
	Field   string // e.g. "availability", the name of the form control
	Label   string // e.g. "Availability", the control's visible label
	Message string
}

func (w Warning) String() string {
	if w.Field == "" {
		return w.Message
	}
	return w.Field + ": " + w.Message
}

const (
	// maxCourseDays is generous on purpose. The longest published course runs
	// four days; anything past a working week is far more likely a typo in the
	// year than a genuinely long training.
	maxCourseDays = 7
	// maxShortTitle is what fits the timeline card before it wraps badly.
	maxShortTitle = 32
	maxBlurb      = 320
)

// DateWarnings judges one date. today is passed in rather than read from the
// clock so the rules stay testable. isNew suppresses the conventions that only
// apply to something being created: a published id can never change, so
// nagging about it on every edit would be noise. taken is what the other dates
// use: the convention for a second run in one month is its free "-2" variant,
// and advising the value its sibling already has would contradict Rules.
func DateWarnings(d model.Date, courseID, today string, isNew bool, taken model.Taken) []Warning {
	var ws []Warning
	add := func(field, label, format string, args ...any) {
		ws = append(ws, Warning{Field: field, Label: label, Message: fmt.Sprintf(format, args...)})
	}

	// Only when creating. Editing a date that has already run is routine —
	// marking it cancelled, correcting a trainer — and nagging about a past
	// start every time would train the operator to ignore the warning bar.
	if isNew && d.Start != "" && d.Start < today {
		add("start", "First day", "this date started on %s, which is in the past", d.Start)
	}

	if days, ok := spanDays(d.Start, d.End); ok {
		switch {
		case days == 0:
			add("end", "Last day", "starts and ends on the same day — is it really a one-day course?")
		case days+1 > maxCourseDays:
			add("end", "Last day", "runs %d days; published courses run 2 to 4", days+1)
		}
	}

	if d.Format == "online" && d.City != "" {
		add("city", "City", "an online date normally has no city, but %q is set", d.City)
	}
	if d.Format != "online" && d.Format != "" && d.Country == "" {
		add("country", "Country", "a %s date usually names a country", d.Format)
	}

	// The rule that caused the 23-25 February 2027 date to be published
	// unbookable. Only open and waitlist dates are listed in the registration
	// form on trainings.arc42.org; waitlist registrations go through the same
	// form and the back office tells them it is a waiting-list place.
	switch d.Status {
	case "full":
		add("availability", "Availability", "%q keeps this date out of the registration form on trainings.arc42.org, and its card has no button; nobody can register or join a waiting list", d.Status)
	case "cancelled":
		add("availability", "Availability", "%q hides this date everywhere; nobody can register", d.Status)
	}
	// No warning for "few seats" on a fully booked date any more: the form
	// offers one availability choice, so that pair cannot be entered
	// (model.Date.SetAvailability), and a hand-edited one is repaired on save.

	// No card-template warning any more: a course without a hand-made card is
	// rendered by the site's data-driven card, so every date has a card.

	if want := taken.BookingCode(courseID, d.Start, d.Language); want != "" && d.Code != "" && d.Code != want {
		add("code", "Booking code", "the house convention for this course and date is %q", want)
	}
	if isNew {
		if want := taken.DateID(courseID, d.Start, d.Language); want != "" && d.ID != "" && d.ID != want {
			add("id", "Date ID", "ids are usually %q; this one will not match its siblings", want)
		}
	}
	return ws
}

// CourseWarnings judges one course.
func CourseWarnings(c model.Course) []Warning {
	var ws []Warning
	add := func(field, label, format string, args ...any) {
		ws = append(ws, Warning{Field: field, Label: label, Message: fmt.Sprintf(format, args...)})
	}

	if n := len([]rune(c.ShortTitle)); n > maxShortTitle {
		add("short_title", "Short title", "%d characters is long for the timeline card; %d or fewer fits", n, maxShortTitle)
	}
	if isPlainHTTP(c.URL) {
		add("url", "Course URL", "links must start with https://")
	}
	// url_en is optional — most courses have no English page — but when it is
	// given it is a published link like any other.
	if isPlainHTTP(c.URLEn) {
		add("url_en", "English course page", "links must start with https://")
	}

	// A hand-made card ignores the descriptions in the data, so any warning
	// about them would be untrue; CourseNotes says so instead.
	if model.HasHandMadeCard(c.ID) {
		return ws
	}
	if c.BlurbDE == "" && c.BlurbEN == "" {
		add("blurb_de", "Description", "The home page cards for this course will show no description.")
	}
	for _, b := range []struct{ field, label, text string }{
		{"blurb_de", "Description (German)", c.BlurbDE},
		{"blurb_en", "Description (English)", c.BlurbEN},
	} {
		if n := len([]rune(b.text)); n > maxBlurb {
			add(b.field, b.label, "%d characters is long for a card; two or three sentences read better", n)
		}
	}
	return ws
}

// CourseNotes are information about a course, not questions: they never gate
// a save and never ask for "Save anyway". They say how the home pages will
// show the course, where that is not what the operator would expect from the
// form alone.
func CourseNotes(c model.Course) []string {
	if model.HasHandMadeCard(c.ID) {
		// The language notes below describe the standard card; a hand-made
		// one shows neither description, so only this line is true.
		return []string{"This course still has its own hand-made card on the home pages. Changes to its descriptions show up there once it is switched to the standard card."}
	}
	// The card falls back to the other language rather than show nothing, so
	// one description is enough — but the operator should know which page
	// shows text in the "wrong" language.
	switch {
	case c.BlurbDE != "" && c.BlurbEN == "":
		return []string{"Only a German description: the English home page shows it in German too."}
	case c.BlurbEN != "" && c.BlurbDE == "":
		return []string{"Only an English description: the German home page shows it in English too."}
	}
	return nil
}

// spanDays returns the number of nights between two "YYYY-MM-DD" dates. It
// reports ok=false when either date is unparseable or end precedes start —
// that case is already a blocking error in Rules and must not warn twice.
func spanDays(start, end string) (int, bool) {
	s, err := time.Parse("2006-01-02", start)
	if err != nil {
		return 0, false
	}
	e, err := time.Parse("2006-01-02", end)
	if err != nil || e.Before(s) {
		return 0, false
	}
	return int(e.Sub(s).Hours() / 24), true
}

// isPlainHTTP reports a URL that is given but not secure. An empty URL is not
// a problem here — whether it is required at all is a blocking rule's job.
func isPlainHTTP(u string) bool {
	return strings.HasPrefix(u, "http://")
}
