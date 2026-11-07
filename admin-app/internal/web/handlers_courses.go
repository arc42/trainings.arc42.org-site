package web

import (
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"arc42-trainings-admin/internal/gh"
	"arc42-trainings-admin/internal/model"
	"arc42-trainings-admin/internal/validate"
)

// otherTrainers returns the names that are not on the canonical roster. They
// are shown in the free-text field and preserved exactly as stored — an
// existing "Peter Hruschka" is never silently promoted to "Dr. Peter Hruschka",
// because that would change what the public site prints.
func otherTrainers(all []string) []string {
	var out []string
	for _, t := range all {
		if !slices.Contains(model.KnownTrainers, t) {
			out = append(out, t)
		}
	}
	return out
}

// parseTrainers merges the checked roster names with the free-text ones. Roster
// names come first, in roster order, so the YAML ordering stays stable no
// matter which checkbox the browser reports first.
func parseTrainers(r *http.Request) []string {
	picked := map[string]bool{}
	for _, v := range r.PostForm["trainer"] {
		picked[v] = true
	}
	var out []string
	for _, name := range model.KnownTrainers {
		if picked[name] {
			out = append(out, name)
		}
	}
	for _, name := range strings.Split(r.PostFormValue("trainers_other"), ",") {
		if n := strings.TrimSpace(name); n != "" && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// otherSites and parseShowOn are the trainer roster's pattern applied to the
// "Shown on" group: known sites as checkboxes, anything else in one free-text
// field, preserved exactly as typed.
func otherSites(all []string) []string {
	var out []string
	for _, site := range all {
		if !slices.Contains(model.KnownSites, site) {
			out = append(out, site)
		}
	}
	return out
}

// parseShowOn returns nil, not an empty list, when nothing is ticked or typed:
// no show_on key means every site, while an empty list would mean a course
// shown nowhere, which the site's validator rejects.
func parseShowOn(r *http.Request) []string {
	picked := map[string]bool{}
	for _, v := range r.PostForm["show_on"] {
		picked[v] = true
	}
	var out []string
	for _, site := range model.KnownSites {
		if picked[site] {
			out = append(out, site)
		}
	}
	for _, site := range strings.Split(r.PostFormValue("show_on_other"), ",") {
		if n := strings.TrimSpace(site); n != "" && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// sameSites reports whether two show_on lists name the same set of sites, in
// any order. The form posts in KnownSites order, so a hand-edited order would
// otherwise come back rearranged and show up as a change in the pull request
// although the operator changed nothing. Containment is checked both ways and
// length is ignored: a hand edit can leave a duplicate (nothing dedupes
// show_on), and ["arc42.org", "arc42.org"] must not pass for the same list as
// ["www.arc42.de", "arc42.org"] just because the lengths match.
func sameSites(a, b []string) bool {
	within := func(xs, ys []string) bool {
		for _, x := range xs {
			if !slices.Contains(ys, x) {
				return false
			}
		}
		return true
	}
	return within(a, b) && within(b, a)
}

// siteName is the pattern api/trainings.schema.json and the site's validator
// apply to show_on. Checked here so a typo is caught on the form, in the
// operator's words, instead of failing the pull request later.
var siteName = regexp.MustCompile(`^[a-z0-9.-]+$`)

func (s *Server) handleCourseList(w http.ResponseWriter, r *http.Request, sess Session, client *gh.Client) {
	d, err := s.loadDraft(r.Context(), sess, client)
	if err != nil {
		s.fail(w, "could not read the training dates from GitHub", err)
		return
	}
	s.render(w, "courselist.gohtml", map[string]any{
		"Title": "Courses", "Courses": d.Doc.Model().Courses, "Draft": d, "Login": sess.Login,
	})
}

func (s *Server) handleCourseForm(w http.ResponseWriter, r *http.Request, sess Session, client *gh.Client) {
	d, err := s.loadDraft(r.Context(), sess, client)
	if err != nil {
		s.fail(w, "could not read the training dates from GitHub", err)
		return
	}
	data := map[string]any{
		"Draft": d, "Login": sess.Login, "KnownTrainers": model.KnownTrainers,
		"KnownSites": model.KnownSites,
	}
	id := r.PathValue("id")
	if id == "" { // the literal /courses/new route carries no {id}
		data["Title"] = "New course"
		data["IsNew"] = true
		data["Course"] = model.Course{}
		data["OtherTrainers"] = nil
		data["OtherSites"] = nil
		for k, v := range courseIDState(d, "") {
			data[k] = v
		}
		s.render(w, "courseform.gohtml", data)
		return
	}
	for _, c := range d.Doc.Model().Courses {
		if c.ID == id {
			data["Title"] = "Edit " + c.ShortTitle
			data["IsNew"] = false
			data["Course"] = c
			for k, v := range courseIDState(d, id) {
				data[k] = v
			}
			data["OtherTrainers"] = otherTrainers(c.Trainers)
			data["OtherSites"] = otherSites(c.ShowOn)
			data["Notes"] = validate.CourseNotes(c)
			s.render(w, "courseform.gohtml", data)
			return
		}
	}
	http.NotFound(w, r)
}

// courseIDState is what the course form needs to know about the id of the
// stored course storedID: whether it still follows the short title, and which
// hint says why or why not. StoredID is the form's own address, which is not
// the id once a rename is being tried: posting a failed rename back to the
// new id would edit a course that does not exist.
func courseIDState(d *Draft, storedID string) map[string]any {
	return map[string]any{
		"StoredID":   storedID,
		"DraftOnly":  storedID != "" && d.AddedCourse(storedID),
		"IDEditable": storedID == "" || d.CourseIDEditable(storedID),
	}
}

// courseProblems checks what HTML validation cannot: the id's form and
// uniqueness, and that a course actually names a trainer. The feed schema
// enforces these at propose time too, but catching them here keeps a broken
// course out of the draft. storedID is the course being edited, "" for a new
// one: its own id is not a duplicate of itself.
//
// The id is made from the short title, so its problems are reported on the
// short title (validate's formFields), in words that say so. They use the
// field "courses.id": the id is what is wrong, so it cannot also be what
// Explain looks the course up by.
func courseProblems(c model.Course, existing []model.Course, storedID string) []validate.Problem {
	var problems []validate.Problem
	field := "courses." + c.ID
	// An empty short title is reported as that, below; this is the short
	// title with nothing in it an id can be made of.
	if c.ID == "" && strings.TrimSpace(c.ShortTitle) != "" {
		problems = append(problems, validate.Problem{Field: "courses.id",
			Message: "needs at least one letter a to z or digit: the course ID is made of them"})
	}
	if msg := idProblem(c.ID); msg != "" {
		problems = append(problems, validate.Problem{Field: "courses.id", Message: msg})
	}
	if courseIDTaken(existing, c.ID, storedID) {
		other, _ := model.Trainings{Courses: existing}.FindCourse(c.ID)
		problems = append(problems, validate.Problem{Field: "courses.id", Message: fmt.Sprintf(
			"makes the course ID %s, which %s already has. Two courses need short titles that differ in their letters or digits, e.g. by an added word",
			c.ID, other.ShortTitle)})
	}
	for _, f := range []struct{ name, val string }{
		{"short_title", c.ShortTitle}, {"title", c.Title}, {"url", c.URL},
	} {
		if strings.TrimSpace(f.val) == "" {
			problems = append(problems, validate.Problem{Field: field + "." + f.name, Message: "must not be empty"})
		}
	}
	// The schema says ^https:// too, but a link is published as it is typed,
	// and this check must not depend on the schema fetch succeeding.
	for _, f := range []struct{ name, val string }{{"url", c.URL}, {"url_en", c.URLEn}} {
		if f.val != "" && !strings.HasPrefix(f.val, "https://") {
			problems = append(problems, validate.Problem{Field: field + "." + f.name, Message: "must start with https://"})
		}
	}
	if len(c.Trainers) == 0 {
		problems = append(problems, validate.Problem{
			Field: field + ".trainers", Message: "name at least one",
		})
	}
	for _, site := range c.ShowOn {
		if !siteName.MatchString(site) {
			problems = append(problems, validate.Problem{
				Field: field + ".show_on", Message: fmt.Sprintf("%q is not a site name like www.arc42.de", site),
			})
		}
	}
	return problems
}

// courseIDTaken reports whether another course than storedID already has id.
func courseIDTaken(existing []model.Course, id, storedID string) bool {
	for _, ex := range existing {
		if id != "" && ex.ID == id && ex.ID != storedID {
			return true
		}
	}
	return false
}

// courseURLPrefix is how form.js derives a course URL from the id.
const courseURLPrefix = "https://www.arc42.de/info-"

// followID keeps a derived course URL in step with the id the server stores.
// form.js derives the URL from the id it makes of the short title. Without
// scripting the operator types the URL, likely as the short title is spelled
// (info-Mai Spec/ for an id mai-spec), and a renamed draft course still sends
// the URL of its old id (storedID). Those two are rewritten to the stored
// id's. The match is normaliseID's, deliberately narrower than courseIDFrom:
// info-mai.spec/ is a page name someone chose, not a spelling of mai-spec,
// and is left alone like any other URL.
func followID(u, id, storedID string) string {
	rest, ok := strings.CutPrefix(u, courseURLPrefix)
	if !ok {
		return u
	}
	typed, ok := strings.CutSuffix(rest, "/")
	if !ok || typed == id || (normaliseID(typed) != id && (storedID == "" || typed != storedID)) {
		return u
	}
	return courseURLPrefix + id + "/"
}

// parseCreditPoints reads the three iSAQB category counts. All three empty (or
// all zero) means the course carries no credit points and the key is left out
// of the YAML entirely, rather than written as an empty mapping.
func parseCreditPoints(get func(string) string) *model.CreditPoints {
	num := func(k string) int {
		n, err := strconv.Atoi(get(k))
		if err != nil || n < 0 {
			return 0
		}
		return n
	}
	cp := &model.CreditPoints{
		Methodical:    num("credits_methodical"),
		Technical:     num("credits_technical"),
		Communication: num("credits_communication"),
	}
	if cp.Empty() {
		return nil
	}
	return cp
}

func (s *Server) handleCourseSave(w http.ResponseWriter, r *http.Request, sess Session, client *gh.Client) {
	d, err := s.loadDraft(r.Context(), sess, client)
	if err != nil {
		s.fail(w, "could not read the training dates from GitHub", err)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.fail(w, "could not read the form", err)
		return
	}
	get := func(k string) string { return strings.TrimSpace(r.PostFormValue(k)) }
	c := model.Course{
		ShortTitle: get("short_title"), Title: get("title"),
		BlurbDE: get("blurb_de"), BlurbEN: get("blurb_en"),
		Certification: get("certification"), CreditPoints: parseCreditPoints(get),
		URL: get("url"), URLEn: get("url_en"),
		Trainers: parseTrainers(r), ShowOn: parseShowOn(r),
	}
	storedID := r.PathValue("id")
	isNew := storedID == ""
	// The id is made from the short title for as long as it may change: a
	// new course, or one only in this draft and without dates. After that
	// it is fixed and the short title is just text. The form has no id
	// field; one posted anyway (a tab open since before) is not read.
	c.ID = storedID
	if isNew || d.CourseIDEditable(storedID) {
		c.ID = courseIDFrom(c.ShortTitle)
	}
	c.URL = followID(c.URL, c.ID, storedID)
	if !isNew {
		if stored, ok := d.Doc.Model().FindCourse(storedID); ok && sameSites(stored.ShowOn, c.ShowOn) {
			c.ShowOn = stored.ShowOn
		}
	}

	reshow := func(title string, problems []validate.Problem, warnings []validate.Warning) {
		data := map[string]any{
			"Title": title, "Course": c, "Draft": d, "Login": sess.Login,
			"IsNew": isNew, "Problems": problems, "Warnings": warnings,
			"KnownTrainers": model.KnownTrainers, "OtherTrainers": otherTrainers(c.Trainers),
			"KnownSites": model.KnownSites, "OtherSites": otherSites(c.ShowOn),
			"Notes": validate.CourseNotes(c),
		}
		for k, v := range courseIDState(d, storedID) {
			data[k] = v
		}
		s.render(w, "courseform.gohtml", data)
	}

	// The form's own checks and everything Review & propose would say about
	// this course, in one round. A course with no id, or another course's,
	// cannot be told apart from that course in the model, so it is checked
	// under an id no course can have; its id problem is the form's own.
	before := d.Doc.Model()
	probe := c
	if c.ID == "" || courseIDTaken(before.Courses, c.ID, storedID) {
		probe.ID = "\x00"
	}
	local := courseProblems(c, before.Courses, storedID)
	if problems := s.editProblems(r, client, before, withCourse(before, storedID, probe), "/courses/"+probe.ID, local); len(problems) > 0 {
		reshow("Fix these first", problems, nil)
		return
	}

	// Same two-stage gate as the date form: advisory first, then through.
	// Notes are deliberately not part of it: they inform, they ask nothing.
	if r.PostFormValue("confirm_warnings") != "1" {
		if warnings := validate.CourseWarnings(c); len(warnings) > 0 {
			reshow("Have a look at these", nil, warnings)
			return
		}
	}

	if isNew {
		err = d.AddCourse(c)
	} else {
		err = d.UpdateCourse(storedID, c)
	}
	if err != nil {
		s.fail(w, "could not apply the course edit", err)
		return
	}
	http.Redirect(w, r, "/courses", http.StatusSeeOther)
}

// withCourse is m with the course form's edit applied, for checking. The form
// does not carry the dates, so an edited course keeps the ones it has.
func withCourse(m model.Trainings, storedID string, c model.Course) model.Trainings {
	out := model.Trainings{}
	for _, ex := range m.Courses {
		if storedID != "" && ex.ID == storedID {
			nc := c
			nc.Dates = ex.Dates
			out.Courses = append(out.Courses, nc)
			continue
		}
		out.Courses = append(out.Courses, ex)
	}
	if storedID == "" {
		out.Courses = append(out.Courses, c)
	}
	return out
}

// handleCourseRemoveConfirm asks before a course added in this draft is taken
// out again, the way a date's removal is asked about: it is a GET, changes
// nothing, and names what goes, the dates included. A course on main has no
// removal; a course owns its dates, and removing a published one would take
// them along without a word.
func (s *Server) handleCourseRemoveConfirm(w http.ResponseWriter, r *http.Request, sess Session, client *gh.Client) {
	d, err := s.loadDraft(r.Context(), sess, client)
	if err != nil {
		s.fail(w, "could not read the training dates from GitHub", err)
		return
	}
	id := r.PathValue("id")
	c, ok := d.Doc.Model().FindCourse(id)
	if !ok {
		// Already gone: a second tab, or the back button after removing it.
		http.Redirect(w, r, "/courses", http.StatusSeeOther)
		return
	}
	if !d.AddedCourse(id) {
		http.Redirect(w, r, "/courses/"+id, http.StatusSeeOther)
		return
	}
	s.render(w, "confirmremovecourse.gohtml", map[string]any{
		"Title": "Remove " + c.ShortTitle + " from the draft?", "Draft": d, "Login": sess.Login,
		"Course": c,
	})
}

func (s *Server) handleCourseRemove(w http.ResponseWriter, r *http.Request, sess Session, client *gh.Client) {
	d, err := s.loadDraft(r.Context(), sess, client)
	if err != nil {
		s.fail(w, "could not read the training dates from GitHub", err)
		return
	}
	id := r.PathValue("id")
	if _, ok := d.Doc.Model().FindCourse(id); !ok {
		http.Redirect(w, r, "/courses", http.StatusSeeOther)
		return
	}
	if !d.AddedCourse(id) {
		// Only a hand-made POST gets here: the form offers no removal.
		http.Redirect(w, r, "/courses/"+id, http.StatusSeeOther)
		return
	}
	if err := d.RemoveAddedCourse(id); err != nil {
		s.fail(w, "could not remove the course", err)
		return
	}
	http.Redirect(w, r, "/courses", http.StatusSeeOther)
}
