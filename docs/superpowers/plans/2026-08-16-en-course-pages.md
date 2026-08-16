# English course detail pages — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** English readers of arc42.org / docs / faq who click a course in the training schedule land on an English course page (`https://trainings.arc42.org/courses/msa/`) instead of the German `arc42.de/info-msa/`.

**Architecture:** One new Jekyll page on trainings.arc42.org (`/courses/msa/`, ported from `arc42.de/info-msa-EN/`); one new optional feed field `courses[].url_en` carried through YAML, JSON schema, Ruby validator and the Go admin-app; three consumer includes prefer `url_en`; the old arc42.de English page becomes a redirect stub. No sync automation between DE and EN — deliberately.

**Tech Stack:** Jekyll (Liquid, Markdown, front matter), JSON Schema draft-07, Ruby stdlib validator, Go 1.26 admin-app (`admin-app/`, `go test ./...`), Docker `make site` / `make check-links` (html-proofer).

**Spec:** `docs/superpowers/specs/2026-08-16-en-course-pages-design.md` (this repo). Read it first — every decision below argues from it.

## Global Constraints

- Repos involved (all siblings under `/Users/gernotstarke/projects/arc42/`): `trainings.arc42.org-site` (branch `feat/en-course-pages`, main work), `arc42.de-site` (branch `schulungen-redesign`), `arc42.org-site` (branch `main`, has **unrelated uncommitted changes — commit only the files named in Task 8**), `docs.arc42.org-site` (`main`), `faq.arc42.org-site` (`main`).
- Feed: `url` stays **required, German**. `url_en` is **optional**, `^https://`. Never remove or rename `url`.
- Page URL convention: `/courses/<course-id>/` where `<course-id>` == `courses[].id` in `_data/trainings.yml`.
- Buttons on the new page: `btn btn--primary` (Register) and `btn btn--inverse` (everything else). No new SCSS. Do not use `.button`/`buttonMSA` (timeline-scoped).
- Content of the new page: text of `arc42.de-site/_pages/info-msa-engl.md` 1:1, only these typos fixed: archtects→architects, architeftures→architectures, devolping→developing, organisize→organise, "developers an architects"→"developers and architects".
- Commit messages: imperative, prefixed `feat:` / `fix:` / `docs:` / `test:`; every commit ends with
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01G6EiTPnL1eFLYhwYHA7EMt`.
- Ruby, Go and Docker are installed locally; `make site` / `make check-links` run in Docker (`make dev` must not be running — port 4000 conflict).

---

## File map

| Repo | File | Change |
|---|---|---|
| trainings | `_pages/courses/msa.md` | **create** — the English MSA page |
| trainings | `_includes/timeline_msa_online.html` | link `/courses/msa/` |
| trainings | `_data/trainings.yml` | `url_en` on course `msa` |
| trainings | `api/trainings.schema.json` | `courses[].properties.url_en` |
| trainings | `scripts/validate_trainings.rb` | accept `url_en`, require https |
| trainings | `admin-app/internal/model/model.go` | `Course.URLEn` |
| trainings | `admin-app/internal/yamldoc/parse.go` | read `url_en` |
| trainings | `admin-app/internal/yamldoc/render.go` | write `url_en` after `url`, quoted, if set |
| trainings | `admin-app/internal/yamldoc/apply_test.go` | round-trip test |
| trainings | `admin-app/internal/validate/schema.go` | `url_en` in `FeedJSON` |
| trainings | `admin-app/internal/validate/validate_test.go` | schema accepts `url_en` |
| trainings | `admin-app/internal/web/handlers_courses.go` | form field → model |
| trainings | `admin-app/internal/web/templates/courseform.gohtml` | optional input |
| trainings | `README.md` | recipe "adding an English course page" + feed field |
| arc42.org | `_includes/subtle-ads/subtle-ads.html:87` | `url_en \| default: url` |
| arc42.org | `_pages/learn.md:58` | link `/courses/msa/` |
| docs | `_includes/training-dates.html:87` | `url_en \| default: url` |
| faq | `_includes/training-dates.html:87` | `url_en \| default: url` |
| arc42.de | `_pages/info-msa-engl.md` | redirect stub |
| arc42.de | `_includes/timeline_msa_online.html:26` | link new URL |

---

### Task 1: The English MSA page

**Files:**
- Create: `trainings.arc42.org-site/_pages/courses/msa.md`
- Read (source text): `arc42.de-site/_pages/info-msa-engl.md`
- Read (front-matter examples): `trainings.arc42.org-site/_pages/registration.md`, `_pages/imprint-privacy.md`

**Interfaces:**
- Produces: the URL `https://trainings.arc42.org/courses/msa/` that Tasks 2, 3, 8, 9 link to.

- [ ] **Step 1: Create the page**

Write `_pages/courses/msa.md` with exactly this front matter, then the body below it. (Jekyll picks up `_pages/**` because `_config.yml` has `include: [_pages]` and a defaults entry with `path: "_pages"`, so a subfolder is fine.)

```markdown
---
title: "Mastering Software Architectures"
layout: page
permalink: /courses/msa/
lang: en
translation_url: https://www.arc42.de/info-msa/
description: "Three-day iSAQB CPSA-F training by Peter Hruschka and Gernot Starke — content, target audience, certification."
header:
  overlay_color: "#743442"
---

<p class="course-actions">
  <a class="btn btn--primary" href="/registration/">Register</a>
  <a class="btn btn--inverse" href="https://www.arc42.de/downloads/flyer-msa-EN.pdf" target="_blank" rel="noopener noreferrer nofollow">Course description (PDF)</a>
  <a class="btn btn--inverse" href="https://www.arc42.de/terms-en/">Terms</a>
</p>

3 day introductory training developed by Peter Hruschka and Gernot Starke

## You can expect..
a solid, practical and pragmatic introduction to architecture with lots of exercises.
You will learn and practise the different tasks of software architects using an extensive case study.
The focus is on a methodical and systematic approach to architectural design and evaluation.
You will receive concrete tips on how to proceed in development projects, supported with practical examples.

In short (for more info see below)

* Roles and tasks
* Agility and architecture
* Setting the scope and external interfaces
* Design Methods (Domain Driven Design, Quality Driven Architecture, ...)
* Architectural Views
* Architecture Patterns
* Documentation of Architectures
* Analyzing and evaluating architectures

## Target Audience
This course is primarily for developers and architects that want to understand the role "software architect" and learn about their tasks and skills.

If you are project manager, product manager, product owner or Scrum coach? You will get an easy to understand introduction to software architecture so that you and your development teams will be able to communicate in a common language.

## Ideal Preparation for the iSAQB CPSA-F exam

This training is accredited by iSAQB and covers the complete curriculum of CPSA Foundation Level.

Therefore, besides offering a high degree of practice our seminar _Mastering Software Architecture_ offers an ideal and effective way to prepare yourself for the iSAQB CPSA Foundation Level certification.

We have successfully prepared more than 3000 persons for the exam in the last couple of years.

>## About the iSAQB certification
>We organise the exam for you, and (optionally) reserve a seat for the exam. The fee for the exam (currently € 250,- + VAT - where applicable) will be sent to you **after** the course.
>You don't need to do anything explicitly - you can cancel your interest in the exam even during the seminar.

## Extensive Description of Content

### Introduction and Motivation

* Tasks, role and responsibilities of software architectures
* Architecture in the development process
* Clarifying Requirements & constraints
* Deriving architectural or quality goals

### Documentation of Architecture

* Goals and requirements for architecture documentation
* Typical architecture documents
* Architectural views

### The Architecture Development Process

* Methodological tools for developing software architecture
* developing business architectures
* Domain Driven Design
* Quality Driven Design: tactics and practices to achieve quality goals
* Top Down vs. Bottom Up Approaches

### Architectural Views

* Building Block View: Building Blocks & Interfaces
* Runtime View: describe processes and scenarios
* Deployment View: Infrastructure and deployment of building blocks to it

### Cross-cutting Concepts
* conceptual design

### Patterns and Principles

* Architecture and Design Patterns
* Fundamental Design Principles

### Analysis and Evaluation of Software Architectures
* qualitative and scenario-based Evaluation of architecture (e.g. ATAM)
* quantitative evaluations and effective use of metrics

### and now...

<p class="course-actions">
  <a class="btn btn--primary" href="/registration/">Register</a>
  <a class="btn btn--inverse" href="/#training-dates">All dates</a>
</p>
```

- [ ] **Step 2: Build the site and check the rendered page**

Run (repo root): `make site`
Expected: build succeeds; `_site/courses/msa/index.html` exists.

Run:
```bash
grep -c 'href="https://www.arc42.de/info-msa/"' _site/courses/msa/index.html
grep -o '<link rel="alternate" hreflang="de" href="[^"]*"' _site/courses/msa/index.html
grep -o 'class="btn btn--primary" href="/registration/"' _site/courses/msa/index.html | head -1
```
Expected: first ≥ 1 (the DE|EN switch), second `href="https://www.arc42.de/info-msa/"`, third one match.

- [ ] **Step 3: Link check**

Run: `make check-links`
Expected: html-proofer passes (external links are disabled by the Makefile; internal `/registration/` and `/#training-dates` must resolve).

- [ ] **Step 4: Commit**

```bash
git add _pages/courses/msa.md
git commit -m "feat: English MSA course page at /courses/msa/"
```

---

### Task 2: Point this site's MSA-online card at the new page

**Files:**
- Modify: `trainings.arc42.org-site/_includes/timeline_msa_online.html:56`

- [ ] **Step 1: Change the link**

Replace
```html
<a class="button buttonMSA" href="https://www.arc42.de/info-msa-EN/" hreflang="en">{{ info_label }}</a>
```
with
```html
<a class="button buttonMSA" href="/courses/msa/" hreflang="en">{{ info_label }}</a>
```

- [ ] **Step 2: Verify**

Run: `grep -rn "info-msa-EN" _includes _pages _layouts` — Expected: no output.
Run: `make site && grep -c 'href="/courses/msa/"' _site/index.html` — Expected: ≥ 1 (the online MSA date `msa-online-sep-2026` is upcoming, so its card renders on `/`).

- [ ] **Step 3: Commit**

```bash
git add _includes/timeline_msa_online.html
git commit -m "fix: MSA online card links the local English course page"
```

---

### Task 3: `url_en` in the feed — YAML, JSON schema, Ruby validator

**Files:**
- Modify: `trainings.arc42.org-site/_data/trainings.yml` (course `msa`, right after `url:`)
- Modify: `trainings.arc42.org-site/api/trainings.schema.json:22`
- Modify: `trainings.arc42.org-site/scripts/validate_trainings.rb`

**Interfaces:**
- Produces: `courses[].url_en` (optional string, `^https://`) in `_data/trainings.yml` and in the built `/api/trainings.json`. Tasks 4–8 rely on that exact key name.

- [ ] **Step 1: Make the validator reject a bad `url_en` (test first)**

Create a throwaway file in the scratchpad (not in the repo):
```bash
S=/private/tmp/claude-501/-Users-gernotstarke-projects-arc42/085e51ff-c2ac-4cc3-a432-f3e0270b279b/scratchpad
mkdir -p $S && cat > $S/bad_url_en.yml <<'EOF'
courses:
  - id: msa
    short_title: "MSA"
    title: "Mastering"
    url: "https://example.org/msa"
    url_en: "http://example.org/msa-en"
    trainers: ["Peter Hruschka"]
    dates: []
EOF
ruby scripts/validate_trainings.rb $S/bad_url_en.yml; echo "exit=$?"
```
Expected now: `OK: 1 courses, 0 dates`, `exit=0` — i.e. the validator does not yet know the field. (That is the failing state.)

- [ ] **Step 2: Teach the validator**

In `scripts/validate_trainings.rb`, directly after the block
```ruby
  %w[id short_title title url].each do |f|
    errors << "course #{cid}: missing/empty '#{f}'" unless c[f].is_a?(String) && !c[f].empty?
  end
```
add
```ruby
  # url_en is optional (English detail page on trainings.arc42.org); when
  # present it must be a non-empty https URL, exactly like url.
  if c.key?("url_en") && !(c["url_en"].is_a?(String) && c["url_en"].start_with?("https://"))
    errors << "course #{cid}: 'url_en' must be an https URL when present (got #{c['url_en'].inspect})"
  end
```

- [ ] **Step 3: Verify the validator now fails on http and passes on https**

Run: `ruby scripts/validate_trainings.rb $S/bad_url_en.yml; echo "exit=$?"`
Expected: `ERROR: course msa: 'url_en' must be an https URL when present (got "http://example.org/msa-en")`, `exit=1`.

Run: `sed -i '' 's#http://example.org/msa-en#https://example.org/msa-en#' $S/bad_url_en.yml && ruby scripts/validate_trainings.rb $S/bad_url_en.yml`
Expected: `OK: 1 courses, 0 dates`.

- [ ] **Step 4: Add the field to the schema**

In `api/trainings.schema.json`, after
```json
          "url": { "type": "string", "pattern": "^https://" },
```
(the one inside `courses.items.properties`, line 22 — **not** the one inside `dates`) add
```json
          "url_en": { "type": "string", "pattern": "^https://" },
```
Do not add it to `required`.

- [ ] **Step 5: Add the field to the data**

In `_data/trainings.yml`, course `msa`, after `    url: "https://www.arc42.de/info-msa/"` add
```yaml
    url_en: "https://trainings.arc42.org/courses/msa/"
```
Also extend the header comment block at the top of the file with one line after the `language (de|en) is REQUIRED…` line:
```
# url_en (optional, per course) = English detail page, https://trainings.arc42.org/courses/<id>/
```

- [ ] **Step 6: Validate the real data and the built feed**

Run: `ruby scripts/validate_trainings.rb` — Expected: `OK: 4 courses, N dates`.
Run: `make site && python3 -c "import json;d=json.load(open('_site/api/trainings.json'));print([ (c['id'],c.get('url_en')) for c in d['courses']])"`
Expected: `[('msa', 'https://trainings.arc42.org/courses/msa/'), ('improve', None), ('req4arc', None), ('adoc', None)]` (order as in the YAML).

Run the schema check the way CI does — look at `.github/workflows/validate-trainings.yml` for the exact command; if it uses `check-jsonschema` or `ajv`, run the same locally against `_site/api/trainings.json`; if none is installed, run the Go schema test in Task 5 instead (it compiles the same schema).

- [ ] **Step 7: Commit**

```bash
git add _data/trainings.yml api/trainings.schema.json scripts/validate_trainings.rb
git commit -m "feat(feed): optional courses[].url_en, set for msa"
```

---

### Task 4: `url_en` in the admin-app model, YAML parse and render

**Files:**
- Modify: `admin-app/internal/model/model.go` (`Course` struct)
- Modify: `admin-app/internal/yamldoc/parse.go:63`
- Modify: `admin-app/internal/yamldoc/render.go` (`alwaysQuoted`, course head after `put("url", c.URL)`)
- Test: `admin-app/internal/yamldoc/apply_test.go`

**Interfaces:**
- Produces: `model.Course.URLEn string` (YAML key `url_en`). Tasks 5 and 6 use this exact field name.

- [ ] **Step 1: Write the failing round-trip test**

Append to `admin-app/internal/yamldoc/apply_test.go`:

```go
func TestCourseURLEnSurvivesUpdateCourse(t *testing.T) {
	src := `courses:
  - id: msa
    short_title: "MSA"
    title: "Mastering"
    url: "https://www.arc42.de/info-msa/"
    url_en: "https://trainings.arc42.org/courses/msa/"
    trainers: ["Peter Hruschka"]
    dates: []
`
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c := doc.Model().Courses[0]
	if c.URLEn != "https://trainings.arc42.org/courses/msa/" {
		t.Fatalf("parsed URLEn = %q", c.URLEn)
	}
	// An unrelated edit must not drop url_en (UpdateCourse re-renders the head).
	c.Title = "Mastering Software Architectures"
	if err := doc.UpdateCourse("msa", c); err != nil {
		t.Fatalf("UpdateCourse: %v", err)
	}
	out := string(doc.Bytes())
	if !strings.Contains(out, `url_en: "https://trainings.arc42.org/courses/msa/"`) {
		t.Errorf("url_en was dropped or unquoted:\n%s", out)
	}
	// url_en is written directly after url.
	if !strings.Contains(out, "url: \"https://www.arc42.de/info-msa/\"\n    url_en: ") {
		t.Errorf("url_en is not directly after url:\n%s", out)
	}
	// A course without url_en renders no url_en line at all.
	c.URLEn = ""
	if err := doc.UpdateCourse("msa", c); err != nil {
		t.Fatalf("UpdateCourse (clear): %v", err)
	}
	if strings.Contains(string(doc.Bytes()), "url_en") {
		t.Errorf("empty url_en must be omitted:\n%s", doc.Bytes())
	}
}
```
(`strings` is already imported in that file.)

- [ ] **Step 2: Run it, expect compile failure**

Run: `cd admin-app && go test ./internal/yamldoc/ -run TestCourseURLEnSurvivesUpdateCourse`
Expected: FAIL — `c.URLEn undefined`.

- [ ] **Step 3: Add the field, parse it, render it**

`internal/model/model.go`, in `Course`, after `URL string`:
```go
	URLEn         string // optional: English detail page, https://trainings.arc42.org/courses/<id>/
```

`internal/yamldoc/parse.go`, in the course literal after `URL: scalar(cn, "url"),`:
```go
			URLEn:         scalar(cn, "url_en"),
```

`internal/yamldoc/render.go`:
- add `"url_en": true,` to the `alwaysQuoted` map (same line as `"url": true`);
- after `put("url", c.URL)` add `put("url_en", c.URLEn)`.

Check that `put` skips empty values (read the closure a few lines above `put("id", c.ID)`); if it does not, guard with `if c.URLEn != "" { put("url_en", c.URLEn) }`.

- [ ] **Step 4: Run the package tests**

Run: `cd admin-app && go test ./internal/yamldoc/ ./internal/model/`
Expected: PASS (new test and all existing golden tests — the golden files have no `url_en`, so nothing changes for them).

- [ ] **Step 5: Commit**

```bash
git add admin-app/internal/model/model.go admin-app/internal/yamldoc/parse.go admin-app/internal/yamldoc/render.go admin-app/internal/yamldoc/apply_test.go
git commit -m "feat(admin): carry courses[].url_en through model, parse and render"
```

---

### Task 5: `url_en` in the admin-app feed JSON + schema check

**Files:**
- Modify: `admin-app/internal/validate/schema.go` (`jsonCourse`, and the `jc := jsonCourse{...}` literal)
- Test: `admin-app/internal/validate/validate_test.go`

**Interfaces:**
- Consumes: `model.Course.URLEn` (Task 4), `api/trainings.schema.json` with `url_en` (Task 3).

- [ ] **Step 1: Write the failing test**

`helper_test.go` in the same package already loads the schema with `os.ReadFile(filepath.Join("..", "..", "..", "api", "trainings.schema.json"))` (inside `RulesFromYAML`). Add a small helper next to it and use it below:

```go
func schemaBytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "trainings.schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	return b
}
```
Then append to `validate_test.go`:

```go
func TestFeedJSONCarriesURLEnAndSchemaAcceptsIt(t *testing.T) {
	tr := ok()
	tr.Courses[0].URLEn = "https://trainings.arc42.org/courses/msa/"
	feed, err := FeedJSON(tr)
	if err != nil {
		t.Fatalf("FeedJSON: %v", err)
	}
	if !strings.Contains(string(feed), `"url_en":"https://trainings.arc42.org/courses/msa/"`) {
		t.Errorf("feed lacks url_en:\n%s", feed)
	}
	problems, err := Schema(schemaBytes(t), feed)
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if len(problems) != 0 {
		t.Errorf("schema rejected url_en: %v", problems)
	}

	// Without url_en the key must be absent, not "".
	tr.Courses[0].URLEn = ""
	feed, _ = FeedJSON(tr)
	if strings.Contains(string(feed), "url_en") {
		t.Errorf("empty url_en must be omitted:\n%s", feed)
	}
}
```
Add `"strings"` to `validate_test.go`'s imports if missing (`os` and `path/filepath` are already imported in `helper_test.go`).

- [ ] **Step 2: Run, expect failure**

Run: `cd admin-app && go test ./internal/validate/ -run TestFeedJSONCarriesURLEn`
Expected: FAIL (`feed lacks url_en`).

- [ ] **Step 3: Implement**

In `schema.go`, `jsonCourse`: after `URL string \`json:"url"\`` add
```go
		URLEn         string     `json:"url_en,omitempty"`
```
and in the literal `jc := jsonCourse{ ... URL: c.URL, ...}` add `URLEn: c.URLEn,`.

- [ ] **Step 4: Run all admin-app tests**

Run: `cd admin-app && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add admin-app/internal/validate/schema.go admin-app/internal/validate/validate_test.go
git commit -m "feat(admin): emit url_en in the feed JSON"
```

---

### Task 6: `url_en` editable in the admin-app course form

**Files:**
- Modify: `admin-app/internal/web/templates/courseform.gohtml` (after the "Course URL" label)
- Modify: `admin-app/internal/web/handlers_courses.go:133-136` (the `model.Course{...}` literal in `handleCourseSave`)
- Test: none — there is no `handlers_courses_test.go` (the web package tests are `draft_test.go`, `handlers_dates_test.go`, `handlers_propose_test.go`, `preview_test.go`, `session_test.go`); the field is a straight pass-through and is covered by Task 4's round-trip test plus `go vet`/`go test ./...`.

- [ ] **Step 1: Template**

After the `</label>` that closes the "Course URL" input add:
```html
    <label>English course page (optional)
      <input type="url" name="url_en" value="{{.Course.URLEn}}"
             placeholder="https://trainings.arc42.org/courses/msa/" aria-describedby="url-en-hint">
      <small id="url-en-hint" class="hint">Shown instead of the course URL on the English
        arc42 sites. Leave empty if there is no English page.</small>
    </label>
```

- [ ] **Step 2: Handler**

In `handleCourseSave`, extend the literal:
```go
		Credits: get("credits"), URL: get("url"), URLEn: get("url_en"), Trainers: parseTrainers(r),
```

- [ ] **Step 3: Verify**

Run: `cd admin-app && go build ./... && go vet ./... && go test ./...` — Expected: PASS.
Run: `grep -n url_en internal/web/templates/courseform.gohtml internal/web/handlers_courses.go` — Expected: one hit in each file.

- [ ] **Step 4: Commit**

```bash
git add admin-app/internal/web/templates/courseform.gohtml admin-app/internal/web/handlers_courses.go
git commit -m "feat(admin): optional English course page field on the course form"
```

---

### Task 7: README — feed field and the "next course" recipe

**Files:**
- Modify: `trainings.arc42.org-site/README.md` — section "JSON Feed" (after the paragraph ending "…`validate-trainings.yml`)."), and section "Maintaining training dates" (add a subsection at its end).

- [ ] **Step 1: Document the field**

In "## JSON Feed (the supported machine interface)", after the paragraph that ends with `(see [validate-trainings.yml](/.github/workflows/validate-trainings.yml)).` add:

```markdown
Every course carries a German `url` (its detail page on arc42.de) and may
carry an optional `url_en` — an English detail page on this site,
`https://trainings.arc42.org/courses/<id>/`. English consumers render
`course.url_en | default: course.url`; arc42.de ignores `url_en`.
```

- [ ] **Step 2: Document the recipe**

At the end of "## Maintaining training dates" add:

```markdown
### Adding an English course page

Course descriptions in German live on arc42.de (`/info-<id>/`). English
descriptions live here, one page per course, and are maintained by hand —
there is deliberately no DE↔EN sync. To add one (today only `msa` has one):

1. Create `_pages/courses/<id>.md` (`<id>` = the course id in
   `_data/trainings.yml`). Copy the front matter of `_pages/courses/msa.md`;
   set `translation_url:` to the German page on arc42.de so the masthead
   DE | EN switch and `hreflang` point there. Buttons use the site-wide
   `btn btn--primary` / `btn btn--inverse` classes.
2. Add `url_en: "https://trainings.arc42.org/courses/<id>/"` to that course in
   `_data/trainings.yml`.

The consumer sites already prefer `url_en`, so nothing else changes.
```

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: url_en and the English course page recipe"
```

---

### Task 8: English consumers prefer `url_en` (arc42.org, docs, faq) + `/learn/` link

**Files:**
- Modify: `arc42.org-site/_includes/subtle-ads/subtle-ads.html:87`
- Modify: `arc42.org-site/_pages/learn.md:58`
- Modify: `docs.arc42.org-site/_includes/training-dates.html:87`
- Modify: `faq.arc42.org-site/_includes/training-dates.html:87`

**Interfaces:**
- Consumes: `course.url_en` from each site's `_data/trainings.json` (arrives via the refresh workflow after Task 3 is merged; until then `default:` keeps today's behaviour).

- [ ] **Step 1: The three includes**

In each of the three files, replace
```liquid
href="{{ course.url }}">{{ course.short_title }}</a>
```
with
```liquid
href="{{ course.url_en | default: course.url }}">{{ course.short_title }}</a>
```
Also add one comment line to each file's header comment block (each has one; the arc42.org one starts with `Build-time training dates from _data/trainings.json`), e.g. `· course title links prefer the English detail page (url_en), falling back to url`.

- [ ] **Step 2: `/learn/` on arc42.org**

`arc42.org-site/_pages/learn.md:58`: change `[Mastering Software Architectures](https://trainings.arc42.org)` to `[Mastering Software Architectures](https://trainings.arc42.org/courses/msa/)`. Leave line 102 ("See all dates & register") as is.

- [ ] **Step 3: Verify each site builds with a `url_en`-bearing data file**

For each of the three repos: temporarily inject the field into the local data copy, build, check, restore.
```bash
python3 - <<'EOF'
import json;p='_data/trainings.json';d=json.load(open(p))
for c in d['courses']:
    if c['id']=='msa': c['url_en']='https://trainings.arc42.org/courses/msa/'
json.dump(d,open(p,'w'),indent=2)
EOF
make site        # arc42.org and docs have this target; faq has no `site` target — use: docker compose run --rm jekyll bundle exec jekyll build   (check faq's Makefile `dev` target for the exact service name and mirror it)
```
Then check the page that renders the schedule — arc42.org: `_site/index.html` (home.md includes subtle-ads); docs: `_site/index.html` (page layout includes subtle-ads → training-dates); faq: `_site/index.html` (page layout includes training-dates):
```bash
grep -o 'href="https://trainings.arc42.org/courses/msa/">Mastering[^<]*' _site/index.html | head -1
grep -c 'href="https://www.arc42.de/info-improve/"' _site/index.html
git checkout -- _data/trainings.json
```
Expected: one match with the new URL, IMPROVE still links arc42.de (count ≥ 1 — the schedule shows the next 8 dates and IMPROVE Nov 2026 is among them; if it is not, check any other course's arc42.de link instead).

- [ ] **Step 4: Commit — only the named files**

arc42.org has unrelated uncommitted changes. Commit by path:
```bash
cd arc42.org-site && git commit -m "feat: training schedule links English course page when the feed has url_en" -- _includes/subtle-ads/subtle-ads.html _pages/learn.md
cd ../docs.arc42.org-site && git commit -am "feat: training schedule links English course page when the feed has url_en"
cd ../faq.arc42.org-site  && git commit -am "feat: training schedule links English course page when the feed has url_en"
```
(For arc42.org, if the repo is on `main`, create branch `feat/en-course-link` first: `git switch -c feat/en-course-link` — the unrelated dirty files travel along in the working tree, unharmed. Same for docs/faq: `git switch -c feat/en-course-link`.)

---

### Task 9: arc42.de — redirect stub and card link

**Files:**
- Modify: `arc42.de-site/_pages/info-msa-engl.md` (replace whole file)
- Modify: `arc42.de-site/_includes/timeline_msa_online.html:26`
- Read: `arc42.de-site/_pages/more.md` (redirect pattern), `arc42.de-site/_layouts/redirect.html`

- [ ] **Step 1: Turn the page into a redirect**

Replace the entire content of `_pages/info-msa-engl.md` with:
```markdown
---
permalink: /info-msa-EN/
layout: redirect
redirect_to: https://trainings.arc42.org/courses/msa/
redirect_label: "Mastering Software Architectures (English course page)"
sitemap: false
---
```

- [ ] **Step 2: Card link**

`_includes/timeline_msa_online.html:26`: replace `<a href="/info-msa-EN/">` with `<a href="https://trainings.arc42.org/courses/msa/" hreflang="en">`.

- [ ] **Step 3: Verify**

Run: `grep -rn "info-msa-EN\|info-msa-engl" _pages _includes _data _layouts | grep -v "^_pages/info-msa-engl.md"` — Expected: no output.
Build the site (`make site` or the repo's documented command — check `CLAUDE.md`/`Makefile`), then:
```bash
grep -o 'http-equiv="refresh" content="0; url=[^"]*"' _site/info-msa-EN/index.html
```
Expected: `content="0; url=https://trainings.arc42.org/courses/msa/"`.

- [ ] **Step 4: Commit**

```bash
git add _pages/info-msa-engl.md _includes/timeline_msa_online.html
git commit -m "feat: /info-msa-EN/ redirects to the English course page on trainings.arc42.org"
```

---

### Task 10: End-to-end check on trainings.arc42.org

**Files:** none modified.

- [ ] **Step 1: Full local verification**

In `trainings.arc42.org-site`:
```bash
ruby scripts/validate_trainings.rb
(cd admin-app && go test ./...)
make site && make check-links
```
Expected: `OK: 4 courses, …`; all Go tests PASS; html-proofer passes.

- [ ] **Step 2: Rendered-page checks**

```bash
test -f _site/courses/msa/index.html && echo page-ok
grep -c 'hreflang="de" href="https://www.arc42.de/info-msa/"' _site/courses/msa/index.html
grep -c 'href="/courses/msa/"' _site/index.html
python3 -c "import json;d=json.load(open('_site/api/trainings.json'));print(sorted(c['id'] for c in d['courses'] if 'url_en' in c))"
```
Expected: `page-ok`; `≥1`; `≥1`; `['msa']`.

- [ ] **Step 3: Report**

Summarise per repo: branch, commits, what was verified. Nothing is pushed or PR'd in this plan — that is the owner's step (trainings PR must merge before consumers show the new link; consumers only need their weekly refresh / dispatch afterwards).
