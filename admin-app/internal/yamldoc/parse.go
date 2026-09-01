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
	"strconv"

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
			CreditPoints:  creditPoints(cn),
			URL:           scalar(cn, "url"),
			URLEn:         scalar(cn, "url_en"),
			Trainers:      stringList(cn, "trainers"),
		}
		if dn := mapValue(cn, "dates"); dn != nil {
			for _, en := range dn.Content {
				c.Dates = append(c.Dates, model.Date{
					ID:           scalar(en, "id"),
					Code:         scalar(en, "code"),
					Start:        scalar(en, "start"),
					End:          scalar(en, "end"),
					City:         scalar(en, "city"),
					Country:      scalar(en, "country"),
					Language:     scalar(en, "language"),
					Format:       scalar(en, "format"),
					Trainers:     stringList(en, "trainers"),
					Price:        price(en),
					SeatsLimited: boolean(en, "seats_limited"),
					URL:          scalar(en, "url"),
					Status:       scalar(en, "status"),
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

// integer reads a mapping's scalar as an int. Anything unparseable, including
// a missing key or a YAML null, reads as 0, which every caller treats as
// "absent". The Ruby validator in the site repo is the thing that complains
// about a malformed number; this parser's job is to not crash on one.
func integer(n *yaml.Node, key string) int {
	i, err := strconv.Atoi(scalar(n, key))
	if err != nil {
		return 0
	}
	return i
}

func boolean(n *yaml.Node, key string) bool {
	return scalar(n, key) == "true"
}

// price decodes the price mapping. Absent, or present with no amount, is nil:
// several courses genuinely have no published price.
func price(n *yaml.Node) *model.Price {
	pn := mapValue(n, "price")
	if pn == nil || pn.Kind != yaml.MappingNode {
		return nil
	}
	p := &model.Price{
		Amount:   integer(pn, "amount"),
		Currency: scalar(pn, "currency"),
		Alumni:   integer(pn, "alumni"),
	}
	if p.Amount == 0 {
		return nil
	}
	if p.Currency == "" {
		p.Currency = "EUR"
	}
	// An expired early_bird is kept exactly as stored. It is inert (neither the
	// site nor the feed renders it once the deadline passes) and deleting it
	// behind the operator's back would lose the record of what was offered.
	if eb := mapValue(pn, "early_bird"); eb != nil && eb.Kind == yaml.MappingNode {
		if amount := integer(eb, "amount"); amount > 0 {
			p.EarlyBird = &model.EarlyBird{Amount: amount, Until: scalar(eb, "until")}
		}
	}
	return p
}

// creditPoints decodes the per-category credit mapping, nil when the course
// has none.
func creditPoints(n *yaml.Node) *model.CreditPoints {
	cn := mapValue(n, "credit_points")
	if cn == nil || cn.Kind != yaml.MappingNode {
		return nil
	}
	cp := &model.CreditPoints{
		Methodical:    integer(cn, "methodical"),
		Technical:     integer(cn, "technical"),
		Communication: integer(cn, "communication"),
	}
	if cp.Empty() {
		return nil
	}
	return cp
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
