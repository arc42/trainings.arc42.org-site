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

func (s *Server) handleDateForm(w http.ResponseWriter, r *http.Request, sess Session, client *gh.Client) {
	d, err := s.loadDraft(r.Context(), sess, client)
	if err != nil {
		s.fail(w, "could not read the training dates from GitHub", err)
		return
	}
	m := d.Doc.Model()
	data := map[string]any{
		"Title": "New date", "Courses": m.Courses, "Draft": d, "Login": sess.Login,
		"Formats": model.Formats, "Languages": model.Languages, "Statuses": model.Statuses,
		"IsNew": true, "Date": model.Date{Status: "open", Format: "public", Language: "de"},
	}
	// "Duplicate" is the common real action — next year's run of a course — so
	// /dates/new?from=<id> pre-fills from an existing date with a cleared id.
	if from := r.URL.Query().Get("from"); from != "" {
		if row, ok := m.FindDate(from); ok {
			src := row.Date
			src.ID, src.Code, src.URL = "", "", ""
			data["Date"] = src
			data["CourseID"] = row.CourseID
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
		URL: get("url"), Status: get("status"),
	}
	if t := get("trainers"); t != "" {
		for _, name := range strings.Split(t, ",") {
			if n := strings.TrimSpace(name); n != "" {
				d.Trainers = append(d.Trainers, n)
			}
		}
	}
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
	if problems := validate.Rules(probe); len(problems) > 0 {
		m := d.Doc.Model()
		s.render(w, "dateform.gohtml", map[string]any{
			"Title": "Fix these first", "Courses": m.Courses, "Draft": d, "Login": sess.Login,
			"Formats": model.Formats, "Languages": model.Languages, "Statuses": model.Statuses,
			"IsNew": isNew, "Date": nd, "CourseID": courseID, "Problems": problems,
		})
		return
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
