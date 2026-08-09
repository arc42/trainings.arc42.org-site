package web

import (
	"sync"
	"time"

	"arc42-trainings-admin/internal/model"
	"arc42-trainings-admin/internal/yamldoc"
)

// Change is one entry in the "3 unpublished changes" summary and one bullet in
// the generated PR body.
type Change struct {
	Kind    string // "added" | "updated" | "removed"
	DateID  string
	Summary string
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

func (d *Draft) record(kind, dateID, summary string) {
	// Repeated edits of the same date collapse into one entry, so a PR body
	// describes outcomes rather than keystrokes. An add followed by edits stays
	// an "added".
	for i, c := range d.Changes {
		if c.DateID == dateID {
			if c.Kind == "added" && kind == "updated" {
				d.Changes[i].Summary = summary
				return
			}
			d.Changes[i] = Change{Kind: kind, DateID: dateID, Summary: summary}
			return
		}
	}
	d.Changes = append(d.Changes, Change{Kind: kind, DateID: dateID, Summary: summary})
}

func (d *Draft) UpdateDate(id string, nd model.Date) error {
	if err := d.Doc.UpdateDate(id, nd); err != nil {
		return err
	}
	d.record("updated", id, nd.Code+" — "+nd.Start+" to "+nd.End+", "+nd.Status)
	return nil
}

func (d *Draft) AddDate(courseID string, nd model.Date) error {
	if err := d.Doc.AddDate(courseID, nd); err != nil {
		return err
	}
	d.record("added", nd.ID, nd.Code+" — "+nd.Start+" to "+nd.End)
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
	d.record("removed", id, row.Date.Code)
	return nil
}

func (d *Draft) UpdateCourse(id string, nc model.Course) error {
	if err := d.Doc.UpdateCourse(id, nc); err != nil {
		return err
	}
	d.record("updated", "course:"+id, nc.ShortTitle)
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
