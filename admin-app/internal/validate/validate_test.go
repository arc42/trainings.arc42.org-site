package validate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"arc42-trainings-admin/internal/model"
)

func ok() model.Trainings {
	return model.Trainings{Courses: []model.Course{{
		ID: "msa", ShortTitle: "MSA", Title: "Mastering", URL: "https://example.org",
		Trainers: []string{"Peter Hruschka"},
		Dates: []model.Date{{
			ID: "a", Code: "A", Start: "2026-01-01", End: "2026-01-02",
			City: "München", Country: "DE", Language: "de", Format: "public",
			URL: "https://example.org/a", Status: "open",
		}},
	}}}
}

func TestRulesAcceptValidData(t *testing.T) {
	if p := Rules(ok()); len(p) != 0 {
		t.Errorf("unexpected problems: %v", p)
	}
}

func TestRuleEndBeforeStart(t *testing.T) {
	tr := ok()
	tr.Courses[0].Dates[0].End = "2025-12-31"
	assertProblem(t, Rules(tr), "before start")
}

func TestRuleNonOnlineNeedsCity(t *testing.T) {
	tr := ok()
	tr.Courses[0].Dates[0].City = ""
	assertProblem(t, Rules(tr), "city")
}

func TestRuleOnlineDoesNotNeedCity(t *testing.T) {
	tr := ok()
	tr.Courses[0].Dates[0].City = ""
	tr.Courses[0].Dates[0].Format = "online"
	if p := Rules(tr); len(p) != 0 {
		t.Errorf("online date should not require a city: %v", p)
	}
}

func TestRuleDuplicateDateID(t *testing.T) {
	tr := ok()
	d := tr.Courses[0].Dates[0]
	d.Code = "B"
	tr.Courses[0].Dates = append(tr.Courses[0].Dates, d)
	assertProblem(t, Rules(tr), "duplicate date id")
}

func TestRuleDuplicateBookingCode(t *testing.T) {
	tr := ok()
	d := tr.Courses[0].Dates[0]
	d.ID = "b"
	tr.Courses[0].Dates = append(tr.Courses[0].Dates, d)
	assertProblem(t, Rules(tr), "duplicate booking code")
}

func TestSchemaRejectsBadLanguage(t *testing.T) {
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "trainings.schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	tr := ok()
	tr.Courses[0].Dates[0].Language = "fr"
	feed, err := FeedJSON(tr)
	if err != nil {
		t.Fatalf("FeedJSON: %v", err)
	}
	problems, err := Schema(schema, feed)
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if len(problems) == 0 {
		t.Error("schema accepted language \"fr\"")
	}
}

func TestSchemaAcceptsTheRealFile(t *testing.T) {
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "trainings.schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	feed, err := FeedJSON(ok())
	if err != nil {
		t.Fatalf("FeedJSON: %v", err)
	}
	problems, err := Schema(schema, feed)
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if len(problems) != 0 {
		t.Errorf("schema rejected valid data: %v", problems)
	}
}

// TestAgreesWithRubyValidator is the anti-drift guard. The Go validator and
// scripts/validate_trainings.rb must reach the same verdict on every fixture.
// Without this, the duplication silently rots.
func TestAgreesWithRubyValidator(t *testing.T) {
	if _, err := exec.LookPath("ruby"); err != nil {
		// Skipping here is safe: CI installs Ruby 3.3, so the cross-check
		// always runs before anything deploys.
		t.Skip("ruby not installed; this check runs in CI")
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "..", "scripts", "validate_trainings.rb"))
	if err != nil {
		t.Fatalf("abs script path: %v", err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Skipf("validator not found: %v", err)
	}
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("testdata", name)
			// The Ruby validator reads _data/trainings.yml relative to the repo
			// root, so it is invoked with the fixture path as an override. The
			// path must be made absolute first: cmd.Dir below changes the
			// child process's working directory to the repo root, and a
			// relative ARGV entry would then be resolved from there instead
			// of from this package's directory, so the fixture would not be
			// found.
			absPath, err := filepath.Abs(path)
			if err != nil {
				t.Fatalf("abs path: %v", err)
			}
			cmd := exec.Command("ruby", script, absPath)
			cmd.Dir = filepath.Join("..", "..", "..")
			rubyOK := cmd.Run() == nil

			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			goOK := len(RulesFromYAML(t, src)) == 0

			if rubyOK != goOK {
				t.Errorf("%s: ruby ok=%v, go ok=%v — validators disagree", name, rubyOK, goOK)
			}
		})
	}
}

func TestFeedJSONCarriesURLEnAndSchemaAcceptsIt(t *testing.T) {
	tr := ok()
	tr.Courses[0].URLEn = "https://trainings.arc42.org/courses/msa/"
	feed, err := FeedJSON(tr)
	if err != nil {
		t.Fatalf("FeedJSON: %v", err)
	}
	if !strings.Contains(string(feed), `"url_en":"https://trainings.arc42.org/courses/msa/"`) {
		t.Errorf("feed lacks url_en:\n%s", feed)
	}
	problems, err := Schema(schemaBytes(t), feed)
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if len(problems) != 0 {
		t.Errorf("schema rejected url_en: %v", problems)
	}

	// Without url_en the key must be absent, not "".
	tr.Courses[0].URLEn = ""
	feed, _ = FeedJSON(tr)
	if strings.Contains(string(feed), "url_en") {
		t.Errorf("empty url_en must be omitted:\n%s", feed)
	}
}

func assertProblem(t *testing.T, problems []Problem, substr string) {
	t.Helper()
	for _, p := range problems {
		if strings.Contains(strings.ToLower(p.Message), strings.ToLower(substr)) {
			return
		}
	}
	t.Errorf("no problem mentioning %q; got %v", substr, problems)
}
