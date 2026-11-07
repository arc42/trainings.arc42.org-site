# New course types without a developer

**Date:** 2026-11-06
**Status:** implemented and merged (PR #79, 6 Nov 2026)
**Course renamed (7 Nov 2026):** the course written as MAS / `mas` throughout
this document goes live as **`maispec`** (booking codes `YY-MM MAISPEC`),
because `mas` is easily misread as `msa`. The text below keeps the planning
name; read `maispec` wherever it says MAS.
**Scope:** `admin-app/` (pre-PR check, messages, warnings, course form),
the site's timeline cards (`_includes/timeline_*.html`), `_data/trainings.yml`,
`api/trainings.json` and `api/trainings.schema.json` (additive fields, one
retired data key), `scripts/validate_trainings.rb`,
`scripts/verify_consumers.sh`, README / CLAUDE.md wording.
**Out of scope:** changing the consumer sites (arc42.de, arc42.org, docs,
faq, examples, softwareknigge, gernotstarke.de, and the new systems42.org).
Gernot adjusts them separately, starting the same day; §9 is the handover
for that.

---

## 1. What happened

On 6 Nov 2026 Peter Hruschka tried to add a new course in the admin app:
**MAS, Mastering AI-Driven Specification**, two dates in January 2027,
held in German, trainers Peter Hruschka and Peter Gerstbach, info page
`https://www.systems42.org/training`. MAS is **not** iSAQB accredited. He
could not open a pull request. Screenshots are in `todo/`.

Two things went wrong:

1. **A false block.** He saved the course before adding dates, which is a
   normal order of work. On *Review & propose* the app refused with
   `courses/4/dates: expected array, but got null` and the line *"Fix these
   before opening a pull request — CI would reject them anyway."* Both are
   wrong. The YAML the app writes is `dates: []`, which CI accepts
   (`scripts/validate_trainings.rb` checks `is_a?(Array)`). The null comes
   from the app's own feed rendering, `validate.FeedJSON`
   (`admin-app/internal/validate/schema.go`): a course with no dates leaves
   `jc.Dates` a nil slice, and `encoding/json` writes that as `null`. So the
   app's own check was stricter than CI. It blamed the operator, gave a JSON
   pointer as the location, and offered no way past it.

2. **A warning only a developer can act on.** On the course form: *"the site
   has no timeline card for "mas" yet - add _includes/timeline_mas.html (and
   timeline_mas_online.html) plus their cases in timeline_course.html…"*.
   Peter cannot do that. The warning also says *"Nothing here blocks the
   save"*. Had he saved and the PR gone through, CI would have failed it
   (`validate_trainings.rb` errors on a missing card template). Without that
   check, the dates would have been in the feed and invisible on both home
   pages, the way `improve-apr-2027` once shipped.

## 2. Intent

A trainer must be able to create a **new course type with its dates entirely
in the admin app**, open the pull request, and see the course on both home
pages and in the registration form once it is merged. No developer step
comes first. (Gernot, 6 Nov 2026: "everything, including dates, without a
developer.")

Over time, every course is rendered by **one data-driven card**. The
hand-written per-course templates are retired gradually. Until a course is
migrated, its own template wins. The data-driven card is also the fallback
for any course without one. (Gernot: "one data-driven card for every
course, gradually. Reasonable fallback.")

Not every course belongs on every site. MAS is a systems42 course. The
operator decides **where a course is advertised**, in the admin app.

### Goals

- G1. Every order of work that is valid in real life saves and proposes. A
  course with no dates yet is valid.
- G2. The admin app's pre-PR check never blocks something CI would accept.
- G3. Every message the operator sees names the course and date by their
  titles, says what is wrong in plain words, and says what *they* can do. No
  file paths, no JSON pointers, no Liquid. If the operator cannot fix it,
  the message says so and says it is not their fault.
- G4. Any course, including one created minutes ago, renders as a card on
  `/` and `/de/` without a template of its own.
- G5. The iSAQB distinction is visible: an accredited course shows its
  certification on the card, and a non-accredited course shows nothing
  about iSAQB.
- G6. Course descriptions are explicitly per language, and a page never
  shows a description in a language the course does not have one in.
- G7. The operator chooses which consumer sites advertise a course, and the
  consumer check in this repository respects that choice.

### Non-goals

- Changing the consumer sites (§9 hands that over).
- Migrating the existing four courses to the data-driven card. This project
  makes migration possible; each course is migrated in a later small change
  (§7).
- A course detail page on trainings.arc42.org for new courses. `url` and
  `url_en` can point anywhere, as MAS's points to systems42.org.
- Editing card layout or colours from the admin app.
- Moving GitHub Pages to an Actions-based deploy (see §5 B4).

### Anti-goals (a failure even if it technically works)

- A message the operator cannot act on, or one that blames them for the
  app's own fault.
- A valid, merged date that renders as nothing on a home page.
- A sentence written in one language rendered as if it were the other.
- Changing the type or meaning of an existing feed field (ADR-0004: additive
  is free, changing an existing field is not).
- A consumer check that goes red because a site correctly does not show a
  course it was never meant to show. A guard that cries wolf gets switched
  off.

## 3. Decisions taken by Gernot (6 Nov 2026)

- **No explicit "iSAQB accredited" flag.** A course is accredited if and
  only if `certification` is non-empty. All four existing courses have one;
  MAS has none.
- Data-driven card for all courses, introduced gradually, and a fallback
  from day one.
- **Descriptions are explicit per language**: `blurb_de` and `blurb_en`.
  MAS has a German description only, for now.
- **Each course says which consumer sites advertise it.** MAS: arc42.de,
  arc42.org and systems42.org. Whether other sites show it is decided later.
  trainings.arc42.org always shows every course. arc42.de must be *able* to
  render every course, as trainings.arc42.org can.
- The admin app first; the consumer sites are adjusted separately.

## 4. Part A: the pre-PR check never blocks what CI accepts

### A1. Fix the null

`validate.FeedJSON` emits `"dates": []` for a course without dates
(initialise `jc.Dates` to an empty slice). Check every other slice the shim
emits as a required array the same way. `trainers` is required at course
level, so an empty trainer list must also render as `[]`, not `null`.

### A2. Make "stricter than CI" a test failure, not a user's afternoon

Add a test that takes YAML files CI accepts and requires the app's check to
pass them too. At minimum: a course with `dates: []`, a course with no
`certification`, no description at all, no `url_en`, no `credit_points`,
no `show_on`, and a course with an id the site has never seen. Put them in
`admin-app/internal/validate/testdata/`. Where possible, the test also runs
`scripts/validate_trainings.rb` on the same fixture, so both gates are
proven to agree. If Ruby isn't available in the Go test run, the fixtures
are also listed in the Ruby validator's CI job; how to wire that is the
builder's call.

### A3. Say who is at fault

Every problem shown on *Review & propose* is one of two kinds, and the page
says which:

- **Something in your entries.** The operator can fix it on a form. The
  message names the course (short title) and date (start date and city, or
  "online"), names the field by its form label, and links to that form.
  Example: *"MAS, 13 Jan 2027 online: the end date (12 Jan 2027) is before
  the start date. → Edit this date"*.
- **Something the admin app got wrong.** Anything the operator could not
  have caused: the schema could not be fetched, the feed could not be
  rendered, or the schema rejected something on a field no form sets. The
  message: *"This is not caused by your entries, and your draft is saved.
  The admin app could not check it before opening the pull request. Please
  send Gernot a screenshot of this page."* The raw technical detail (JSON
  pointer, library message) is in a collapsed `<details>` underneath, for
  whoever fixes it.

How a schema problem is put in the right pile: translate its JSON pointer
(`/courses/4/dates/1/start`) back to the course and date it points into, and
to the form field that sets that property. If no form field sets it, it is
the app's fault. The builder decides how to keep the pointer-to-field map
next to the form definitions so they cannot drift apart.

Remove *"CI would reject them anyway"*. Use it only where it is true. Once
A2 holds, that means problems of the first kind only.

The four cross-field rules in `validate/rules.go` already carry a field
(`dates.<id>.end`). They get the same treatment: course and date by name,
the form label, and a link.

### A4. Warnings speak to the operator

A warning names something the operator can change, or it isn't shown.

**Removed:** the two card-template warnings in `validate/warnings.go`. G4
makes them untrue. The mirrored `coursesWithPublicCard` /
`coursesWithOnlineCard` sets and `HasCardTemplate` / `HasAnyCardTemplate`
go with them.

**Replaced** (my call, from the builder check): the app keeps one short
list of the courses that still have a **hand-made card**, today `msa`,
`improve`, `req4arc` and `adoc`. Their cards ignore the descriptions in the
data, so description warnings would be untrue for them. For those courses
the course form shows one info line instead: *"This course still has its
own hand-made card on the home pages. Changes to its descriptions show up
there once it is switched to the standard card."* Description warnings are
not shown for them. The list shrinks as courses are migrated (§7). If it
goes stale, the cost is a wrong info line, never a wrong file. The
authoritative check is still the built site (§5 B4).

**Reworded** where they leak internals: *"the feed schema requires https"*
becomes *"links must start with https://"*.

**New:**

- A course without a hand-made card and with neither description: *"The
  home page cards for this course will show no description."*
- A course with only one description: an info line, not a warning, e.g.
  *"Only a German description: the English home page shows it in German
  too."*
- A new course with no dates when proposing: an info line, not a warning:
  *"MAS has no dates yet. It will appear on the home pages once it has
  one."*

## 5. Part B: one data-driven card

### B1. Dispatch

`_includes/timeline_course.html` keeps its `case` for the four hand-made
types. The `else` branch, today an HTML comment, renders the data-driven
card instead. To do that the card needs the course and the date, not only
the pre-built strings. `timeline_auto.html` passes `course=c date=d` to
`timeline_course.html`, which forwards them to the data-driven card. The
existing parameters (position, anchor_id, date label, status note,
waitlist label, hide_register, few_seats, pricing, credits, page_lang,
delivery_lang) are passed to it as they are to the hand-made cards. So
status, price and credits wording stays decided once, in
`timeline_auto.html`.

The name of the new include is the builder's call (suggested:
`_includes/timeline_card.html`). Every variable it assigns is prefixed
(e.g. `tc_`), per the scope rule in CLAUDE.md.

### B2. What the card shows

Same container, classes and order as the hand-made cards, so it sits in
the timeline without visual seams. Top to bottom:

1. Date label and location (city, or nothing for online), then the
   delivery-language note, exactly as the hand-made cards do it.
2. Title: `course.title`. If `course.certification` is non-empty, a second
   line in parentheses, e.g. *(iSAQB CPSA-Foundation)*, as the MSA card
   does today. Nothing about iSAQB otherwise (G5).
3. Description (B3).
4. Trainers: the date's `trainers` if set, otherwise the course's, joined
   with " und " / " and ". The hand-made cards bake trainers into their
   blurb; the data-driven card shows them on their own line, labelled
   *Mit* / *With* (wording is the builder's call).
5. Status note, few-seats line, price, credits: the strings handed down by
   `timeline_auto.html`, rendered exactly as in the hand-made cards. Credits
   only appear when `credit_points` is set, which in practice means an
   accredited course.
6. Buttons. *More info* links to `url` on the German page. On the English
   page it links to `url_en` if set; otherwise to `url`, with the label
   *More info (in German)*, as the hand-made cards do. *Register* /
   *Anmeldung* links like the hand-made cards do
   (`/anmeldung/?kurs=<anchor id>` or `/registration/?kurs=<anchor id>`),
   and obeys `hide_register` and `waitlist_label`.

No per-course colours. The hand-made cards have no per-course button styles
in `_sass` either (`buttonMSA` etc. have no rules), so nothing is lost.

### B3. Descriptions, explicit per language

`blurb` is prose in the data file, today English in every course, and the
data-driven card cannot know which page it suits. Gernot: be explicit.

**In `_data/trainings.yml`** (and `model.Course`, the YAML reader/writer
including the surgical apply path, the course form):

- `blurb_de`: German description, optional.
- `blurb_en`: English description, optional.
- The key `blurb` is **retired in the data file**. The four existing values
  move to `blurb_en` in this project. `validate_trainings.rb` rejects
  `blurb` by name with a message pointing to the two new keys, the way it
  rejects `pricing`, `credits` and `few_seats`. `blurb_de` for the four
  existing courses is filled in when each is migrated (§7), not now.
- The course form shows two clearly labelled fields: *Description
  (German)* and *Description (English)*.

**In the feed** (`api/trainings.json`, `api/trainings.schema.json`):

- `blurb_de` and `blurb_en` are published as new optional fields
  (additive).
- `blurb` stays in the feed with its type unchanged, now **generated**:
  `blurb_en` if present, otherwise `blurb_de`, omitted if neither. Consumers
  that render `blurb` verbatim keep working. The schema description marks it
  deprecated in favour of the two explicit fields, like `credits`.

**On the card:** the description in the page's language if the course has
one; otherwise the other one; otherwise none. MAS shows its German
description on both `/de/` and `/`, next to the "held in German" note the
card already carries. A German text is then shown as German, which is not
the anti-goal; English shown as if it were German would be.

**House rule wording:** the *No prose in `_data/trainings.yml`* rule in
CLAUDE.md, README and the header comment of `_data/trainings.yml` becomes:
no prose without a stated language. A field that says which language it is
in (`blurb_de`, `blurb_en`) is allowed.

### B4. Guardrails that change

- `scripts/validate_trainings.rb`: the "no card template" error is removed.
  A missing template is no longer a problem, because the data-driven card
  covers it.
- What replaces it is a check of the **output**: after a site build, every
  upcoming, non-cancelled date's anchor id must appear in both
  `_site/index.html` and `_site/de/index.html`. This catches any future way
  a date can render as nothing, not only a missing template.
- Where it runs (my call, from the builder check): as a `make` target and
  as a **status check on pull requests** that touch `_data/trainings.yml`
  or `_includes/`. The site is published by GitHub Pages in legacy mode,
  which runs no custom checks, so the check cannot stop publishing itself.
  It can stop a merge once it is marked *required* in the repository's
  branch protection. That is a settings switch for Gernot, listed in the
  plan's last step, not code.
- CLAUDE.md's bullet *"A card template's name is derived, not chosen"* is
  rewritten. A new course needs no template. A hand-made template
  overrides the data-driven card for its course while it exists.

## 6. Part C: where a course is advertised

### C1. The field

New optional course field **`show_on`**: a list of consumer site names.

- Absent means **every consumer site**. The four existing courses carry no
  `show_on` and behave exactly as today.
- Present means only those sites advertise the course. MAS:
  `show_on: ["www.arc42.de", "arc42.org", "www.systems42.org"]`.
- The site names are the names `scripts/verify_consumers.sh` already uses in
  `$SITES` (`faq.arc42.org`, `docs.arc42.org`, `arc42.org`,
  `examples.arc42.org`, `www.arc42.de`, `softwareknigge.arc42.org`,
  `www.gernotstarke.de`), plus `www.systems42.org`. One spelling,
  everywhere.
- trainings.arc42.org is **not** in this list and ignores the field: it
  shows every course, because registration happens there.
- Published in the feed as an optional array of strings (additive).
- An empty list `[]` is rejected by `validate_trainings.rb` and the app
  ("a course shown nowhere": leave the field out to mean everywhere, or
  name at least one site).

### C2. In the admin app

The course form gets a *Shown on* group: one checkbox per known site, plus
a free-text field for a site not yet in the list, the same pattern as the
trainer roster (`model.KnownTrainers`). Leaving every box unticked means
"all sites", and the form says so next to the group. The known-site list
lives in `admin-app/internal/model` next to the trainer roster.

### C3. The consumer check follows it

`scripts/verify_consumers.sh` expects a date on a site only if its course's
`show_on` is absent or names that site. Both its rules change:

- `chrono` and `trainer=X`: the "earliest N dates" are counted among the
  dates of courses shown on that site.
- `per-course`: only courses shown on that site get a row.

The other direction, "every date id on the page still exists in the feed",
is unchanged.

`www.systems42.org` is not added to `$SITES` in this project. It joins
when that site exists and reads the feed (§9).

## 7. Gradual migration (enabled here, done later)

Migrating a course to the data-driven card is a small change of its own:

1. Make sure its data carries what the hand-made card had baked in:
   `blurb_de`, `blurb_en`, `certification`, trainers.
2. Delete `timeline_<id>.html`, `timeline_<id>_online.html`, their two
   `when` cases, and the course's entry in the app's hand-made-card list
   (§4 A4).
3. Build the site and compare the course's cards on `/` and `/de/` before
   and after.

Order and timing are Gernot's. Not part of this project.

## 8. Acceptance: Peter's MAS, end to end

Done when, in the admin app with a GitHub test repository (`ghfake` or a
fork):

1. Creating course MAS (id `mas`, short title `MAS`, title *Mastering
   AI-Driven Specification*, no certification, no credit points, a German
   description only, info URL `https://www.systems42.org/training` for both
   languages, trainers Peter Hruschka and Peter Gerstbach, shown on
   arc42.de, arc42.org and systems42.org) saves without any warning about
   templates or cards. It shows the "only a German description" info line.
2. *Review & propose* with MAS and **no dates** shows no problem and opens a
   pull request (plus the "no dates yet" info line).
3. Adding two January 2027 dates, held in German, and proposing again
   opens a pull request.
4. CI on that pull request is green: `validate_trainings.rb`, schema, and
   the output check from §5 B4.
5. In the built site, both home pages show two MAS cards with the title,
   the German description, the trainers, the price, no iSAQB line and no
   credits. *More info* goes to systems42.org, and *Register* preselects
   the date. The registration form lists both dates.
6. The built feed has, for MAS, `blurb_de`, a generated `blurb` with the
   same text, no `blurb_en`, and `show_on`. For MSA it has `blurb_en` and
   a generated `blurb` byte-identical to today's, and no `show_on`.
7. `verify_consumers.sh`, run against a feed containing MAS, does not
   expect MAS dates on faq, docs, examples, softwareknigge or
   gernotstarke.de. (The test can feed it a fixture rather than the live
   sites; the builder decides how.)
8. A deliberately broken entry (end before start) shows a message of the
   first kind from §4 A3, with a working link to the date's form.
9. A simulated app fault (e.g. the schema fetch failing) shows a message of
   the second kind, with the detail collapsed.

Points 1–3, 8 and 9 are Go tests in `admin-app`. Points 4–6 are shown by a
site build plus grep against `_site/`. Per CLAUDE.md, a Liquid change is not
shown to work until the built HTML proves it.

## 9. Handover: consumer sites (adjusted separately, from 6 Nov 2026)

Not built here. Recorded so that work starts from the right facts:

- **arc42.de** must render every course, like trainings.arc42.org. Its own
  `_includes/timeline_course.html` still turns an unknown type into an HTML
  comment, so it needs the data-driven card (port this project's include,
  as `timeline_auto.html` was ported once). It pulls `_data/trainings.yml`
  directly, so it also sees `blurb_de` / `blurb_en` / `show_on` and no
  longer sees `blurb` in the data file. Any arc42.de template reading
  `course.blurb` from the YAML must switch.
- **Every consumer** must honour `show_on` (absent = show) and tolerate an
  unknown course id and a course with `dates: []`. Consumers reading the
  feed can keep using `blurb`; switching to `blurb_de` / `blurb_en` gets
  them the right language.
- **systems42.org** reads the feed and shows courses whose `show_on` names
  `www.systems42.org`. Once it is live, add it to `$SITES` in
  `verify_consumers.sh`.
- **Order matters for MAS**: until arc42.de renders the data-driven card,
  merging MAS dates leaves them invisible on arc42.de/termine, and
  `verify_consumers.sh` (which expects them there) goes red. Merge MAS dates
  after the arc42.de change, or accept that red run knowingly.

## 10. Left to the builder

- Name and exact markup of the data-driven card include, within §5 B2.
- How the pointer-to-form-field map is kept in step with the forms (§4 A3).
- How the app-vs-CI agreement test reaches Ruby (§4 A2).
- How `verify_consumers.sh` is tested against a fixture feed (§8 point 7).
- Exact wording of messages, within the rules of G3. UI text follows the
  admin app's current language (English).
