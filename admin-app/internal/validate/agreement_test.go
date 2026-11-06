package validate

import (
	"os"
	"path/filepath"
	"testing"

	"arc42-trainings-admin/internal/yamldoc"
)

// TestAppAcceptsWhatCIAccepts: every file in testdata/ci_accepts/ also passes
// scripts/validate_trainings.rb (CI runs it over the same directory). If the
// app rejects one, an operator is blocked by a check CI would not make.
func TestAppAcceptsWhatCIAccepts(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "ci_accepts", "*.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures found: %v", err)
	}
	schema := schemaBytes(t)
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := yamldoc.Parse(src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			model := doc.Model()
			for _, p := range Rules(model) {
				t.Errorf("rule: %+v", p)
			}
			feed, err := FeedJSON(model)
			if err != nil {
				t.Fatal(err)
			}
			problems, err := Schema(schema, feed)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range problems {
				t.Errorf("schema: %+v", p)
			}
		})
	}
}
