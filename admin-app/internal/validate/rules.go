// Package validate checks a trainings model before it is allowed to become a
// pull request.
//
// It deliberately does not re-implement scripts/validate_trainings.rb. Structure
// and enums are judged by the repository's own api/trainings.schema.json, which
// the app fetches at runtime and therefore cannot drift from. Only the four
// cross-field rules JSON Schema cannot express live here.
package validate

import (
	"fmt"

	"arc42-trainings-admin/internal/model"
)

// A Problem blocks a pull request, unless it is the app's own fault (see
// ProblemKind). Field and Message are what the check found; the rest is filled
// in by Explain, so the operator is told where to look in words they know:
// the course, the date, and the label of the form field.
type Problem struct {
	Field   string // e.g. "dates.msa-dez-2026.end", or a schema pointer
	Message string
	Keyword string // for a schema problem, the keyword that failed: "pattern", "type", ...

	Kind   ProblemKind
	Where  string // e.g. "MAS, 13 Jan 2027 online"; empty for a course being created
	Label  string // the form field's visible label, e.g. "Last day"
	Link   string // the form that sets it, e.g. "/dates/mas-jan-2027"
	Detail string // the raw technical text, for whoever fixes an app fault
}

// ProblemKind says whose a problem is. The zero value is EntryProblem on
// purpose: a problem nobody explained still blocks the pull request.
type ProblemKind int

const (
	// EntryProblem is something the operator typed and can fix on a form.
	EntryProblem ProblemKind = iota
	// AppProblem is something the operator could not have caused: the schema
	// could not be fetched, the feed could not be rendered, or the schema
	// rejected a value no form sets. It does not block the pull request; CI
	// checks it again. (Not "AppFault": that is the constructor in explain.go.)
	AppProblem
)

func (p Problem) String() string {
	if p.Field == "" {
		return p.Message
	}
	return p.Field + ": " + p.Message
}

// Rules applies the cross-field checks JSON Schema cannot express. The messages
// are read by the operator, next to the field's form label: plain words, dates
// as "13 Jan 2027", never a key name.
func Rules(t model.Trainings) []Problem {
	var problems []Problem
	// The first date to use each value, so a duplicate can say whose it is.
	seenID := map[string]model.Date{}
	seenCode := map[string]model.Date{}
	// Every value in the file, so a suggestion is never another taken one.
	all := t.TakenExcept("")

	for _, c := range t.Courses {
		for _, d := range c.Dates {
			field := "dates." + d.ID

			// 1. end must not precede start (both are "YYYY-MM-DD", so string
			//    comparison is date comparison).
			if d.Start != "" && d.End != "" && d.End < d.Start {
				problems = append(problems, Problem{
					Field:   field + ".end",
					Message: fmt.Sprintf("the last day (%s) is before the first day (%s)", day(d.End), day(d.Start)),
				})
			}

			// 2. A non-online date must name a city.
			if d.Format != "online" && d.City == "" {
				problems = append(problems, Problem{
					Field:   field + ".city",
					Message: "must not be empty unless the format is online",
				})
			}

			// 3. Date ids are unique across the whole file.
			if first, dup := seenID[d.ID]; dup {
				problems = append(problems, Problem{
					Field: field + ".id",
					Message: fmt.Sprintf("%s is already used by %s — use %s",
						d.ID, when(first), model.NextFree(orElse(model.DateID(c.ID, d.Start, d.Language), d.ID), all.IDs)),
				})
			} else {
				seenID[d.ID] = d
			}

			// 4. Booking codes are unique — they are the registration form's
			//    option values, so a collision silently misroutes a
			//    registration.
			if first, dup := seenCode[d.Code]; dup {
				problems = append(problems, Problem{
					Field: field + ".code",
					Message: fmt.Sprintf("%s is already used by %s — use %s",
						d.Code, when(first), model.NextFree(orElse(model.BookingCode(c.ID, d.Start, d.Language), d.Code), all.Codes)),
				})
			} else {
				seenCode[d.Code] = d
			}
		}
	}
	return problems
}

// orElse is a, unless it is empty. The suggestion for a duplicate starts from
// the house convention, so the third run of a month is told "-3" rather than
// "-2-2"; a date too incomplete to derive one falls back to its own value.
func orElse(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
