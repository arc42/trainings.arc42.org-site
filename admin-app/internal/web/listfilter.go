package web

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"arc42-trainings-admin/internal/model"
)

// The dates list is a filtered view, and everything about that filtering lives
// here: which controls exist, where their options come from, and which rows a
// given query string keeps.
//
// The state is the query string and the narrowing happens in the handler. That
// is a decision, not a stage on the way to something cleverer: a filtered view
// can be bookmarked and pasted into a mail, it survives a reload and the back
// button, it works with scripting switched off — and it can be asserted on in
// a Go test, which rows hidden one by one in the browser cannot. htmx only
// saves the round trip; it is never what makes the filtering work.

// The parameter names are short and named after the column they narrow, so a
// URL stays readable where someone pastes it: /?course=msa&format=online&lang=en.
// "past" is the odd one out — it narrows no column, and it is the only filter
// that is on without anybody asking for it.
const (
	paramCourse = "course"
	paramFormat = "format"
	paramLang   = "lang"
	paramWhere  = "where"
	paramPast   = "past"

	// pastShow is the one recognised value of the past filter. A value, not a
	// bare presence check, so an old bookmark carrying ?past=1 is reported as
	// unrecognised instead of quietly doing something.
	pastShow = "show"
)

// listRow is one row of the table: the flat model row plus whether the date is
// already over. Past drives two different things — whether the row is shown at
// all, and the muted styling it gets when it is — so it is computed once, from
// the server's clock, and not re-derived in the template.
type listRow struct {
	model.Row
	Past bool
}

// filterOption is one entry of a filter's <select>.
type filterOption struct{ Value, Label string }

// filterControl is one <select> in the bar. It carries everything the template
// needs and nothing it has to work out: the options, the current selection
// echoed back so that a reload does not silently reset it, and whether that
// selection is a value this list knows.
type filterControl struct {
	Name    string // the query parameter, and the control's name=
	Label   string // the <label> text
	Options []filterOption
	Value   string // the current selection, "" for "no restriction"
	Unknown bool   // Value is not among Options — see deriveFilters
}

// listFilters is the whole bar, plus the verdict on the query string it was
// built from.
type listFilters struct {
	Controls []filterControl
	// Active reports whether anything beyond the default view is in force,
	// which is what the "Clear filters" link keys off. past=show counts:
	// clearing returns to the default, and the default hides past dates.
	Active bool
	// UnknownNote is the sentence shown above the table when a parameter
	// carried a value this list does not offer. Empty when every value was
	// recognised.
	UnknownNote string
}

// deriveFilters builds the filter bar from the rows currently in the draft and
// reads the selection out of the query string.
//
// The option lists are DERIVED, never declared. A new course, a new city or a
// format nobody has used yet has to appear here without anyone editing Go —
// which also rules out reading them from the mirrored course lists in
// internal/model, since those are a copy of the site's templates and are
// allowed to go stale. The other half of the same rule: a derived list never
// offers a value that matches nothing, and an option that can only ever
// produce an empty table is a worse lie than a missing one.
func deriveFilters(rows []model.Row, q url.Values) listFilters {
	courses := map[string]string{}
	formats := map[string]string{}
	langs := map[string]string{}
	cities := map[string]string{}
	online := false
	for _, r := range rows {
		if r.CourseID != "" {
			label := r.CourseShortTitle
			if label == "" {
				label = r.CourseID
			}
			courses[r.CourseID] = label
		}
		if f := r.Date.Format; f != "" {
			formats[f] = f
		}
		if l := r.Date.Language; l != "" {
			// Uppercase in the label only. The value stays exactly what the
			// file stores, because that is what the row is compared against.
			langs[l] = strings.ToUpper(l)
		}
		switch w := r.Location(); w {
		case "":
			// A date that is neither online nor has a city. The schema forbids
			// it, so this is broken data rather than a case to support: it
			// gets no option of its own (an empty value is already taken by
			// "Anywhere") and stays visible in the unfiltered list, which is
			// where somebody will notice it.
		case model.LocationOnline:
			online = true
		default:
			cities[w] = w
		}
	}

	// Sorted by label throughout, so the bar looks the same on every reload
	// and a new city lands where the eye expects it. "Online" is pinned to the
	// top of the Where list instead of sorting into the cities: it is not one.
	where := []filterOption{}
	if online {
		where = append(where, filterOption{model.LocationOnline, "Online — no city"})
	}
	where = append(where, sortedOptions(cities)...)

	f := listFilters{Controls: []filterControl{
		{Name: paramCourse, Label: "Course", Options: withAny("Any course", sortedOptions(courses))},
		{Name: paramFormat, Label: "Format", Options: withAny("Any format", sortedOptions(formats))},
		{Name: paramLang, Label: "Language", Options: withAny("Any language", sortedOptions(langs))},
		// One control, not two. The schema makes a city required unless the
		// format is online, so "where is this run" has exactly one answer per
		// date and the Where column already prints it that way; splitting it
		// into a city list plus a separate "online" checkbox would invite the
		// combination that means nothing.
		{Name: paramWhere, Label: "Where", Options: withAny("Anywhere", where)},
		// Not "Any …": the empty value here is a real restriction, and the
		// label has to say so or the bar claims to be showing everything.
		{Name: paramPast, Label: "Past dates", Options: []filterOption{
			{"", "Upcoming only"}, {pastShow, "Past dates too"},
		}},
	}}

	var unrecognised []string
	for i := range f.Controls {
		c := &f.Controls[i]
		c.Value = strings.TrimSpace(q.Get(c.Name))
		if c.Value == "" {
			continue
		}
		f.Active = true
		if !hasOption(c.Options, c.Value) {
			// An unrecognised value is never dropped. Dropping it would serve
			// the unfiltered table under a filtered URL, which is the one
			// outcome a filter must not have: this table is read as the whole
			// truth about the dates. So the value stays selected, matches
			// nothing, and the page says which value it was.
			c.Unknown = true
			unrecognised = append(unrecognised,
				fmt.Sprintf("“%s” is not a value the %s filter offers", c.Value, strings.ToLower(c.Label)))
		}
	}
	if len(unrecognised) > 0 {
		f.UnknownNote = strings.Join(unrecognised, "; ") + " — so nothing is shown."
	}
	return f
}

// withAny puts the "no restriction" entry at the head of a list.
func withAny(label string, opts []filterOption) []filterOption {
	return append([]filterOption{{"", label}}, opts...)
}

// sortedOptions turns a value→label map into options ordered by label, with
// the value breaking ties so the order is total and therefore stable.
func sortedOptions(m map[string]string) []filterOption {
	opts := make([]filterOption, 0, len(m))
	for v, label := range m {
		opts = append(opts, filterOption{Value: v, Label: label})
	}
	sort.Slice(opts, func(i, j int) bool {
		if opts[i].Label != opts[j].Label {
			return opts[i].Label < opts[j].Label
		}
		return opts[i].Value < opts[j].Value
	})
	return opts
}

func hasOption(opts []filterOption, value string) bool {
	for _, o := range opts {
		if o.Value == value {
			return true
		}
	}
	return false
}

// apply returns the rows the current selection keeps, in the order they came.
func (f listFilters) apply(rows []listRow) []listRow {
	kept := make([]listRow, 0, len(rows))
	for _, r := range rows {
		if f.keeps(r) {
			kept = append(kept, r)
		}
	}
	return kept
}

func (f listFilters) keeps(r listRow) bool {
	for _, c := range f.Controls {
		switch {
		case c.Unknown:
			// See deriveFilters: a value we do not understand narrows to
			// nothing rather than widening to everything.
			return false
		case c.Value == "":
			continue
		case c.Name == paramPast:
			continue // its only value widens, and is applied below
		case c.Value != rowValue(c.Name, r.Row):
			return false
		}
	}
	return !r.Past || f.showPast()
}

// showPast reports whether dates that are already over are included.
func (f listFilters) showPast() bool {
	for _, c := range f.Controls {
		if c.Name == paramPast {
			return c.Value == pastShow
		}
	}
	return false
}

// rowValue is the value one control compares a row against. Each case reads
// exactly what the matching table column shows, so a filter can never disagree
// with the row the operator is looking at.
func rowValue(param string, r model.Row) string {
	switch param {
	case paramCourse:
		return r.CourseID
	case paramFormat:
		return r.Date.Format
	case paramLang:
		return r.Date.Language
	case paramWhere:
		return r.Location()
	}
	return ""
}
