package web

import (
	"strings"
	"testing"

	"arc42-trainings-admin/internal/model"
	"arc42-trainings-admin/internal/yamldoc"
)

const draftSrc = `courses:
  - id: msa
    short_title: "MSA"
    dates:
      - id: a
        code: "A"
        start: "2026-01-01"
        end: "2026-01-02"
        city: "München"
        language: de
        format: public
        url: "https://example.org/a"
        status: open
`

func newTestDraft(t *testing.T) *Draft {
	t.Helper()
	doc, err := yamldoc.Parse([]byte(draftSrc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return &Draft{Doc: doc, FileSHA: "filesha", HeadSHA: "headsha"}
}

func TestDraftRecordsChanges(t *testing.T) {
	d := newTestDraft(t)
	row, _ := d.Doc.Model().FindDate("a")
	nd := row.Date
	nd.Status = "full"
	if err := d.UpdateDate("a", nd); err != nil {
		t.Fatalf("UpdateDate: %v", err)
	}
	if len(d.Changes) != 1 || d.Changes[0].Kind != "updated" || d.Changes[0].DateID != "a" {
		t.Fatalf("Changes = %+v", d.Changes)
	}
	if !d.Dirty() {
		t.Error("draft should be dirty")
	}
}

func TestDraftCollapsesRepeatedEditsOfOneDate(t *testing.T) {
	d := newTestDraft(t)
	row, _ := d.Doc.Model().FindDate("a")
	nd := row.Date
	nd.Status = "full"
	_ = d.UpdateDate("a", nd)
	nd.Status = "waitlist"
	_ = d.UpdateDate("a", nd)
	if len(d.Changes) != 1 {
		t.Errorf("want 1 collapsed change, got %d: %+v", len(d.Changes), d.Changes)
	}
}

func TestDraftAddAndDelete(t *testing.T) {
	d := newTestDraft(t)
	err := d.AddDate("msa", model.Date{
		ID: "b", Code: "B", Start: "2026-05-01", End: "2026-05-02",
		Language: "en", Format: "online", URL: "https://example.org/b", Status: "open",
	})
	if err != nil {
		t.Fatalf("AddDate: %v", err)
	}
	if err := d.DeleteDate("a"); err != nil {
		t.Fatalf("DeleteDate: %v", err)
	}
	if len(d.Changes) != 2 {
		t.Fatalf("Changes = %+v", d.Changes)
	}
	if !strings.Contains(string(d.Doc.Bytes()), "id: b") {
		t.Error("added date missing from document")
	}
}

func TestDraftsAreIsolatedPerSession(t *testing.T) {
	ds := NewDrafts()
	ds.Put("s1", newTestDraft(t))
	if _, ok := ds.Get("s2"); ok {
		t.Error("session s2 sees s1's draft")
	}
	ds.Discard("s1")
	if _, ok := ds.Get("s1"); ok {
		t.Error("discard did not remove the draft")
	}
}
