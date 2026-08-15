package model

import "strings"

// The two identifiers on a date carry the same three facts — course, year,
// month — but serve different readers, so the file keeps both:
//
//   - ID is the anchor in https://www.arc42.de/termine#<id> and the key in
//     api/trainings.json. It must never change once published, or links rot.
//   - Code is the Formspark <option value>, so it reaches the back office
//     verbatim in a registration mail. Humans read it; hence the space and the
//     mixed case.
//
// Typing both by hand is what is redundant, not storing both. The functions
// here derive Code and URL so the operator states the ID once. Both remain
// editable: the derivation reproduces 13 of the 14 published codes exactly, and
// the fourteenth (a course starting 2027-11-30 but booked as "27-12 MSA")
// is why this prefills a field rather than replacing one.

// codeTokens overrides the default upper-cased course id where the published
// booking codes disagree with it. Like KnownTrainers this is a convenience, not
// a constraint — a course missing from here still derives a usable code, and
// the operator can always overwrite the result.
var codeTokens = map[string]string{
	"req4arc": "Req4Arc",
}

// CodeToken is the course's abbreviation as it appears inside a booking code.
func CodeToken(courseID string) string {
	if t, ok := codeTokens[courseID]; ok {
		return t
	}
	return strings.ToUpper(courseID)
}

// BookingCode derives the Formspark option value: "26-12 MSA", with an "-EN"
// suffix when the course is taught in English. It returns "" when it has too
// little to work with, so a half-filled form prefills nothing rather than
// something wrong.
func BookingCode(courseID, start, language string) string {
	if courseID == "" || len(start) != len("YYYY-MM-DD") {
		return ""
	}
	year, month := start[2:4], start[5:7]
	code := year + "-" + month + " " + CodeToken(courseID)
	if language == "en" {
		code += "-EN"
	}
	return code
}

// RegistrationURL derives the public link for a date. Every published date
// points at the same anchored page, so this is the whole rule.
func RegistrationURL(dateID string) string {
	if dateID == "" {
		return ""
	}
	return "https://www.arc42.de/termine#" + dateID
}
