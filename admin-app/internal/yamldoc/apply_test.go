package yamldoc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arc42-trainings-admin/internal/model"
)

const twoDates = `# header comment must survive
courses:
  - id: msa
    short_title: "MSA"
    dates:
      - id: a
        code: "A"
        start: "2026-01-01"
        end: "2026-01-02"
        city: "München"
        country: "DE"
        language: de
        format: public
        url: "https://example.org/a"
        status: open

      - id: b
        code: "B"
        start: "2026-02-01"
        end: "2026-02-02"
        language: en
        format: online
        url: "https://example.org/b"
        status: open
`

func TestUpdateDateTouchesOnlyThatEntry(t *testing.T) {
	doc, err := Parse([]byte(twoDates))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	row, _ := doc.Model().FindDate("a")
	d := row.Date
	d.Status = "full"
	if err := doc.UpdateDate("a", d); err != nil {
		t.Fatalf("UpdateDate: %v", err)
	}
	out := string(doc.Bytes())

	if !strings.Contains(out, "# header comment must survive") {
		t.Error("header comment was lost")
	}
	if !strings.Contains(out, `status: full`) {
		t.Error("status was not updated")
	}
	// Entry b must be byte-identical, blank separator line included.
	if !strings.Contains(out, "\n\n      - id: b\n") {
		t.Error("blank line before entry b was lost")
	}
	if !strings.Contains(out, `        url: "https://example.org/b"`) {
		t.Error("entry b was reformatted")
	}
	if strings.Contains(out, "status: open\n        status") {
		t.Error("duplicated key")
	}
}

func TestUpdateDateKeepsLoadBearingQuoting(t *testing.T) {
	doc, _ := Parse([]byte(twoDates))
	row, _ := doc.Model().FindDate("a")
	d := row.Date
	d.Country = "NO" // unquoted NO is boolean false in YAML 1.1
	_ = doc.UpdateDate("a", d)
	out := string(doc.Bytes())
	if !strings.Contains(out, `country: "NO"`) {
		t.Errorf("country lost its quotes:\n%s", out)
	}
	if !strings.Contains(out, `start: "2026-01-01"`) {
		t.Error("start lost its quotes")
	}
	// Re-parsing must yield the string "NO", not a boolean.
	reparsed, err := Parse(doc.Bytes())
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	r2, _ := reparsed.Model().FindDate("a")
	if r2.Date.Country != "NO" {
		t.Errorf("country round-tripped as %q", r2.Date.Country)
	}
}

func TestUpdateDateOmitsEmptyOptionalFields(t *testing.T) {
	doc, _ := Parse([]byte(twoDates))
	row, _ := doc.Model().FindDate("a")
	d := row.Date
	d.City = ""
	d.Country = ""
	d.Format = "online"
	_ = doc.UpdateDate("a", d)
	out := string(doc.Bytes())
	if strings.Contains(out, "city:") && strings.Contains(out, `city: ""`) {
		t.Error("empty city was emitted instead of omitted")
	}
}

func TestAddDateAppendsToTheRightCourse(t *testing.T) {
	doc, _ := Parse([]byte(twoDates))
	err := doc.AddDate("msa", model.Date{
		ID: "c", Code: "C", Start: "2026-03-01", End: "2026-03-02",
		Language: "de", Format: "online", URL: "https://example.org/c", Status: "open",
	})
	if err != nil {
		t.Fatalf("AddDate: %v", err)
	}
	reparsed, err := Parse(doc.Bytes())
	if err != nil {
		t.Fatalf("reparse after add: %v\n%s", err, doc.Bytes())
	}
	if len(reparsed.Model().Courses[0].Dates) != 3 {
		t.Fatalf("want 3 dates, got %d:\n%s", len(reparsed.Model().Courses[0].Dates), doc.Bytes())
	}
	if _, ok := reparsed.Model().FindDate("c"); !ok {
		t.Error("new date not found after add")
	}
}

func TestDeleteDateLeavesNeighbourIntact(t *testing.T) {
	doc, _ := Parse([]byte(twoDates))
	if err := doc.DeleteDate("a"); err != nil {
		t.Fatalf("DeleteDate: %v", err)
	}
	out := string(doc.Bytes())
	if strings.Contains(out, "id: a") {
		t.Error("entry a still present")
	}
	if !strings.Contains(out, "id: b") {
		t.Error("entry b was removed too")
	}
	if !strings.Contains(out, "# header comment must survive") {
		t.Error("header comment was lost")
	}
	reparsed, err := Parse(doc.Bytes())
	if err != nil {
		t.Fatalf("reparse after delete: %v\n%s", err, out)
	}
	if len(reparsed.Model().Courses[0].Dates) != 1 {
		t.Errorf("want 1 date left, got %d", len(reparsed.Model().Courses[0].Dates))
	}
}

func TestUpdateCourseDoesNotDisturbItsDates(t *testing.T) {
	doc, _ := Parse([]byte(twoDates))
	c := doc.Model().Courses[0]
	c.ShortTitle = "MSA renamed"
	if err := doc.UpdateCourse("msa", c); err != nil {
		t.Fatalf("UpdateCourse: %v", err)
	}
	out := string(doc.Bytes())
	if !strings.Contains(out, `short_title: "MSA renamed"`) {
		t.Error("short_title not updated")
	}
	if !strings.Contains(out, "\n\n      - id: b\n") {
		t.Error("dates were reformatted by a course-level edit")
	}
}

// TestGoldenUpdateStatus pins the exact output so a reviewer can see, in the
// repository, that a status change is a one-line diff.
func TestGoldenUpdateStatus(t *testing.T) {
	doc, _ := Parse([]byte(twoDates))
	row, _ := doc.Model().FindDate("a")
	d := row.Date
	d.Status = "waitlist"
	_ = doc.UpdateDate("a", d)

	goldenPath := filepath.Join("testdata", "golden_update_status.yml")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, doc.Bytes(), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (regenerate with UPDATE_GOLDEN=1): %v", err)
	}
	if string(doc.Bytes()) != string(want) {
		t.Errorf("output differs from golden:\n--- got ---\n%s", doc.Bytes())
	}
}

func TestAddCourseAppendsAValidCourse(t *testing.T) {
	doc, err := Parse([]byte(twoDates))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	err = doc.AddCourse(model.Course{
		ID: "flex", ShortTitle: "FLEX", Title: "Flexible Architectures",
		URL: "https://example.org/flex", Trainers: []string{"Dr. Gernot Starke"},
		Blurb: "A course about keeping architectures able to change.",
	})
	if err != nil {
		t.Fatalf("AddCourse: %v", err)
	}
	out := string(doc.Bytes())

	reparsed, err := Parse(doc.Bytes())
	if err != nil {
		t.Fatalf("reparse after AddCourse: %v\n%s", err, out)
	}
	m := reparsed.Model()
	if len(m.Courses) != 2 {
		t.Fatalf("want 2 courses, got %d:\n%s", len(m.Courses), out)
	}
	c := m.Courses[1]
	if c.ID != "flex" || c.ShortTitle != "FLEX" {
		t.Errorf("new course = %+v", c)
	}
	// validate_trainings.rb requires 'dates' to be an Array on every course, so
	// a brand-new course must carry an empty one rather than nothing.
	if c.Dates != nil && len(c.Dates) != 0 {
		t.Errorf("new course should start with no dates, got %d", len(c.Dates))
	}
	if !strings.Contains(out, "dates: []") {
		t.Errorf("new course is missing an empty dates array:\n%s", out)
	}
	// The existing course must be untouched.
	if !strings.Contains(out, "# header comment must survive") {
		t.Error("header comment was lost")
	}
	if !strings.Contains(out, "\n\n      - id: b\n") {
		t.Error("the existing course's dates were reformatted")
	}
}

// TestAddDateIntoFreshCourse covers the sequel to AddCourse: the first date
// added to a course whose dates key is the empty flow sequence "[]". Inserting
// block entries under "dates: []" would produce YAML that no longer parses, so
// that line has to be rewritten rather than appended to.
func TestAddDateIntoFreshCourse(t *testing.T) {
	doc, _ := Parse([]byte(twoDates))
	if err := doc.AddCourse(model.Course{
		ID: "flex", ShortTitle: "FLEX", Title: "Flexible Architectures",
		URL: "https://example.org/flex", Trainers: []string{"Dr. Gernot Starke"},
	}); err != nil {
		t.Fatalf("AddCourse: %v", err)
	}
	err := doc.AddDate("flex", model.Date{
		ID: "flex-1", Code: "26-11 FLEX", Start: "2026-11-02", End: "2026-11-04",
		City: "Berlin", Country: "DE", Language: "de", Format: "public",
		URL: "https://example.org/flex-1", Status: "open",
	})
	if err != nil {
		t.Fatalf("AddDate into fresh course: %v", err)
	}
	reparsed, err := Parse(doc.Bytes())
	if err != nil {
		t.Fatalf("reparse: %v\n%s", err, doc.Bytes())
	}
	m := reparsed.Model()
	if len(m.Courses) != 2 || len(m.Courses[1].Dates) != 1 {
		t.Fatalf("expected 1 date on the new course:\n%s", doc.Bytes())
	}
	if m.Courses[1].Dates[0].ID != "flex-1" {
		t.Errorf("date = %+v", m.Courses[1].Dates[0])
	}
	if strings.Contains(string(doc.Bytes()), "dates: []") {
		t.Error("the empty flow sequence should have been replaced by a block sequence")
	}
}

func TestCourseURLEnSurvivesUpdateCourse(t *testing.T) {
	src := `courses:
  - id: msa
    short_title: "MSA"
    title: "Mastering"
    url: "https://www.arc42.de/info-msa/"
    url_en: "https://trainings.arc42.org/courses/msa/"
    trainers: ["Peter Hruschka"]
    dates: []
`
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c := doc.Model().Courses[0]
	if c.URLEn != "https://trainings.arc42.org/courses/msa/" {
		t.Fatalf("parsed URLEn = %q", c.URLEn)
	}
	// An unrelated edit must not drop url_en (UpdateCourse re-renders the head).
	c.Title = "Mastering Software Architectures"
	if err := doc.UpdateCourse("msa", c); err != nil {
		t.Fatalf("UpdateCourse: %v", err)
	}
	out := string(doc.Bytes())
	if !strings.Contains(out, `url_en: "https://trainings.arc42.org/courses/msa/"`) {
		t.Errorf("url_en was dropped or unquoted:\n%s", out)
	}
	// url_en is written directly after url.
	if !strings.Contains(out, "url: \"https://www.arc42.de/info-msa/\"\n    url_en: ") {
		t.Errorf("url_en is not directly after url:\n%s", out)
	}
	// A course without url_en renders no url_en line at all.
	c.URLEn = ""
	if err := doc.UpdateCourse("msa", c); err != nil {
		t.Fatalf("UpdateCourse (clear): %v", err)
	}
	if strings.Contains(string(doc.Bytes()), "url_en") {
		t.Errorf("empty url_en must be omitted:\n%s", doc.Bytes())
	}
}
