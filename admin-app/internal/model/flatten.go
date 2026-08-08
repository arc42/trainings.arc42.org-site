package model

import "sort"

// Row is one date presented with the identity of the course that owns it.
type Row struct {
	Date             Date
	CourseID         string
	CourseShortTitle string
}

// Rows flattens every date across every course, sorted by start date.
// Code breaks ties so the order is stable.
func (t Trainings) Rows() []Row {
	var rows []Row
	for _, c := range t.Courses {
		for _, d := range c.Dates {
			rows = append(rows, Row{Date: d, CourseID: c.ID, CourseShortTitle: c.ShortTitle})
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
				return Row{Date: d, CourseID: c.ID, CourseShortTitle: c.ShortTitle}, true
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
