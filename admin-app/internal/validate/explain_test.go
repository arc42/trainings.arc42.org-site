package validate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"arc42-trainings-admin/internal/model"
)

// twoCourses is MSA with one public date and MAS with an online date and a
// public one, so a schema pointer's indices have something to be wrong about.
func twoCourses() model.Trainings {
	return model.Trainings{Courses: []model.Course{
		{ID: "msa", ShortTitle: "MSA", Dates: []model.Date{
			{ID: "msa-a", Start: "2026-01-01", City: "München", Format: "public"},
		}},
		{ID: "mas", ShortTitle: "MAS", Dates: []model.Date{
			{ID: "mas-jan-2027", Start: "2027-01-13", End: "2027-01-12", Format: "online"},
			{ID: "mas-mar-2027", Start: "2027-03-02", City: "Köln", Format: "public"},
		}},
	}}
}

func TestExplainRuleProblem(t *testing.T) {
	p := Explain(twoCourses(), Problem{
		Field: "dates.mas-jan-2027.end", Message: "end 2027-01-12 is before start 2027-01-13",
	})
	if p.Kind != EntryProblem {
		t.Errorf("Kind = %v, want EntryProblem", p.Kind)
	}
	if p.Where != "MAS, 13 Jan 2027 online" {
		t.Errorf("Where = %q", p.Where)
	}
	if p.Label != "Last day" {
		t.Errorf("Label = %q, want the form's label %q", p.Label, "Last day")
	}
	if p.Link != "/dates/mas-jan-2027" {
		t.Errorf("Link = %q, want the date's edit form", p.Link)
	}
	if p.Message != "end 2027-01-12 is before start 2027-01-13" {
		t.Errorf("Message = %q, want it unchanged", p.Message)
	}
}

func TestExplainSchemaPointer(t *testing.T) {
	p := Explain(twoCourses(), Problem{Field: "courses/1/dates/1/start", Message: "does not match pattern"})
	if p.Kind != EntryProblem {
		t.Errorf("Kind = %v, want EntryProblem", p.Kind)
	}
	if p.Where != "MAS, 2 Mar 2027 Köln" {
		t.Errorf("Where = %q, want the second date of the second course", p.Where)
	}
	if p.Label != "First day" {
		t.Errorf("Label = %q", p.Label)
	}
	if p.Link != "/dates/mas-mar-2027" {
		t.Errorf("Link = %q", p.Link)
	}
}

// An array index inside a property (the third trainer) is still that
// property's form field.
func TestExplainSchemaPointerIntoAList(t *testing.T) {
	p := Explain(twoCourses(), Problem{Field: "courses/0/trainers/2", Message: "expected string"})
	if p.Kind != EntryProblem || p.Label != "Trainers" || p.Where != "MSA" || p.Link != "/courses/msa" {
		t.Errorf("got %+v", p)
	}
}

func TestExplainCourseFormField(t *testing.T) {
	p := Explain(twoCourses(), Problem{Field: "courses.mas.show_on", Message: "not a site name"})
	if p.Kind != EntryProblem || p.Label != "Shown on" || p.Where != "MAS" || p.Link != "/courses/mas" {
		t.Errorf("got %+v", p)
	}
	// A course still being created is not in the model yet; its field still
	// has a label.
	p = Explain(model.Trainings{}, Problem{Field: "courses.new-one.short_title", Message: "must not be empty"})
	if p.Kind != EntryProblem || p.Label != "Short title" {
		t.Errorf("got %+v", p)
	}
}

func TestExplainUnknownPropertyIsAppFault(t *testing.T) {
	p := Explain(twoCourses(), Problem{Field: "courses/0/generated_thing", Message: "is not allowed"})
	if p.Kind != AppProblem {
		t.Fatalf("Kind = %v, want AppProblem", p.Kind)
	}
	if !strings.Contains(p.Detail, "courses/0/generated_thing") || !strings.Contains(p.Detail, "is not allowed") {
		t.Errorf("Detail = %q, want the pointer and the message", p.Detail)
	}
}

// The registration link is derived by the server, never typed: a schema
// complaint about it cannot be the operator's.
func TestExplainServerDerivedFieldIsAppFault(t *testing.T) {
	p := Explain(twoCourses(), Problem{Field: "courses/1/dates/0/url", Message: "does not match pattern"})
	if p.Kind != AppProblem {
		t.Errorf("Kind = %v, want AppProblem", p.Kind)
	}
}

// A pointer that does not land on a course and date the app knows cannot be
// sent to a form.
func TestExplainUnresolvablePointerIsAppFault(t *testing.T) {
	for _, field := range []string{"", "courses/7/title", "courses/1/dates/9/start", "dates.nope.end", "courses/1"} {
		p := Explain(twoCourses(), Problem{Field: field, Message: "m"})
		if p.Kind != AppProblem {
			t.Errorf("%q: Kind = %v, want AppProblem", field, p.Kind)
		}
	}
}

func TestAppFault(t *testing.T) {
	p := AppFault("could not fetch the schema: 404")
	if p.Kind != AppProblem || p.Detail != "could not fetch the schema: 404" {
		t.Errorf("got %+v", p)
	}
}

// TestFormFieldsCoverForms keeps formFields next to the forms it describes.
// Every control on the course and date forms that sets a property of the data
// must be in the table, and every label in the table must be the label the
// form shows, verbatim; otherwise a problem would send the operator looking
// for a field that is not on the page.
func TestFormFieldsCoverForms(t *testing.T) {
	dir := filepath.Join("..", "web", "templates")
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	// The trainer picker is a shared partial; both forms include it.
	shared := read("_fields.gohtml")
	forms := map[string]string{
		"course": read("courseform.gohtml") + shared,
		"date":   read("dateform.gohtml") + shared,
	}
	// Controls that set no property of their own: the warning acknowledgement,
	// the free-text halves of the pickers (they feed the same list as the
	// checkboxes), and the course a date belongs to (it moves the date, it is
	// not a value in it).
	ignore := map[string]bool{"confirm_warnings": true, "course_id": true}

	names := regexp.MustCompile(`name="([a-z_]+)"`)
	for form, src := range forms {
		inTable := map[string]bool{}
		for prop, f := range formFields {
			if (form == "date") != strings.HasPrefix(prop, "dates/") {
				continue
			}
			inTable[f.Name] = true
			if !strings.Contains(src, f.Label) {
				t.Errorf("%s form: label %q for %s is not on the form", form, f.Label, prop)
			}
			if !strings.Contains(src, `name="`+f.Name+`"`) {
				t.Errorf("%s form: no control named %q for %s", form, f.Name, prop)
			}
		}
		for _, m := range names.FindAllStringSubmatch(src, -1) {
			name := m[1]
			if ignore[name] || strings.HasSuffix(name, "_other") {
				continue
			}
			if !inTable[name] {
				t.Errorf("%s form: control %q is not in formFields", form, name)
			}
		}
	}
}
