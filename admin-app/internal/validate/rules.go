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

type Problem struct {
	Field   string // e.g. "dates.msa-dez-2026.end"
	Message string
}

func (p Problem) String() string {
	if p.Field == "" {
		return p.Message
	}
	return p.Field + ": " + p.Message
}

// Rules applies the cross-field checks JSON Schema cannot express.
func Rules(t model.Trainings) []Problem {
	var problems []Problem
	seenID := map[string]bool{}
	seenCode := map[string]bool{}

	for _, c := range t.Courses {
		for _, d := range c.Dates {
			field := "dates." + d.ID

			// 1. end must not precede start (both are "YYYY-MM-DD", so string
			//    comparison is date comparison).
			if d.Start != "" && d.End != "" && d.End < d.Start {
				problems = append(problems, Problem{
					Field:   field + ".end",
					Message: fmt.Sprintf("end %s is before start %s", d.End, d.Start),
				})
			}

			// 2. A non-online date must name a city.
			if d.Format != "online" && d.City == "" {
				problems = append(problems, Problem{
					Field:   field + ".city",
					Message: "a non-online date needs a city",
				})
			}

			// 3. Date ids are unique across the whole file.
			if seenID[d.ID] {
				problems = append(problems, Problem{
					Field:   field + ".id",
					Message: fmt.Sprintf("duplicate date id %q", d.ID),
				})
			}
			seenID[d.ID] = true

			// 4. Booking codes are unique — they are the Formspark option
			//    values, so a collision silently misroutes a registration.
			if seenCode[d.Code] {
				problems = append(problems, Problem{
					Field:   field + ".code",
					Message: fmt.Sprintf("duplicate booking code %q", d.Code),
				})
			}
			seenCode[d.Code] = true
		}
	}
	return problems
}
