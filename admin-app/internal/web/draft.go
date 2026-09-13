package web

import (
	"strings"
	"sync"
	"time"

	"arc42-trainings-admin/internal/model"
	"arc42-trainings-admin/internal/yamldoc"
)

// Change is one entry in the "3 unpublished changes" summary, one card in the
// review report, and one section in the generated pull request body.
//
// It carries the whole date on both sides, not a summary sentence, because the
// review screen has to answer "what exactly did I change" without the reader
// parsing YAML. Before is nil for an addition and After is nil for a removal;
// the zero value on the missing side is what makes the comparison work in one
// direction as well as two.
type Change struct {
	Kind             string // "added" | "updated" | "removed"
	DateID           string // "course:<id>" for a course edit — also the collapse key
	Summary          string
	CourseID         string
	CourseShortTitle string
	Code             string // the booking code, for the report header

	Before, After             *model.Date
	BeforeCourse, AfterCourse *model.Course
}

// Fields is the report: the fields this change actually touched, rendered.
func (c Change) Fields() []model.FieldChange {
	if c.BeforeCourse != nil || c.AfterCourse != nil {
		return model.DiffCourses(course(c.BeforeCourse), course(c.AfterCourse))
	}
	return model.DiffDates(date(c.Before), date(c.After))
}

// Headline names the thing that changed, in the order a maintainer reads it:
// which course, which booking code, which dates.
func (c Change) Headline() string {
	var parts []string
	if c.CourseShortTitle != "" {
		parts = append(parts, c.CourseShortTitle)
	} else if c.CourseID != "" {
		parts = append(parts, c.CourseID)
	}
	if c.BeforeCourse != nil || c.AfterCourse != nil {
		return strings.Join(append(parts, "course details"), " · ")
	}
	if c.Code != "" {
		parts = append(parts, c.Code)
	}
	if span := date(c.After).Span(); span != "" {
		parts = append(parts, span)
	} else if span := date(c.Before).Span(); span != "" {
		parts = append(parts, span)
	}
	if len(parts) == 0 {
		return c.DateID
	}
	return strings.Join(parts, " · ")
}

func date(d *model.Date) model.Date {
	if d == nil {
		return model.Date{}
	}
	return *d
}

func course(c *model.Course) model.Course {
	if c == nil {
		return model.Course{}
	}
	return *c
}

// Draft is an editing session's uncommitted state. It lives in memory only:
// there is no database and no volume, so a restart discards it. That is a
// deliberate trade — the source of truth is untouched in GitHub throughout, so
// the worst case is re-entering a few fields.
type Draft struct {
	Doc      *yamldoc.Doc
	FileSHA  string
	HeadSHA  string
	Changes  []Change
	LoadedAt time.Time
}

func (d *Draft) Dirty() bool { return len(d.Changes) > 0 }

func (d *Draft) record(c Change) {
	// Repeated edits of the same date collapse into one entry, so a PR body
	// describes outcomes rather than keystrokes. An add followed by edits stays
	// an "added".
	for i, old := range d.Changes {
		if old.DateID != c.DateID {
			continue
		}
		// The surviving "before" is the earliest one. Five edits to one date
		// are one before/after pair; keeping the latest would report the last
		// keystroke and call it the change. A date added in this same draft has
		// no earlier before, and then the new one stands.
		if old.Before != nil {
			c.Before = old.Before
		}
		if old.BeforeCourse != nil {
			c.BeforeCourse = old.BeforeCourse
		}
		if old.Kind == "added" && c.Kind == "updated" {
			c.Kind = "added"
		}
		d.Changes[i] = c
		return
	}
	d.Changes = append(d.Changes, c)
}

func (d *Draft) UpdateDate(id string, nd model.Date) error {
	// Read the row before applying: afterwards the previous values are gone,
	// and they are half of every line of the report.
	row, found := d.Doc.Model().FindDate(id)
	if err := d.Doc.UpdateDate(id, nd); err != nil {
		return err
	}
	c := Change{
		Kind: "updated", DateID: id, Code: nd.Code, After: &nd,
		Summary: nd.Code + " — " + nd.Start + " to " + nd.End + ", " + nd.Status,
	}
	if found {
		before := row.Date
		c.Before, c.CourseID, c.CourseShortTitle = &before, row.CourseID, row.CourseShortTitle
	}
	d.record(c)
	return nil
}

func (d *Draft) AddDate(courseID string, nd model.Date) error {
	if err := d.Doc.AddDate(courseID, nd); err != nil {
		return err
	}
	c := Change{
		Kind: "added", DateID: nd.ID, Code: nd.Code, After: &nd, CourseID: courseID,
		Summary: nd.Code + " — " + nd.Start + " to " + nd.End,
	}
	if row, ok := d.Doc.Model().FindDate(nd.ID); ok {
		c.CourseShortTitle = row.CourseShortTitle
	}
	d.record(c)
	return nil
}

func (d *Draft) DeleteDate(id string) error {
	row, ok := d.Doc.Model().FindDate(id)
	if !ok {
		return nil
	}
	if err := d.Doc.DeleteDate(id); err != nil {
		return err
	}
	before := row.Date
	d.record(Change{
		Kind: "removed", DateID: id, Code: row.Date.Code, Before: &before,
		CourseID: row.CourseID, CourseShortTitle: row.CourseShortTitle,
		Summary: row.Date.Code,
	})
	return nil
}

func (d *Draft) AddCourse(nc model.Course) error {
	if err := d.Doc.AddCourse(nc); err != nil {
		return err
	}
	d.record(Change{
		Kind: "added", DateID: "course:" + nc.ID, AfterCourse: &nc,
		CourseID: nc.ID, CourseShortTitle: nc.ShortTitle,
		Summary: nc.ShortTitle + " — " + nc.Title,
	})
	return nil
}

func (d *Draft) UpdateCourse(id string, nc model.Course) error {
	before, found := d.Doc.Model().FindCourse(id)
	if err := d.Doc.UpdateCourse(id, nc); err != nil {
		return err
	}
	c := Change{
		Kind: "updated", DateID: "course:" + id, AfterCourse: &nc,
		CourseID: id, CourseShortTitle: nc.ShortTitle, Summary: nc.ShortTitle,
	}
	if found {
		c.BeforeCourse = &before
	}
	d.record(c)
	return nil
}

// Drafts holds one draft per session.
type Drafts struct {
	mu sync.Mutex
	m  map[string]*Draft
}

func NewDrafts() *Drafts { return &Drafts{m: map[string]*Draft{}} }

func (s *Drafts) Get(sessionID string) (*Draft, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.m[sessionID]
	return d, ok
}

func (s *Drafts) Put(sessionID string, d *Draft) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[sessionID] = d
}

func (s *Drafts) Discard(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, sessionID)
}
