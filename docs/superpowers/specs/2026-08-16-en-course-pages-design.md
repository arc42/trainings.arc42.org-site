# English course detail pages — design

**Date:** 2026-08-16
**Status:** approved (brainstorming complete)
**Scope:** where English course descriptions live, how the feed carries their
URL, and how the English arc42 sites (arc42.org, docs, faq) get there from the
schedule. First course: MSA (Mastering Software Architectures). Req4Arc and
IMPROVE follow the same recipe once English online dates exist for them.

Related: `2026-08-08-trainings-admin-design.md` (the admin app that edits
`_data/trainings.yml`; it has to accept the new field, nothing more).

---

## 1. Problem

`_data/trainings.yml` → `/api/trainings.json` is the single source of truth for
training dates. Every course has exactly one `url` and it points at a **German**
detail page on arc42.de (`https://www.arc42.de/info-msa/`, `/info-improve/`,
…). The English arc42 sites (arc42.org `_includes/subtle-ads/subtle-ads.html`,
docs and faq `_includes/training-dates.html`) render `course.url` verbatim, so
an English reader who clicks the course title on an English site — including
on an English-held date such as `26-09 MSA-EN` — lands on a German page on a
different domain.

An English MSA description already exists (`arc42.de/info-msa-EN/`,
`arc42.de-site/_pages/info-msa-engl.md`), but the feed does not know it. Only
one hand-written timeline card links it (`_includes/timeline_msa_online.html`
here and on arc42.de). IMPROVE, Req4Arc and ADOC have no English page.

## 2. What this is not

- **Not** a translation of arc42.de. German course pages stay on arc42.de,
  unchanged, and remain the canonical German source. arc42.de does not need
  English pages.
- **Not** an automated DE↔EN sync. Course descriptions change rarely; both
  languages are maintained by hand. No CI check, no content hashing, no
  drift warnings.
- **Not** all courses. Only MSA now. The design must make adding
  `/courses/req4arc/` a two-step job (page file + one feed field), and it must
  not need anything else.
- **Not** a change to date-level links. `dates[].url` keeps pointing at
  `arc42.de/termine#<id>` (or wherever it points today), including for
  English dates. Follow-up, see §8.

## 3. Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | English course pages live on **trainings.arc42.org**, at `/courses/<course-id>/` | This site is already bilingual (`lang`, `translation_url`, hreflang triple, DE\|EN masthead switch, `/registration/` vs `/anmeldung/`). Schedule → course details → registration becomes one origin, one repo. arc42.org has none of that plumbing and would need it ported; arc42.de would keep English readers on a German-chrome site. |
| D2 | `<course-id>` is the feed's `courses[].id` (`msa`, `req4arc`, `improve`, `adoc`) | One identifier for the card type, the feed entry and the URL. A future DE twin would be `/de/courses/<id>/`, matching the site's `/` vs `/de/` convention. |
| D3 | The feed gets an **optional** `courses[].url_en`; `url` stays required and German | Nothing that consumes `url` today breaks. English consumers switch to `url_en \| default: url`; arc42.de keeps `url`. Deriving the URL from the id inside consumers was rejected: the feed should stay the only place that knows where pages live. |
| D4 | The English page's `translation_url` is the **absolute** arc42.de URL of the German page, on arc42.de's canonical host (no `www` — `https://arc42.de/info-msa/`) | `masthead.html` and `head.html` pass `translation_url` through `relative_url` / `absolute_url`, both of which leave absolute URLs untouched, so the DE\|EN switch and `hreflang="de"` point at the German original for free. Non-reciprocal hreflang is harmless. |
| D5 | Content is the existing `info-msa-engl.md` text, ported 1:1, typos fixed | It is the approved English description; rewriting it is out of scope. Fixed: "archtects", "architeftures", "devolping", "organisize", "an architects", "software architectures" (in "Tasks, role and responsibilities of…") → "software architects". |
| D6 | Action buttons reuse this site's `.btn` classes (`btn--primary`, `btn--inverse`); **no CSS ported** from arc42.de, no new SCSS | arc42.de's `course-actions` / `btn--arc42-*` are that site's design; this site's design comes from meta.arc42.org and its own `_sass/oneflow/`. |
| D7 | The flyer PDF and the terms page stay on arc42.de and are **linked** | One flyer, one place. `/terms-en/` is what this site already links from the timeline cards. |
| D8 | **No masthead nav entry** "Courses" | One page does not justify a menu item; the page is reached from timeline cards, the feed consumers and `/learn/` on arc42.org. Revisit when there are three. |
| D9 | `arc42.de/info-msa-EN/` becomes a **redirect stub** to the new page | Old links and bookmarks keep working; arc42.de already has `layout: redirect` (used by `/more/`, `/articles/`, `/videos/`) — meta refresh + canonical. |

## 4. The page

`_pages/courses/msa.md`:

```yaml
---
title: "Mastering Software Architectures"
layout: page
permalink: /courses/msa/
lang: en
translation_url: https://arc42.de/info-msa/
description: "Three-day iSAQB CPSA-F training by Peter Hruschka and Gernot Starke — content, target audience, certification."
header:
  overlay_color: "#743442"
---
```

Body, in order:

1. Action row (top): **Register** → `/registration/` · **Course description
   (PDF)** → `https://www.arc42.de/downloads/flyer-msa-EN.pdf` (new tab) ·
   **Terms** → `https://www.arc42.de/terms-en/`.
2. The ported text: intro line, "You can expect…", short topic list, "Target
   Audience", "Ideal Preparation for the iSAQB CPSA-F exam" (with the
   certification note), "Extensive Description of Content" with its
   subsections.
3. Action row (bottom): **Register** → `/registration/` · **All dates** →
   `/#training-dates`.

The action row is a plain `<p class="course-actions">` using this site's
site-wide `.btn` classes (`btn btn--primary` for *Register*, `btn btn--inverse`
for the others — the same pair `registration-form.html` uses). The
`.button`/`buttonMSA` classes are scoped inside `.timeline` and are not used
here. No SCSS is added.

The `page` layout is used as-is (masthead with DE|EN switch, footer, hreflang).

## 5. The feed

`_data/trainings.yml`, course `msa`:

```yaml
    url: "https://www.arc42.de/info-msa/"
    url_en: "https://trainings.arc42.org/courses/msa/"
```

Everything that describes or validates the feed learns the field, all as
*optional string, https URL*:

- `api/trainings.schema.json` — `courses[].url_en`, same constraints as `url`.
- `scripts/validate_trainings.rb` — accept and, if present, require https.
- `admin-app` — `model.Course.URLEn`, parsed by `yamldoc/parse.go`, rendered
  by `yamldoc/render.go` (right after `url`, always quoted, omitted when
  empty), emitted by `validate/schema.go`'s `FeedJSON` as `url_en` (omitempty),
  editable as an optional field on `courseform.gohtml` / `handlers_courses.go`.
  This matters because `Doc.UpdateCourse` re-renders a course head from the
  model: without the field, editing MSA in the admin app would silently drop
  `url_en`. The https rule is enforced by the JSON schema and, as an advisory
  warning, by `validate/warnings.go` — the same plain-http check `url` gets.
- `api/trainings.json` is a Jekyll build product of the YAML — no hand edit.

Consumers (three files, one-line change each, `course.url_en | default: course.url`):

- `arc42.org-site/_includes/subtle-ads/subtle-ads.html`
- `docs.arc42.org-site/_includes/training-dates.html`
- `faq.arc42.org-site/_includes/training-dates.html`

Their `_data/trainings.json` copies refresh via the existing weekly workflow /
dispatch, so the new field arrives without further action. arc42.de is not
touched.

## 6. Links to the old English page

| Where | Today | After |
|---|---|---|
| this repo `_includes/timeline_msa_online.html` | `https://www.arc42.de/info-msa-EN/` | `/courses/msa/` |
| `arc42.de-site/_includes/timeline_msa_online.html` | `/info-msa-EN/` | `https://trainings.arc42.org/courses/msa/` |
| `arc42.de-site/_pages/info-msa-engl.md` | full page | `layout: redirect`, `redirect_to: https://trainings.arc42.org/courses/msa/`, `sitemap: false` |
| `arc42.org-site/_pages/learn.md` | "Mastering Software Architectures" → `https://trainings.arc42.org` | → `https://trainings.arc42.org/courses/msa/` |

## 7. Adding the next course (documented in README)

1. Create `_pages/courses/<id>.md` with the front matter of §4
   (`translation_url` = the German page on arc42.de).
2. Add `url_en: "https://trainings.arc42.org/courses/<id>/"` to that course in
   `_data/trainings.yml`.

That is the whole recipe. The consumers already prefer `url_en`.

## 8. Follow-ups (explicitly out of scope)

- Date-level `url` for English-held dates could point at
  `https://trainings.arc42.org/#<date-id>` instead of `arc42.de/termine#…`.
- Once three courses have English pages: a "Courses" masthead entry and a
  `/courses/` index.
- Moving the German course pages here as `/de/courses/<id>/` (arc42.de
  redirecting) would make DE and EN twins in one repo. Not planned.

## 9. Verification

- This repo: `make site` builds; `make check-links` passes (new internal
  links, external arc42.de links); `ruby scripts/validate_trainings.rb` passes;
  `go test ./...` in `admin-app/` passes.
- Rendered `/courses/msa/` shows the DE|EN switch pointing at
  `https://www.arc42.de/info-msa/`, and `<link rel="alternate" hreflang="de">`
  with the same URL.
- `_site/api/trainings.json` contains `url_en` for `msa` only.
- arc42.org, docs, faq: Jekyll build passes with a locally patched
  `_data/trainings.json` that contains `url_en`; the MSA course link renders
  the new URL, other courses still render `arc42.de/info-*`.
- arc42.de: build passes; `/info-msa-EN/` renders the redirect stub.
- **Rollout order:** this repo merges and GitHub Pages publishes `/courses/msa/` FIRST; the consumer sites (arc42.org, docs, faq) any time after; arc42.de (whose `/info-msa-EN/` becomes a redirect to the new page) LAST — otherwise the redirect points at a page that does not exist yet.
