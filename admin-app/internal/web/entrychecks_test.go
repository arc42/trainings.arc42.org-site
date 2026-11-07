package web

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"arc42-trainings-admin/internal/ghfake"
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

// courseForm is a complete course form with the given short title, the
// course URL derived from it as form.js would.
func courseForm(shortTitle string) url.Values {
	f := newMASForm()
	f.Del("id")
	f.Set("short_title", shortTitle)
	f.Set("title", "Spezifikation mit KI")
	f.Set("url", "https://www.arc42.de/info-"+courseIDFrom(shortTitle)+"/")
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

// The save runs the live schema, not a copy of it: a constraint only the
// schema has (here a made-up one on the full title) stops a course save, in
// the field's words. Every constraint the real schema puts on a course field is
// also checked by the form itself by now, so the real one cannot show this.
func TestCourseSaveRunsProposeChecks(t *testing.T) {
	gh, _ := fakeGitHubFiles(t, map[string][]byte{
		"_data/trainings.yml": []byte(fixtureYAML),
		"api/trainings.schema.json": []byte(`{"type": "object", "properties": {"courses": {"type": "array",
			"items": {"properties": {"title": {"type": "string", "pattern": "^[A-Z]"}}}}}}`),
	})
	defer gh.Close()
	s := testServer(t, gh.URL)

	form := courseForm("MAISPEC")
	form.Set("title", "spezifikation mit KI")
	form.Set("confirm_warnings", "1")
	rec := send(t, s, http.MethodPost, "/courses/new", form)
	if d, ok := s.drafts.Get("sid"); ok && d.Dirty() {
		t.Fatalf("a course the review page rejects was saved:\n%s", d.Doc.Bytes())
	}
	if !strings.Contains(rec.Body.String(), "<b>Full title</b> — is not in the expected form") {
		t.Errorf("the schema problem is not on the form:\n%s", rec.Body.String())
	}
}

// A course URL that is not https is the form's own problem, not a warning and
// not the schema's: it blocks even when the schema cannot be fetched.
func TestCourseURLMustBeHTTPS(t *testing.T) {
	for field, label := range map[string]string{"url": "Course URL", "url_en": "English course page (optional)"} {
		for _, u := range []string{"http://www.arc42.de/info-maispec/", "ftp://example.org/maispec"} {
			gh, _ := fakeGitHub(t) // the trivial schema: this is not the schema's check
			s := testServer(t, gh.URL)
			form := courseForm("MAISPEC")
			form.Set(field, u)
			form.Set("confirm_warnings", "1")
			rec := send(t, s, http.MethodPost, "/courses/new", form)
			gh.Close()
			if d, ok := s.drafts.Get("sid"); ok && d.Dirty() {
				t.Errorf("%s %s was saved", field, u)
			}
			if !strings.Contains(rec.Body.String(), "<b>"+label+"</b> — must start with https://") {
				t.Errorf("%s %s: no problem labelled %s:\n%s", field, u, label, rec.Body.String())
			}
		}
	}
}

// One round: the form's own checks and the schema's are reported together, so
// the operator does not fix one problem only to be shown the next.
func TestCourseFormReportsAllProblemsAtOnce(t *testing.T) {
	gh, _ := fakeGitHubFiles(t, map[string][]byte{
		"_data/trainings.yml": []byte(fixtureYAML),
		"api/trainings.schema.json": []byte(`{"type": "object", "properties": {"courses": {"type": "array",
			"items": {"properties": {"title": {"type": "string", "pattern": "^[A-Z]"}}}}}}`),
	})
	defer gh.Close()
	s := testServer(t, gh.URL)

	for _, id := range []string{"!!!", "MSA", ""} { // no id, taken, missing
		form := courseForm(id)
		form.Set("title", "spezifikation mit KI")
		form.Set("url", "http://www.arc42.de/info-maispec/")
		form.Del("trainer")
		form.Set("confirm_warnings", "1")
		body := send(t, s, http.MethodPost, "/courses/new", form).Body.String()
		for _, label := range []string{"Short title", "Course URL", "Trainers", "Full title"} {
			if !strings.Contains(body, "<b>"+label+"</b> — ") {
				t.Errorf("id %q: no problem on %s in the same round:\n%s", id, label, between(body, `class="problems"`, "</div>"))
			}
		}
		if n := strings.Count(between(body, `class="problems"`, "</div>"), "<b>Short title</b>"); n != 1 {
			t.Errorf("short title %q: named %d times, want once", id, n)
		}
	}
}

// The schema is fetched from GitHub on every save. When that fails, the app
// could not check, which is its fault and not the operator's: the save goes
// through (the review page and CI check again), and the log says so.
func TestSchemaFetchFailureNeverBlocksASave(t *testing.T) {
	f := ghfake.New(ghfake.Options{
		Repo: "arc42/site", Login: "gernotstarke", CanPush: true,
		Files:        map[string][]byte{"_data/trainings.yml": []byte(fixtureYAML)},
		OnUnexpected: func(method, path string) { t.Errorf("unexpected %s %s", method, path) },
	})
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/trainings.schema.json") {
			http.Error(w, `{"message":"Server Error"}`, http.StatusInternalServerError)
			return
		}
		f.Handler().ServeHTTP(w, r)
	}))
	defer gh.Close()
	s := testServer(t, gh.URL)

	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)

	if rec := send(t, s, http.MethodPost, "/courses/new", courseForm("MAISPEC")); rec.Code != http.StatusSeeOther {
		t.Errorf("course save -> %d, want it saved:\n%s", rec.Code, rec.Body.String())
	}
	if rec := send(t, s, http.MethodPost, "/dates/new", dateFormFor("maispec")); rec.Code != http.StatusSeeOther {
		t.Errorf("date save -> %d, want it saved:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logged.String(), "could not fetch the schema") {
		t.Errorf("the skipped check was not logged: %q", logged.String())
	}
}

// form.js derives the course URL from the id it makes of the short title.
// Without scripting the URL is typed by hand, likely as the short title is
// spelled: "Mai Spec" must not be saved as mai-spec with a page at
// info-Mai Spec/.
func TestCourseURLFollowsTheNormalisedID(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	// What the browser sends: the short title, and a URL typed after it.
	for raw, id := range map[string]string{
		"Mai Spec": "mai-spec", "mai lab": "mai-lab", " Mai_Talk ": "mai-talk",
	} {
		form := courseForm(raw)
		form.Set("url", "https://www.arc42.de/info-"+strings.TrimSpace(raw)+"/")
		if rec := send(t, s, http.MethodPost, "/courses/new", form); rec.Code != http.StatusSeeOther {
			t.Fatalf("%q -> %d:\n%s", raw, rec.Code, rec.Body.String())
		}
		d, _ := s.drafts.Get("sid")
		c, ok := d.Doc.Model().FindCourse(id)
		if !ok {
			t.Fatalf("%q: no course %s:\n%s", raw, id, d.Doc.Bytes())
		}
		if want := "https://www.arc42.de/info-" + id + "/"; c.URL != want {
			t.Errorf("%q: URL = %q, want %q", raw, c.URL, want)
		}
	}

	// A URL the operator chose is theirs, and is left alone.
	form := courseForm("Flex")
	form.Set("url", "https://www.arc42.de/info-FLEX-kurs/")
	send(t, s, http.MethodPost, "/courses/new", form)
	d, _ := s.drafts.Get("sid")
	if c, _ := d.Doc.Model().FindCourse("flex"); c.URL != "https://www.arc42.de/info-FLEX-kurs/" {
		t.Errorf("a hand-chosen URL was rewritten to %q", c.URL)
	}
}

// form.js lower-cases while typing and normalises like the server when the
// field is left. The two must agree; checked when node is installed.
func TestFormJSNormalisesLikeTheServer(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	js, err := assets.ReadFile("static/form.js")
	if err != nil {
		t.Fatal(err)
	}
	fn := regexp.MustCompile(`(?s)function normaliseId\(.*?\n  \}\n`).Find(js)
	if fn == nil {
		t.Fatal("form.js has no normaliseId function")
	}
	inputs := []string{" MAISPEC ", "Mai Spec", "mai__ _spec", "Mai\tSpec", "a-b_c d", "mäi/spec", "x"}
	in, _ := json.Marshal(inputs)
	out, err := exec.Command(node, "-e", string(fn)+
		"console.log(JSON.stringify(JSON.parse(process.argv[1]).map(normaliseId)))", string(in)).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v: %s", err, out)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("node said %s", out)
	}
	for i, v := range inputs {
		if got[i] != normaliseID(v) {
			t.Errorf("%q: form.js %q, server %q", v, got[i], normaliseID(v))
		}
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
	send(t, s, http.MethodPost, "/courses/new", courseForm("MAISPEC"))

	body := send(t, s, http.MethodGet, "/courses/maispec", nil).Body.String()
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
	send(t, s, http.MethodPost, "/courses/new", courseForm("MAISPEC"))
	if rec := send(t, s, http.MethodPost, "/dates/new", dateFormFor("maispec")); rec.Code != http.StatusSeeOther {
		t.Fatalf("date not added:\n%s", rec.Body.String())
	}

	body := send(t, s, http.MethodGet, "/courses/maispec", nil).Body.String()
	if !strings.Contains(body, "remove its dates to change it") {
		t.Errorf("the hint does not say how to change the id:\n%s", body)
	}
	// Still removable as a whole: the dates go with it.
	if !strings.Contains(body, `href="/courses/maispec/delete"`) {
		t.Errorf("no way to remove the course from the draft:\n%s", body)
	}

	// A new short title does not rename it: its dates reference it.
	form := courseForm("OTHER")
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
	if strings.Contains(body, "/courses/msa/delete") {
		t.Error("a published course offers removal")
	}

	// Nor can a hand-made POST rename or remove it.
	send(t, s, http.MethodPost, "/courses/msa", url.Values{
		"id": {"msa-neu"}, "short_title": {"MSA Neu"}, "title": {"Mastering Software Architectures"},
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
	send(t, s, http.MethodPost, "/courses/new", courseForm("MAISPEC-X"))

	rec := send(t, s, http.MethodPost, "/courses/maispec-x", courseForm("MAISPEC"))
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
	send(t, s, http.MethodPost, "/courses/new", courseForm("MAISPEC"))
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
	for _, want := range []string{"MAISPEC", "27-03 MAISPEC", `action="/courses/maispec/delete"`, "draft"} {
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
	for _, target := range []string{"/dates/new"} { // the course form has no id field
		input := tag.FindString(send(t, s, http.MethodGet, target, nil).Body.String())
		for _, want := range []string{`autocapitalize="none"`, `spellcheck="false"`, "data-lowercase",
			// The browser shows the title when the pattern refuses a value.
			`title="lower-case letters, digits and hyphens; spaces and underscores become hyphens"`} {
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
