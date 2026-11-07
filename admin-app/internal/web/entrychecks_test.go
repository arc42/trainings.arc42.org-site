package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// These tests pin "checked where it is entered": the course MAISPEC that the
// course form saved, the review page then rejected, and nobody could correct
// because its id had turned read-only.

// send is one request through the real routes, as the signed-in test user.
func send(t *testing.T, s *Server, method, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, method, target, form))
	return rec
}

// realSchemaServer is a stand-in GitHub serving yaml and the repository's real
// schema, which is what the save-time checks have to agree with.
func realSchemaServer(t *testing.T, yaml string) *Server {
	t.Helper()
	gh, _ := fakeGitHubFiles(t, map[string][]byte{
		"_data/trainings.yml":       []byte(yaml),
		"api/trainings.schema.json": realSchema(t),
	})
	t.Cleanup(gh.Close)
	return testServer(t, gh.URL)
}

// courseForm is a complete new-course form for the given id.
func courseForm(id string) url.Values {
	f := newMASForm()
	f.Set("id", id)
	f.Set("short_title", "MAI-SPEC")
	f.Set("title", "Spezifikation mit KI")
	f.Set("url", "https://www.arc42.de/info-maispec/")
	return f
}

// dateFormFor is a complete new-date form for a course, confirmed past the
// warnings so a test is about the problems alone.
func dateFormFor(courseID string) url.Values {
	return url.Values{
		"course_id": {courseID}, "start": {"2027-03-01"}, "end": {"2027-03-02"},
		"city": {"München"}, "country": {"DE"}, "language": {"de"}, "format": {"public"},
		"availability": {"open"}, "confirm_warnings": {"1"},
	}
}

// idInput is the course form's id control, the whole tag.
var idInput = regexp.MustCompile(`<input name="id" id="c-id"[^>]*>`)

func TestCourseFormNormalisesID(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	rec := send(t, s, http.MethodPost, "/courses/new", courseForm(" MAISPEC "))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST -> %d, want the save to go through:\n%s", rec.Code, rec.Body.String())
	}
	d, _ := s.drafts.Get("sid")
	if _, ok := d.Doc.Model().FindCourse("maispec"); !ok {
		t.Fatalf("no course maispec in the draft:\n%s", d.Doc.Bytes())
	}
	if strings.Contains(string(d.Doc.Bytes()), "MAISPEC") {
		t.Errorf("the id was stored as typed:\n%s", d.Doc.Bytes())
	}

	// Spaces and underscores become one hyphen each run.
	rec = send(t, s, http.MethodPost, "/courses/new", courseForm("Mai_ Spec"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST -> %d:\n%s", rec.Code, rec.Body.String())
	}
	if _, ok := d.Doc.Model().FindCourse("mai-spec"); !ok {
		t.Errorf("Mai_ Spec was not stored as mai-spec:\n%s", d.Doc.Bytes())
	}
}

func TestCourseFormRejectsInvalidIDAfterNormalising(t *testing.T) {
	for _, id := range []string{"mai/spec", "mäispec", "mai.spec"} {
		t.Run(id, func(t *testing.T) {
			gh, _ := fakeGitHub(t)
			defer gh.Close()
			s := testServer(t, gh.URL)

			form := courseForm(id)
			form.Set("confirm_warnings", "1") // a problem is not a warning
			rec := send(t, s, http.MethodPost, "/courses/new", form)
			if d, ok := s.drafts.Get("sid"); ok && d.Dirty() {
				t.Fatalf("a course with id %q was saved:\n%s", id, d.Doc.Bytes())
			}
			body := rec.Body.String()
			problems := between(body, `class="problems"`, "</div>")
			if !strings.Contains(problems, "<b>Course ID</b> — ") {
				t.Fatalf("no problem labelled Course ID:\n%s", body)
			}
			// Plain words: no regular expression, no place in the file.
			for _, bad := range []string{"[a-z", "^", "courses.", "courses/"} {
				if strings.Contains(problems, bad) {
					t.Errorf("the problem shows %q:\n%s", bad, problems)
				}
			}
		})
	}
}

// A Course URL "ftp://…" passes the browser (it is a URL), the course form's
// own checks (it is not empty) and the warnings (it is not http://). Only the
// schema's ^https:// catches it, which used to mean the review page.
func TestCourseSaveRunsProposeChecks(t *testing.T) {
	s := realSchemaServer(t, fixtureYAML)

	form := courseForm("maispec")
	form.Set("url", "ftp://example.org/maispec")
	form.Set("confirm_warnings", "1")
	rec := send(t, s, http.MethodPost, "/courses/new", form)
	if d, ok := s.drafts.Get("sid"); ok && d.Dirty() {
		t.Fatalf("a course the review page rejects was saved:\n%s", d.Doc.Bytes())
	}
	if !strings.Contains(rec.Body.String(), "<b>Course URL</b> — must start with https://") {
		t.Errorf("the schema problem is not on the form:\n%s", rec.Body.String())
	}
}

// Country "d1" passes the server's upper-casing as "D1"; only the schema's
// ^[A-Z]{2}$ rejects it.
func TestDateSaveRunsProposeChecks(t *testing.T) {
	s := realSchemaServer(t, fixtureYAML)

	form := dateFormFor("msa")
	form.Set("country", "d1")
	rec := send(t, s, http.MethodPost, "/dates/new", form)
	if d, ok := s.drafts.Get("sid"); ok && d.Dirty() {
		t.Fatalf("a date the review page rejects was saved:\n%s", d.Doc.Bytes())
	}
	if !strings.Contains(rec.Body.String(), "<b>Country</b> — must be two capital letters, e.g. DE") {
		t.Errorf("the schema problem is not on the form:\n%s", rec.Body.String())
	}
}

// The date id goes through the same normalising, and what is still wrong after
// it is a problem on the form, scripting or not.
func TestDateFormNormalisesAndChecksID(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	form := dateFormFor("msa")
	form.Set("id", " MSA_Mar 2027 ")
	if rec := send(t, s, http.MethodPost, "/dates/new", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST -> %d:\n%s", rec.Code, rec.Body.String())
	}
	d, _ := s.drafts.Get("sid")
	if _, ok := d.Doc.Model().FindDate("msa-mar-2027"); !ok {
		t.Fatalf("the date id was not normalised:\n%s", d.Doc.Bytes())
	}

	form = dateFormFor("msa")
	form.Set("start", "2027-04-01")
	form.Set("end", "2027-04-02")
	form.Set("id", "msa/apr-2027")
	rec := send(t, s, http.MethodPost, "/dates/new", form)
	if _, ok := d.Doc.Model().FindDate("msa/apr-2027"); ok {
		t.Fatal("a date id with a slash was saved")
	}
	if !strings.Contains(rec.Body.String(), "<b>Date ID</b> — ") {
		t.Errorf("no problem labelled Date ID:\n%s", rec.Body.String())
	}
}

// What is wrong elsewhere in the draft is the review page's to report. Saving
// one date must not be held back by another date's entry, and saving a course
// must not be held back by its dates.
func TestSaveChecksStayWithTheEditedEntry(t *testing.T) {
	broken := strings.Replace(fixtureYAML, `city: "Hamburg"
        country: "DE"`, `city: "Hamburg"
        country: "de"`, 1)
	s := realSchemaServer(t, broken)

	form := dateFormFor("msa")
	if rec := send(t, s, http.MethodPost, "/dates/new", form); rec.Code != http.StatusSeeOther {
		t.Errorf("a new date was held back by another date's problem:\n%s", rec.Body.String())
	}

	rec := send(t, s, http.MethodPost, "/courses/msa", url.Values{
		"id": {"msa"}, "short_title": {"MSA"}, "title": {"Mastering Software Architectures"},
		"url": {"https://example.org/msa"}, "trainer": {"Dr. Gernot Starke"},
		"confirm_warnings": {"1"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Errorf("a course save was held back by one of its dates:\n%s", rec.Body.String())
	}
}

// The one way an edit can break another entry: msa-b takes the booking code
// msa-a, later in the file, already has. Rules reports the second of the two,
// which is msa-a, but it is this edit that caused it.
func TestDateSaveCatchesAProblemItCausesElsewhere(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	rec := send(t, s, http.MethodPost, "/dates/msa-b", url.Values{
		"course_id": {"msa"}, "id": {"msa-b"}, "code": {"26-01 MSA"},
		"start": {"2025-09-14"}, "end": {"2025-09-17"}, "city": {"Hamburg"},
		"country": {"DE"}, "language": {"en"}, "format": {"public"},
		"availability": {"open"}, "confirm_warnings": {"1"},
	})
	if d, ok := s.drafts.Get("sid"); ok && d.Dirty() {
		t.Fatalf("a duplicate booking code was saved:\n%s", d.Doc.Bytes())
	}
	if !strings.Contains(rec.Body.String(), "<b>Booking code</b> — 26-01 MSA is already used") {
		t.Errorf("the duplicate is not on the form:\n%s", rec.Body.String())
	}
}

func TestDraftOnlyCourseIDEditableWithoutDates(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)
	send(t, s, http.MethodPost, "/courses/new", courseForm("maispec"))

	body := send(t, s, http.MethodGet, "/courses/maispec", nil).Body.String()
	tag := idInput.FindString(body)
	if tag == "" || strings.Contains(tag, "readonly") {
		t.Errorf("the id of a course only in the draft is not editable: %s", tag)
	}
	if !strings.Contains(body, "only in your draft") {
		t.Errorf("the hint does not say why the id is editable:\n%s", body)
	}
	if !strings.Contains(body, `href="/courses/maispec/delete"`) {
		t.Errorf("no way to remove the course from the draft:\n%s", body)
	}
}

func TestDraftOnlyCourseIDReadOnlyWithDates(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)
	send(t, s, http.MethodPost, "/courses/new", courseForm("maispec"))
	if rec := send(t, s, http.MethodPost, "/dates/new", dateFormFor("maispec")); rec.Code != http.StatusSeeOther {
		t.Fatalf("date not added:\n%s", rec.Body.String())
	}

	body := send(t, s, http.MethodGet, "/courses/maispec", nil).Body.String()
	if tag := idInput.FindString(body); !strings.Contains(tag, "readonly") {
		t.Errorf("the id of a course with dates is editable: %s", tag)
	}
	if !strings.Contains(body, "remove its dates to change it") {
		t.Errorf("the hint does not say how to change the id:\n%s", body)
	}
	// Still removable as a whole: the dates go with it.
	if !strings.Contains(body, `href="/courses/maispec/delete"`) {
		t.Errorf("no way to remove the course from the draft:\n%s", body)
	}

	// A posted id cannot rename it either: its dates reference it.
	form := courseForm("other")
	form.Set("confirm_warnings", "1")
	send(t, s, http.MethodPost, "/courses/maispec", form)
	d, _ := s.drafts.Get("sid")
	if _, ok := d.Doc.Model().FindCourse("other"); ok {
		t.Errorf("a course with dates was renamed:\n%s", d.Doc.Bytes())
	}
}

func TestCourseOnMainIDReadOnly(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	body := send(t, s, http.MethodGet, "/courses/msa", nil).Body.String()
	if tag := idInput.FindString(body); !strings.Contains(tag, "readonly") {
		t.Errorf("the id of a published course is editable: %s", tag)
	}
	if strings.Contains(body, "/courses/msa/delete") {
		t.Error("a published course offers removal")
	}

	// Nor can a hand-made POST rename or remove it.
	send(t, s, http.MethodPost, "/courses/msa", url.Values{
		"id": {"msa-neu"}, "short_title": {"MSA"}, "title": {"Mastering Software Architectures"},
		"url": {"https://example.org/msa"}, "trainer": {"Dr. Gernot Starke"},
		"confirm_warnings": {"1"},
	})
	send(t, s, http.MethodPost, "/courses/msa/delete", url.Values{})
	d, _ := s.drafts.Get("sid")
	if _, ok := d.Doc.Model().FindCourse("msa"); !ok {
		t.Errorf("a published course was renamed or removed:\n%s", d.Doc.Bytes())
	}
}

func TestRenameDraftOnlyCourse(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)
	send(t, s, http.MethodPost, "/courses/new", courseForm("maispec-x"))

	rec := send(t, s, http.MethodPost, "/courses/maispec-x", courseForm("maispec"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("rename -> %d:\n%s", rec.Code, rec.Body.String())
	}
	d, _ := s.drafts.Get("sid")
	if strings.Contains(string(d.Doc.Bytes()), "maispec-x") {
		t.Errorf("the file still carries the old id:\n%s", d.Doc.Bytes())
	}
	if _, ok := d.Doc.Model().FindCourse("maispec"); !ok {
		t.Fatalf("no course maispec:\n%s", d.Doc.Bytes())
	}
	if len(d.Changes) != 1 || d.Changes[0].Kind != "added" || d.Changes[0].CourseID != "maispec" {
		t.Errorf("changes = %+v, want one addition of maispec", d.Changes)
	}

	body := send(t, s, http.MethodGet, "/propose", nil).Body.String()
	if !strings.Contains(body, `<td class="now">maispec</td>`) {
		t.Errorf("the review page does not show the new id:\n%s", body)
	}
	if strings.Contains(body, "maispec-x") {
		t.Errorf("the review page still shows the old id:\n%s", body)
	}
}

func TestRemoveDraftOnlyCourse(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := dirtyServer(t, gh.URL) // msa-a marked full: the change that must survive
	send(t, s, http.MethodPost, "/courses/new", courseForm("maispec"))
	if rec := send(t, s, http.MethodPost, "/dates/new", dateFormFor("maispec")); rec.Code != http.StatusSeeOther {
		t.Fatalf("date not added:\n%s", rec.Body.String())
	}
	d, _ := s.drafts.Get("sid")
	if len(d.Changes) != 3 {
		t.Fatalf("setup: changes = %+v", d.Changes)
	}

	// The confirmation names what goes, and changes nothing.
	rec := send(t, s, http.MethodGet, "/courses/maispec/delete", nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("GET confirm -> %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{"MAI-SPEC", "27-03 MAI", `action="/courses/maispec/delete"`, "draft"} {
		if !strings.Contains(body, want) {
			t.Errorf("confirmation does not show %q:\n%s", want, body)
		}
	}
	if _, ok := d.Doc.Model().FindCourse("maispec"); !ok || len(d.Changes) != 3 {
		t.Fatal("viewing the confirmation changed the draft")
	}

	if rec := send(t, s, http.MethodPost, "/courses/maispec/delete", url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST remove -> %d:\n%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(string(d.Doc.Bytes()), "maispec") {
		t.Errorf("the course or its date is still in the file:\n%s", d.Doc.Bytes())
	}
	if len(d.Changes) != 1 || d.Changes[0].DateID != "msa-a" {
		t.Errorf("changes = %+v, want only the msa-a edit", d.Changes)
	}
	if !strings.Contains(string(d.Doc.Bytes()), "status: full") {
		t.Errorf("the other edit was lost:\n%s", d.Doc.Bytes())
	}
}

// Browsers compile pattern= with the RegExp v flag, where an unescaped "-" at
// the edge of a class is a syntax error, and an invalid pattern is ignored
// without a word: the id field "checked" [a-z0-9-]+ and let MAISPEC through.
// Go cannot compile a JavaScript v-mode expression, so this checks the
// constructs v-mode refuses inside a class, and asks node too when it is
// installed.
func TestIDPatternsValidInVMode(t *testing.T) {
	attr := regexp.MustCompile(`pattern="([^"]*)"`)
	var patterns []string
	err := fs.WalkDir(assets, "templates", func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		b, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range attr.FindAllStringSubmatch(string(b), -1) {
			patterns = append(patterns, m[1])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) == 0 {
		t.Fatal("found no pattern= attributes; the test is looking in the wrong place")
	}
	for _, p := range patterns {
		if msg := vModeProblem(p); msg != "" {
			t.Errorf("pattern=%q is invalid under the v flag: %s", p, msg)
		}
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Log("node not installed; checked the constructs only")
		return
	}
	for _, p := range patterns {
		out, err := exec.Command(node, "-e",
			`new RegExp("^(?:" + process.argv[1] + ")$", "v")`, p).CombinedOutput()
		if err != nil {
			t.Errorf("node rejects pattern=%q under the v flag: %s", p, out)
		}
	}
}

// vModeProblem reports what the v flag rejects inside a character class:
// a "-" that is not between two characters of a range, and the syntax
// characters that must be escaped there. Nested classes and set operations
// are not used in these templates and are reported too.
func vModeProblem(p string) string {
	inClass := false
	var prev rune // the previous class atom, 0 at the start or after a range
	rs := []rune(p)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if !inClass {
			switch r {
			case '\\':
				i++
			case '[':
				inClass, prev = true, 0
				if i+1 < len(rs) && rs[i+1] == '^' {
					i++
				}
			}
			continue
		}
		switch r {
		case '\\':
			i++
			prev = 'x'
		case ']':
			inClass = false
		case '-':
			if prev == 0 || i+1 >= len(rs) || rs[i+1] == ']' {
				return "unescaped - at the edge of a class or after a range"
			}
			i++ // the end of the range
			prev = 0
		case '(', ')', '[', '{', '}', '/', '|':
			return "unescaped " + string(r) + " inside a class"
		default:
			prev = r
		}
	}
	return ""
}

// Type-time help on both id fields: no capital first letter from a phone
// keyboard, no spelling squiggles under "maispec", and form.js lower-cases
// what is typed in a field marked data-lowercase.
func TestIDInputsHelpWhileTyping(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	tag := regexp.MustCompile(`<input name="id"[^>]*>`)
	for _, target := range []string{"/courses/new", "/dates/new"} {
		input := tag.FindString(send(t, s, http.MethodGet, target, nil).Body.String())
		for _, want := range []string{`autocapitalize="none"`, `spellcheck="false"`, "data-lowercase"} {
			if !strings.Contains(input, want) {
				t.Errorf("%s: the id field lacks %s: %s", target, want, input)
			}
		}
	}
	js, err := assets.ReadFile("static/form.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), `querySelectorAll("input[data-lowercase]"), lowerAsTyped`) {
		t.Error("form.js does not lower-case the fields marked data-lowercase")
	}
}
