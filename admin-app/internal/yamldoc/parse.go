// Package yamldoc reads _data/trainings.yml and applies edits as text splices
// into the original bytes.
//
// It deliberately never re-serialises the whole document. yaml.v3 drops blank
// lines and re-flows folded scalars, so a Marshal round-trip would rewrite the
// file's formatting, bury real changes in a several-hundred-line diff, and
// defeat the human review gate that app-generated PRs exist for. Instead the
// parser is used only to locate nodes (yaml.Node carries Line and Column), and
// edits replace just the affected line range.
package yamldoc

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"arc42-trainings-admin/internal/model"
)

// Doc is a parsed trainings.yml together with its original bytes.
type Doc struct {
	src  []byte
	root *yaml.Node // the document's top-level mapping
}

// Parse reads YAML source and indexes it for locating and editing.
func Parse(src []byte) (*Doc, error) {
	var file yaml.Node
	if err := yaml.Unmarshal(src, &file); err != nil {
		return nil, fmt.Errorf("parse trainings.yml: %w", err)
	}
	if file.Kind != yaml.DocumentNode || len(file.Content) != 1 {
		return nil, fmt.Errorf("parse trainings.yml: expected a single YAML document")
	}
	root := file.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("parse trainings.yml: top level is not a mapping")
	}
	if mapValue(root, "courses") == nil {
		return nil, fmt.Errorf("parse trainings.yml: top-level 'courses' key missing")
	}
	return &Doc{src: src, root: root}, nil
}

// Bytes returns the current source, including any applied edits.
func (d *Doc) Bytes() []byte { return d.src }

// Model decodes the document into domain types for display and validation.
func (d *Doc) Model() model.Trainings {
	var t model.Trainings
	courses := mapValue(d.root, "courses")
	if courses == nil {
		return t
	}
	for _, cn := range courses.Content {
		c := model.Course{
			ID:            scalar(cn, "id"),
			ShortTitle:    scalar(cn, "short_title"),
			Title:         scalar(cn, "title"),
			Blurb:         scalar(cn, "blurb"),
			Certification: scalar(cn, "certification"),
			Credits:       scalar(cn, "credits"),
			URL:           scalar(cn, "url"),
			URLEn:         scalar(cn, "url_en"),
			Trainers:      stringList(cn, "trainers"),
		}
		if dn := mapValue(cn, "dates"); dn != nil {
			for _, en := range dn.Content {
				c.Dates = append(c.Dates, model.Date{
					ID:       scalar(en, "id"),
					Code:     scalar(en, "code"),
					Start:    scalar(en, "start"),
					End:      scalar(en, "end"),
					City:     scalar(en, "city"),
					Country:  scalar(en, "country"),
					Language: scalar(en, "language"),
					Format:   scalar(en, "format"),
					Trainers: stringList(en, "trainers"),
					Pricing:  scalar(en, "pricing"),
					FewSeats: scalar(en, "few_seats"),
					URL:      scalar(en, "url"),
					Status:   scalar(en, "status"),
				})
			}
		}
		t.Courses = append(t.Courses, c)
	}
	return t
}

// mapValue returns the value node for a key in a mapping node, or nil.
func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// scalar returns a mapping's scalar value as a string. A YAML null reads as "".
func scalar(n *yaml.Node, key string) string {
	v := mapValue(n, key)
	if v == nil || v.Tag == "!!null" {
		return ""
	}
	return v.Value
}

func stringList(n *yaml.Node, key string) []string {
	v := mapValue(n, key)
	if v == nil || v.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]string, 0, len(v.Content))
	for _, item := range v.Content {
		out = append(out, item.Value)
	}
	return out
}
