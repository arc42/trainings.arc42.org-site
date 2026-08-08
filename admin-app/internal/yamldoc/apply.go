package yamldoc

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"arc42-trainings-admin/internal/model"
)

// UpdateDate replaces one date entry in place. Only that entry's lines change.
func (d *Doc) UpdateDate(id string, nd model.Date) error {
	start, end, indent, ok := d.dateExtent(id)
	if !ok {
		return fmt.Errorf("update date %q: not found", id)
	}
	d.replaceLines(start, end, RenderDate(nd, indent))
	return nil
}

// DeleteDate removes one date entry, leaving surrounding blank lines and
// comments with whatever follows.
func (d *Doc) DeleteDate(id string) error {
	start, end, _, ok := d.dateExtent(id)
	if !ok {
		return fmt.Errorf("delete date %q: not found", id)
	}
	// Also drop a single blank separator line directly after the entry, so
	// deleting does not leave a double blank.
	src := d.lines()
	if end < len(src) && strings.TrimSpace(src[end]) == "" {
		end++
	}
	d.replaceLines(start, end, "")
	return nil
}

// AddDate appends a date to a course's dates sequence.
func (d *Doc) AddDate(courseID string, nd model.Date) error {
	m := d.Model()
	var course *model.Course
	for i := range m.Courses {
		if m.Courses[i].ID == courseID {
			course = &m.Courses[i]
		}
	}
	if course == nil {
		return fmt.Errorf("add date: course %q not found", courseID)
	}
	if len(course.Dates) == 0 {
		return d.addFirstDate(courseID, nd)
	}
	last := course.Dates[len(course.Dates)-1]
	start, end, indent, ok := d.dateExtent(last.ID)
	if !ok {
		return fmt.Errorf("add date: cannot locate last date of course %q", courseID)
	}
	existing := strings.Join(d.lines()[start-1:end], "\n") + "\n"
	d.replaceLines(start, end, existing+"\n"+RenderDate(nd, indent))
	return nil
}

// addFirstDate handles a course whose dates sequence is empty or absent.
func (d *Doc) addFirstDate(courseID string, nd model.Date) error {
	courses := mapValue(d.root, "courses")
	for _, cn := range courses.Content {
		if scalar(cn, "id") != courseID {
			continue
		}
		datesKey := mappingKeyNode(cn, "dates")
		if datesKey == nil {
			return fmt.Errorf("add date: course %q has no 'dates' key", courseID)
		}
		indent := strings.Repeat(" ", datesKey.Column+1)
		line := datesKey.Line
		existing := d.lines()[line-1]
		d.replaceLines(line, line, existing+"\n"+RenderDate(nd, indent))
		return nil
	}
	return fmt.Errorf("add date: course %q not found", courseID)
}

// UpdateCourse rewrites a course's own scalar fields, leaving its dates
// sequence untouched.
func (d *Doc) UpdateCourse(id string, nc model.Course) error {
	courses := mapValue(d.root, "courses")
	for ci, cn := range courses.Content {
		if scalar(cn, "id") != id {
			continue
		}
		datesKey := mappingKeyNode(cn, "dates")
		if datesKey == nil {
			return fmt.Errorf("update course %q: no 'dates' key to anchor on", id)
		}
		start := cn.Line
		end := datesKey.Line - 1 // stop right before "dates:"
		indent := strings.Repeat(" ", maxInt(cn.Column-3, 0))
		_ = ci
		d.replaceLines(start, end, renderCourseScalars(nc, indent))
		return nil
	}
	return fmt.Errorf("update course %q: not found", id)
}

// mappingKeyNode returns the *key* node (not the value) for a mapping key,
// which is what carries the line we need as an anchor.
func mappingKeyNode(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i]
		}
	}
	return nil
}
