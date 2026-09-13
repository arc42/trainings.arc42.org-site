package web

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// filterFixtureYAML is the shared fixture widened enough to tell the filters
// apart: two courses, three formats, both languages, two cities and one online
// run with no city — and one date that is already over.
//
// Dates are chosen against testToday (2025-12-01), not against the wall clock:
// msa-past is the only one that has ended. It is also the only date in
// Hamburg, which is what proves the option lists are built from every row and
// not just from the visible ones.
const filterFixtureYAML = `courses:
  - id: msa
    short_title: "MSA"
    title: "Mastering Software Architectures"
    url: "https://example.org/msa"
    trainers: ["Dr. Gernot Starke"]
    dates:
      - id: msa-past
        code: "25-03 MSA"
        start: "2025-03-02"
        end: "2025-03-05"
        city: "Hamburg"
        country: "DE"
        language: de
        format: public
        url: "https://example.org/msa-past"
        status: open
      - id: msa-online
        code: "26-02 MSA-EN"
        start: "2026-02-10"
        end: "2026-02-13"
        country: "DE"
        language: en
        format: online
        url: "https://example.org/msa-online"
        status: open
      - id: msa-ffm
        code: "26-05 MSA"
        start: "2026-05-04"
        end: "2026-05-07"
        city: "Frankfurt/Main"
        country: "DE"
        language: de
        format: public
        url: "https://example.org/msa-ffm"
        status: open
  - id: improve
    short_title: "IMPROVE"
    title: "Improving Software Architectures"
    url: "https://example.org/improve"
    trainers: ["Dr. Carola Lilienthal"]
    dates:
      - id: improve-muc
        code: "26-03 IMPR"
        start: "2026-03-09"
        end: "2026-03-10"
        city: "München"
        country: "DE"
        language: de
        format: inhouse
        url: "https://example.org/improve-muc"
        status: open
`

// Every booking code in filterFixtureYAML. A test names the ones it expects;
// the rest are asserted absent, because a filter test that only counts rows
// passes just as happily on the wrong ones.
var filterFixtureCodes = []string{"25-03 MSA", "26-02 MSA-EN", "26-05 MSA", "26-03 IMPR"}

// filteredList fetches the list at target and returns the rendered page.
func filteredList(t *testing.T, s *Server, target string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, body:\n%s", target, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// rowCodes reports which booking codes are in the table, in the order the page
// prints them — so the want lists double as an assertion that filtering has
// not disturbed the start-date sort. It matches the row link rather than the
// bare string, so a code appearing in a filter option or a message can never
// be mistaken for a row.
func rowCodes(body string) []string {
	type hit struct {
		at   int
		code string
	}
	var hits []hit
	for _, code := range filterFixtureCodes {
		if at := strings.Index(body, ">"+code+"</a>"); at >= 0 {
			hits = append(hits, hit{at, code})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].at < hits[j].at })
	var got []string
	for _, h := range hits {
		got = append(got, h.code)
	}
	return got
}

func assertCodes(t *testing.T, body, target string, want []string) {
	t.Helper()
	got := rowCodes(body)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("GET %s: rows = %v, want %v", target, got, want)
	}
}

// TestListFiltersByQueryString is the core of the feature: which rows each
// query string keeps. Codes, not counts — the table would also be "3 rows"
// with the wrong three in it.
func TestListFiltersByQueryString(t *testing.T) {
	gh, _ := fakeGitHubWith(t, filterFixtureYAML)
	defer gh.Close()
	s := testServer(t, gh.URL)

	// Rows come out sorted by start date, so every want list is in that order.
	cases := []struct {
		name   string
		target string
		want   []string
		note   bool // the "not a value this filter offers" sentence is expected
	}{
		{
			// The default is already a filtered view. That is the whole reason
			// the count below says "of 4".
			name:   "default hides what is over",
			target: "/",
			want:   []string{"26-02 MSA-EN", "26-03 IMPR", "26-05 MSA"},
		},
		{name: "course", target: "/?course=improve", want: []string{"26-03 IMPR"}},
		{name: "format", target: "/?format=online", want: []string{"26-02 MSA-EN"}},
		{name: "language", target: "/?lang=en", want: []string{"26-02 MSA-EN"}},
		{name: "city", target: "/?where=Frankfurt%2FMain", want: []string{"26-05 MSA"}},
		{
			// The online run has no city, and is reachable through the same
			// control as the cities — that is the point of Row.Location.
			name:   "where=online reaches the date with no city",
			target: "/?where=online",
			want:   []string{"26-02 MSA-EN"},
		},
		{
			name:   "past=show adds the finished run, and nothing else",
			target: "/?past=show",
			want:   []string{"25-03 MSA", "26-02 MSA-EN", "26-03 IMPR", "26-05 MSA"},
		},
		{
			name:   "course and past combined",
			target: "/?course=msa&past=show",
			want:   []string{"25-03 MSA", "26-02 MSA-EN", "26-05 MSA"},
		},
		{
			// msa-past is public and German too, and stays hidden: the default
			// past filter still applies underneath the two explicit ones.
			name:   "format and language combined",
			target: "/?format=public&lang=de",
			want:   []string{"26-05 MSA"},
		},
		{
			// Hamburg exists only on the finished date, so the option is
			// offered (see TestFilterOptionsAreDerivedFromTheDraft) and
			// selecting it alone legitimately shows nothing.
			name:   "a city only a past date used",
			target: "/?where=Hamburg",
			want:   nil,
		},
		{
			name:   "two filters that contradict each other",
			target: "/?format=online&where=Frankfurt%2FMain",
			want:   nil,
		},
		{
			name:   "unknown format",
			target: "/?format=onlien",
			want:   nil,
			note:   true,
		},
		{
			name:   "unknown course does not fall back to everything",
			target: "/?course=no-such-course",
			want:   nil,
			note:   true,
		},
		{
			// An old bookmark from before the parameter had a value.
			name:   "unknown value for the past filter",
			target: "/?past=1",
			want:   nil,
			note:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := filteredList(t, s, tc.target)
			assertCodes(t, body, tc.target, tc.want)

			// The count is part of the answer: a filtered table that does not
			// say what it is leaving out is read as the whole file.
			wantCount := "Showing " + strconv.Itoa(len(tc.want)) + " of 4 dates"
			if !strings.Contains(body, wantCount) {
				t.Errorf("GET %s: missing %q", tc.target, wantCount)
			}

			hasNote := strings.Contains(body, "is not a value the")
			if hasNote != tc.note {
				t.Errorf("GET %s: unrecognised-value note present = %v, want %v",
					tc.target, hasNote, tc.note)
			}
			if len(tc.want) == 0 && !strings.Contains(body, "No date matches these filters") {
				t.Errorf("GET %s: an empty result got no explanation:\n%s", tc.target, body)
			}
		})
	}
}

// An unrecognised value must not 500, and must not quietly behave as if the
// parameter had not been there — which would serve the unfiltered table under
// a filtered URL. It stays selected in the control, matches nothing, and the
// page names it.
func TestUnknownFilterValueIsShownRatherThanDropped(t *testing.T) {
	gh, _ := fakeGitHubWith(t, filterFixtureYAML)
	defer gh.Close()
	s := testServer(t, gh.URL)

	body := filteredList(t, s, "/?format=onlien")

	if !strings.Contains(body, `<option value="onlien" selected>onlien — not a known value</option>`) {
		t.Errorf("the unrecognised value is not echoed back into its control:\n%s", body)
	}
	if strings.Contains(body, `<option value="" selected>Any format</option>`) {
		t.Error(`the format control claims "Any format" while showing an empty table`)
	}
	if !strings.Contains(body, "“onlien” is not a value the format filter offers") {
		t.Errorf("the page does not say which value it did not recognise:\n%s", body)
	}
}

// The bar is built from the draft, so a course or a city that exists only in
// the file has to appear without anyone editing Go — and one that exists
// nowhere must not appear at all.
func TestFilterOptionsAreDerivedFromTheDraft(t *testing.T) {
	extra := strings.Replace(filterFixtureYAML, `  - id: improve`, `  - id: flex
    short_title: "FLEX"
    title: "Flexible Architectures"
    url: "https://example.org/flex"
    trainers: ["Dr. Gernot Starke"]
    dates:
      - id: flex-bruges
        code: "27-01 FLEX"
        start: "2027-01-11"
        end: "2027-01-12"
        city: "Bruges"
        country: "BE"
        language: en
        format: public
        url: "https://example.org/flex-bruges"
        status: open
  - id: improve`, 1)
	if extra == filterFixtureYAML {
		t.Fatal("fixture edit did not apply — the anchor moved")
	}

	gh, _ := fakeGitHubWith(t, extra)
	defer gh.Close()
	s := testServer(t, gh.URL)
	body := filteredList(t, s, "/")

	// The new course and the new city, neither of which this app knows about
	// anywhere else. Bruges sorts before Frankfurt/Main, which also pins that
	// the list is ordered rather than map-random.
	for _, want := range []string{
		`<option value="flex">FLEX</option>`,
		`<option value="Bruges">Bruges</option>`,
		`<option value="improve">IMPROVE</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("derived option missing: %s\n%s", want, body)
		}
	}
	if strings.Index(body, `value="Bruges"`) > strings.Index(body, `value="Frankfurt/Main"`) {
		t.Error("the city options are not in label order")
	}
	// Nothing in the fixture is a waitlist date or an adoc course, so neither
	// may be offered: a filter that can only ever produce an empty table is
	// worse than a missing one.
	for _, unwanted := range []string{`value="adoc"`, `value="req4arc"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("an option was offered that no date uses: %s", unwanted)
		}
	}
}

// The one thing that must survive a reload: what the operator selected. The
// server, not the browser, is what puts it back.
func TestFilterFormKeepsTheCurrentSelection(t *testing.T) {
	gh, _ := fakeGitHubWith(t, filterFixtureYAML)
	defer gh.Close()
	s := testServer(t, gh.URL)

	body := filteredList(t, s, "/?course=msa&format=online&lang=en&past=show")

	for _, want := range []string{
		`<option value="msa" selected>MSA</option>`,
		`<option value="online" selected>online</option>`,
		`<option value="en" selected>EN</option>`,
		`<option value="show" selected>Past dates too</option>`,
		// Untouched controls stay at their "no restriction" entry.
		`<option value="" selected>Anywhere</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("selection not preserved: %s\n%s", want, body)
		}
	}
}

// Server-side filtering over a real GET form is what makes the screen work
// with scripting switched off — and htmx is an enhancement layered on top,
// never the thing doing the work. Both halves are pinned: the form has to be a
// real form, and the query string it produces has to filter on its own.
func TestTheFilterFormWorksWithoutScripting(t *testing.T) {
	gh, _ := fakeGitHubWith(t, filterFixtureYAML)
	defer gh.Close()
	s := testServer(t, gh.URL)
	body := filteredList(t, s, "/")

	if !strings.Contains(body, `<form class="filters" method="get" action="/"`) {
		t.Errorf("the filter bar is not a GET form:\n%s", body)
	}
	if !strings.Contains(body, `<button type="submit">Apply</button>`) {
		t.Error("the filter bar has no submit button, so it cannot be used without htmx")
	}
	// Every control is a named field with a label bound to it by id — without
	// which the select is an unlabelled box to a screen reader.
	for _, name := range []string{"course", "format", "lang", "where", "past"} {
		if !strings.Contains(body, `<label for="filter-`+name+`">`) {
			t.Errorf("no <label for> for the %s control", name)
		}
		if !strings.Contains(body, `<select id="filter-`+name+`" name="`+name+`">`) {
			t.Errorf("the %s control is not a named select bound to its label", name)
		}
	}
	// The htmx attributes enhance that same form rather than replacing it, and
	// the count is swapped in place so its live region survives — replacing the
	// announcer along with the announcement announces nothing.
	for _, want := range []string{
		`hx-get="/"`, `hx-trigger="change, submit"`, `hx-target="#dates"`,
		`hx-select-oob="#date-count:innerHTML,#filter-clear:outerHTML"`,
		`<p class="count" id="date-count" role="status">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("htmx enhancement missing: %s", want)
		}
	}

	// And the same URL that form would produce filters on its own, with no
	// htmx header anywhere in the request.
	assertCodes(t, filteredList(t, s, "/?course=improve"), "/?course=improve",
		[]string{"26-03 IMPR"})
}

// Past dates are hidden by default but keep their marking when asked for: the
// muted row is how an operator tells a finished run from an upcoming one at a
// glance once both are on screen.
func TestPastDatesAreHiddenByDefaultAndStillMarkedWhenShown(t *testing.T) {
	gh, _ := fakeGitHubWith(t, filterFixtureYAML)
	defer gh.Close()
	s := testServer(t, gh.URL)

	if strings.Contains(filteredList(t, s, "/"), `<tr class="past">`) {
		t.Error("a past row is rendered in the default view")
	}
	shown := filteredList(t, s, "/?past=show")
	if !strings.Contains(shown, `<tr class="past">`) {
		t.Errorf("the past row lost its marking under past=show:\n%s", shown)
	}
	if strings.Count(shown, `<tr class="past">`) != 1 {
		t.Errorf("wrong number of rows marked past: %d", strings.Count(shown, `<tr class="past">`))
	}
}

// "Clear filters" exists to get back to the default view, so it only makes
// sense while something is in force — including past=show, which is a
// departure from the default even though it widens rather than narrows.
func TestClearFiltersAppearsOnlyWhileAFilterIsInForce(t *testing.T) {
	gh, _ := fakeGitHubWith(t, filterFixtureYAML)
	defer gh.Close()
	s := testServer(t, gh.URL)

	cases := map[string]bool{
		"/":                 false,
		"/?course=":         false, // what the form itself submits when nothing is chosen
		"/?course=msa":      true,
		"/?past=show":       true,
		"/?format=nonsense": true,
	}
	for target, want := range cases {
		body := filteredList(t, s, target)
		got := strings.Contains(body, `<a href="/">Clear filters</a>`)
		if got != want {
			t.Errorf("GET %s: clear link present = %v, want %v", target, got, want)
		}
		// The element itself is always there: the out-of-band swap needs
		// something with that id in every response.
		if !strings.Contains(body, `id="filter-clear"`) {
			t.Errorf("GET %s: the clear slot is missing entirely", target)
		}
	}
}

// A file with no dates at all is a different message from a filter that
// matched none, and the empty-state has to tell them apart — "No dates yet"
// under an active filter would send somebody looking for a lost file.
func TestEmptyListAndEmptyResultSayDifferentThings(t *testing.T) {
	empty := `courses:
  - id: msa
    short_title: "MSA"
    title: "Mastering Software Architectures"
    url: "https://example.org/msa"
    trainers: ["Dr. Gernot Starke"]
    dates: []
`
	gh, _ := fakeGitHubWith(t, empty)
	defer gh.Close()
	s := testServer(t, gh.URL)

	body := filteredList(t, s, "/")
	if !strings.Contains(body, "No dates yet.") {
		t.Errorf("an empty file does not say so:\n%s", body)
	}
	if !strings.Contains(body, "Showing 0 of 0 dates") {
		t.Error("the count is missing on an empty file")
	}

	gh2, _ := fakeGitHubWith(t, filterFixtureYAML)
	defer gh2.Close()
	s2 := testServer(t, gh2.URL)
	if !strings.Contains(filteredList(t, s2, "/?where=Hamburg"), "No date matches these filters") {
		t.Error("an empty result is not distinguished from an empty file")
	}
}
