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

// Most published dates name no trainers of their own, so a row that reads
// Date.Trainers alone is blank for the majority of the table. The fallback is
// the site's (`d.trainers | default: course.trainers`), and the row has to say
// which of the two it is showing.
func TestTrainersFallBackToTheCourseAndSayThatTheyDid(t *testing.T) {
	tr := Trainings{Courses: []Course{{
		ID: "msa", ShortTitle: "MSA", Trainers: []string{"Peter Hruschka", "Dr. Gernot Starke"},
		Dates: []Date{
			{ID: "msa-a", Start: "2026-01-01"},
			{ID: "msa-b", Start: "2026-02-01", Trainers: []string{"Wolfgang Reimesch"}},
		},
	}}}
	rows := tr.Rows()

	if got := rows[0].TrainerSurnames(); got != "Hruschka, Starke" {
		t.Errorf("inherited trainers = %q, want %q", got, "Hruschka, Starke")
	}
	if !rows[0].TrainersInherited() {
		t.Error("a date with no trainers of its own is not marked as inheriting them")
	}
	if got := rows[1].TrainerSurnames(); got != "Reimesch" {
		t.Errorf("own trainers = %q, want %q", got, "Reimesch")
	}
	if rows[1].TrainersInherited() {
		t.Error("a date naming its own trainer was marked as inheriting")
	}
}

// Nothing to inherit is not inheritance: the row would otherwise claim the
// course assigned somebody when neither of them names anyone.
func TestNoTrainersAnywhereIsTheAbsentValue(t *testing.T) {
	r := Trainings{Courses: []Course{{ID: "msa", Dates: []Date{{ID: "msa-a"}}}}}.Rows()[0]
	if r.TrainersInherited() {
		t.Error("an empty course roster was reported as inherited")
	}
	if got := r.TrainerSurnames(); got != NoValue {
		t.Errorf("TrainerSurnames() = %q, want the absent-value dash", got)
	}
}

// A missing price is a broken record (the Ruby validator requires one on every
// date), so the list has to show that rather than an empty cell or a guess.
func TestAMissingPriceShowsTheAbsentValue(t *testing.T) {
	rows := Trainings{Courses: []Course{{ID: "msa", Dates: []Date{
		{ID: "msa-a", Start: "2026-01-01"},
		{ID: "msa-b", Start: "2026-02-01", Price: &Price{Amount: 2100, Currency: "EUR", Alumni: 1900}},
	}}}}.Rows()
	if got := rows[0].PriceShort(); got != NoValue {
		t.Errorf("PriceShort() with no price = %q, want the absent-value dash", got)
	}
	if got := rows[1].PriceShort(); got != "€ 2,100" {
		t.Errorf("PriceShort() = %q, want %q (main amount only)", got, "€ 2,100")
	}
}

func TestCourseOfReturnsIndex(t *testing.T) {
	tr := fixture()
	c, idx, ok := tr.CourseOf("msa-dez-2026")
	if !ok || c.ID != "msa" || idx != 0 {
		t.Fatalf("CourseOf = %v, %d, %v", c, idx, ok)
	}
}
