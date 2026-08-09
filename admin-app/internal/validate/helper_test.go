package validate

import (
	"testing"

	"arc42-trainings-admin/internal/yamldoc"
)

// RulesFromYAML parses fixture YAML and runs the cross-field rules on it.
func RulesFromYAML(t *testing.T, src []byte) []Problem {
	t.Helper()
	doc, err := yamldoc.Parse(src)
	if err != nil {
		return []Problem{{Field: "", Message: "unparseable: " + err.Error()}}
	}
	return Rules(doc.Model())
}
