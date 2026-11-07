package validate

import (
	"strconv"
	"strings"
	"time"

	"arc42-trainings-admin/internal/model"
)

// formField is the form control that sets one property of the data.
type formField struct {
	Name  string // the control's name= attribute
	Label string // its visible label, verbatim, so the operator can find it
	// Hint says, in plain words, what a value must look like where the schema
	// checks it against a pattern. A regular expression is not an answer to
	// "what is wrong with my entry"; TestEveryPatternHasAHint keeps these
	// complete.
	Hint string
}

// formFields maps every property an operator can set to the control that sets
// it. Keys are property paths relative to a course, array indices removed:
// "short_title", "trainers", "dates/start", "dates/price/early_bird/until".
//
// It is the line between the two kinds of problem. A problem on a property in
// this table is the operator's, and they are sent to that field. A problem on a
// property that is not here (the registration link, the generated blurb, the
// dates list itself) is something the app wrote on its own, so it is the app's
// fault, however it came about.
//
// Keep this table next to the forms' field names: when a control is added to,
// renamed on or relabelled in courseform.gohtml or dateform.gohtml, change it
// here too. TestFormFieldsCoverForms enforces it, both ways.
var formFields = map[string]formField{
	// courseform.gohtml
	"id":                          {"id", "Course ID", mustBeAnID},
	"short_title":                 {"short_title", "Short title", ""},
	"title":                       {"title", "Full title", ""},
	"url":                         {"url", "Course URL", mustBeHTTPS},
	"url_en":                      {"url_en", "English course page (optional)", mustBeHTTPS},
	"trainers":                    {"trainer", "Trainers", ""},
	"show_on":                     {"show_on", "Shown on", "must be site names like www.arc42.de: lower-case letters, digits, dots and hyphens"},
	"blurb_de":                    {"blurb_de", "Description (German)", ""},
	"blurb_en":                    {"blurb_en", "Description (English)", ""},
	"certification":               {"certification", "Certification", ""},
	"credit_points/methodical":    {"credits_methodical", "iSAQB points, methodical", ""},
	"credit_points/technical":     {"credits_technical", "iSAQB points, technical", ""},
	"credit_points/communication": {"credits_communication", "iSAQB points, communication", ""},

	// dateform.gohtml. No "dates/url": the server derives the registration
	// link from the date id, so nobody can have typed a wrong one.
	"dates/id":       {"id", "Date ID", mustBeAnID},
	"dates/code":     {"code", "Booking code", ""},
	"dates/start":    {"start", "First day", mustBeADate},
	"dates/end":      {"end", "Last day", mustBeADate},
	"dates/format":   {"format", "Format", ""},
	"dates/city":     {"city", "City", ""},
	"dates/country":  {"country", "Country", "must be two capital letters, e.g. DE"},
	"dates/language": {"language", "Language taught in", ""},
	"dates/trainers": {"trainer", "Trainers", ""},
	// One choice on the form, two keys in the file (model.Date.SetAvailability).
	"dates/status":                  {"availability", "Availability", ""},
	"dates/seats_limited":           {"availability", "Availability", ""},
	"dates/price/amount":            {"price_amount", "Regular price", ""},
	"dates/price/alumni":            {"price_alumni", "Alumni price", ""},
	"dates/price/currency":          {"price_currency", "Currency", "must be three capital letters, e.g. EUR"},
	"dates/price/early_bird/amount": {"early_bird_amount", "Early-bird price", ""},
	"dates/price/early_bird/until":  {"early_bird_until", "Early-bird deadline", mustBeADate},
}

const (
	mustBeAnID   = "must be lower-case letters, digits and hyphens only"
	mustBeHTTPS  = "must start with https://"
	mustBeADate  = "must be a date like 2027-01-13"
	mustBeFilled = "must not be empty"
)

// plainMessage says what the schema found wrong, in the words of the form.
// Only keywords a form entry can violate get an answer; ok is false for the
// rest (a wrong JSON type, an extra property, a malformed object), because
// those are about how the app wrote the feed, not about what was typed.
func plainMessage(keyword string, f formField) (msg string, ok bool) {
	switch keyword {
	case "required", "minLength", "minItems":
		return mustBeFilled, true
	case "pattern":
		if f.Hint != "" {
			return f.Hint, true
		}
		return "is not in the expected form", true
	case "enum":
		return "must be one of the choices on the form", true
	case "minimum":
		return "must not be negative", true
	}
	return "", false
}

// Explain says where a problem is, in the operator's words: which course,
// which date, which form field, and a link to the form. It understands the
// three shapes a Field comes in:
//
//   - "dates.<id>.<prop>" from Rules,
//   - "courses.<id>.<prop>" from the course form's own checks,
//   - "courses/<i>/dates/<j>/<prop>", a schema pointer into FeedJSON's output.
//
// Anything it cannot place on a form field is returned as an AppProblem, and so
// is a schema problem of a kind no form entry can cause (plainMessage).
func Explain(t model.Trainings, p Problem) Problem {
	switch {
	case strings.HasPrefix(p.Field, "dates."):
		id, prop, _ := cutProp(strings.TrimPrefix(p.Field, "dates."))
		for _, c := range t.Courses {
			for _, d := range c.Dates {
				if d.ID == id {
					return onForm(p, "dates/"+prop, where(c, &d), "/dates/"+d.ID)
				}
			}
		}
		return asAppFault(p)

	case strings.HasPrefix(p.Field, "courses."):
		rest := strings.TrimPrefix(p.Field, "courses.")
		id, prop, ok := cutProp(rest)
		if !ok {
			id, prop = "", rest // "courses.id": the id itself is missing
		}
		// A course being created is not in the model yet. Its fields still
		// have labels; there is just no saved course to name or link to.
		if c, found := t.FindCourse(id); found && id != "" {
			return onForm(p, prop, where(c, nil), "/courses/"+c.ID)
		}
		return onForm(p, prop, "", "")

	case strings.HasPrefix(p.Field, "courses/"):
		return explainPointer(t, p)
	}
	return asAppFault(p)
}

// cutProp splits "<id>.<prop>" at the last dot. A property name never holds
// one, while an id can, before it has been checked: "mai.spec" is exactly the
// kind of entry these problems are about, and cut at the first dot it read as
// course "mai" with a property "spec.id" no form has.
func cutProp(s string) (id, prop string, ok bool) {
	i := strings.LastIndex(s, ".")
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+1:], true
}

func explainPointer(t model.Trainings, p Problem) Problem {
	segs := strings.Split(p.Field, "/")[1:]
	c, ok := at(t.Courses, segs)
	if !ok {
		return asAppFault(p)
	}
	rest := segs[1:]
	if len(rest) >= 2 && rest[0] == "dates" {
		d, ok := at(c.Dates, rest[1:])
		if !ok {
			return asAppFault(p)
		}
		return schemaOnForm(p, "dates/"+propPath(rest[2:]), where(c, &d), "/dates/"+d.ID)
	}
	return schemaOnForm(p, propPath(rest), where(c, nil), "/courses/"+c.ID)
}

// schemaOnForm is onForm for a schema problem, whose message is the library's
// ("does not match pattern '^https://'") and is replaced by a plain one. The
// original stays in Detail.
func schemaOnForm(p Problem, prop, where, link string) Problem {
	msg, ok := plainMessage(p.Keyword, formFields[prop])
	if !ok {
		return asAppFault(p)
	}
	p = onForm(p, prop, where, link)
	if p.Kind == EntryProblem {
		p.Detail, p.Message = p.String(), msg
	}
	return p
}

// at resolves the index in segs[0]. The pointer came from a feed rendered from
// this very model, so a miss means the two disagree, which is the app's fault.
func at[T any](list []T, segs []string) (T, bool) {
	var zero T
	if len(segs) == 0 {
		return zero, false
	}
	i, err := strconv.Atoi(segs[0])
	if err != nil || i < 0 || i >= len(list) {
		return zero, false
	}
	return list[i], true
}

// propPath drops array indices: the third trainer is still the trainers field.
func propPath(segs []string) string {
	var out []string
	for _, s := range segs {
		if _, err := strconv.Atoi(s); err == nil {
			continue
		}
		out = append(out, s)
	}
	return strings.Join(out, "/")
}

func onForm(p Problem, prop, where, link string) Problem {
	f, ok := formFields[prop]
	if !ok {
		return asAppFault(p)
	}
	p.Kind, p.Label, p.Where, p.Link = EntryProblem, f.Label, where, link
	return p
}

func asAppFault(p Problem) Problem {
	p.Kind, p.Detail = AppProblem, p.String()
	return p
}

// AppFault is a problem the operator could not have caused: the schema could
// not be fetched, the feed could not be rendered, the schema did not compile.
func AppFault(detail string) Problem {
	return Problem{Kind: AppProblem, Message: detail, Detail: detail}
}

// ExplainAll is Explain over a list.
func ExplainAll(t model.Trainings, ps []Problem) []Problem {
	out := make([]Problem, 0, len(ps))
	for _, p := range ps {
		out = append(out, Explain(t, p))
	}
	return out
}

// where names a course by its short title and a date by its first day and its
// city, or "online": "MAS, 13 Jan 2027 online". Month names are English, like
// the rest of the app.
func where(c model.Course, d *model.Date) string {
	name := c.ShortTitle
	if name == "" {
		name = c.ID
	}
	if d == nil {
		return name
	}
	if w := when(*d); w != "" {
		return name + ", " + w
	}
	return name
}

// when names a date without its course: "13 Jan 2027 online".
func when(d model.Date) string {
	var parts []string
	if d.Start != "" {
		parts = append(parts, day(d.Start))
	}
	if place := (model.Row{Date: d}).Location(); place != "" {
		parts = append(parts, place)
	}
	return strings.Join(parts, " ")
}

// day writes a "YYYY-MM-DD" date the way the operator reads one: "13 Jan
// 2027". Anything unparseable is shown as it is, rather than hidden.
func day(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return t.Format("2 Jan 2006")
}
