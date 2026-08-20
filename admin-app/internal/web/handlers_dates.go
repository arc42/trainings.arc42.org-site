package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"arc42-trainings-admin/internal/gh"
	"arc42-trainings-admin/internal/model"
	"arc42-trainings-admin/internal/validate"
	"arc42-trainings-admin/internal/yamldoc"
)

// loadDraft returns the session's draft, fetching trainings.yml from GitHub the
// first time. The file and head SHAs recorded here are what publish compares
// against to detect a concurrent edit.
func (s *Server) loadDraft(ctx context.Context, sess Session, client *gh.Client) (*Draft, error) {
	if d, ok := s.drafts.Get(sess.ID); ok {
		return d, nil
	}
	content, fileSHA, headSHA, err := client.ReadFile(ctx, dataPath)
	if err != nil {
		return nil, err
	}
	doc, err := yamldoc.Parse(content)
	if err != nil {
		return nil, err
	}
	d := &Draft{Doc: doc, FileSHA: fileSHA, HeadSHA: headSHA, LoadedAt: time.Now()}
	s.drafts.Put(sess.ID, d)
	return d, nil
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request, sess Session, client *gh.Client) {
	d, err := s.loadDraft(r.Context(), sess, client)
	if err != nil {
		s.fail(w, "could not read the training dates from GitHub", err)
		return
	}
	m := d.Doc.Model()
	today := time.Now().Format("2006-01-02")
	type listRow struct {
		model.Row
		Past bool
	}
	var rows []listRow
	for _, r0 := range m.Rows() {
		rows = append(rows, listRow{Row: r0, Past: r0.Date.End < today})
	}
	s.render(w, "list.gohtml", map[string]any{
		"Title": "Training dates", "Rows": rows, "Draft": d, "Login": sess.Login,
	})
}

// newDateDefaults is the starting point for a blank new-date form: the enum
// defaults, plus what the first course in the dropdown would repeat. Switching
// the dropdown re-applies the rest client-side, so the two stay in step.
func newDateDefaults(courses []model.Course) model.Date {
	d := model.Date{Status: "open", Format: "public", Language: "de"}
	if len(courses) == 0 {
		return d
	}
	def := model.DefaultsFor(courses[0])
	d.City, d.Country, d.Pricing, d.Trainers = def.City, def.Country, def.Pricing, def.Trainers
	return d
}

func (s *Server) handleDateForm(w http.ResponseWriter, r *http.Request, sess Session, client *gh.Client) {
	d, err := s.loadDraft(r.Context(), sess, client)
	if err != nil {
		s.fail(w, "could not read the training dates from GitHub", err)
		return
	}
	m := d.Doc.Model()
	blank := newDateDefaults(m.Courses)
	data := map[string]any{
		"Title": "New date", "Courses": m.Courses, "Draft": d, "Login": sess.Login,
		"Formats": model.Formats, "Languages": model.Languages, "Statuses": model.Statuses,
		"IsNew": true, "Date": blank,
		// CourseID is set unconditionally: a key the template reads but the
		// handler never wrote used to abort rendering halfway down the form.
		"CourseID": "", "KnownTrainers": model.KnownTrainers,
		"OtherTrainers": otherTrainers(blank.Trainers),
	}
	// "Duplicate" is the common real action — next year's run of a course — so
	// /dates/new?from=<id> pre-fills from an existing date with a cleared id.
	// Start and end are cleared along with the identity: they are what changes
	// between two runs, and inheriting them let a half-edited pair through with
	// one date still in the source year.
	if from := r.URL.Query().Get("from"); from != "" {
		if row, ok := m.FindDate(from); ok {
			src := row.Date
			src.ID, src.Code, src.URL = "", "", ""
			src.Start, src.End = "", ""
			data["Date"] = src
			data["CourseID"] = row.CourseID
			data["OtherTrainers"] = otherTrainers(src.Trainers)
		}
	}
	if id := r.PathValue("id"); id != "" {
		row, ok := m.FindDate(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		data["Title"] = "Edit " + row.Date.Code
		data["Date"] = row.Date
		data["CourseID"] = row.CourseID
		data["OtherTrainers"] = otherTrainers(row.Date.Trainers)
		data["IsNew"] = false
	}
	s.render(w, "dateform.gohtml", data)
}

// parseDateForm reads the detail form. Empty optional fields stay empty and are
// omitted from the YAML rather than written as "".
func parseDateForm(r *http.Request) (model.Date, string) {
	get := func(k string) string { return strings.TrimSpace(r.PostFormValue(k)) }
	d := model.Date{
		ID: get("id"), Code: get("code"), Start: get("start"), End: get("end"),
		City: get("city"), Country: strings.ToUpper(get("country")),
		Language: get("language"), Format: get("format"),
		Pricing: get("pricing"), FewSeats: get("few_seats"),
		Status: get("status"),
	}
	// The form no longer asks for the registration link. Every published date
	// points at the same anchored page, so the id already determines it.
	d.URL = model.RegistrationURL(d.ID)
	d.Trainers = parseTrainers(r)
	return d, get("course_id")
}

func (s *Server) handleDateSave(w http.ResponseWriter, r *http.Request, sess Session, client *gh.Client) {
	d, err := s.loadDraft(r.Context(), sess, client)
	if err != nil {
		s.fail(w, "could not read the training dates from GitHub", err)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.fail(w, "could not read the form", err)
		return
	}
	nd, courseID := parseDateForm(r)
	isNew := r.PathValue("id") == "new"

	// Validate the prospective document before touching the draft, so a
	// rejected edit leaves no trace.
	probe := d.Doc.Model()
	probe = applyToModel(probe, nd, courseID, isNew, r.PathValue("id"))

	reshow := func(title string, problems []validate.Problem, warnings []validate.Warning) {
		m := d.Doc.Model()
		s.render(w, "dateform.gohtml", map[string]any{
			"Title": title, "Courses": m.Courses, "Draft": d, "Login": sess.Login,
			"Formats": model.Formats, "Languages": model.Languages, "Statuses": model.Statuses,
			"IsNew": isNew, "Date": nd, "CourseID": courseID,
			"Problems": problems, "Warnings": warnings,
			"KnownTrainers": model.KnownTrainers, "OtherTrainers": otherTrainers(nd.Trainers),
		})
	}

	if problems := validate.Rules(probe); len(problems) > 0 {
		reshow("Fix these first", problems, nil)
		return
	}

	// Warnings gate the first submit but never the second. Errors above are
	// checked first and independently, so acknowledging a warning can never
	// carry a genuinely invalid date past the blocking rules.
	if r.PostFormValue("confirm_warnings") != "1" {
		today := time.Now().Format("2006-01-02")
		if warnings := validate.DateWarnings(nd, courseID, today, isNew); len(warnings) > 0 {
			reshow("Have a look at these", nil, warnings)
			return
		}
	}

	if isNew {
		err = d.AddDate(courseID, nd)
	} else {
		err = d.UpdateDate(r.PathValue("id"), nd)
	}
	if err != nil {
		s.fail(w, "could not apply the edit", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// applyToModel produces the model as it would look after the edit, for
// validation. It never mutates the draft.
func applyToModel(m model.Trainings, nd model.Date, courseID string, isNew bool, existingID string) model.Trainings {
	out := model.Trainings{}
	for _, c := range m.Courses {
		nc := c
		nc.Dates = nil
		for _, d := range c.Dates {
			if !isNew && d.ID == existingID {
				nc.Dates = append(nc.Dates, nd)
				continue
			}
			nc.Dates = append(nc.Dates, d)
		}
		if isNew && c.ID == courseID {
			nc.Dates = append(nc.Dates, nd)
		}
		out.Courses = append(out.Courses, nc)
	}
	return out
}

// handleDateDeleteConfirm renders the row about to be removed and asks. The
// table's Remove control links here instead of posting: removal is the one
// action in the app with no per-change undo — the only way back is discarding
// the entire draft, which takes every unrelated edit with it.
//
// It is a GET and therefore must not change anything, not even lazily: a
// prefetching browser follows it without the operator ever clicking.
func (s *Server) handleDateDeleteConfirm(w http.ResponseWriter, r *http.Request, sess Session, client *gh.Client) {
	d, err := s.loadDraft(r.Context(), sess, client)
	if err != nil {
		s.fail(w, "could not read the training dates from GitHub", err)
		return
	}
	row, ok := d.Doc.Model().FindDate(r.PathValue("id"))
	if !ok {
		// Already gone — a second tab, or the back button after removing it.
		// Nothing to confirm and nothing broken, so just show the list.
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, "confirmdelete.gohtml", map[string]any{
		"Title": "Remove " + row.Date.Code + "?", "Draft": d, "Login": sess.Login,
		"Row": row,
	})
}

func (s *Server) handleDateDelete(w http.ResponseWriter, r *http.Request, sess Session, client *gh.Client) {
	d, err := s.loadDraft(r.Context(), sess, client)
	if err != nil {
		s.fail(w, "could not read the training dates from GitHub", err)
		return
	}
	if err := d.DeleteDate(r.PathValue("id")); err != nil {
		s.fail(w, "could not remove the date", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleDiscard(w http.ResponseWriter, r *http.Request, sess Session, _ *gh.Client) {
	s.drafts.Discard(sess.ID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
