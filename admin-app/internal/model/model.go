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
	Credits       string
	URL           string
	URLEn         string // optional: English detail page, https://trainings.arc42.org/courses/<id>/
	Trainers      []string
	Dates         []Date
}

type Date struct {
	ID       string
	Code     string
	Start    string // always "YYYY-MM-DD", always quoted in YAML
	End      string
	City     string // required unless Format == "online"
	Country  string // ISO 3166-1 alpha-2, always quoted ("NO" is boolean false unquoted)
	Language string // "de" | "en" — no default, ever
	Format   string // "public" | "inhouse" | "online"
	Trainers []string
	Pricing  string
	FewSeats string
	URL      string
	Status   string // "open" | "waitlist" | "full" | "cancelled"
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

// Formats, Languages and Statuses back the form <select>s and the validator.
var (
	Formats   = []string{"public", "inhouse", "online"}
	Languages = []string{"de", "en"}
	Statuses  = []string{"open", "waitlist", "full", "cancelled"}
)
