package yamldoc

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

// lines splits the source into 1-based-addressable lines (index 0 unused).
func (d *Doc) lines() []string {
	return strings.Split(string(d.src), "\n")
}

// dateExtent returns the 1-based inclusive line range covering one date entry,
// plus the indentation of its "- " bullet.
//
// The end is derived from the next node that starts at or before this entry's
// indentation, then walked back over trailing blank lines and comments. Those
// belong to whatever follows, not to this entry — swallowing them would make
// deletions eat their neighbour's header comment.
func (d *Doc) dateExtent(dateID string) (start, end int, indent string, ok bool) {
	courses := mapValue(d.root, "courses")
	if courses == nil {
		return 0, 0, "", false
	}
	var entry *yaml.Node
	var next *yaml.Node // the node that begins after entry, at any level
	for ci, cn := range courses.Content {
		dates := mapValue(cn, "dates")
		if dates == nil {
			continue
		}
		for di, en := range dates.Content {
			if scalar(en, "id") != dateID {
				continue
			}
			entry = en
			switch {
			case di+1 < len(dates.Content):
				next = dates.Content[di+1] // the following date
			case ci+1 < len(courses.Content):
				next = courses.Content[ci+1] // the following course
			}
		}
	}
	if entry == nil {
		return 0, 0, "", false
	}

	src := d.lines()
	start = entry.Line
	if next != nil {
		end = next.Line - 1
	} else {
		end = len(src) // 1-based count of lines
	}
	// Walk back over blank lines and comments that introduce what comes next.
	for end > start {
		l := strings.TrimSpace(src[end-1])
		if l == "" || strings.HasPrefix(l, "#") {
			end--
			continue
		}
		break
	}

	// yaml.v3 reports Column of the entry's first key ("id"), which sits two
	// characters past the "- " bullet.
	indent = strings.Repeat(" ", maxInt(entry.Column-3, 0))
	return start, end, indent, true
}

// lineOffsets returns the byte offset at which each 1-based line starts.
func (d *Doc) lineOffsets() []int {
	offsets := []int{0, 0} // index 0 unused; line 1 starts at byte 0
	for i, b := range d.src {
		if b == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

// replaceLines swaps the 1-based inclusive line range [start,end] for repl,
// which must already carry its own trailing newline (or be empty to delete).
func (d *Doc) replaceLines(start, end int, repl string) {
	offsets := d.lineOffsets()
	from := offsets[start]
	var to int
	if end+1 < len(offsets) {
		to = offsets[end+1]
	} else {
		to = len(d.src)
	}
	var buf bytes.Buffer
	buf.Write(d.src[:from])
	buf.WriteString(repl)
	buf.Write(d.src[to:])
	d.src = buf.Bytes()
	// Re-parse so subsequent extents reflect the new line numbers.
	if reparsed, err := Parse(d.src); err == nil {
		d.root = reparsed.root
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// courseExtent returns the 1-based inclusive line range covering one course
// entry, plus the indentation of its "- " bullet. Same trailing-trim rule as
// dateExtent: blank lines and comments after the entry introduce whatever
// follows and belong to it, not to this course.
func (d *Doc) courseExtent(courseID string) (start, end int, indent string, ok bool) {
	courses := mapValue(d.root, "courses")
	if courses == nil {
		return 0, 0, "", false
	}
	var entry, next *yaml.Node
	for ci, cn := range courses.Content {
		if scalar(cn, "id") != courseID {
			continue
		}
		entry = cn
		if ci+1 < len(courses.Content) {
			next = courses.Content[ci+1]
		}
	}
	if entry == nil {
		return 0, 0, "", false
	}
	src := d.lines()
	start = entry.Line
	if next != nil {
		end = next.Line - 1
	} else {
		end = len(src)
	}
	for end > start {
		l := strings.TrimSpace(src[end-1])
		if l == "" || strings.HasPrefix(l, "#") {
			end--
			continue
		}
		break
	}
	indent = strings.Repeat(" ", maxInt(entry.Column-3, 0))
	return start, end, indent, true
}
