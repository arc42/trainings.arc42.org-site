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
		s.render(w, "courseform.gohtml", data)
		return
	}
	for _, c := range d.Doc.Model().Courses {
		if c.ID == id {
			data["Title"] = "Edit " + c.ShortTitle
			data["IsNew"] = false
			data["Course"] = c
			data["OtherTrainers"] = otherTrainers(c.Trainers)
			data["OtherSites"] = otherSites(c.ShowOn)
			data["Notes"] = validate.CourseNotes(c)
			s.render(w, "courseform.gohtml", data)
			return
		}
	}
	http.NotFound(w, r)
}

// courseProblems checks what HTML validation cannot: uniqueness of the id, and
// that a course actually names a trainer. The feed schema enforces both at
// propose time, but catching them here keeps a broken course out of the draft.
func courseProblems(c model.Course, existing []model.Course, isNew bool) []validate.Problem {
	var problems []validate.Problem
	field := "courses." + c.ID
	if c.ID == "" {
		problems = append(problems, validate.Problem{Field: "courses.id", Message: "must not be empty"})
	}
	for _, ex := range existing {
		if isNew && ex.ID == c.ID {
			problems = append(problems, validate.Problem{
				Field: field + ".id", Message: c.ID + " is already used by another course",
			})
		}
	}
	for _, f := range []struct{ name, val string }{
		{"short_title", c.ShortTitle}, {"title", c.Title}, {"url", c.URL},
	} {
		if strings.TrimSpace(f.val) == "" {
			problems = append(problems, validate.Problem{Field: field + "." + f.name, Message: "must not be empty"})
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
		ID: get("id"), ShortTitle: get("short_title"), Title: get("title"),
		BlurbDE: get("blurb_de"), BlurbEN: get("blurb_en"),
		Certification: get("certification"), CreditPoints: parseCreditPoints(get),
		URL: get("url"), URLEn: get("url_en"),
		Trainers: parseTrainers(r), ShowOn: parseShowOn(r),
	}
	isNew := r.PathValue("id") == ""
	if !isNew {
		if stored, ok := d.Doc.Model().FindCourse(r.PathValue("id")); ok && sameSites(stored.ShowOn, c.ShowOn) {
			c.ShowOn = stored.ShowOn
		}
	}

	reshow := func(title string, problems []validate.Problem, warnings []validate.Warning) {
		s.render(w, "courseform.gohtml", map[string]any{
			"Title": title, "Course": c, "Draft": d, "Login": sess.Login,
			"IsNew": isNew, "Problems": problems, "Warnings": warnings,
			"KnownTrainers": model.KnownTrainers, "OtherTrainers": otherTrainers(c.Trainers),
			"KnownSites": model.KnownSites, "OtherSites": otherSites(c.ShowOn),
			"Notes": validate.CourseNotes(c),
		})
	}

	if problems := courseProblems(c, d.Doc.Model().Courses, isNew); len(problems) > 0 {
		reshow("Fix these first", validate.ExplainAll(d.Doc.Model(), problems), nil)
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
		err = d.UpdateCourse(r.PathValue("id"), c)
	}
	if err != nil {
		s.fail(w, "could not apply the course edit", err)
		return
	}
	http.Redirect(w, r, "/courses", http.StatusSeeOther)
}
