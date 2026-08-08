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

// Formats, Languages and Statuses back the form <select>s and the validator.
var (
	Formats   = []string{"public", "inhouse", "online"}
	Languages = []string{"de", "en"}
	Statuses  = []string{"open", "waitlist", "full", "cancelled"}
)
