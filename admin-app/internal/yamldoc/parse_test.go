package yamldoc

import (
	"os"
	"path/filepath"
	"testing"
)

// realFile loads the repository's actual trainings.yml. The container mounts
// the whole repo at /src, so the data file is two levels up from admin-app.
func realFile(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "_data", "trainings.yml"))
	if err != nil {
		t.Fatalf("read trainings.yml: %v", err)
	}
	return b
}

// TestBytesIsIdenticalWithoutEdits is the load-bearing guarantee: parsing and
// serialising with no edits must not change a single byte. It holds by
// construction — we never re-emit the document — and this test is what keeps
// a future refactor from quietly reintroducing a yaml.Marshal round-trip.
func TestBytesIsIdenticalWithoutEdits(t *testing.T) {
	src := realFile(t)
	doc, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if string(doc.Bytes()) != string(src) {
		t.Error("Bytes() differs from the input with no edits applied")
	}
}

func TestModelReadsCoursesAndDates(t *testing.T) {
	doc, err := Parse(realFile(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	m := doc.Model()
	if len(m.Courses) == 0 {
		t.Fatal("no courses parsed")
	}
	rows := m.Rows()
	if len(rows) == 0 {
		t.Fatal("no dates parsed")
	}
	for _, r := range rows {
		if r.Date.Language != "de" && r.Date.Language != "en" {
			t.Errorf("date %s: language = %q, want de|en", r.Date.ID, r.Date.Language)
		}
		if r.Date.Start == "" || r.Date.Status == "" {
			t.Errorf("date %s: missing start or status", r.Date.ID)
		}
	}
}

func TestDateExtentCoversTheWholeEntry(t *testing.T) {
	src := []byte(`courses:
  - id: msa
    short_title: "MSA"
    dates:
      - id: a
        code: "A"
        status: open

      - id: b
        code: "B"
        status: open
`)
	doc, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	start, end, indent, ok := doc.dateExtent("a")
	if !ok {
		t.Fatal("dateExtent(a) not found")
	}
	// Lines are 1-based: entry "a" spans lines 5..7. The blank line 8 belongs
	// to neither entry and must not be swallowed.
	if start != 5 || end != 7 {
		t.Errorf("extent = %d..%d, want 5..7", start, end)
	}
	if indent != "      " {
		t.Errorf("indent = %q, want six spaces", indent)
	}
}

func TestDateExtentOfLastEntryReachesEndOfCourse(t *testing.T) {
	src := []byte(`courses:
  - id: msa
    dates:
      - id: a
        code: "A"
  - id: flex
    dates:
      - id: b
        code: "B"
`)
	doc, _ := Parse(src)
	start, end, _, ok := doc.dateExtent("a")
	if !ok || start != 4 || end != 5 {
		t.Errorf("extent = %d..%d, ok=%v; want 4..5", start, end, ok)
	}
}
