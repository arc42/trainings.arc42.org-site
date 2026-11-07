package model

import (
	"regexp"
	"strconv"
	"strings"
)

// The two identifiers on a date carry the same three facts — course, year,
// month — but serve different readers, so the file keeps both:
//
//   - ID is the anchor in https://trainings.arc42.org/#<id> and the key in
//     api/trainings.json. It must never change once published, or links rot.
//   - Code is the Formspark <option value>, so it reaches the back office
//     verbatim in a registration mail. Humans read it; hence the space and the
//     mixed case.
//
// Typing both by hand is what is redundant, not storing both. The functions
// here derive ID, Code and URL from course, first day and language, so the
// operator types neither. Both remain overridable: the derivation reproduces
// 17 of the 19 published codes exactly, and one of the other two (a course
// starting 2027-11-30 but booked as "27-12 MSA") is a genuine exception rather
// than a typo — which is why the form offers an override instead of no field.

// tokenShape is a short title that can stand in a booking code as it is:
// letters and digits, words joined by single hyphens. No space, because the
// space in "26-12 MSA" separates the month from the course.
var tokenShape = regexp.MustCompile(`^[A-Za-z0-9]+(-[A-Za-z0-9]+)*$`)

// CodeToken is the course's abbreviation as it appears inside a booking code.
// It is the short title, spelled as the operator spelled it ("Req4Arc"), as
// long as the short title is the course id in other letter case: the case of
// every course there is, because the id is made from the short title. When it
// is not (a short title edited after the id was fixed, or one with a space)
// the token is the id upper-cased, so new codes do not wander with the
// wording. There used to be a table here with Req4Arc as its one entry; the
// data already said the same.
func CodeToken(c Course) string {
	if tokenShape.MatchString(c.ShortTitle) && strings.ToLower(c.ShortTitle) == c.ID {
		return c.ShortTitle
	}
	return strings.ToUpper(c.ID)
}

// BookingCode derives the Formspark option value: "26-12 MSA", with an "-EN"
// suffix when the course is taught in English. It returns "" when it has too
// little to work with, so a half-filled form prefills nothing rather than
// something wrong.
func BookingCode(c Course, start, language string) string {
	return bookingCode(CodeToken(c), start, language)
}

func bookingCode(token, start, language string) string {
	if token == "" || len(start) != len("YYYY-MM-DD") {
		return ""
	}
	year, month := start[2:4], start[5:7]
	code := year + "-" + month + " " + token
	if language == "en" {
		code += "-EN"
	}
	return code
}

// months indexes the three-letter forms used in date ids. English throughout:
// the published ids are already mostly English ("oct", "mar", "sep") with
// "dez" the lone German holdout, and an id never changes once published, so
// only new ones are affected by settling on one language.
var months = [...]string{
	"jan", "feb", "mar", "apr", "may", "jun",
	"jul", "aug", "sep", "oct", "nov", "dec",
}

// DateID derives the anchor id: "msa-feb-2027", from the course and the month
// the course starts in, with "-en" when it is taught in English. The suffix is
// not decoration: a German and an English run of one course in the same month
// derived the same id without it, and the second one failed as a duplicate. It
// mirrors the "-EN" of BookingCode, which never had that collision.
func DateID(courseID, start, language string) string {
	if courseID == "" || len(start) != len("YYYY-MM-DD") {
		return ""
	}
	m := (start[5]-'0')*10 + (start[6] - '0')
	if m < 1 || m > 12 {
		return ""
	}
	id := courseID + "-" + months[m-1] + "-" + start[0:4]
	if language == "en" {
		id += "-en"
	}
	return id
}

// NextFree returns base, or when another date already uses it, the first of
// base-2, base-3, … that nobody does. Two runs of one course in the same month
// and language are a real schedule (MAS, January 2027), and both derivations
// above give them the same value: the second used to be blocked as a
// duplicate, and the convention warnings then advised the taken value. A
// numbered suffix mirrors the "-EN" one, so the code still reads as the month.
func NextFree(base string, taken map[string]bool) string {
	if base == "" || !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		if v := base + "-" + strconv.Itoa(n); !taken[v] {
			return v
		}
	}
}

// Taken is the date ids and booking codes the file already uses, except those
// of the date being edited: its own values are not a collision. Tokens is
// each course's CodeToken by id, so that what derives a date's code from a
// course id alone (the date form, its warnings) spells it like the course.
type Taken struct {
	IDs, Codes map[string]bool
	Tokens     map[string]string
}

// TakenExcept collects every date's id and code, leaving out the date with id
// except ("" while creating one).
func (t Trainings) TakenExcept(except string) Taken {
	tk := Taken{IDs: map[string]bool{}, Codes: map[string]bool{}, Tokens: map[string]string{}}
	for _, c := range t.Courses {
		tk.Tokens[c.ID] = CodeToken(c)
		for _, d := range c.Dates {
			if except != "" && d.ID == except {
				continue
			}
			tk.IDs[d.ID], tk.Codes[d.Code] = true, true
		}
	}
	return tk
}

// DateID is the package's DateID, moved on to the first free variant.
func (tk Taken) DateID(courseID, start, language string) string {
	return NextFree(DateID(courseID, start, language), tk.IDs)
}

// BookingCode is the package's BookingCode for the course with this id, moved
// on to the first free variant.
func (tk Taken) BookingCode(courseID, start, language string) string {
	return NextFree(bookingCode(tk.token(courseID), start, language), tk.Codes)
}

// token is the course's CodeToken, or for an id no course has (yet), the id
// upper-cased: the same fallback CodeToken uses.
func (tk Taken) token(courseID string) string {
	if t, ok := tk.Tokens[courseID]; ok {
		return t
	}
	return strings.ToUpper(courseID)
}

// RegistrationURL derives the public link for a date. Every published date
// points at the same anchored page, so this is the whole rule.
//
// That page is the English trainings home, not arc42.de/termine: all
// registrations go through trainings.arc42.org, and the sites that render this
// link (docs, faq, arc42.org, examples) are English. arc42.de links its own
// /termine anchors and never reads this field.
func RegistrationURL(dateID string) string {
	if dateID == "" {
		return ""
	}
	return "https://trainings.arc42.org/#" + dateID
}

// Defaults are the values a new date for a course starts with. They come from
// the course's most recent date, because a repeat run is overwhelmingly the
// same city at the same price — the two most tedious things to retype.
// Nothing here is identity: id, code, url, start and end are what make the new
// date new, and are never carried over.
//
// Price carries the AMOUNTS but never the early-bird deadline. A deadline is
// tied to the run it belongs to, so copying it forward would seed the next
// date with an offer that expires before anybody can take it — which is a
// tidier version of the bug that motivated this whole shape. The operator sets
// the new deadline deliberately, or leaves the offer off.
type Defaults struct {
	City     string
	Country  string
	Price    *Price
	Trainers []string
}

// DefaultsFor reads a course's most recent date by start, falling back to the
// course's own trainer roster when no date names one.
func DefaultsFor(c Course) Defaults {
	var latest *Date
	for i := range c.Dates {
		if latest == nil || c.Dates[i].Start > latest.Start {
			latest = &c.Dates[i]
		}
	}
	d := Defaults{Trainers: c.Trainers}
	if latest == nil {
		return d
	}
	d.City, d.Country = latest.City, latest.Country
	if latest.Price != nil {
		p := *latest.Price
		p.EarlyBird = nil
		d.Price = &p
	}
	if len(latest.Trainers) > 0 {
		d.Trainers = latest.Trainers
	}
	return d
}

// IDMask and CodeMask describe the *shape* of the two derived identifiers for
// a given course. They exist because the placeholders used to be frozen MSA
// examples, which were wrong for every other course and, worse, looked enough
// like stored values that an operator could try to delete one.
//
// A mask rather than an example: these two fields fill themselves in as soon
// as the first day is set, so a placeholder is only ever seen in the gap
// before that, where the useful thing to show is the format, not a plausible
// date that is not this one.

// IDMask is the date id shape: "req4arc-mmm-yyyy".
func IDMask(courseID string) string {
	if courseID == "" {
		return "course-mmm-yyyy"
	}
	return courseID + "-mmm-yyyy"
}

// CodeMask is the booking code shape for the course with this id:
// "YY-MM Req4Arc".
func (tk Taken) CodeMask(courseID string) string {
	if courseID == "" {
		return "YY-MM CODE"
	}
	return "YY-MM " + tk.token(courseID)
}
