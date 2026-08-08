package model

import "testing"

func fixture() Trainings {
	return Trainings{Courses: []Course{
		{ID: "msa", ShortTitle: "MSA", Dates: []Date{
			{ID: "msa-dez-2026", Code: "26-12 MSA", Start: "2026-12-01"},
			{ID: "msa-sep-2026", Code: "26-09 MSA-EN", Start: "2026-09-29"},
		}},
		{ID: "flex", ShortTitle: "FLEX", Dates: []Date{
			{ID: "flex-okt-2026", Code: "26-10 FLEX", Start: "2026-10-15"},
		}},
	}}
}

func TestRowsSortedByStartAcrossCourses(t *testing.T) {
	rows := fixture().Rows()
	got := []string{rows[0].Date.ID, rows[1].Date.ID, rows[2].Date.ID}
	want := []string{"msa-sep-2026", "flex-okt-2026", "msa-dez-2026"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Rows() = %v, want %v", got, want)
		}
	}
}

func TestRowsCarryCourseIdentity(t *testing.T) {
	rows := fixture().Rows()
	if rows[1].CourseID != "flex" || rows[1].CourseShortTitle != "FLEX" {
		t.Errorf("row 1 course = %q/%q", rows[1].CourseID, rows[1].CourseShortTitle)
	}
}

func TestFindDate(t *testing.T) {
	r, ok := fixture().FindDate("flex-okt-2026")
	if !ok || r.Date.Code != "26-10 FLEX" {
		t.Fatalf("FindDate = %+v, %v", r, ok)
	}
	if _, ok := fixture().FindDate("nope"); ok {
		t.Error("FindDate found a nonexistent id")
	}
}

func TestCourseOfReturnsIndex(t *testing.T) {
	tr := fixture()
	c, idx, ok := tr.CourseOf("msa-dez-2026")
	if !ok || c.ID != "msa" || idx != 0 {
		t.Fatalf("CourseOf = %v, %d, %v", c, idx, ok)
	}
}
