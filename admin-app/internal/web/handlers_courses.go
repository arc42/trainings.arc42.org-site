package web

import (
	"net/http"
	"strings"

	"arc42-trainings-admin/internal/gh"
	"arc42-trainings-admin/internal/model"
)

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
	id := r.PathValue("id")
	for _, c := range d.Doc.Model().Courses {
		if c.ID == id {
			s.render(w, "courseform.gohtml", map[string]any{
				"Title": "Edit " + c.ShortTitle, "Course": c, "Draft": d, "Login": sess.Login,
			})
			return
		}
	}
	http.NotFound(w, r)
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
		Blurb: get("blurb"), Certification: get("certification"),
		Credits: get("credits"), URL: get("url"),
	}
	for _, name := range strings.Split(get("trainers"), ",") {
		if n := strings.TrimSpace(name); n != "" {
			c.Trainers = append(c.Trainers, n)
		}
	}
	if err := d.UpdateCourse(r.PathValue("id"), c); err != nil {
		s.fail(w, "could not apply the course edit", err)
		return
	}
	http.Redirect(w, r, "/courses", http.StatusSeeOther)
}
