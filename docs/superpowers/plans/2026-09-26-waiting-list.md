# Waiting List and Registration Price Box Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `waitlist` dates joinable through the normal form, make `full` dates stop promising a waiting list, show only the regular price in the form's booking summary, and give the admin app words for the four statuses.

**Architecture:** Status wording is decided once, in `_includes/timeline_auto.html`, and handed to the eight card templates as finished strings (the pattern price and credits already use). The registration form widens its "bookable" filter from `open` to `open` or `waitlist`. The admin app gets a label map next to `model.Statuses` and rewrites its status warnings and PR-title phrases.

**Tech Stack:** Jekyll/Liquid (site, verified by `make site` + grep of `_site/`), Go 1.23 standard library (admin app, `make app-check`).

**Spec:** `docs/superpowers/specs/2026-09-25-registration-service-design.md`, sections 2 ("No early bird") and 8 ("Waiting list"). This plan is rollout stage 1 (section 7.2) and does not depend on the registration service.

## Global Constraints

- No em-dashes in any visible copy (house rule; `—` is fine inside Liquid comments only if the file already uses them, but new copy uses commas or colons).
- DE card wording: `Ausgebucht, Warteliste verfügbar` (waitlist), `Ausgebucht` (full), button `Auf die Warteliste`.
- EN card wording: `Fully booked, waiting list available` (waitlist), `Fully booked` (full), button `Join waiting list`.
- Form label suffix for waitlist dates: DE ` (Warteliste)`, EN ` (waiting list)`.
- The `<option value>` stays the booking code byte-for-byte (CLAUDE.md contract). Suffixes go in the label only.
- `_pages/home.html` and `_pages/home-de.html` are not touched; all card changes flow through the includes.
- Every new variable assigned inside an include that other includes call is prefixed (CLAUDE.md: includes share the caller's scope). This plan uses the prefix `ta_` in `timeline_auto.html`.
- No price in the form summary except the regular amount via `_includes/money.html`. No early bird, no alumni clause.
- `_data/trainings.yml` is never committed with a test status. Verification edits to it are reverted with `git checkout -- _data/trainings.yml` in the same task.

## Review Focus

- A `waitlist` date whose `seats_limited` is also set: the admin app must warn (it contradicts itself on the card), and the card must still show the waiting-list line and button. Pinned in Task 2.
- A date flipped from `waitlist` back to `open`: the card must show the normal "Anmeldung"/"Register" button again, with no leftover waiting-list label from a previous card in the same loop (Liquid include scope leaks). Pinned in Task 4, step 5 (two adjacent dates with different statuses).
- A `full` date that was preselected via `?kurs=` in an old link: it is no longer in the form, so preselection must silently fall back to the empty option (existing behaviour, re-checked in Task 5).
- The English form listing a German-held waitlist date: both suffixes must appear in order, `(held in German) (waiting list)`, never a doubled or missing one. Pinned in Task 5.
- A date with an early-bird price: the booking summary must show only the regular amount (`2.890 €` / `€2,890`), no "Frühbucher"/"Early bird" text. Pinned in Task 5.

---

### Task 1: Admin app, status labels and hint

**Files:**
- Modify: `admin-app/internal/model/model.go` (next to `Statuses`, around line 143-148)
- Modify: `admin-app/internal/web/server.go` (`templateFuncs`, around line 206)
- Modify: `admin-app/internal/web/templates/dateform.gohtml:88-97`
- Test: `admin-app/internal/model/model_test.go` (create if absent; otherwise append), `admin-app/internal/web/handlers_dates_test.go`

**Interfaces:**
- Produces: `model.StatusLabel(status string) string`; template func `statusLabel`.

- [ ] **Step 1: Write the failing tests**

Append to `admin-app/internal/model/model_test.go` (create the file with `package model` and `import "testing"` if it does not exist):

```go
// Every status the form offers needs words. A status without a label would
// render as a bare token in the dropdown, which is exactly what this replaces.
func TestEveryStatusHasALabel(t *testing.T) {
	for _, s := range Statuses {
		if StatusLabel(s) == s || StatusLabel(s) == "" {
			t.Errorf("status %q has no label", s)
		}
	}
	if got := StatusLabel("waitlist"); got != "fully booked, waiting list available" {
		t.Errorf("waitlist label = %q", got)
	}
	if got := StatusLabel("bogus"); got != "bogus" {
		t.Errorf("unknown status must fall back to itself, got %q", got)
	}
}
```

Append to `admin-app/internal/web/handlers_dates_test.go`:

```go
// The dropdown used to show four bare tokens and a hint that still talked
// about arc42.de's retired form. It must say what each status does.
func TestStatusDropdownExplainsEachStatus(t *testing.T) {
	s := testServer(t, "http://127.0.0.1:1")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, signedIn(t, s, http.MethodGet, "/dates/new", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`<option value="waitlist"`,
		`waitlist: fully booked, waiting list available</option>`,
		`full: fully booked, no waiting list</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("status dropdown lacks %q", want)
		}
	}
	if strings.Contains(body, "registration form on arc42.de") {
		t.Error("status hint still refers to arc42.de's retired form")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd admin-app && go test ./internal/model ./internal/web -run 'TestEveryStatusHasALabel|TestStatusDropdownExplainsEachStatus' -v`
Expected: FAIL, `undefined: StatusLabel`.

- [ ] **Step 3: Implement**

In `admin-app/internal/model/model.go`, directly below the `var ( Formats ... Statuses ... )` block:

```go
// statusLabels says what a status does on the site, in the words the
// operator thinks in. The card and form wording (DE/EN) lives in the Liquid
// includes; this is only the admin app's own explanation.
var statusLabels = map[string]string{
	"open":      "open for registration",
	"waitlist":  "fully booked, waiting list available",
	"full":      "fully booked, no waiting list",
	"cancelled": "cancelled, hidden everywhere",
}

// StatusLabel returns the explanation for a status, or the status itself if
// there is none, so an unexpected value still renders as something.
func StatusLabel(status string) string {
	if l, ok := statusLabels[status]; ok {
		return l
	}
	return status
}
```

In `admin-app/internal/web/server.go`, inside `templateFuncs()`'s map, add:

```go
		// statusLabel explains a status in the dropdown and the list badges.
		"statusLabel": func(s any) string { return model.StatusLabel(fmt.Sprint(s)) },
```

In `admin-app/internal/web/templates/dateform.gohtml`, replace the status `<select>` options and hint (lines 88-97) with:

```gohtml
      <label>Booking status <span class="req" aria-hidden="true">*</span>
        <select name="status" required>
          {{$s := .Date.Status}}
          {{range .Statuses}}<option value="{{.}}" {{if eqStr . $s}}selected{{end}}>{{.}}: {{statusLabel .}}</option>{{end}}
        </select>
        <small id="status-hint" class="hint"><b>open</b> and <b>waitlist</b>
          appear in the registration form on trainings.arc42.org; a waitlist
          card says "fully booked, waiting list available" and its button
          joins the waiting list. <b>full</b> stays on the timeline as fully
          booked, with no button and no form entry. <b>cancelled</b>
          disappears everywhere.</small>
      </label>
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd admin-app && go test ./internal/model ./internal/web -v -run 'TestEveryStatusHasALabel|TestStatusDropdownExplainsEachStatus|TestDateForm'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add admin-app/internal/model admin-app/internal/web
git commit -m "feat(admin): explain each booking status in the date form"
```

---

### Task 2: Admin app, status warnings

**Files:**
- Modify: `admin-app/internal/validate/warnings.go:71-79`
- Test: `admin-app/internal/validate/warnings_test.go:37` and new cases

**Interfaces:**
- Consumes: nothing new. `DateWarnings(d model.Date, courseID, today string, isNew bool) []Warning` keeps its signature.

- [ ] **Step 1: Write the failing tests**

In `warnings_test.go`, replace the case on line 37

```go
		{"a status that hides the date from booking", func(d *model.Date) { d.Status = "waitlist" }, "status"},
```

with

```go
		{"a full date cannot be registered for", func(d *model.Date) { d.Status = "full" }, "status"},
		{"a waitlist date is still registrable and warns about nothing", func(d *model.Date) { d.Status = "waitlist" }, ""},
		{"few seats on a waitlist date contradicts itself", func(d *model.Date) { d.Status, d.SeatsLimited = "waitlist", true }, "seats_limited"},
```

and add below the table test:

```go
// The old message named arc42.de's form, which no longer takes registrations.
func TestStatusWarningNamesTheRightSite(t *testing.T) {
	d := model.Date{ID: "msa-feb-2027-en", Code: "27-02 MSA-EN", Start: "2027-02-23",
		End: "2027-02-25", Language: "en", Format: "online", Status: "full"}
	for _, w := range DateWarnings(d, "msa", "2026-08-16", false) {
		if w.Field == "status" && strings.Contains(w.Message, "arc42.de") {
			t.Errorf("status warning still mentions arc42.de: %q", w.Message)
		}
	}
}
```

Check that the test at line 130 (`Status: "waitlist"` on a past date) still states what it means after this change: it asserts on its own field, read it and adjust only if it asserted a `status` warning.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd admin-app && go test ./internal/validate -v`
Expected: FAIL on "a waitlist date is still registrable", "few seats on a waitlist date", and the arc42.de test.

- [ ] **Step 3: Implement**

Replace lines 71-79 of `warnings.go` with:

```go
	// The rule that caused the 23-25 February 2027 date to be published
	// unbookable. Only open and waitlist dates are listed in the registration
	// form on trainings.arc42.org; waitlist registrations go through the same
	// form and the back office tells them it is a waiting-list place.
	switch d.Status {
	case "full":
		add("status", "%q keeps this date out of the registration form on trainings.arc42.org, and its card has no button; nobody can register or join a waiting list", d.Status)
	case "cancelled":
		add("status", "%q hides this date everywhere; nobody can register", d.Status)
	}
	if d.SeatsLimited && (d.Status == "waitlist" || d.Status == "full" || d.Status == "cancelled") {
		add("seats_limited", "seats are advertised on a date whose status is %q", d.Status)
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd admin-app && go test ./... && go vet ./...`
Expected: PASS. If a web test posts `status=full` with `confirm_warnings=1` it keeps passing; if one posts `status=waitlist` expecting a warning, change its comment and expectation to match the new rule.

- [ ] **Step 5: Commit**

```bash
git add admin-app/internal/validate
git commit -m "feat(admin): waitlist dates are registrable, warn only on full and cancelled"
```

---

### Task 3: Admin app, PR titles say what a status change means

**Files:**
- Modify: `admin-app/internal/web/handlers_propose.go` (`phrase`, the `case "status":` branch)
- Modify: `admin-app/internal/web/handlers_propose_test.go:196-197`
- Modify: `admin-app/README.md:88` (the example `status full`)

- [ ] **Step 1: Write the failing test**

In `handlers_propose_test.go`, replace

```go
		{"a status change carries the new status",
			[]Change{statusChange("full")}, "MSA 26-09 MSA-EN: status full"},
```

with

```go
		{"a status change says what it means",
			[]Change{statusChange("full")}, "MSA 26-09 MSA-EN: fully booked"},
		{"waitlist says there is a waiting list",
			[]Change{statusChange("waitlist")}, "MSA 26-09 MSA-EN: fully booked, waiting list"},
		{"back to open",
			[]Change{statusChange("open")}, "MSA 26-09 MSA-EN: open for registration again"},
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd admin-app && go test ./internal/web -run TestPRTitleNamesTheCourse -v`
Expected: FAIL, got `status full`.

- [ ] **Step 3: Implement**

In `phrase`, replace `case "status": return "status " + f.After` with:

```go
	case "status":
		switch f.After {
		case "open":
			return "open for registration again"
		case "waitlist":
			return "fully booked, waiting list"
		case "full":
			return "fully booked"
		case "cancelled":
			return "cancelled"
		}
		return "status " + f.After
```

In `admin-app/README.md` line 88, change the example `` `status full` `` to `` `fully booked` ``.

- [ ] **Step 4: Run to verify it passes**

Run: `make app-check`
Expected: `==> tests, vet and gofmt are clean`.

- [ ] **Step 5: Commit**

```bash
git add admin-app
git commit -m "feat(admin): PR titles describe status changes in words"
```

---

### Task 4: Site, cards distinguish waitlist from full

**Files:**
- Modify: `_includes/timeline_auto.html:45-46` and the `{% include timeline_course.html ... %}` call (around line 72-85)
- Modify: `_includes/timeline_course.html:24-38` (all eight `{% include timeline_*.html ... %}` lines)
- Modify: the eight card templates `_includes/timeline_{msa,msa_online,req4arc,req4arc_online,improve,improve_online,adoc,adoc_online}.html`

**Interfaces:**
- Produces: three new include parameters on every card template: `status_note` (string or nil), `waitlist_label` (string or nil), `hide_register` (bool). `sold_out` keeps its meaning "grey the card and show the note" and is now true for both `waitlist` and `full`.

- [ ] **Step 1: Record the current output as the baseline**

Run: `make site && grep -c 'timeline-sold-out' _site/index.html _site/de/index.html`
Expected: `0` for both (no date is currently waitlist or full; if not 0, note the number).

- [ ] **Step 2: Compute the wording once in `timeline_auto.html`**

Replace lines 45-46

```liquid
  {%- assign sold_out = false -%}
  {%- if d.status == "waitlist" or d.status == "full" -%}{%- assign sold_out = true -%}{%- endif -%}
```

with

```liquid
  {%- comment -%}
    Status wording is decided HERE, once, like price and credits below, and
    handed to the card templates as finished strings. waitlist and full both
    grey the card (sold_out), but only full hides the button: a waitlist date
    is registered through the same form, and the button says so.
    ta_-prefixed because the card templates run in this include's scope and
    assign their own register_label; a bare name would be overwritten by the
    previous card and leak into the next one.
  {%- endcomment -%}
  {%- assign sold_out = false -%}
  {%- assign ta_status_note = nil -%}
  {%- assign ta_waitlist_label = nil -%}
  {%- assign ta_hide_register = false -%}
  {%- if d.status == "waitlist" -%}
    {%- assign sold_out = true -%}
    {%- if page_lang == "de" -%}
      {%- assign ta_status_note = "Ausgebucht, Warteliste verfügbar" -%}
      {%- assign ta_waitlist_label = "Auf die Warteliste" -%}
    {%- else -%}
      {%- assign ta_status_note = "Fully booked, waiting list available" -%}
      {%- assign ta_waitlist_label = "Join waiting list" -%}
    {%- endif -%}
  {%- elsif d.status == "full" -%}
    {%- assign sold_out = true -%}
    {%- assign ta_hide_register = true -%}
    {%- if page_lang == "de" -%}
      {%- assign ta_status_note = "Ausgebucht" -%}
    {%- else -%}
      {%- assign ta_status_note = "Fully booked" -%}
    {%- endif -%}
  {%- endif -%}
```

In the `{% include timeline_course.html ... %}` call further down, add three parameters after `sold_out=sold_out`:

```liquid
     status_note=ta_status_note
     waitlist_label=ta_waitlist_label
     hide_register=ta_hide_register
```

- [ ] **Step 3: Forward them in `timeline_course.html`**

On each of the eight `{% include timeline_*.html ... %}` lines (24, 26, 28, 30, 32, 34, 36, 38), insert directly after `sold_out=include.sold_out`:

```liquid
 status_note=include.status_note waitlist_label=include.waitlist_label hide_register=include.hide_register
```

(CLAUDE.md: `timeline_course.html` must forward to every template; three of six once did not forward `pricing=`. Count afterwards: `grep -c 'hide_register=include.hide_register' _includes/timeline_course.html` must print `8`.)

- [ ] **Step 4: Use them in all eight card templates**

In each of the eight files:

1. Delete the two lines `{%- assign sold_out_text = "(Ausgebucht, nur noch Warteliste)" -%}` and `{%- assign sold_out_text = "(sold out — waitlist only)" -%}`.
2. Replace

```liquid
        {% if sold_out %}
            <p class="timeline-sold-out">{{ sold_out_text }}</p>
        {% endif %}
```

with

```liquid
        {% if sold_out and include.status_note %}
            <p class="timeline-sold-out">({{ include.status_note }})</p>
        {% endif %}
```

3. Replace

```liquid
        {% unless sold_out %}
            <a class="button buttonAnmeldung" href="{{ register_url }}?kurs={{ anchor_id }}">{{ register_label }}</a>
        {% endunless %}
```

with

```liquid
        {% unless include.hide_register %}
            <a class="button buttonAnmeldung" href="{{ register_url }}?kurs={{ anchor_id }}">{{ include.waitlist_label | default: register_label }}</a>
        {% endunless %}
```

Check each file after editing: `grep -c 'sold_out_text' _includes/timeline_*.html` must print `0` for every file, and `grep -c 'include.hide_register' _includes/timeline_*.html` must print `1` for each of the eight card templates.

- [ ] **Step 5: Verify with temporary statuses, then revert them**

Pick two adjacent dates in `_data/trainings.yml` (for example the first two `msa` dates, currently `msa-online-sep-2026` and `msa-dez-2026`). Temporarily set the first to `status: waitlist` and the second to `status: full`, and a third date of another course to `status: waitlist`. Then:

```bash
make site
grep -o '<p class="timeline-sold-out">[^<]*</p>' _site/index.html _site/de/index.html
grep -c 'Join waiting list' _site/index.html
grep -c 'Auf die Warteliste' _site/de/index.html
grep -o 'href="/registration/?kurs=msa-dez-2026"' _site/index.html | wc -l
grep -c 'sold out' _site/index.html
git checkout -- _data/trainings.yml
```

Expected:
- the first grep shows `(Fully booked, waiting list available)` twice and `(Fully booked)` once on the EN page, and `(Ausgebucht, Warteliste verfügbar)` twice and `(Ausgebucht)` once on the DE page;
- `Join waiting list` and `Auf die Warteliste` each count `2`;
- the `msa-dez-2026` register link count is `0` (full has no button);
- `sold out` counts `0` (the old wording is gone);
- the card directly after the waitlist card with status `open` shows `Register`, not `Join waiting list` (look at the next `buttonAnmeldung` after the waitlist card in `_site/index.html`).

Finally `git status --short _data/` must print nothing.

- [ ] **Step 6: Commit**

```bash
git add _includes/timeline_auto.html _includes/timeline_course.html _includes/timeline_*.html
git commit -m "feat: waitlist cards keep a join button, full cards stop promising a waiting list"
```

---

### Task 5: Site, registration form lists waitlist dates and shows only the regular price

**Files:**
- Modify: `_includes/registration-form.html` (the two status filters at lines 95 and 111, the `langhint`/label capture, the `pricelabel` capture, and the comment block above the counting loop)

- [ ] **Step 1: Widen the bookable filter**

Replace both occurrences of

```liquid
    {%- if d.status != "open" -%}{%- continue -%}{%- endif -%}
```

with

```liquid
    {%- unless d.status == "open" or d.status == "waitlist" -%}{%- continue -%}{%- endunless -%}
```

and in the comment above the counting loop change `(status "open", not past)` to `(status "open" or "waitlist", not past; a waitlist registration goes through the same form and the back office tells the registrant it is a waiting-list place)`.

- [ ] **Step 2: Add the waitlist suffix to the label**

Directly after the `{%- capture langhint -%}...{%- endcapture -%}` line, add:

```liquid
      {%- capture waithint -%}{% if d.status == "waitlist" %}{% if include.lang == "de" %} (Warteliste){% else %} (waiting list){% endif %}{% endif %}{%- endcapture -%}
```

and at the end of the `<option ...>` label, change `{{ langhint }}</option>` to `{{ langhint }}{{ waithint }}</option>`. Also append it to the summary panel's location line: change the `wherelabel` capture's `{{ langhint }}` to `{{ langhint }}{{ waithint }}`, so the "Your selection" panel says it too.

- [ ] **Step 3: Regular price only in the summary**

Replace

```liquid
      {%- capture pricelabel -%}{% include price-label.html price=d.price lang=include.lang %}{%- endcapture -%}
```

with

```liquid
      {%- comment -%}
        Regular amount only. Early-bird and alumni prices, and the special
        agreements many clients have, are applied by hand when invoicing
        (spec 2026-09-25, section 2). price-label.html would state them here
        and contradict the invoice; money.html states the list price and
        nothing else. The timeline cards still advertise early bird.
      {%- endcomment -%}
      {%- capture pricelabel -%}{% if d.price.amount %}{% include money.html amount=d.price.amount currency=d.price.currency lang=include.lang %}{% endif %}{%- endcapture -%}
```

- [ ] **Step 4: Verify with temporary statuses, then revert**

Temporarily set `msa-mar-2027` (it has an early-bird price) to `status: waitlist` in `_data/trainings.yml`, and `msa-dez-2026` to `status: full`. Then:

```bash
make site
grep -o '<option value="27-03 MSA"[^>]*>[^<]*</option>' _site/registration/index.html
grep -o 'data-price="[^"]*"' _site/registration/index.html | sort -u
grep -o 'data-price="[^"]*"' _site/anmeldung/index.html | sort -u
grep -c 'value="26-12 MSA"' _site/anmeldung/index.html
grep -c -i -E 'early bird|Frühbucher|alumni' _site/registration/index.html _site/anmeldung/index.html
git checkout -- _data/trainings.yml
```

Expected:
- the `27-03 MSA` option label ends in `(held in German) (waiting list)` and its `value` is exactly `27-03 MSA`;
- every `data-price` is a bare amount: `€2,890`, `€2,100`, ... on the EN page and `2.890 €`, `2.100 €`, ... on the DE page;
- `value="26-12 MSA"` counts `0` (full is not bookable);
- the early-bird/alumni count is `0` for both files.

Then, in a browser against `make dev` (http://localhost:4260/registration/?kurs=msa-dez-2026 with `msa-dez-2026` still set to `full` before reverting): the dropdown stays on the empty option and the summary panel stays hidden. Revert with `git checkout -- _data/trainings.yml` and confirm `git status --short _data/` is empty.

- [ ] **Step 5: Run the link check**

Run: `make check-links`
Expected: passes as before.

- [ ] **Step 6: Commit**

```bash
git add _includes/registration-form.html
git commit -m "feat: waitlist dates are registrable, the booking summary shows the list price only"
```

---

### Task 6: Pull request

- [ ] **Step 1:** Push the branch and open a PR titled `Waiting list: registrable through the form; full dates say fully booked`. The description lists the five Review Focus items above and the verification commands used. Mention that no data changed: every status in `_data/trainings.yml` is still `open`, so the live site looks the same until someone sets a status in the admin app.
- [ ] **Step 2:** After merge and the admin app deploy, set one real date to `waitlist` only when the business needs it. There is no follow-up task here.
