package validate

import (
	"encoding/json"
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
		Field: "dates.mas-jan-2027.end", Message: "12 Jan 2027 is before the first day, 13 Jan 2027",
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
	if p.Message != "12 Jan 2027 is before the first day, 13 Jan 2027" {
		t.Errorf("Message = %q, want it unchanged", p.Message)
	}
}

func TestExplainSchemaPointer(t *testing.T) {
	p := Explain(twoCourses(), Problem{
		Field: "courses/1/dates/1/start", Keyword: "pattern",
		Message: `does not match pattern '^\d{4}-\d{2}-\d{2}$'`,
	})
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
	// The library's message is not for the operator; the regular expression
	// goes to Detail, the plain words to Message.
	if p.Message != "must be a date like 2027-01-13" || strings.Contains(p.Message, "^") {
		t.Errorf("Message = %q", p.Message)
	}
	if !strings.Contains(p.Detail, "does not match pattern") {
		t.Errorf("Detail = %q, want the original message", p.Detail)
	}
}

// Each schema keyword an entry can violate gets plain words.
func TestExplainSchemaKeywordsSpeakPlainly(t *testing.T) {
	for _, tc := range []struct{ field, keyword, want string }{
		{"courses/0/url", "pattern", "must start with https://"},
		{"courses/0/short_title", "minLength", "must not be empty"},
		{"courses/0/trainers", "minItems", "must not be empty"},
		{"courses/1/dates/1/city", "required", "must not be empty"},
		{"courses/1/dates/1/country", "pattern", "must be two capital letters, e.g. DE"},
		{"courses/1/dates/1/language", "enum", "must be one of the choices on the form"},
		{"courses/1/dates/1/price/amount", "minimum", "must not be negative"},
	} {
		p := Explain(twoCourses(), Problem{Field: tc.field, Keyword: tc.keyword, Message: "raw"})
		if p.Kind != EntryProblem || p.Message != tc.want {
			t.Errorf("%s %s: got %v %q, want %q", tc.field, tc.keyword, p.Kind, p.Message, tc.want)
		}
	}
}

// A wrong JSON type is how the app wrote the feed; no form can produce one.
func TestExplainSchemaTypeErrorIsAppFault(t *testing.T) {
	p := Explain(twoCourses(), Problem{Field: "courses/0/trainers", Keyword: "type", Message: "expected array, but got null"})
	if p.Kind != AppProblem {
		t.Errorf("Kind = %v, want AppProblem", p.Kind)
	}
}

// "required" is reported at the object that lacks the property. Schema splits
// it so the problem lands on the empty field, not in the app's pile.
func TestSchemaRequiredPointsAtTheProperty(t *testing.T) {
	tr := ok()
	tr.Courses[0].Dates[0].City = ""
	feed, err := FeedJSON(tr)
	if err != nil {
		t.Fatal(err)
	}
	problems, err := Schema(schemaBytes(t), feed)
	if err != nil {
		t.Fatal(err)
	}
	var got []Problem
	for _, p := range problems {
		if p.Field == "courses/0/dates/0/city" && p.Keyword == "required" {
			got = append(got, Explain(tr, p))
		}
	}
	if len(got) != 1 || got[0].Kind != EntryProblem || got[0].Label != "City" {
		t.Errorf("want one entry problem on City, got %+v from %+v", got, problems)
	}
}

// TestEveryPatternHasAHint: wherever the schema checks a form-set value
// against a regular expression, the operator is told what the value must look
// like instead of being shown the expression.
func TestEveryPatternHasAHint(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(schemaBytes(t), &schema); err != nil {
		t.Fatal(err)
	}
	obj := func(v any, key string) map[string]any { m, _ := v.(map[string]any)[key].(map[string]any); return m }
	var visit func(node map[string]any, path string)
	visit = func(node map[string]any, path string) {
		for name, child := range obj(node, "properties") {
			prop := strings.TrimPrefix(path+"/"+name, "/")
			c := child.(map[string]any)
			_, own := c["pattern"]
			items := obj(c, "items")
			_, itemsToo := items["pattern"]
			if f, onForm := formFields[prop]; onForm && (own || itemsToo) && f.Hint == "" {
				t.Errorf("%s (%s) has a pattern but no Hint", prop, f.Label)
			}
			if _, ok := c["properties"]; ok {
				visit(c, prop)
			}
			if _, ok := items["properties"]; ok {
				visit(items, prop)
			}
		}
	}
	visit(obj(obj(schema, "properties"), "courses")["items"].(map[string]any), "")
}

// An array index inside a property (the second site) is still that
// property's form field.
func TestExplainSchemaPointerIntoAList(t *testing.T) {
	p := Explain(twoCourses(), Problem{Field: "courses/0/show_on/1", Keyword: "pattern", Message: "does not match pattern"})
	if p.Kind != EntryProblem || p.Label != "Shown on" || p.Where != "MSA" || p.Link != "/courses/msa" {
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
	p := Explain(twoCourses(), Problem{Field: "courses/1/dates/0/url", Keyword: "pattern", Message: "does not match pattern"})
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

// An id is the operator's entry and may hold a dot before it is checked. The
// property is the last segment of a Field, never what follows the first dot,
// or "mai.spec" read as course "mai", property "spec.id": an app fault.
func TestExplainPlacesAnIDWithADot(t *testing.T) {
	m := model.Trainings{Courses: []model.Course{
		{ID: "mai.spec", ShortTitle: "MAI", Dates: []model.Date{{ID: "mai.spec-mar-2027", Start: "2027-03-01"}}},
	}}
	for field, label := range map[string]string{
		"courses.mai.spec.id":        "Course ID",
		"dates.mai.spec-mar-2027.id": "Date ID",
	} {
		p := Explain(m, Problem{Field: field, Message: "must be lower-case letters, digits and hyphens only"})
		if p.Kind != EntryProblem || p.Label != label {
			t.Errorf("%s: Kind = %v, Label = %q, want an entry problem on %q", field, p.Kind, p.Label, label)
		}
	}
}
