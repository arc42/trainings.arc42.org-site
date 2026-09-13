package model

import (
	"sort"
	"strings"
)

// Row is one date presented with the identity of the course that owns it.
type Row struct {
	Date             Date
	CourseID         string
	CourseShortTitle string
	// CourseTrainers is the owning course's roster, which a date carries only
	// so it can fall back to it. Date.Trainers is optional and most published
	// dates leave it out, so a view reading Date.Trainers alone is blank for
	// the majority of rows; the site resolves the same way
	// (_includes/registration-form.html: `d.trainers | default: course.trainers`).
	CourseTrainers []string
}

// Trainers returns the names actually in force for this date: its own if it
// names any, the course's roster otherwise.
func (r Row) Trainers() []string {
	if len(r.Date.Trainers) > 0 {
		return r.Date.Trainers
	}
	return r.CourseTrainers
}

// TrainersInherited reports whether Trainers came from the course rather than
// from the date. A view marks the two apart because they are different facts:
// "these people are assigned to this run" and "nobody is assigned yet, so this
// is whoever teaches the course" — and only the second is an edit waiting to
// happen.
func (r Row) TrainersInherited() bool {
	return len(r.Date.Trainers) == 0 && len(r.CourseTrainers) > 0
}

// TrainerSurnames is the trainer cell of a one-line-per-date view.
func (r Row) TrainerSurnames() string {
	if s := strings.Join(Surnames(r.Trainers()), ", "); s != "" {
		return s
	}
	return NoValue
}

// PriceShort is the price cell of a one-line-per-date view: the main amount,
// without the alumni and early-bird detail.
//
// The dash is not cosmetic padding. scripts/validate_trainings.rb requires a
// price on every date, so a row showing it is a row whose data is broken —
// which is exactly what the list should surface rather than smooth over with
// an invented fallback.
func (r Row) PriceShort() string {
	if s := FormatPriceShort(r.Date.Price); s != "" {
		return s
	}
	return NoValue
}

// Rows flattens every date across every course, sorted by start date.
// Code breaks ties so the order is stable.
func (t Trainings) Rows() []Row {
	var rows []Row
	for _, c := range t.Courses {
		for _, d := range c.Dates {
			rows = append(rows, Row{Date: d, CourseID: c.ID, CourseShortTitle: c.ShortTitle, CourseTrainers: c.Trainers})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Date.Start != rows[j].Date.Start {
			return rows[i].Date.Start < rows[j].Date.Start
		}
		return rows[i].Date.Code < rows[j].Date.Code
	})
	return rows
}

// FindDate returns the row for a date id.
func (t Trainings) FindDate(id string) (Row, bool) {
	for _, c := range t.Courses {
		for _, d := range c.Dates {
			if d.ID == id {
				return Row{Date: d, CourseID: c.ID, CourseShortTitle: c.ShortTitle, CourseTrainers: c.Trainers}, true
			}
		}
	}
	return Row{}, false
}

// CourseOf returns the course owning a date, plus the date's index in it.
func (t *Trainings) CourseOf(dateID string) (*Course, int, bool) {
	for i := range t.Courses {
		for j := range t.Courses[i].Dates {
			if t.Courses[i].Dates[j].ID == dateID {
				return &t.Courses[i], j, true
			}
		}
	}
	return nil, 0, false
}

// FindCourse returns a course by id. The report needs the course as it was
// before an edit, which only the caller that has not yet applied the edit can
// still see.
func (t Trainings) FindCourse(id string) (Course, bool) {
	for _, c := range t.Courses {
		if c.ID == id {
			return c, true
		}
	}
	return Course{}, false
}
