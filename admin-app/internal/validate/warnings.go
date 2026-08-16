package validate

import (
	"fmt"
	"time"

	"arc42-trainings-admin/internal/model"
)

// A Warning is advisory. It never blocks a save and never reaches the gate in
// Rules that stops a pull request — the operator sees it, decides, and saves
// anyway if they meant it. Every rule here encodes a house convention or a
// combination that has actually gone wrong, not a law: the December run that
// starts on 30 November is off-convention on purpose, and must stay possible.
type Warning struct {
	Field   string // e.g. "status", matching the form control it belongs to
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
// nagging about it on every edit would be noise.
func DateWarnings(d model.Date, courseID, today string, isNew bool) []Warning {
	var ws []Warning
	add := func(field, format string, args ...any) {
		ws = append(ws, Warning{Field: field, Message: fmt.Sprintf(format, args...)})
	}

	// Only when creating. Editing a date that has already run is routine —
	// marking it cancelled, correcting a trainer — and nagging about a past
	// start every time would train the operator to ignore the warning bar.
	if isNew && d.Start != "" && d.Start < today {
		add("start", "this date started on %s, which is in the past", d.Start)
	}

	if days, ok := spanDays(d.Start, d.End); ok {
		switch {
		case days == 0:
			add("end", "starts and ends on the same day — is it really a one-day course?")
		case days+1 > maxCourseDays:
			add("end", "runs %d days; published courses run 2 to 4", days+1)
		}
	}

	if d.Format == "online" && d.City != "" {
		add("city", "an online date normally has no city, but %q is set", d.City)
	}
	if d.Format != "online" && d.Format != "" && d.Country == "" {
		add("country", "a %s date usually names a country", d.Format)
	}

	// The rule that caused the 23-25 February 2027 date to be published
	// unbookable: anything but "open" is filtered out of the registration
	// dropdown on arc42.de, silently.
	if d.Status != "" && d.Status != "open" {
		add("status", "%q keeps this date out of the registration form on arc42.de — nobody can book it", d.Status)
	}
	if d.FewSeats != "" && (d.Status == "full" || d.Status == "cancelled") {
		add("few_seats", "seats are advertised on a date whose status is %q", d.Status)
	}

	if want := model.BookingCode(courseID, d.Start, d.Language); want != "" && d.Code != "" && d.Code != want {
		add("code", "the house convention for this course and date is %q", want)
	}
	if isNew {
		if want := model.DateID(courseID, d.Start); want != "" && d.ID != "" && d.ID != want {
			add("id", "ids are usually %q; this one will not match its siblings", want)
		}
	}
	return ws
}

// CourseWarnings judges one course.
func CourseWarnings(c model.Course) []Warning {
	var ws []Warning
	add := func(field, format string, args ...any) {
		ws = append(ws, Warning{Field: field, Message: fmt.Sprintf(format, args...)})
	}

	if n := len([]rune(c.ShortTitle)); n > maxShortTitle {
		add("short_title", "%d characters is long for the timeline card; %d or fewer fits", n, maxShortTitle)
	}
	if c.URL != "" && len(c.URL) >= 7 && c.URL[:7] == "http://" {
		add("url", "the feed schema requires https, and this is plain http")
	}
	if c.Blurb == "" {
		add("blurb", "without a blurb the timeline card shows the course name alone")
	} else if n := len([]rune(c.Blurb)); n > maxBlurb {
		add("blurb", "%d characters is long for a card; two or three sentences read better", n)
	}
	return ws
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
