package web

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"arc42-trainings-admin/internal/gh"
	"arc42-trainings-admin/internal/model"
	"arc42-trainings-admin/internal/validate"
)

// An entry is checked where it is entered, never later. The course MAISPEC
// was saved by the course form and rejected on Review & propose, whose Edit
// link led back to a form with the id read-only: nothing left to do but
// discard the draft. So a save runs the very checks the review page runs, and
// what they find about the entry being saved stops the save, on its form.

// idRuns are what normaliseID turns into one hyphen: the spaces and
// underscores people type where the house convention has a hyphen.
var idRuns = regexp.MustCompile(`[\s_]+`)

// normaliseID turns what was typed into a course or date id into the id:
// trimmed, lower-cased, each run of spaces and underscores one hyphen.
// "MAISPEC" is not a mistake worth a message; the convention is the app's to
// apply. It runs on the server because the browser's help (form.js lower-casing
// as you type) is gone with scripting off.
func normaliseID(s string) string {
	return idRuns.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-")
}

// idChars is the pattern api/trainings.schema.json applies to course and date
// ids. Checked here as well, rather than only by the schema, because the
// schema is fetched from GitHub and a fetch that fails must not let an id
// through that the review page would then reject.
var idChars = regexp.MustCompile(`^[a-z0-9-]+$`)

// idProblem says what is still wrong with a normalised id once nothing more can
// be converted, naming the characters, or "" when nothing is. An empty id is
// not judged here: whether one is required is the caller's question.
func idProblem(id string) string {
	if id == "" || idChars.MatchString(id) {
		return ""
	}
	var bad []string
	seen := map[rune]bool{}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || seen[r] {
			continue
		}
		seen[r] = true
		bad = append(bad, fmt.Sprintf("%q", string(r)))
	}
	verb := "is"
	if len(bad) > 1 {
		verb = "are"
	}
	return fmt.Sprintf("use only the letters a to z, digits and hyphens — %s %s not possible in an id",
		joinAnd(bad), verb)
}

// fetchSchema reads the repository's own schema, live, so the app cannot
// drift from what CI enforces on the pull request.
func (s *Server) fetchSchema(r *http.Request, client *gh.Client) ([]byte, error) {
	schema, _, _, err := client.ReadFile(r.Context(), schemaPath)
	return schema, err
}

// checkModel is every problem with m, explained and split by whose it is: the
// operator's entries, and the app's own faults. It is the one check, shared by
// the review page and every form save, so the two cannot disagree.
func checkModel(m model.Trainings, schema []byte, schemaErr error) (entries, faults []validate.Problem) {
	// Rules first: where a rule and the schema object to the same field (a
	// public date without a city), the rule's message is the one written for
	// this case, and the operator needs to hear about the field once.
	all := append(validate.Rules(m), schemaProblems(m, schema, schemaErr)...)
	seen := map[string]bool{}
	for _, p := range validate.ExplainAll(m, all) {
		if p.Kind == validate.AppProblem {
			faults = append(faults, p)
			continue
		}
		if key := p.Link + "\x00" + p.Label; !seen[key] {
			seen[key] = true
			entries = append(entries, p)
		}
	}
	return entries, faults
}

// schemaProblems validates m against the schema fetched by fetchSchema.
func schemaProblems(m model.Trainings, schema []byte, schemaErr error) []validate.Problem {
	if schemaErr != nil {
		return []validate.Problem{validate.AppFault("could not fetch the schema: " + schemaErr.Error())}
	}
	feed, err := validate.FeedJSON(m)
	if err != nil {
		return []validate.Problem{validate.AppFault("could not render the feed: " + err.Error())}
	}
	problems, err := validate.Schema(schema, feed)
	if err != nil {
		return []validate.Problem{validate.AppFault(err.Error())}
	}
	return problems
}

// editProblems is what Review & propose would hold against one edit, found
// while the operator is still on its form. before is the draft as it is,
// after the draft with the edit applied, own the form link of the entry being
// saved ("/courses/msa", "/dates/msa-a"), local the form's own checks.
//
// It keeps two kinds of entry problem from after:
//
//   - every one on the entry itself, already there or not: the form sets
//     those fields, so this is where they get fixed;
//   - one elsewhere that this edit caused: editing msa-b to the booking code
//     msa-a already has is reported on msa-a, the later of the two, but only
//     this form can undo it.
//
// What was already wrong elsewhere stays the review page's to report: one
// date's problem must not hold back the save of another, nor a course's dates
// the save of the course. App faults never block a save, as they never block
// the proposal; the review page reports them.
func (s *Server) editProblems(r *http.Request, client *gh.Client, before, after model.Trainings, own string, local []validate.Problem) []validate.Problem {
	schema, err := s.fetchSchema(r, client)
	had := map[string]bool{}
	prior, _ := checkModel(before, schema, err)
	for _, p := range prior {
		had[problemKey(p)] = true
	}
	now, faults := checkModel(after, schema, err)
	for _, p := range faults {
		// Not the operator's to fix and not blocking; the review page shows
		// it again. The log is what tells Gernot a save went unchecked.
		log.Printf("save: not fully checked, saved anyway: %s", p.Detail)
	}

	var out []validate.Problem
	seen := map[string]bool{}
	add := func(p validate.Problem, mine bool) {
		// On its own form a field is named once, whichever check found it:
		// the form's own wording comes first and wins.
		key := p.Label
		if !mine {
			key = p.Link + "\x00" + p.Label
		}
		if seen[key] {
			return
		}
		seen[key] = true
		if mine {
			p.Where = "" // the form is the where
		}
		out = append(out, p)
	}
	for _, p := range validate.ExplainAll(after, local) {
		add(p, true)
	}
	for _, p := range now {
		mine := p.Link == own
		if !mine && had[problemKey(p)] {
			continue
		}
		add(p, mine)
	}
	return out
}

// problemKey identifies a problem across two versions of the draft. Not by
// Where, which names the course by its short title and would make every date
// problem "new" when the course's short title is edited.
func problemKey(p validate.Problem) string {
	return p.Link + "\x00" + p.Label + "\x00" + p.Message
}
