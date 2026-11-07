package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"arc42-trainings-admin/internal/model"
)

// newMASForm is a complete new-course form for a course without a hand-made
// card, carrying a German description so that nothing but the field under test
// warns (an only-German description is a note, and notes never gate a save).
func newMASForm() url.Values {
	return url.Values{
		"id": {"mas"}, "short_title": {"MAS"}, "title": {"Methodische Architektur-Skills"},
		"url": {"https://www.arc42.de/info-mas/"}, "trainer": {"Dr. Peter Hruschka"},
		"blurb_de": {"Methodik für Architekten."},
	}
}

// postCourse posts a course form and returns the response and the stored
// course, if the save went through.
func postCourse(t *testing.T, s *Server, target string, form url.Values) (*httptest.ResponseRecorder, model.Course, bool) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodPost, target, form))
	d, ok := s.drafts.Get("sid")
	if !ok || !d.Dirty() {
		return rec, model.Course{}, false
	}
	id := form.Get("id")
	c, found := d.Doc.Model().FindCourse(id)
	return rec, c, found
}

func TestCourseFormSavesShowOn(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	form := newMASForm()
	form["show_on"] = []string{"www.arc42.de", "arc42.org", "www.systems42.org"}
	form.Set("show_on_other", "example.org")
	rec, c, ok := postCourse(t, s, "/courses/new", form)
	if !ok {
		t.Fatalf("course not saved:\n%s", rec.Body.String())
	}
	want := []string{"www.arc42.de", "arc42.org", "www.systems42.org", "example.org"}
	if !slices.Equal(c.ShowOn, want) {
		t.Errorf("ShowOn = %q, want %q", c.ShowOn, want)
	}
}

// No box ticked is the normal case and means every site: the key must be left
// out, never written as an empty list, which the site's validator rejects as
// "a course shown nowhere".
func TestCourseFormNoSiteTickedMeansEverySite(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	form := newMASForm()
	form.Set("show_on_other", " , ")
	rec, c, ok := postCourse(t, s, "/courses/new", form)
	if !ok {
		t.Fatalf("course not saved:\n%s", rec.Body.String())
	}
	if c.ShowOn != nil {
		t.Errorf("ShowOn = %#v, want nil", c.ShowOn)
	}
}

func TestCourseFormSavesBothDescriptions(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	form := newMASForm()
	form.Set("blurb_en", "Methods for architects.")
	rec, c, ok := postCourse(t, s, "/courses/new", form)
	if !ok {
		t.Fatalf("course not saved:\n%s", rec.Body.String())
	}
	if c.BlurbDE != "Methodik für Architekten." || c.BlurbEN != "Methods for architects." {
		t.Errorf("BlurbDE = %q, BlurbEN = %q", c.BlurbDE, c.BlurbEN)
	}
}

func TestCourseFormRejectsBadSiteName(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	form := newMASForm()
	form.Set("show_on_other", "Not A Site")
	// Problems are blocking: acknowledging warnings must not get past them.
	form.Set("confirm_warnings", "1")
	rec, _, saved := postCourse(t, s, "/courses/new", form)
	if saved {
		t.Fatal("a course with an impossible site name was saved")
	}
	if body := rec.Body.String(); !strings.Contains(body, "<b>Shown on</b>") || strings.Contains(body, "courses.mas") {
		t.Errorf("the problem is not labelled for the operator:\n%s", body)
	}

	c := model.Course{ID: "mas", ShortTitle: "MAS", Title: "MAS", URL: "https://x.org",
		Trainers: []string{"A"}, ShowOn: []string{"Not A Site"}}
	var got []string
	for _, p := range courseProblems(c, nil, "") {
		if p.Field == "courses.mas.show_on" {
			got = append(got, p.Message)
		}
	}
	want := `"Not A Site" is not a site name like www.arc42.de`
	if len(got) != 1 || got[0] != want {
		t.Errorf("show_on problems = %q, want [%q]", got, want)
	}
}

// Notes are information, not a question: a course whose only remark is a note
// is saved on the first submit, without a "Save anyway" round.
func TestCourseNotesDoNotGateTheSave(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	rec, _, ok := postCourse(t, s, "/courses/new", newMASForm())
	if !ok {
		t.Fatalf("a course with only a note was not saved on the first submit:\n%s", rec.Body.String())
	}
}

// A course with a reorderable list, an off-list site and both descriptions:
// the shape most likely to be lost or rewritten by a round trip through the
// form, which rewrites the whole course block from what it posts.
const masYAML = `courses:
  - id: mas
    short_title: "MAS"
    title: "Methodische Architektur-Skills"
    blurb_de: >-
      Methodik für Architekten.
    blurb_en: >-
      Methods for architects.
    show_on: ["arc42.org", "www.arc42.de", "example.org"]
    url: "https://www.arc42.de/info-mas/"
    trainers: ["Dr. Peter Hruschka"]
    dates: []
`

// The edit form has to show what is stored, and posting it back unchanged
// has to store the same thing — including the order of a hand-edited
// show_on, which would otherwise surface as a change in the pull request.
func TestCourseFormEditPreservesDescriptionsAndShowOn(t *testing.T) {
	gh, _ := fakeGitHubWith(t, masYAML)
	defer gh.Close()
	s := testServer(t, gh.URL)

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/courses/mas", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /courses/mas -> %d:\n%s", rec.Code, body)
	}
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`name="blurb_de"[^>]*>Methodik für Architekten\.</textarea>`),
		regexp.MustCompile(`name="blurb_en"[^>]*>Methods for architects\.</textarea>`),
		regexp.MustCompile(`name="show_on" value="arc42\.org"\s+checked`),
		regexp.MustCompile(`name="show_on" value="www\.arc42\.de"\s+checked`),
		regexp.MustCompile(`name="show_on_other" value="example\.org"`),
	} {
		if !want.MatchString(body) {
			t.Errorf("edit form does not match %s", want)
		}
	}
	if regexp.MustCompile(`name="show_on" value="docs\.arc42\.org"\s+checked`).MatchString(body) {
		t.Error("an unlisted site is ticked")
	}

	// Posted the way the browser sends it: checkboxes in document order.
	form := url.Values{
		"id": {"mas"}, "short_title": {"MAS"}, "title": {"Methodische Architektur-Skills"},
		"url": {"https://www.arc42.de/info-mas/"}, "trainer": {"Dr. Peter Hruschka"},
		"blurb_de": {"Methodik für Architekten."}, "blurb_en": {"Methods for architects."},
		"show_on": {"www.arc42.de", "arc42.org"}, "show_on_other": {"example.org"},
	}
	rec, c, ok := postCourse(t, s, "/courses/mas", form)
	if !ok {
		t.Fatalf("edit not saved:\n%s", rec.Body.String())
	}
	if c.BlurbDE != "Methodik für Architekten." || c.BlurbEN != "Methods for architects." {
		t.Errorf("descriptions changed: BlurbDE = %q, BlurbEN = %q", c.BlurbDE, c.BlurbEN)
	}
	if want := []string{"arc42.org", "www.arc42.de", "example.org"}; !slices.Equal(c.ShowOn, want) {
		t.Errorf("ShowOn = %q, want %q unchanged", c.ShowOn, want)
	}
}

func TestCourseFormShowsNotes(t *testing.T) {
	gh, _ := fakeGitHub(t) // msa: a hand-made card
	defer gh.Close()
	s := testServer(t, gh.URL)

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/courses/msa", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `class="notes" role="note"`) || !strings.Contains(body, "hand-made card") {
		t.Errorf("the edit form shows no note for a hand-made card:\n%s", body)
	}
	if strings.Contains(body, "Save anyway") {
		t.Error("a note turned the button into Save anyway")
	}
}

// The form offers every known site, and says what no ticked box means.
func TestCourseFormOffersTheKnownSites(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/courses/new", nil))
	body := rec.Body.String()
	for _, site := range model.KnownSites {
		if !strings.Contains(body, `name="show_on" value="`+site+`"`) {
			t.Errorf("no checkbox for %s", site)
		}
	}
	for _, want := range []string{"Description (German)", "Description (English)", "Shown on",
		"No box ticked: every site shows this course. trainings.arc42.org always shows every course."} {
		if !strings.Contains(body, want) {
			t.Errorf("course form is missing %q", want)
		}
	}
	if strings.Contains(body, `name="blurb"`) {
		t.Error("the retired single blurb field is still offered")
	}
}

// The operator reads the form's own label next to a warning, not the name of
// a form control or a YAML key.
func TestWarningsShowTheFormLabel(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	form := newMASForm()
	form.Set("short_title", "Methodische Architektur-Skills für alle")
	rec, _, saved := postCourse(t, s, "/courses/new", form)
	if saved {
		t.Fatal("a warned save went through on the first submit")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<b>Short title</b> — 39 characters is long for the home page card") {
		t.Errorf("warning not shown under its form label:\n%s", body)
	}
	if strings.Contains(body, "<b>short_title</b>") {
		t.Error("warning shown under the control name")
	}
	// The reshow keeps the note too.
	if !strings.Contains(body, "Only a German description") {
		t.Error("the reshown form lost its note")
	}
}

func TestSameSitesComparesSetsBothWays(t *testing.T) {
	for _, c := range []struct {
		a, b []string
		want bool
	}{
		{nil, nil, true},
		{[]string{"arc42.org", "www.arc42.de"}, []string{"www.arc42.de", "arc42.org"}, true},
		{[]string{"arc42.org", "arc42.org"}, []string{"www.arc42.de", "arc42.org"}, false},
		{[]string{"www.arc42.de", "arc42.org"}, []string{"arc42.org", "arc42.org"}, false},
		{[]string{"arc42.org"}, nil, false},
	} {
		if got := sameSites(c.a, c.b); got != c.want {
			t.Errorf("sameSites(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// A hand edit can leave a duplicate in show_on; nothing dedupes it. The
// operator's change must still win over the stored list.
func TestCourseFormEditOverDuplicatedShowOnKeepsTheChange(t *testing.T) {
	gh, _ := fakeGitHubWith(t, strings.Replace(masYAML,
		`show_on: ["arc42.org", "www.arc42.de", "example.org"]`,
		`show_on: ["arc42.org", "arc42.org"]`, 1))
	defer gh.Close()
	s := testServer(t, gh.URL)

	form := url.Values{
		"id": {"mas"}, "short_title": {"MAS"}, "title": {"Methodische Architektur-Skills"},
		"url": {"https://www.arc42.de/info-mas/"}, "trainer": {"Dr. Peter Hruschka"},
		"blurb_de": {"Methodik für Architekten."}, "blurb_en": {"Methods for architects."},
		"show_on": {"www.arc42.de", "arc42.org"},
	}
	rec, c, ok := postCourse(t, s, "/courses/mas", form)
	if !ok {
		t.Fatalf("edit not saved:\n%s", rec.Body.String())
	}
	if want := []string{"www.arc42.de", "arc42.org"}; !slices.Equal(c.ShowOn, want) {
		t.Errorf("ShowOn = %q, want %q: the operator's change was lost", c.ShowOn, want)
	}
}
