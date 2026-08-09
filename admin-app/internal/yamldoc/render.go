package yamldoc

import (
	"strings"

	"arc42-trainings-admin/internal/model"
)

// quoted fields are always emitted double-quoted. start/end because an
// unquoted YYYY-MM-DD parses as a timestamp; country because unquoted NO is
// boolean false in YAML 1.1; code and url because they routinely contain
// characters that would otherwise need escaping anyway.
var alwaysQuoted = map[string]bool{
	"code": true, "start": true, "end": true, "city": true,
	"country": true, "url": true, "pricing": true, "few_seats": true,
	"short_title": true, "title": true, "certification": true,
}

// RenderDate emits one date entry, bullet included, at the given indent.
// Field order matches the existing file so diffs stay readable.
// Optional fields are omitted entirely when empty — never emitted as "".
func RenderDate(d model.Date, indent string) string {
	var b strings.Builder
	first := true
	put := func(key, val string) {
		if val == "" {
			return
		}
		if first {
			b.WriteString(indent + "- ")
			first = false
		} else {
			b.WriteString(indent + "  ")
		}
		b.WriteString(key + ": " + fmtScalar(key, val) + "\n")
	}
	put("id", d.ID)
	put("code", d.Code)
	put("start", d.Start)
	put("end", d.End)
	put("city", d.City)
	put("country", d.Country)
	put("language", d.Language)
	put("format", d.Format)
	if len(d.Trainers) > 0 {
		b.WriteString(indent + "  trainers: " + fmtList(d.Trainers) + "\n")
	}
	put("pricing", d.Pricing)
	put("few_seats", d.FewSeats)
	put("url", d.URL)
	put("status", d.Status)
	return b.String()
}

// renderCourseScalars emits a course's own fields (not its dates), each line
// prefixed with the mapping indent. The caller supplies the bullet.
func renderCourseScalars(c model.Course, indent string) string {
	var b strings.Builder
	first := true
	put := func(key, val string) {
		if val == "" {
			return
		}
		if first {
			b.WriteString(indent + "- ")
			first = false
		} else {
			b.WriteString(indent + "  ")
		}
		b.WriteString(key + ": " + fmtScalar(key, val) + "\n")
	}
	put("id", c.ID)
	put("short_title", c.ShortTitle)
	put("title", c.Title)
	if c.Blurb != "" {
		b.WriteString(indent + "  blurb: >-\n")
		for _, line := range wrapWords(c.Blurb, 68) {
			b.WriteString(indent + "    " + line + "\n")
		}
	}
	put("certification", c.Certification)
	if c.Credits == "" {
		b.WriteString(indent + "  credits: null\n")
	} else {
		b.WriteString(indent + "  credits: " + fmtScalar("credits", c.Credits) + "\n")
	}
	put("url", c.URL)
	if len(c.Trainers) > 0 {
		b.WriteString(indent + "  trainers: " + fmtList(c.Trainers) + "\n")
	}
	return b.String()
}

func fmtScalar(key, val string) string {
	if alwaysQuoted[key] {
		return quote(val)
	}
	// Bare enum-ish values (language, format, status, id) stay unquoted, as in
	// the existing file — but only while they really are bare.
	if val != "" && !strings.ContainsAny(val, ` :#"'{}[],&*?|<>=!%@`+"`") {
		return val
	}
	return quote(val)
}

func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}

func fmtList(items []string) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, quote(it))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// wrapWords greedily wraps text to width columns for folded blurb blocks.
func wrapWords(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var out []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > width {
			out = append(out, line)
			line = w
			continue
		}
		line += " " + w
	}
	return append(out, line)
}

// RenderCourse emits a complete new course entry, bullet included, ending in an
// empty dates sequence. validate_trainings.rb requires `dates` to be an Array on
// every course, so a course with no dates yet still needs "dates: []" — omitting
// the key would fail CI on the first pull request.
func RenderCourse(c model.Course, indent string) string {
	return renderCourseScalars(c, indent) + indent + "  dates: []\n"
}
