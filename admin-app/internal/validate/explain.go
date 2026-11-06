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
	"id":                          {"id", "Course ID"},
	"short_title":                 {"short_title", "Short title"},
	"title":                       {"title", "Full title"},
	"url":                         {"url", "Course URL"},
	"url_en":                      {"url_en", "English course page (optional)"},
	"trainers":                    {"trainer", "Trainers"},
	"show_on":                     {"show_on", "Shown on"},
	"blurb_de":                    {"blurb_de", "Description (German)"},
	"blurb_en":                    {"blurb_en", "Description (English)"},
	"certification":               {"certification", "Certification"},
	"credit_points/methodical":    {"credits_methodical", "iSAQB points, methodical"},
	"credit_points/technical":     {"credits_technical", "iSAQB points, technical"},
	"credit_points/communication": {"credits_communication", "iSAQB points, communication"},

	// dateform.gohtml. No "dates/url": the server derives the registration
	// link from the date id, so nobody can have typed a wrong one.
	"dates/id":       {"id", "Date ID"},
	"dates/code":     {"code", "Booking code"},
	"dates/start":    {"start", "First day"},
	"dates/end":      {"end", "Last day"},
	"dates/format":   {"format", "Format"},
	"dates/city":     {"city", "City"},
	"dates/country":  {"country", "Country"},
	"dates/language": {"language", "Language taught in"},
	"dates/trainers": {"trainer", "Trainers"},
	// One choice on the form, two keys in the file (model.Date.SetAvailability).
	"dates/status":                  {"availability", "Availability"},
	"dates/seats_limited":           {"availability", "Availability"},
	"dates/price/amount":            {"price_amount", "Regular price"},
	"dates/price/alumni":            {"price_alumni", "Alumni price"},
	"dates/price/currency":          {"price_currency", "Currency"},
	"dates/price/early_bird/amount": {"early_bird_amount", "Early-bird price"},
	"dates/price/early_bird/until":  {"early_bird_until", "Early-bird deadline"},
}

// Explain says where a problem is, in the operator's words: which course,
// which date, which form field, and a link to the form. It understands the
// three shapes a Field comes in:
//
//   - "dates.<id>.<prop>" from Rules,
//   - "courses.<id>.<prop>" from the course form's own checks,
//   - "courses/<i>/dates/<j>/<prop>", a schema pointer into FeedJSON's output.
//
// Anything it cannot place on a form field is returned as an AppProblem.
func Explain(t model.Trainings, p Problem) Problem {
	switch {
	case strings.HasPrefix(p.Field, "dates."):
		id, prop, _ := strings.Cut(strings.TrimPrefix(p.Field, "dates."), ".")
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
		id, prop, ok := strings.Cut(rest, ".")
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
		return onForm(p, "dates/"+propPath(rest[2:]), where(c, &d), "/dates/"+d.ID)
	}
	return onForm(p, propPath(rest), where(c, nil), "/courses/"+c.ID)
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
	var parts []string
	if day, err := time.Parse("2006-01-02", d.Start); err == nil {
		parts = append(parts, day.Format("2 Jan 2006"))
	} else if d.Start != "" {
		parts = append(parts, d.Start)
	}
	if place := (model.Row{Date: *d}).Location(); place != "" {
		parts = append(parts, place)
	}
	if len(parts) == 0 {
		return name
	}
	return name + ", " + strings.Join(parts, " ")
}
