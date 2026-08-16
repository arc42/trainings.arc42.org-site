package validate

import (
	"os"
	"path/filepath"
	"testing"

	"arc42-trainings-admin/internal/yamldoc"
)

// RulesFromYAML parses fixture YAML and returns every problem the app would
// raise for it — the schema check AND the cross-field rules, exactly as
// handlePropose does before opening a pull request.
//
// Both halves are needed for TestAgreesWithRubyValidator to compare like with
// like. validate_trainings.rb checks structure and enums as well as the
// cross-field rules, so running only Rules() here would make any fixture with,
// say, `language: fr` look like a validator disagreement when it is really just
// the two sides being asked different questions.
func schemaBytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "trainings.schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	return b
}

func RulesFromYAML(t *testing.T, src []byte) []Problem {
	t.Helper()
	doc, err := yamldoc.Parse(src)
	if err != nil {
		return []Problem{{Message: "unparseable: " + err.Error()}}
	}
	model := doc.Model()

	problems := Rules(model)

	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "trainings.schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	feed, err := FeedJSON(model)
	if err != nil {
		t.Fatalf("FeedJSON: %v", err)
	}
	schemaProblems, err := Schema(schema, feed)
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	return append(problems, schemaProblems...)
}
