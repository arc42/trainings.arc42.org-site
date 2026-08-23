package web

import (
	"regexp"
	"strings"
	"testing"

	"arc42-trainings-admin/internal/model"
)

// A placeholder that reads like a stored value is worse than no placeholder:
// an operator deleted a Certification field for a while before working out it
// had been empty the whole time. So every example carries "e.g. ", which no
// stored value ever starts with, and the only exceptions are format masks —
// strings that describe the shape of a value rather than showing one.
var placeholderMasks = []*regexp.Regexp{
	// Date id: "<course>-mmm-yyyy", the course part filled in per course.
	regexp.MustCompile(`^[a-z0-9-]+-mmm-yyyy$`),
	// Booking code: "YY-MM <TOKEN>", likewise.
	regexp.MustCompile(`^YY-MM \S+$`),
}

var placeholderAttr = regexp.MustCompile(`placeholder="([^"]*)"`)

// A placeholder built by a template action is checked through the function
// that builds it, in TestTheDerivedMasksAreMasks below, since its source form
// says nothing about what a reader ends up seeing.
var templateAction = regexp.MustCompile(`^\{\{[^{}]*\}\}$`)

func TestEveryPlaceholderSaysItIsAnExample(t *testing.T) {
	files, err := assets.ReadDir("templates")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, f := range files {
		src, err := assets.ReadFile("templates/" + f.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range placeholderAttr.FindAllStringSubmatch(string(src), -1) {
			value := m[1]
			seen++
			if value == "" || strings.HasPrefix(value, "e.g. ") {
				continue
			}
			if templateAction.MatchString(value) {
				continue
			}
			if isMask(value) {
				continue
			}
			t.Errorf("%s: placeholder %q reads like a value; prefix it with \"e.g. \" "+
				"or make it a format mask", f.Name(), value)
		}
	}
	if seen == 0 {
		t.Fatal("no placeholders found — the check is looking in the wrong place")
	}
}

func isMask(v string) bool {
	for _, re := range placeholderMasks {
		if re.MatchString(v) {
			return true
		}
	}
	return false
}

// The two masks are the only placeholders allowed to skip "e.g. ", so they
// have to actually look like masks — including for a course whose booking
// token is not just its id upper-cased, and for the blank new-date form.
func TestTheDerivedMasksAreMasks(t *testing.T) {
	for _, courseID := range []string{"", "msa", "req4arc"} {
		if got := model.IDMask(courseID); !isMask(got) {
			t.Errorf("IDMask(%q) = %q, which is not a recognised mask", courseID, got)
		}
		if got := model.CodeMask(courseID); !isMask(got) {
			t.Errorf("CodeMask(%q) = %q, which is not a recognised mask", courseID, got)
		}
	}
	if got := model.CodeMask("req4arc"); got != "YY-MM Req4Arc" {
		t.Errorf("CodeMask should carry the course's own booking token, got %q", got)
	}
}
