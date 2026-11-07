package web

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// A course's id and its short title were the same word in every course there
// is (MSA/msa, IMPROVE/improve, Req4Arc/req4arc, ADOC/adoc, MAISPEC/maispec),
// typed twice. The course form now asks for the short title only and makes the
// id from it, for as long as the id may still change.

// followOutput is the course ID output marked for form.js to keep in step
// with the short title.
var followOutput = regexp.MustCompile(`<output[^>]*id="c-id"[^>]*data-follow`)

func TestCourseIDFromShortTitle(t *testing.T) {
	for in, want := range map[string]string{
		"MAISPEC":       "maispec",
		"Req4Arc":       "req4arc",
		" MAI Spec ":    "mai-spec",
		"MAI_Spec":      "mai-spec",
		"MAI-SPEC":      "mai-spec",
		"AI/Spec":       "ai-spec",
		"C++ Basics":    "c-basics",
		"--X--":         "x",
		"Prüfung":       "pruefung",
		"GRÖSSE":        "groesse",
		"Größe":         "groesse",
		"Café Èclair":   "cafe-eclair",
		"iSAQB: Expert": "isaqb-expert",
		"!!!":           "",
		"日本":            "",
		"":              "",
	} {
		if got := courseIDFrom(in); got != want {
			t.Errorf("courseIDFrom(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewCourseIDComesFromShortTitle(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	form := courseForm("MAISPEC")
	form.Set("id", "something-else") // a stale tab of the old form: not used
	if rec := send(t, s, http.MethodPost, "/courses/new", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST -> %d:\n%s", rec.Code, rec.Body.String())
	}
	d, _ := s.drafts.Get("sid")
	c, ok := d.Doc.Model().FindCourse("maispec")
	if !ok {
		t.Fatalf("no course maispec:\n%s", d.Doc.Bytes())
	}
	if c.ShortTitle != "MAISPEC" {
		t.Errorf("short title = %q, want it as typed", c.ShortTitle)
	}
	if strings.Contains(string(d.Doc.Bytes()), "something-else") {
		t.Errorf("a posted id was used:\n%s", d.Doc.Bytes())
	}
}

// One field to type; the id is shown as what it is, a result.
func TestNewCourseFormAsksForTheShortTitleOnly(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	body := send(t, s, http.MethodGet, "/courses/new", nil).Body.String()
	if strings.Contains(body, `name="id"`) {
		t.Errorf("the new-course form still asks for an id:\n%s", body)
	}
	if !regexp.MustCompile(`<output[^>]*id="c-id"`).MatchString(body) {
		t.Errorf("the form does not show the course ID it will make:\n%s", body)
	}
	if !strings.Contains(body, "Made from the short title") {
		t.Errorf("the form does not say where the course ID comes from:\n%s", body)
	}
}

// A short title that makes no id is a problem on the short title, the one
// field the operator can change.
func TestShortTitleThatMakesNoID(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	form := courseForm("!!!")
	form.Set("confirm_warnings", "1")
	rec := send(t, s, http.MethodPost, "/courses/new", form)
	if d, ok := s.drafts.Get("sid"); ok && d.Dirty() {
		t.Fatalf("a course without an id was saved:\n%s", d.Doc.Bytes())
	}
	problems := between(rec.Body.String(), `class="problems"`, "</div>")
	if !strings.Contains(problems, "<b>Short title</b> — needs at least one letter a to z or digit") {
		t.Errorf("no plain problem on the short title:\n%s", problems)
	}
	if strings.Contains(problems, "<b>Course ID</b>") {
		t.Errorf("the problem names a field the form does not have:\n%s", problems)
	}
}

func TestShortTitleOfAnotherCourse(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	form := courseForm("Msa")
	form.Set("confirm_warnings", "1")
	rec := send(t, s, http.MethodPost, "/courses/new", form)
	if d, ok := s.drafts.Get("sid"); ok && d.Dirty() {
		t.Fatalf("a second course msa was saved:\n%s", d.Doc.Bytes())
	}
	problems := between(rec.Body.String(), `class="problems"`, "</div>")
	if !strings.Contains(problems, "<b>Short title</b> — makes the course ID msa, which MSA already has") {
		t.Errorf("no plain duplicate problem on the short title:\n%s", problems)
	}
}

// While the course is only in the draft and has no dates, editing the short
// title renames it, and a course URL made from the old id follows.
func TestDraftOnlyCourseFollowsItsShortTitle(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)
	form := courseForm("MAI Spec")
	form.Set("url", "https://www.arc42.de/info-mai-spec/")
	send(t, s, http.MethodPost, "/courses/new", form)

	body := send(t, s, http.MethodGet, "/courses/mai-spec", nil).Body.String()
	if strings.Contains(body, `name="id"`) {
		t.Errorf("the form asks for an id:\n%s", body)
	}
	if !strings.Contains(body, "only in your draft") {
		t.Errorf("the hint does not say why the id still follows the short title:\n%s", body)
	}
	if !followOutput.MatchString(body) {
		t.Errorf("form.js is not told that the id still follows the short title:\n%s", body)
	}

	// Without scripting the URL arrives as it was stored.
	form = courseForm("MAISPEC")
	form.Set("url", "https://www.arc42.de/info-mai-spec/")
	if rec := send(t, s, http.MethodPost, "/courses/mai-spec", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("rename -> %d:\n%s", rec.Code, rec.Body.String())
	}
	d, _ := s.drafts.Get("sid")
	c, ok := d.Doc.Model().FindCourse("maispec")
	if !ok {
		t.Fatalf("not renamed to maispec:\n%s", d.Doc.Bytes())
	}
	if c.URL != "https://www.arc42.de/info-maispec/" {
		t.Errorf("URL = %q, want it to follow the new id", c.URL)
	}
	if len(d.Changes) != 1 || d.Changes[0].Kind != "added" || d.Changes[0].CourseID != "maispec" {
		t.Errorf("changes = %+v, want one addition of maispec", d.Changes)
	}
}

// Once dates reference the id it is fixed; the short title is just text again.
func TestCourseWithDatesKeepsItsIDWhenTheShortTitleChanges(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)
	send(t, s, http.MethodPost, "/courses/new", courseForm("MAISPEC"))
	if rec := send(t, s, http.MethodPost, "/dates/new", dateFormFor("maispec")); rec.Code != http.StatusSeeOther {
		t.Fatalf("date not added:\n%s", rec.Body.String())
	}

	// form.js must not preview an id the server will not store.
	if body := send(t, s, http.MethodGet, "/courses/maispec", nil).Body.String(); followOutput.MatchString(body) {
		t.Errorf("the fixed id of a course with dates is marked as following the short title:\n%s", body)
	}

	form := courseForm("MAI-Spec")
	form.Set("confirm_warnings", "1")
	if rec := send(t, s, http.MethodPost, "/courses/maispec", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("save -> %d:\n%s", rec.Code, rec.Body.String())
	}
	d, _ := s.drafts.Get("sid")
	c, ok := d.Doc.Model().FindCourse("maispec")
	if !ok || c.ShortTitle != "MAI-Spec" {
		t.Errorf("want id maispec with short title MAI-Spec:\n%s", d.Doc.Bytes())
	}
}

func TestCourseOnMainKeepsItsIDWhenTheShortTitleChanges(t *testing.T) {
	gh, _ := fakeGitHub(t)
	defer gh.Close()
	s := testServer(t, gh.URL)

	body := send(t, s, http.MethodGet, "/courses/msa", nil).Body.String()
	if !regexp.MustCompile(`<output[^>]*id="c-id"[^>]*>msa</output>`).MatchString(body) {
		t.Errorf("the fixed course ID is not shown:\n%s", body)
	}
	if followOutput.MatchString(body) {
		t.Errorf("the id of a published course is marked as following the short title:\n%s", body)
	}
	if !strings.Contains(body, "Fixed: the course is published") {
		t.Errorf("the hint does not say why the id is fixed:\n%s", body)
	}

	form := courseForm("MSA Plus")
	form.Set("url", "https://www.arc42.de/info-msa/")
	form.Set("confirm_warnings", "1")
	if rec := send(t, s, http.MethodPost, "/courses/msa", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("save -> %d:\n%s", rec.Code, rec.Body.String())
	}
	d, _ := s.drafts.Get("sid")
	if c, ok := d.Doc.Model().FindCourse("msa"); !ok || c.ShortTitle != "MSA Plus" {
		t.Errorf("want msa kept, short title MSA Plus:\n%s", d.Doc.Bytes())
	}
}

// form.js shows the id the server will make; the two must agree.
func TestFormJSMakesTheCourseIDLikeTheServer(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	js, err := assets.ReadFile("static/form.js")
	if err != nil {
		t.Fatal(err)
	}
	fn := regexp.MustCompile(`(?s)var ID_LETTERS = .*?\n  function courseIdFrom\(.*?\n  \}\n`).Find(js)
	if fn == nil {
		t.Fatal("form.js has no ID_LETTERS and courseIdFrom")
	}
	inputs := []string{"MAISPEC", "Req4Arc", " MAI Spec ", "MAI_Spec", "AI/Spec", "C++ Basics",
		"--X--", "Prüfung", "GRÖSSE", "Größe", "Café Èclair", "iSAQB: Expert", "!!!", "日本", ""}
	// Every letter of the two tables, alone and between others, in both
	// cases: a typo in one table is a different id for that letter.
	for _, l := range strings.Split("ä ö ü ß æ œ à á â ã å ç è é ê ë ì í î ï ñ ò ó ô õ ø ù ú û ý ÿ", " ") {
		inputs = append(inputs, "x"+l+"x", "X"+strings.ToUpper(l)+"X")
	}
	in, _ := json.Marshal(inputs)
	out, err := exec.Command(node, "-e", string(fn)+
		"console.log(JSON.stringify(JSON.parse(process.argv[1]).map(courseIdFrom)))", string(in)).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v: %s", err, out)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("node said %s", out)
	}
	for i, v := range inputs {
		if got[i] != courseIDFrom(v) {
			t.Errorf("%q: form.js %q, server %q", v, got[i], courseIDFrom(v))
		}
	}
}
