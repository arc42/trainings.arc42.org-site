# New course types without a developer: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A trainer can create a new course type (MAS) with its dates entirely in the admin app, open a PR that CI accepts, and see it rendered on both home pages by a data-driven card.

**Architecture:** Data contract first (`blurb_de`/`blurb_en`/`show_on` in YAML, schema, feed, Ruby validator), then the Go admin app (model → yamldoc → feed shim → forms → messages), then the Liquid data-driven card plus a built-output check, then the consumer check and docs. Each task leaves `make app-check`, `ruby scripts/validate_trainings.rb` and `make site` green.

**Tech Stack:** Go 1.x (admin-app, `gopkg.in/yaml.v3` nodes, `santhosh-tekuri/jsonschema/v5`, `html/template`), Jekyll/Liquid, Ruby (validator), bash + jq (consumer check), GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-11-06-new-course-types-design.md` (read it first; section numbers below refer to it).

## Global Constraints

- ADR-0004: never change the name or type of an existing feed field. `blurb` stays an optional string in `api/trainings.json`, now generated: `blurb_en` if present, else `blurb_de`, else omitted.
- In `_data/trainings.yml` the key `blurb` is retired; `blurb_de` / `blurb_en` replace it. Existing four values move to `blurb_en` byte-for-byte (after YAML folding).
- `show_on`: optional list of site names. Absent = every consumer site. `[]` is an error. Known names, one spelling everywhere: `faq.arc42.org`, `docs.arc42.org`, `arc42.org`, `examples.arc42.org`, `www.arc42.de`, `softwareknigge.arc42.org`, `www.gernotstarke.de`, `www.systems42.org`. Unknown names are allowed (free text), but must match `^[a-z0-9.-]+$`.
- trainings.arc42.org ignores `show_on` and shows every course.
- iSAQB: a course is accredited iff `certification` is non-empty. No flag.
- Operator-facing text (admin app): no file paths, no JSON pointers, no Liquid, no YAML key names. Course by short title, date by start date + city/"online", field by its form label.
- Liquid: every variable an include assigns is prefixed (`tc_` in the new card) — includes share the caller's scope (CLAUDE.md).
- No Liquid change is "done" until `make site` and a grep of `_site/` prove it.
- Hand-made card list in the admin app: `msa`, `improve`, `req4arc`, `adoc`.
- Commits end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Work on branch `feature/new-course-types`.

## Review Focus

1. **A course with no dates anywhere in the pipeline** (feed template, consumer-check jq, card loop): `dates: []` must render valid JSON and no card, never a Liquid or jq error. Pinned in Task 2 (feed build) and Task 8 (fixture).
2. **A description containing quotes, colons, umlauts or a line break** typed into the form: must round-trip through yamldoc render → parse unchanged and render escaped on the card. Pinned in Task 3.
3. **Editing an existing course that still has the legacy `blurb` in someone's open draft / an old branch**: parse must not silently drop it. Pinned in Task 3 (parse reads legacy `blurb` into `BlurbEN`, render writes `blurb_en`).
4. **A schema problem the operator cannot fix blocks the PR**: app-fault problems must not hide the *Open pull request* form (CI remains authoritative). Pinned in Task 5.
5. **`show_on` with every box unticked** must mean "all sites" (key omitted), not `[]`. Pinned in Task 4.

---

### Task 1: The feed shim never writes `null` for a required list, and the app accepts what CI accepts

**Files:**
- Modify: `admin-app/internal/validate/schema.go` (`FeedJSON`)
- Create: `admin-app/internal/validate/testdata/ci_accepts/*.yml` (one file per case below)
- Create: `admin-app/internal/validate/agreement_test.go`
- Modify: `.github/workflows/validate-trainings.yml` (run the Ruby validator over the same fixtures)

**Interfaces:**
- Produces: fixture directory `admin-app/internal/validate/testdata/ci_accepts/`; every file in it must pass both `ruby scripts/validate_trainings.rb <file>` and the app's `Rules` + `Schema(FeedJSON(...))`. Later tasks add fixtures here.

- [ ] **Step 1: Write fixtures** (each a complete, minimal `courses:` file built from `testdata/valid.yml`, with **no `blurb` key**, which Task 2 retires): `course_without_dates.yml` (a second course with `dates: []`), `course_minimal.yml` (no `certification`, no description, no `url_en`, no `credit_points`). The fixture for an unknown course id (`unknown_course_id.yml`) is added in Task 6, because until then the Ruby validator rightly rejects it.
- [ ] **Step 2: Write `TestAppAcceptsWhatCIAccepts`**: for each file in `testdata/ci_accepts/`, parse with `yamldoc.Parse`, assert `Rules(model)` is empty and `Schema(schemaBytes(t), FeedJSON(model))` returns no problems. Subtest name = file name.
- [ ] **Step 3: Run** `cd admin-app && go test ./internal/validate/ -run TestAppAcceptsWhatCIAccepts -v`. Expected: FAIL on `course_without_dates.yml` with `expected array, but got null`.
- [ ] **Step 4: Fix `FeedJSON`**: `jc.Dates` and `jc.Trainers` start as non-nil empty slices (`[]jsonDate{}`, and `Trainers` copied into a non-nil slice).
- [ ] **Step 5: Run** the test again. Expected: PASS. Run `for f in admin-app/internal/validate/testdata/ci_accepts/*.yml; do ruby scripts/validate_trainings.rb "$f" || exit 1; done` from the repo root. Expected: `OK:` for each.
- [ ] **Step 6: CI**: add a step to `validate-trainings.yml` after "Validate trainings.yml": `for f in admin-app/internal/validate/testdata/ci_accepts/*.yml; do ruby scripts/validate_trainings.rb "$f"; done` (fail on first error), and add `admin-app/internal/validate/testdata/**` to both `paths:` filters.
- [ ] **Step 7: `make app-check`** green, commit `Admin app: a course without dates no longer blocks the pull request`.

### Task 2: Data contract: `blurb_de` / `blurb_en` / `show_on` in data, validator, schema, feed

**Files:**
- Modify: `_data/trainings.yml` (header comment; four `blurb:` → `blurb_en:`)
- Modify: `scripts/validate_trainings.rb`
- Modify: `api/trainings.schema.json`
- Modify: `api/trainings.json` (Liquid feed template)
- Modify: `admin-app/internal/validate/testdata/*.yml` and `ci_accepts/*.yml` if they contain `blurb:`

**Interfaces:**
- Produces: YAML keys `blurb_de`, `blurb_en` (strings), `show_on` (list of strings); feed fields `blurb_de`, `blurb_en`, `show_on` (all optional), `blurb` generated.

- [ ] **Step 1: Validator, failing first.** Add `RETIRED_COURSE_KEYS = { "blurb" => "Use blurb_en (English) and/or blurb_de (German): every description states its language." }` and reject by name per course (same style as `credits`). Add: `blurb_de` / `blurb_en` must be non-empty strings when present; `show_on`, when present, must be a non-empty array of strings each matching `/\A[a-z0-9.-]+\z/`, message for `[]`: `"course #{cid}: 'show_on' is empty - leave it out to mean every site, or name at least one"`. Run `ruby scripts/validate_trainings.rb`. Expected: FAIL with four `'blurb' was retired` errors.
- [ ] **Step 2: Migrate the data.** Rename the four `blurb:` keys to `blurb_en:` (keep the folded text unchanged). Rewrite the header's "NO PROSE IN THIS FILE" paragraph per spec §5 B3: no prose without a stated language; `blurb_de` / `blurb_en` are the allowed exception and why. Document `show_on` (absent = every consumer site; trainings.arc42.org ignores it; the name list). Run the validator. Expected: `OK: 4 courses, N dates`.
- [ ] **Step 3: Schema.** Add to the course `properties`: `"blurb_de": {"type":"string","minLength":1}`, `"blurb_en": {"type":"string","minLength":1}`, `"show_on": {"type":"array","minItems":1,"items":{"type":"string","pattern":"^[a-z0-9.-]+$"}, "description": "Consumer sites that advertise this course. Absent: every site. trainings.arc42.org shows every course regardless."}`. Change `blurb`'s description to say it is generated (blurb_en, else blurb_de) and deprecated in favour of the two explicit fields; type unchanged.
- [ ] **Step 4: Feed template.** In `api/trainings.json`: `blurb` = `course.blurb_en | default: course.blurb_de`, emitted only if that is set; emit `blurb_de`, `blurb_en`, `show_on` (jsonify) when present. Extend the header comment's legacy/new table with `blurb → blurb_de, blurb_en`.
- [ ] **Step 5: Verify.** `make site`, then `npx --yes ajv-cli@5 validate -s api/trainings.schema.json -d _site/api/trainings.json` (or the Docker equivalent) passes. `jq '.courses[] | {id, blurb, blurb_en, blurb_de, show_on}' _site/api/trainings.json`: every existing course has `blurb == blurb_en`. Compare `jq '.courses[]|{id,blurb}'` of the built feed with the same query on `curl -s https://trainings.arc42.org/api/trainings.json`. Expected: identical. Then add a temporary course with `dates: []` to `_data/trainings.yml`, rebuild, confirm the feed is valid JSON and the schema passes, and **revert the temporary course**.
- [ ] **Step 6: Commit** `Data: descriptions per language, show_on, blurb generated in the feed`.

### Task 3: Admin model and YAML round-trip for the new fields

**Files:**
- Modify: `admin-app/internal/model/model.go` (`Course`)
- Modify: `admin-app/internal/yamldoc/parse.go`, `render.go` (and `apply.go` only if it lists course keys)
- Modify: `admin-app/internal/model/diff.go`
- Modify: `admin-app/internal/validate/schema.go` (`FeedJSON` emits the new fields and the generated `blurb`)
- Test: `admin-app/internal/yamldoc/parse_test.go`, `apply_test.go`, `admin-app/internal/model/diff_test.go`, `admin-app/internal/validate/agreement_test.go` fixtures

**Interfaces:**
- Produces: `model.Course` loses `Blurb`, gains `BlurbDE string`, `BlurbEN string`, `ShowOn []string`. `func (c Course) Blurb() string` returns `BlurbEN`, else `BlurbDE` (the feed's generated value). Every other package uses these names.

- [ ] **Step 1: Failing tests.**
  - `TestParseReadsDescriptionsAndShowOn`: YAML with `blurb_de`, `blurb_en`, `show_on: ["www.arc42.de", "arc42.org"]` parses into those fields.
  - `TestParseReadsLegacyBlurbAsEnglish`: a course with `blurb:` only parses into `BlurbEN`.
  - `TestRenderRoundTripsAwkwardDescriptions`: `BlurbDE` = `Zwei „Erfinder“: KI & Spezifikation — "immer" dabei.` and `BlurbEN` with a `#` and a 200-char sentence round-trip AddCourse → Bytes → Parse unchanged.
  - `TestRenderWritesNoShowOnWhenEmpty`: `ShowOn: nil` writes no `show_on` key.
  - `TestUpdateCourseReplacesLegacyBlurb`: updating a course stored with `blurb:` writes `blurb_en:` and no `blurb:`.
  - diff: `TestCourseDiffLabelsDescriptions`: labels are `Description (German)`, `Description (English)`, `Shown on`.
  - Add fixture `ci_accepts/descriptions_and_show_on.yml` (MAS-like course with `blurb_de` only and `show_on`).
- [ ] **Step 2: Run** `cd admin-app && go test ./...`. Expected: compile errors / FAIL.
- [ ] **Step 3: Implement.** Parse `blurb_de`, `blurb_en` (fallback: legacy `blurb` → `BlurbEN`), `show_on` via `stringList`. Render `blurb_de` then `blurb_en` as folded blocks in the position `blurb` had (reuse the existing `wrapWords` path, one helper for both), `show_on` via `fmtList` after `trainers`, omitted when empty. `FeedJSON`: `Blurb` = `c.Blurb()` (omitempty), plus `blurb_de`, `blurb_en`, `show_on` (omitempty). Diff rows replace the `blurb` row with the three rows above.
- [ ] **Step 4: Run** `make app-check`. Expected: PASS (fix any test that referenced `Blurb`).
- [ ] **Step 5: Commit** `Admin app: descriptions per language and show_on in the model and YAML`.

### Task 4: Course form, warnings and info lines

**Files:**
- Modify: `admin-app/internal/model/model.go` (remove `coursesWithPublicCard`, `coursesWithOnlineCard`, `HasCardTemplate`, `HasAnyCardTemplate`; add `HandMadeCards`, `HasHandMadeCard`, `KnownSites`)
- Modify: `admin-app/internal/validate/warnings.go`
- Modify: `admin-app/internal/web/handlers_courses.go`, `templates/courseform.gohtml`, `templates/_fields.gohtml`
- Test: `admin-app/internal/validate/warnings_test.go`, a new `admin-app/internal/web/handlers_courses_test.go` (or the existing courses tests in `handlers_dates_test.go`, follow what exists)

**Interfaces:**
- Consumes: `model.Course{BlurbDE, BlurbEN, ShowOn}` from Task 3.
- Produces:
  - `var HandMadeCards = map[string]bool{"msa": true, "improve": true, "req4arc": true, "adoc": true}`; `func HasHandMadeCard(courseID string) bool`.
  - `var KnownSites = []string{"www.arc42.de", "arc42.org", "www.systems42.org", "docs.arc42.org", "faq.arc42.org", "examples.arc42.org", "softwareknigge.arc42.org", "www.gernotstarke.de"}`.
  - `func CourseNotes(c model.Course) []string` in `validate`: info lines that never gate the save.
  - `Warning` gains `Label string` (the form label); the warnings partial prints `.Label`, falling back to `.Field`.

- [ ] **Step 1: Failing tests** (`warnings_test.go`):
  - `TestNoCardTemplateWarningForANewCourse`: `CourseWarnings` and `DateWarnings` for course `mas` contain no message mentioning "card", "template" or "_includes".
  - `TestCourseWithoutAnyDescriptionWarns`: course `mas`, no descriptions → one warning, label `Description`, message `The home page cards for this course will show no description.`
  - `TestHandMadeCardCourseGetsNoteNotWarning`: course `msa`, no `BlurbDE` → no description warning; `CourseNotes` contains `This course still has its own hand-made card on the home pages. Changes to its descriptions show up there once it is switched to the standard card.`
  - `TestOnlyGermanDescriptionNote`: `mas` with `BlurbDE` only → `CourseNotes` contains `Only a German description: the English home page shows it in German too.` (and the mirrored English sentence for `BlurbEN` only: `Only an English description: the German home page shows it in English too.`).
  - `TestWarningsCarryFormLabels`: every warning from both functions has a non-empty `Label`, and no `Message` contains `schema`, `_includes`, `.html` or `feed`.
  - The https warning message is `links must start with https://`.
- [ ] **Step 2: Failing handler tests**:
  - `TestCourseFormSavesShowOn`: POST with `show_on=www.arc42.de&show_on=arc42.org&show_on=www.systems42.org` stores exactly that list; `show_on_other=example.org` is appended.
  - `TestCourseFormNoSiteTickedMeansEverySite`: POST with no `show_on` stores `ShowOn == nil`.
  - `TestCourseFormSavesBothDescriptions`: fields `blurb_de`, `blurb_en` land in `BlurbDE`, `BlurbEN`.
  - `TestCourseFormRejectsBadSiteName`: `show_on_other=Not A Site` is a blocking problem labelled `Shown on`, message `"Not A Site" is not a site name like www.arc42.de`.
- [ ] **Step 3: Run** `cd admin-app && go test ./internal/validate/ ./internal/web/`. Expected: FAIL.
- [ ] **Step 4: Implement.** Remove the two card-template warnings and their helpers. Add `Label` to every `Warning` (use the form's visible labels: `Course ID`, `Short title`, `Description`, `Course URL`, `English course page`, `Start`, `End`, `City`, `Country`, `Availability`, `Format`, `Booking code`, `Date ID` — match `courseform.gohtml`/`dateform.gohtml` exactly). Form: replace the Blurb textarea with *Description (German)* (`blurb_de`) and *Description (English)* (`blurb_en`); add a *Shown on* fieldset after the trainers one, checkboxes from `KnownSites` plus `show_on_other` (comma-separated), hint text: `No box ticked: every site shows this course. trainings.arc42.org always shows every course.` Reuse the trainers roster pattern (`parseTrainers`/`otherTrainers`) as `parseShowOn`/`otherSites`. Render `CourseNotes` as a neutral `<div class="notes" role="note">` above the form on GET and on reshow; notes never trigger the "Save anyway" gate.
- [ ] **Step 5: Run** `make app-check` and `make app-preview`; open `preview-out/` course form page and check the two description fields and the *Shown on* group render. Expected: PASS.
- [ ] **Step 6: Commit** `Admin app: course form speaks the operator's language; no card-template warnings`.

### Task 5: Problems on *Review & propose* say who is at fault

**Files:**
- Create: `admin-app/internal/validate/explain.go`
- Modify: `admin-app/internal/validate/rules.go` (`Problem` gains fields), `admin-app/internal/web/handlers_propose.go`, `templates/propose.gohtml`
- Test: `admin-app/internal/validate/explain_test.go`, `admin-app/internal/web/handlers_propose_test.go`

**Interfaces:**
- Consumes: `model.Trainings`; `model.HasHandMadeCard` not needed.
- Produces:
  - `Problem` gains `Kind ProblemKind` (`EntryProblem` | `AppFault`), `Where string` (e.g. `MAS, 13 Jan 2027 online`), `Label string` (form label), `Link string` (`/dates/<id>/edit` or `/courses/<id>/edit`, use the app's existing routes), `Detail string` (raw technical text for app faults).
  - `func Explain(t model.Trainings, p Problem) Problem` fills `Where`, `Label`, `Link`, `Kind` from `p.Field` (both rule fields `dates.<id>.<prop>` and schema pointers `courses/<i>/dates/<j>/<prop>`).
  - `func AppFault(detail string) Problem` for schema-fetch / feed-render / compile failures.
  - A single table `formFields` (course props and date props → form label) in `explain.go`. A schema pointer whose property is not in the table is an `AppFault`. Comment: keep this table next to the forms' field names; `TestFormFieldsCoverForms` enforces it.

- [ ] **Step 1: Failing tests** (`explain_test.go`):
  - `TestExplainRuleProblem`: `Problem{Field: "dates.mas-jan-2027.end", Message: "end 2027-01-12 is before start 2027-01-13"}` on a model containing that date (online) → `Kind == EntryProblem`, `Where == "MAS, 13 Jan 2027 online"`, `Label == "End"`, `Link` points at that date's edit form.
  - `TestExplainSchemaPointer`: `Field: "courses/0/dates/1/start"` → the course/date at those indices, `Label == "Start"`.
  - `TestExplainUnknownPropertyIsAppFault`: `Field: "courses/0/generated_thing"` → `Kind == AppFault`, `Detail` contains the pointer and message.
  - `TestFormFieldsCoverForms`: every `name="..."` in `templates/courseform.gohtml` and `templates/dateform.gohtml` that maps to a YAML property appears in `formFields` (parse the template files; a small explicit ignore list for `confirm_warnings`, `*_other`, credit inputs as needed).
- [ ] **Step 2: Failing handler tests** (`handlers_propose_test.go`, with `ghfake`):
  - `TestProposeNewCourseWithoutDatesOpensPR`: draft adds course `mas` (no dates) → propose page has no problems, shows `MAS has no dates yet. It will appear on the home pages once it has one.`, and submitting opens one PR.
  - `TestProposeAppFaultDoesNotBlock`: make the fake GitHub fail the schema read → page shows `This is not caused by your entries, and your draft is saved.` and still renders the *Open pull request* form, with the detail inside a `<details>`.
  - `TestProposeEntryProblemLinksToForm`: a date with end before start → problem text contains `MAS, 13 Jan 2027 online`, `End`, and an `<a href=` to the edit form; no `dates.` or `courses/` substring in the visible list; the form is not rendered.
  - `TestProposePageNoLongerClaimsCI`: the string `CI would reject them anyway` does not appear when the only problems are app faults.
- [ ] **Step 3: Run** `go test ./internal/validate/ ./internal/web/`. Expected: FAIL.
- [ ] **Step 4: Implement.** `handlePropose` and `handleProposeSubmit` pass every problem through `Explain`; `schemaProblems` returns `AppFault(...)` for fetch/render/compile errors. Template: two sections. Entry problems: heading `Fix these in your entries`, each `<li>{{.Where}} — {{.Label}}: {{.Message}} <a href="{{.Link}}">Edit</a></li>`, then `Fix these before opening a pull request — CI would reject them too.` App faults: the sentence from spec §4 A3 plus `You can still open the pull request: CI checks it again.` and `<details><summary>Technical detail</summary>…</details>`. The PR form is hidden only when there are entry problems. Info line for each course with no dates that the draft added.
- [ ] **Step 5: Run** `make app-check`, then `make app-preview` and look at the propose page. Expected: PASS.
- [ ] **Step 6: Commit** `Admin app: problems name the course, the date and the field, and say whose they are`.

### Task 6: The data-driven card

**Files:**
- Create: `_includes/timeline_card.html`
- Modify: `_includes/timeline_course.html` (`else` branch; header comment), `_includes/timeline_auto.html` (pass `course=c date=d`)
- Modify: `scripts/validate_trainings.rb` (remove the card-template error and its comment block, replace with one sentence on the data-driven fallback)

**Interfaces:**
- Consumes: course keys `title`, `certification`, `blurb_de`, `blurb_en`, `trainers`, `url`, `url_en`; date keys `trainers`; include params as passed to the hand-made cards (`position`, `date`, `location`, `anchor_id`, `sold_out`, `status_note`, `waitlist_label`, `hide_register`, `few_seats`, `pricing`, `credits`, `page_lang`, `delivery_lang`).
- Produces: `{% include timeline_card.html course=… date=… <params> %}`; all assigned variables prefixed `tc_`.

- [ ] **Step 1: Make the failing state visible.** Add a temporary MAS course to `_data/trainings.yml` (id `mas`, short title `MAS`, title `Mastering AI-Driven Specification`, `blurb_de` only, `url: "https://www.systems42.org/training"`, `url_en` the same, trainers `["Peter Hruschka", "Peter Gerstbach"]`, `show_on: ["www.arc42.de", "arc42.org", "www.systems42.org"]`, two January 2027 dates: one public in München, one online, `language: de`, priced). `make site`; `grep -c 'id="mas-' _site/index.html _site/de/index.html`. Expected: `0` (HTML comment only).
- [ ] **Step 2: Implement `timeline_card.html`** per spec §5 B2, copying markup and classes from `timeline_msa.html` (location line, language note, sold-out, few-seats, pricing, buttons). Description: `tc_blurb` = page-language field, else the other, else none. Certification line only when `course.certification` is non-empty. Trainers: `include.date.trainers | default: include.course.trainers`, joined with `und`/`and`, on its own line `Mit …` / `With …`. *More info*: DE page → `url`; EN page → `url_en` (label `More info`) else `url` (label `More info (in German)`). Register: `/anmeldung/?kurs=<anchor_id>` / `/registration/?kurs=<anchor_id>`. Wire the `else` branch of `timeline_course.html` to it, forwarding `course`, `date` and every param; pass `course=c date=d` from `timeline_auto.html`.
- [ ] **Step 3: Verify with the build** (`make site`), all against `_site/index.html` and `_site/de/index.html`:
  - both pages contain `id="mas-` twice (the two date ids);
  - the MAS cards contain `Mastering AI-Driven Specification`, the German description on **both** pages, `Peter Hruschka` and `Peter Gerstbach`, `systems42.org/training`, `?kurs=<mas date id>`, a price;
  - the MAS cards contain no `iSAQB` and no credits line (extract each card's `<div class="container …" id="mas-…">…</div>` and grep inside it);
  - the existing MSA/IMPROVE/REQ4ARC/ADOC cards are byte-identical to the build before this task (diff the two `_site/index.html` with the MAS blocks removed);
  - `/anmeldung/` lists both MAS dates.
- [ ] **Step 4: Validator.** Remove the template check; run `ruby scripts/validate_trainings.rb` (with the temporary MAS) → `OK`. Add fixture `admin-app/internal/validate/testdata/ci_accepts/unknown_course_id.yml` (course id `mas`, one online and one public date) and confirm both the Ruby loop and `TestAppAcceptsWhatCIAccepts` pass on it.
- [ ] **Step 5: Revert the temporary MAS course** (`git diff _data/trainings.yml` shows nothing from it), rebuild, run `make check-links`. Expected: PASS.
- [ ] **Step 6: Commit** `Site: a data-driven timeline card for every course without its own`.

### Task 7: Every upcoming date is on both home pages (output check)

**Files:**
- Create: `scripts/check_home_cards.rb`
- Modify: `Makefile` (target `check-cards: site`), `.github/workflows/validate-trainings.yml` (step after "Build site"; add `_includes/**` and `_pages/home*.html` to `paths:`)

**Interfaces:**
- Produces: `ruby scripts/check_home_cards.rb [_data/trainings.yml] [_site]`: exit 0 and `OK: N dates on both home pages`, or exit 1 with one line per missing date: `date <id> (<course short_title>, <start>) is not on <page>`.

- [ ] **Step 1: Write the script** using the same "upcoming" rule as `timeline_auto.html` (`end >= today`, `status != cancelled`; a `full` date still renders, greyed) and look for `id="<date id>"` in `_site/index.html` and `_site/de/index.html`.
- [ ] **Step 2: Prove it fails**: after `make site`, remove one upcoming date's `id="<date id>"` attribute from `_site/de/index.html` with `sed -i.bak`, run the script. Expected: exit 1 and `date <that id> (…) is not on _site/de/index.html`. Restore from the `.bak`.
- [ ] **Step 3: Run** on the clean build. Expected: `OK`.
- [ ] **Step 4: Wire** the Makefile target (Docker-free Ruby call after `site`, like `check-consumers` uses the host) and the CI step `ruby scripts/check_home_cards.rb`.
- [ ] **Step 5: Commit** `Site: CI checks every upcoming date is on both home pages`.

### Task 8: The consumer check follows `show_on`

**Files:**
- Modify: `scripts/verify_consumers.sh`
- Create: `scripts/test/verify_consumers_test.sh` and fixtures under `scripts/test/fixtures/` (a feed with MSA + MAS, and one HTML page per relevant site)
- Modify: `Makefile` (`check-consumers-test`), `.github/workflows/validate-trainings.yml` (run the test; `scripts/**` is already in `paths:`)

**Interfaces:**
- Consumes: feed field `show_on`.
- Produces: `SITES` overridable via environment (`SITES="${SITES:-<current list>}"`), `FEED_URL` already overridable; both accept `file://` URLs so the test runs offline.

- [ ] **Step 1: Failing test.** Fixture feed: MSA dates (no `show_on`) and MAS dates (`show_on: ["www.arc42.de", "arc42.org", "www.systems42.org"]`) with MAS earliest. Fixture pages: `faq.html` shows only MSA ids; `arc42org.html` shows MAS and MSA ids; `examples.html` shows one MSA row. Test runs the script with `SITES` pointing at the three `file://` pages (rules `chrono`, `chrono`, `per-course`), `ATTEMPTS=1`, `SLEEP_SECONDS=0`. Expected now: FAIL (faq "missing" MAS).
- [ ] **Step 2: Implement.** Give the jq queries a site argument: `published`/`lead_chrono`, `per_course` and the `trainer=` query select only courses where `(.show_on // null) == null or (.show_on | index($site))`. Compute per site inside the loop. Also: `(.dates // [])` everywhere a course's dates are read (Review Focus 1). Update the header comment: the rule now respects each course's `show_on`.
- [ ] **Step 3: Run** the test. Expected: PASS. Add a fixture case with a course `dates: []` → still PASS.
- [ ] **Step 4: Wire** the test into the Makefile and CI. Commit `Consumer check: expect a course only where show_on says it is advertised`.

### Task 9: Docs and the final acceptance pass

**Files:**
- Modify: `CLAUDE.md` (the "No prose" bullet; replace the "A card template's name is derived" bullet; mention `show_on` under the feed bullets), `README.md` (same topics, wherever it documents `blurb`, card templates, or consumers), `admin-app/README.md` (course form: descriptions, *Shown on*, hand-made card list in `model.go` and when to shrink it), `docs/superpowers/specs/2026-11-06-new-course-types-design.md` (status: implemented on branch)

- [ ] **Step 1:** Update the docs. `grep -rn "blurb\b\|timeline_<\|card template" CLAUDE.md README.md admin-app/README.md` shows no stale statement.
- [ ] **Step 2: Acceptance** (spec §8) in one pass: `make app-check`, `ruby scripts/validate_trainings.rb`, the `ci_accepts` loop, `make site`, `ruby scripts/check_home_cards.rb`, `make check-links`, the consumer-check test. Run `make training-app-demo` and walk the MAS flow by hand against the fake GitHub: create MAS (no warnings about cards, the "only German" note), propose with no dates (PR opens), add two dates, propose again (PR opens). Record the outcome of each point 1–9 in the PR description.
- [ ] **Step 3:** In the PR description's last section, list the one manual follow-up: mark the `validate` check as required in branch protection (spec §5 B4), and the arc42.de ordering note (spec §9).
- [ ] **Step 4: Commit** `Docs: new course types need no developer`.
