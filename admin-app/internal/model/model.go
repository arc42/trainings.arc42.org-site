// Package model holds the training-dates domain types. Field names and
// optionality mirror api/trainings.schema.json exactly.
package model

type Trainings struct {
	Courses []Course
}

type Course struct {
	ID            string
	ShortTitle    string
	Title         string
	Blurb         string
	Certification string
	CreditPoints  *CreditPoints
	URL           string
	URLEn         string // optional: English detail page, https://trainings.arc42.org/courses/<id>/
	Trainers      []string
	Dates         []Date
}

type Date struct {
	ID           string
	Code         string
	Start        string // always "YYYY-MM-DD", always quoted in YAML
	End          string
	City         string // required unless Format == "online"
	Country      string // ISO 3166-1 alpha-2, always quoted ("NO" is boolean false unquoted)
	Language     string // "de" | "en" — no default, ever
	Format       string // "public" | "inhouse" | "online"
	Trainers     []string
	Price        *Price
	SeatsLimited bool
	URL          string
	Status       string // "open" | "waitlist" | "full" | "cancelled"
}

// Price is the canonical price for one date. It replaced a free-text German
// sentence ("Frühbucherpreis bei Anmeldung bis 8. August 2026: € 2690, ...")
// that broke in two ways: it was rendered verbatim on the English pages, and
// the deadline inside it was invisible to every machine, so the site went on
// advertising an expired offer for weeks.
//
// Amount is whole currency units as an integer (2890 means EUR 2890). arc42
// has never priced a training in cents, and integers keep the value
// comparable and formattable instead of only printable. The wording and the
// thousands separator belong to whoever renders it: _includes/price-label.html
// on the site, its own templates on each consumer.
type Price struct {
	Amount   int
	Currency string // ISO 4217, in practice always "EUR"
	// Alumni is the reduced price for former participants. Unlike EarlyBird it
	// never expires, so it is always rendered when present.
	Alumni    int
	EarlyBird *EarlyBird
}

// EarlyBird is an offer that lapses on its own. Nothing has to delete it when
// Until passes: the site's price-label include and the published feed both
// stop rendering it, so an expired offer is inert rather than wrong.
type EarlyBird struct {
	Amount int
	Until  string // "YYYY-MM-DD", always quoted in YAML
}

// CreditPoints are iSAQB credit points by category, replacing another
// hand-written German string ("20 methodische und 10 technische Punkte") that
// also leaked onto the English pages. Zero means "not in this category" and is
// omitted; a course with no credits at all has a nil *CreditPoints.
type CreditPoints struct {
	Methodical    int
	Technical     int
	Communication int
}

// Empty reports whether no category carries any points, in which case the key
// is left out of the YAML entirely rather than written as an empty mapping.
func (c *CreditPoints) Empty() bool {
	return c == nil || (c.Methodical == 0 && c.Technical == 0 && c.Communication == 0)
}

// KnownTrainers is the roster offered as checkboxes on both forms. It is a
// convenience, not a constraint: any other name can still be typed in, because
// guest trainers happen and a closed list would block a real booking.
//
// Note the titles. Dates already in _data/trainings.yml were written without
// them ("Peter Hruschka"), so an existing entry will not match one of these and
// shows up in the free-text field instead, preserved exactly as stored. Nothing
// is silently rewritten — changing a published trainer name is the operator's
// call, not a side effect of opening a form.
var KnownTrainers = []string{
	"Dr. Carola Lilienthal",
	"Dr. Peter Hruschka",
	"Dr. Gernot Starke",
	"Wolfgang Reimesch",
}

// Card templates in the SITE repository, mirrored here so the forms can warn
// before a date is published rather than after.
//
// The site renders each date through _includes/timeline_<type>.html, where the
// type is DERIVED and not chosen: the course id, plus "_online" when the format
// is online. _includes/timeline_course.html dispatches on that name and falls
// through to an HTML comment for a type it does not know. The fallback is
// silent on purpose, so one missing template cannot break every other card, but
// it means a perfectly valid date can publish to the feed and render as
// nothing. improve-apr-2027 did exactly that: valid, in the feed, invisible on
// both home pages, unbookable.
//
// This app cannot see the site's _includes directory, so these two sets are a
// copy and can go stale. That is deliberate and low-risk: being wrong here
// costs a missing or spurious ADVISORY warning, never a wrong file. The
// authoritative check is scripts/validate_trainings.rb, which reads the actual
// directory and fails the pull request.
//
// Adding a course? Add both templates to the site and both ids here.
var (
	coursesWithPublicCard = map[string]bool{"msa": true, "improve": true, "req4arc": true, "adoc": true}
	coursesWithOnlineCard = map[string]bool{"msa": true, "improve": true, "req4arc": true, "adoc": true}
)

// HasCardTemplate reports whether the site can render a date of this course and
// format. An empty courseID or format is treated as renderable: the form has
// other, better complaints about those.
func HasCardTemplate(courseID, format string) bool {
	if courseID == "" || format == "" {
		return true
	}
	if format == "online" {
		return coursesWithOnlineCard[courseID]
	}
	return coursesWithPublicCard[courseID]
}

// HasAnyCardTemplate reports whether the site knows this course at all.
func HasAnyCardTemplate(courseID string) bool {
	if courseID == "" {
		return true
	}
	return coursesWithPublicCard[courseID] || coursesWithOnlineCard[courseID]
}

// Formats and Languages back the form <select>s; Statuses is what the
// validator accepts in the file.
var (
	Formats   = []string{"public", "inhouse", "online"}
	Languages = []string{"de", "en"}
	Statuses  = []string{"open", "waitlist", "full", "cancelled"}
)

// Availabilities back the one "Availability" <select> on the date form. The
// file and the feed keep two fields for it, status and seats_limited, because
// four consumer sites read both (ADR-0004: an existing field never changes).
// The form used to offer them separately, and that allowed "fully booked" and
// "only a few seats left" on the same date. One choice cannot contradict
// itself. Ordered from most to least bookable, which is how a date moves.
var Availabilities = []string{"open", "few seats", "waitlist", "full", "cancelled"}

// availabilityLabels says what each choice does on the site, in the words the
// operator thinks in. The card and form wording (DE/EN) lives in the Liquid
// includes; this is only the admin app's own explanation.
var availabilityLabels = map[string]string{
	"open":      "open for registration",
	"few seats": "open, only a few seats left",
	"waitlist":  "fully booked, waiting list available",
	"full":      "fully booked, no waiting list",
	"cancelled": "cancelled, hidden everywhere",
}

// AvailabilityLabel returns the explanation for a choice, or the choice
// itself if there is none, so an unexpected value still renders as something.
func AvailabilityLabel(a string) string {
	if l, ok := availabilityLabels[a]; ok {
		return l
	}
	return a
}

// Availability reads the stored pair as the one choice the form offers. The
// seat flag counts only on an open date: on any other status it contradicts
// the status, which wins.
func (d Date) Availability() string {
	if d.Status == "open" && d.SeatsLimited {
		return "few seats"
	}
	return d.Status
}

// SetAvailability stores a choice as the pair. Every choice but "few seats"
// clears the seat flag, so saving a date also repairs a contradictory pair
// left by a hand edit. An unknown choice lands in Status unchanged, where the
// validator rejects it like any other unknown status.
func (d *Date) SetAvailability(a string) {
	d.SeatsLimited = a == "few seats"
	if d.SeatsLimited {
		d.Status = "open"
		return
	}
	d.Status = a
}
