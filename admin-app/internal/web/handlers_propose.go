package web

import (
	"fmt"
	"html"
	"html/template"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"

	"arc42-trainings-admin/internal/gh"
	"arc42-trainings-admin/internal/model"
	"arc42-trainings-admin/internal/validate"
)

func unifiedDiff(before, after []byte) string {
	edits := myers.ComputeEdits(span.URIFromPath(dataPath), string(before), string(after))
	return fmt.Sprint(gotextdiff.ToUnified(dataPath, dataPath, string(before), edits))
}

// safeDiff escapes the diff for embedding inside a <pre> block ourselves,
// using package "html" rather than html/template's auto-escaper. The
// auto-escaper additionally replaces "+" with "&#43;" (a defense against
// legacy UTF-7 charset sniffing), which would mangle every added line of a
// unified diff. We still need &, <, >, etc. escaped, since diff lines come
// from user-supplied training data — so we do that escaping explicitly and
// mark the result template.HTML so it is not escaped a second time.
func safeDiff(diff string) template.HTML {
	return template.HTML(html.EscapeString(diff))
}

// prTitleMax keeps the title inside what GitHub shows in a list without
// eliding it. The operator can still edit the field before submitting; this is
// the default, not a limit.
const prTitleMax = 72

// prTitle names the change in the pull request list. "Training dates: 1 change"
// said nothing that the list did not already say — every proposal from this app
// changes training dates, and the one thing a reviewer needs from a title is
// which course it is about.
func prTitle(changes []Change) string {
	switch len(changes) {
	case 0:
		return "Training dates"
	case 1:
		return truncate(oneChangeTitle(changes[0]), prTitleMax)
	}
	courses := distinctCourses(changes)
	switch len(courses) {
	case 0:
		return fmt.Sprintf("Training dates: %d changes", len(changes))
	case 1:
		return truncate(fmt.Sprintf("%s: %d changes", courses[0], len(changes)), prTitleMax)
	}
	return truncate(fmt.Sprintf("Training dates: %d changes (%s)",
		len(changes), strings.Join(courses, ", ")), prTitleMax)
}

func oneChangeTitle(c Change) string {
	subject := strings.TrimSpace(courseName(c) + " " + c.Code)
	if subject == "" {
		subject = c.DateID
	}
	switch c.Kind {
	case "added":
		if c.AfterCourse != nil {
			return "New course: " + subject
		}
		if span := date(c.After).Span(); span != "" {
			return subject + ": new date, " + span
		}
		return subject + ": new date"
	case "removed":
		return subject + ": date removed"
	}
	return subject + ": " + whatChanged(c.Fields())
}

// whatChanged is the tail of a single-change title. One field gets a phrase
// worth reading; a handful get their names; more than a handful get counted,
// because past that the title stops being a summary and the report below is
// the thing to read.
func whatChanged(fs []model.FieldChange) string {
	switch {
	case len(fs) == 0:
		return "no net change"
	case len(fs) == 1:
		return phrase(fs[0])
	case len(fs) > 3:
		return fmt.Sprintf("%d fields changed", len(fs))
	}
	labels := make([]string, 0, len(fs))
	for _, f := range fs {
		labels = append(labels, strings.ToLower(f.Label))
	}
	return joinAnd(labels) + " changed"
}

// phrase says what a single changed field means, where "means" is worth more
// than the field name. Switching on Key rather than Label so rewording a label
// cannot silently change every title.
func phrase(f model.FieldChange) string {
	switch f.Key {
	case "availability":
		switch f.After {
		case "few seats":
			return "only few seats left"
		case "open":
			if f.Before == "few seats" {
				return "no longer short of seats"
			}
			return "open for registration again"
		case "waitlist":
			return "fully booked, waiting list"
		case "full":
			return "fully booked"
		case "cancelled":
			return "cancelled"
		}
		return "availability " + f.After
	case "start", "end":
		return "moved to " + f.After
	case "city":
		return "now in " + f.After
	}
	return strings.ToLower(f.Label) + " changed"
}

func courseName(c Change) string {
	if c.CourseShortTitle != "" {
		return c.CourseShortTitle
	}
	return c.CourseID
}

// distinctCourses lists the courses a multi-change proposal touches, in the
// order they were first edited, so the title can name them.
func distinctCourses(changes []Change) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range changes {
		name := courseName(c)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func joinAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

// truncate cuts on a rune boundary — the course titles and cities here are not
// all ASCII, and half a rune in a pull request title is a mojibake bug report.
func truncate(s string, max int) string {
	if len([]rune(s)) <= max {
		return s
	}
	return strings.TrimRight(string([]rune(s)[:max-1]), " ,-·") + "…"
}

// prBody writes the same report the review screen shows, as Markdown. A
// reviewer on GitHub sees which fields moved and from what, without opening
// the Files tab and reading YAML.
func prBody(changes []Change, login string) string {
	var b strings.Builder
	b.WriteString("Opened from the trainings admin app by @" + login + ".\n\n")
	for _, c := range changes {
		b.WriteString("### " + c.Kind + " — " + c.Headline() + "\n\n")
		fields := c.Fields()
		if len(fields) == 0 {
			b.WriteString("_No net change: the values were edited back to what the file already says._\n\n")
			continue
		}
		switch c.Kind {
		case "added", "removed":
			b.WriteString("| Field | Value |\n| --- | --- |\n")
			for _, f := range fields {
				value := f.After
				if c.Kind == "removed" {
					value = f.Before
				}
				b.WriteString("| " + mdCell(f.Label) + " | " + mdCell(value) + " |\n")
			}
		default:
			b.WriteString("| Field | Before | After |\n| --- | --- | --- |\n")
			for _, f := range fields {
				b.WriteString("| " + mdCell(f.Label) + " | " + mdCell(f.Before) +
					" | " + mdCell(f.After) + " |\n")
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("CI validates this against `api/trainings.schema.json` and " +
		"`scripts/validate_trainings.rb`. Merging republishes the feed and " +
		"notifies the consumer sites.\n")
	return b.String()
}

// mdCell keeps a value inside its table cell. A pipe would end the cell early
// and shift every following column; a newline (a course blurb has them) would
// end the table. Neither is escapable inside a GitHub table cell, so the
// newline becomes a break and the pipe is backslash-escaped.
func mdCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", "<br>")
}

var unsafeRef = regexp.MustCompile(`[^a-z0-9-]+`)

// branchName builds the ref a proposal is pushed to. The date and the slug are
// there to make the PR list readable; the random tail is there to make the name
// unique.
//
// Uniqueness is not cosmetic. GitHub answers POST /git/refs for an existing ref
// with 422 "Reference already exists", which aborts the whole proposal before
// the commit and the PR — so without the tail, the *second* proposal of the day
// about the same date is simply impossible. That is not an exotic sequence: fix
// a typo on a date in the morning, remove the date in the afternoon, and the
// removal is refused. Deriving the tail from the clock instead only narrows the
// window; two maintainers can still submit within the same second.
func branchName(now time.Time, changes []Change) string {
	slug := "edit"
	if len(changes) > 0 {
		slug = unsafeRef.ReplaceAllString(strings.ToLower(changes[0].DateID), "-")
		slug = strings.Trim(slug, "-")
	}
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	// A DateID that slugs down to nothing (all punctuation) would otherwise
	// leave an empty segment and a "--" run in the middle of the ref.
	if slug == "" {
		slug = "edit"
	}
	return fmt.Sprintf("trainings-admin/%s-%s-%s", now.Format("2006-01-02"), slug, newID()[:6])
}

func (s *Server) handlePropose(w http.ResponseWriter, r *http.Request, sess Session, client *gh.Client) {
	d, ok := s.drafts.Get(sess.ID)
	if !ok || !d.Dirty() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	before, _, _, err := client.ReadFile(r.Context(), dataPath)
	if err != nil {
		s.fail(w, "could not read the current file from GitHub", err)
		return
	}
	entries, faults := s.proposeProblems(r, client, d)

	s.render(w, "propose.gohtml", map[string]any{
		// Wide for the same reason as the dates list: the change report and the
		// YAML diff are tables, and a before/after pair does not fit a reading
		// measure.
		"Title": "Review & propose", "Draft": d, "Login": sess.Login, "Wide": true,
		"Diff":          safeDiff(unifiedDiff(before, d.Doc.Bytes())),
		"EntryProblems": entries,
		"AppFaults":     faults,
		"Notes":         courseNotes(d),
		"PRTitle":       prTitle(d.Changes),
		"PRBody":        prBody(d.Changes, sess.Login),
	})
}

// proposeProblems is every problem with the draft, explained and split by
// whose it is. Only the operator's entries block the pull request. An app
// fault means the app failed to check the draft, not that the draft is wrong,
// and holding the proposal back would leave the operator with nothing to do:
// CI checks the pull request again, and stays the authority on it.
func (s *Server) proposeProblems(r *http.Request, client *gh.Client, d *Draft) (entries, faults []validate.Problem) {
	m := d.Doc.Model()
	// Rules first: where a rule and the schema object to the same field (a
	// public date without a city), the rule's message is the one written for
	// this case, and the operator needs to hear about the field once.
	all := append(validate.Rules(m), s.schemaProblems(r, client, d)...)
	seen := map[string]bool{}
	for _, p := range validate.ExplainAll(m, all) {
		if p.Kind == validate.AppProblem {
			// The page asks the operator for a screenshot; the log is the
			// copy that does not depend on them sending one.
			log.Printf("propose: app fault: %s", p.Detail)
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

// schemaProblems validates against the repository's own schema, fetched live so
// it cannot drift from what CI will enforce.
func (s *Server) schemaProblems(r *http.Request, client *gh.Client, d *Draft) []validate.Problem {
	schema, _, _, err := client.ReadFile(r.Context(), schemaPath)
	if err != nil {
		return []validate.Problem{validate.AppFault("could not fetch the schema: " + err.Error())}
	}
	feed, err := validate.FeedJSON(d.Doc.Model())
	if err != nil {
		return []validate.Problem{validate.AppFault("could not render the feed: " + err.Error())}
	}
	problems, err := validate.Schema(schema, feed)
	if err != nil {
		return []validate.Problem{validate.AppFault(err.Error())}
	}
	return problems
}

// courseNotes are the info lines for each course this draft adds or changes:
// the course form's own notes (validate.CourseNotes), and for a new course
// without a date, that it will not show yet. Not problems and not warnings.
// The course form shows its notes only when it is shown again, and a save
// without warnings goes straight back to the list, so this page is the one
// place a newly created course's notes are sure to be seen.
func courseNotes(d *Draft) []string {
	m := d.Doc.Model()
	var notes []string
	for _, ch := range d.Changes {
		if ch.AfterCourse == nil {
			continue
		}
		c, ok := m.FindCourse(ch.AfterCourse.ID)
		if !ok {
			continue
		}
		name := c.ShortTitle
		if name == "" {
			name = c.ID
		}
		for _, n := range validate.CourseNotes(c) {
			notes = append(notes, name+" — "+n)
		}
		// A course is entered before its first date; the operator only needs
		// to know it will not show yet.
		if ch.Kind == "added" && len(c.Dates) == 0 {
			notes = append(notes, name+" has no dates yet. It will appear on the home pages once it has one.")
		}
	}
	return notes
}

func (s *Server) handleProposeSubmit(w http.ResponseWriter, r *http.Request, sess Session, client *gh.Client) {
	unlock := s.drafts.Submitting(sess.ID)
	defer unlock()
	d, ok := s.drafts.Get(sess.ID)
	if !ok || !d.Dirty() {
		// The same form submitted again — a double click, or a reload of the
		// POST — after the first one already opened the PR.
		if prURL, done := s.drafts.Published(sess.ID); done {
			s.renderPublished(w, prURL, sess)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.fail(w, "could not read the form", err)
		return
	}
	// Optimistic concurrency: if the blob moved since the draft was loaded,
	// somebody else changed the file. Never overwrite silently — and never
	// discard the draft, which is the user's only copy.
	_, fileSHA, headSHA, err := client.ReadFile(r.Context(), dataPath)
	if err != nil {
		s.fail(w, "could not re-read the file from GitHub", err)
		return
	}
	if fileSHA != d.FileSHA {
		w.WriteHeader(http.StatusConflict)
		s.render(w, "conflict.gohtml", map[string]any{
			"Title": "The file changed on GitHub", "Draft": d, "Login": sess.Login,
		})
		return
	}
	// The same gate as the review page, so a stale page or a hand-made POST
	// cannot get past a problem in the entries, and an app fault does not
	// stop here either.
	if entries, faults := s.proposeProblems(r, client, d); len(entries) > 0 {
		s.render(w, "propose.gohtml", map[string]any{
			"Title": "Fix these first", "Draft": d, "Login": sess.Login, "Wide": true,
			"EntryProblems": entries, "AppFaults": faults, "Notes": courseNotes(d),
			"PRTitle": r.PostFormValue("title"), "PRBody": r.PostFormValue("body"),
		})
		return
	}

	url, err := client.OpenPR(r.Context(), gh.PRRequest{
		Branch:  branchName(time.Now(), d.Changes),
		Path:    dataPath,
		Content: string(d.Doc.Bytes()),
		BaseSHA: headSHA,
		FileSHA: fileSHA,
		Title:   r.PostFormValue("title"),
		Body:    r.PostFormValue("body"),
	})
	if err != nil {
		s.fail(w, "could not open the pull request", err)
		return
	}
	s.drafts.Publish(sess.ID, url)
	s.renderPublished(w, url, sess)
}

func (s *Server) renderPublished(w http.ResponseWriter, prURL string, sess Session) {
	s.render(w, "published.gohtml", map[string]any{
		"Title": "Pull request opened", "URL": prURL, "Login": sess.Login,
	})
}
