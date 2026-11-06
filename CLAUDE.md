# CLAUDE.md — working in this repository

Orientation for coding agents. [`README.md`](/README.md) is the long-form
documentation and stays the source of truth; this file is the short list of
things that are easy to get wrong and expensive to get wrong. Where the two
disagree, the README wins — and fix this file.

## What is here

Three programs, three lifecycles: **the site** (Jekyll, static, GitHub Pages),
**the admin app** (Go, one container on fly.io, source under `admin-app/`), and
**the registration service** (Go, its own fly app `arc42-registration`, source
under `registration-app/`, live since the go-live in Sep 2026; its state, registrations until they are
reported, confirmed ids and wrong-code counts, is in Turso, EU region). Since
the confirm-gated redesign (Oct 2026) the back office gets one mail per
registration, after the registrant confirmed, plus a weekly summary that
the service sends itself on the first request after Monday 07:00 (no
scheduler; a cron-job.org job pings `/healthz` then).
[`admin-app/README.md`](/admin-app/README.md#how-it-works) and
[`registration-app/README.md`](/registration-app/README.md) cover the two apps.

## Building and checking

Everything runs in Docker — no local Ruby needed. `make help` lists every
target; [README §Local development](/README.md#local-development) explains them.
The ones that matter: `make dev` (server on :4260), `make site`, `make
check-links` (html-proofer), `make app-check` and `make reg-check` (the Go
tests, vet and gofmt that CI gates each app's deploy on), plus `make
check-cards` (every upcoming date has a card on both home pages) and `make
check-consumers-test` (offline test of the consumer check).

There is **no unit-test suite for the site**. Verification is `make site` plus
assertions against the built HTML in `_site/`, and `make check-links`. When you
change a template, grep the built output to prove the change — do not claim a
Liquid change works because it looks right in the source.

`ruby scripts/validate_trainings.rb` validates `_data/trainings.yml`; CI runs it
on changes to that file, the schema, `scripts/**` and the two registration
includes (`.github/workflows/validate-trainings.yml`).

## Contracts that break silently

These are the ones where a wrong edit produces a page that looks perfectly fine
and is wrong anyway.

- **The form posts to the registration service** (`https://register.arc42.org/submit`).
  The visible field names differ per language and are mapped in
  `registration-app/internal/intake/intake.go` (`fieldNames`); rename a field in
  `_includes/registration-form.html` only together with that map, or the
  service silently loses it. The language of every mail comes from the hidden
  `language` field. (Until the go-live, Sep 2026: one Formspark form per
  language; reverting the go-live PR brings that back.)
- **The `<option value>` is the booking code, byte-for-byte.** It is what
  reaches the back office. Never decorate it; language hints go in the label.
- **`_data/trainings.yml` is the single source of truth** for dates, and the
  admin app writes it by pull request. Booking codes must stay unique.
- **No prose in `_data/trainings.yml` without a stated language.** Every plain
  value there is rendered on the German *and* the English pages, so a
  hand-written sentence is wrong on half of them. The one exception is the
  description, which exists per language: `blurb_de` / `blurb_en` (the old
  single `blurb` key is retired). `price`, `credit_points` and `seats_limited` are numbers and flags;
  the wording is built per language by `_includes/price-label.html`,
  `credits-label.html` and `money.html`. The retired string keys `pricing`,
  `credits` and `few_seats` are rejected by name in
  `scripts/validate_trainings.rb`, because the last time they existed the site
  advertised an expired early-bird price for weeks and showed German pricing to
  English readers. **Every date needs a `price`**: four card templates used to
  carry a hardcoded bilingual price sentence instead, so those courses had no
  price in the feed at all and the booking summary contradicted the card.
  `timeline_course.html` must forward `pricing=` to every template it
  dispatches to; three of the six did not, which was invisible until the price
  moved into the data.
- **`/api/trainings.json` publishes the retired keys too, generated.** Four
  consumers render them verbatim, so they keep their old names and old string
  types (ADR-0004: additive is free, changing an existing field is not). Never
  "clean up" by deleting them, and never turn them back into stored data: they
  are computed against the build date, which is what stops them going stale.
  `blurb` joins them: it is generated from `blurb_en`, else `blurb_de`, else
  omitted. An expired `early_bird` is omitted from the feed entirely.
- **A new course needs no template.** `_includes/timeline_card.html` is the
  data-driven card; `timeline_course.html` falls back to it for any course
  without a hand-made `timeline_<id>[_online].html` (today `msa`, `improve`,
  `req4arc`, `adoc`). A hand-made template overrides the data-driven card for
  its course while it exists, and ignores `blurb_de`/`blurb_en`.
  `scripts/validate_trainings.rb` no longer looks at `_includes/`; instead
  `make check-cards` (`scripts/check_home_cards.rb`, a CI step) fails when an
  upcoming date has no card on a built home page. The admin app keeps a copy
  of the hand-made list in `admin-app/internal/model/model.go`
  (`HandMadeCards`): shrink it when a course is migrated to the standard card.
- **`show_on` is optional and the feed publishes it.** A list of consumer site
  names; absent means every site, `[]` is an error. trainings.arc42.org ignores
  it. The names are spelled once in the schema and mirrored in
  `model.KnownSites`; `scripts/verify_consumers.sh` expects a course only on
  the sites `show_on` lists (`make check-consumers-test` is its offline test).
- **Jekyll includes share the caller's scope.** `assign` inside an include
  cannot shadow a caller's `for`-loop item, so every variable in
  `money.html`, `price-label.html` and `credits-label.html` is prefixed
  (`m_`, `pl_`, `cl_`). A bare name silently reads the caller's value; this
  cost a debugging round when a bare `c` collided with a loop over courses.
  Also: a literal Liquid tag written inside a `{%- comment -%}` block is still
  parsed, and an unclosed one is a build error.
- **The bilingual front-matter contract** (`lang`, `translation_url`, `locale`,
  the hreflang triple, per-language nav) is described in
  [README §Languages](/README.md#languages). `_pages/home.html` and
  `_pages/home-de.html` are structural twins — change one, change the other.
- **The `exclude:` list in `_config.yml` is load-bearing, not cosmetic.**
  Without the `admin-app` and `registration-app` entries, the Go source is
  served publicly. Anything not excluded is copied into `_site/` and published.
- **The registration service reads `/api/trainings.json`** to decide which
  booking codes it accepts (`registration-app/internal/feed`). A code missing
  from the feed is dropped silently; an empty feed counts as unavailable, not
  as "every code unknown". It is a feed consumer under ADR-0004 like the other
  sites. Its mail addresses (`BACKOFFICE_TO`, `TEST_RECIPIENTS`) are fly
  secrets, never `fly.toml`: this repository is public.

## House style

Follow the surrounding file. Two habits this repo does keep:

- Comments explain **why**, especially where a constraint is external
  (Brevo, Turso, fly.io) and cannot be inferred from the code. The
  Liquid `{%- comment -%}` blocks in `_includes/` are the pattern.
- Design decisions are written down under `docs/superpowers/specs/`. Read the
  relevant spec before reworking a feature it covers.
