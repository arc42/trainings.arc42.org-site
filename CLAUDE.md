# CLAUDE.md — working in this repository

Orientation for coding agents. [`README.md`](/README.md) is the long-form
documentation and stays the source of truth; this file is the short list of
things that are easy to get wrong and expensive to get wrong. Where the two
disagree, the README wins — and fix this file.

## What is here

Two programs, two lifecycles: **the site** (Jekyll, static, GitHub Pages) and
**the admin app** (Go, one container on fly.io, source under `admin-app/`).
[`admin-app/README.md`](/admin-app/README.md#how-it-works) covers the second.

## Building and checking

Everything runs in Docker — no local Ruby needed. `make help` lists every
target; [README §Local development](/README.md#local-development) explains them.
The four that matter: `make dev` (server on :4000), `make site`, `make
check-links` (html-proofer), `make app-check` (the Go tests, vet and gofmt that
CI gates the deploy on).

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

- **One Formspark form per language.** `_includes/registration-form.html` posts
  to `AIKiYyJP` (DE) or `Tq1M7LqmX` (EN), each with its own Botpoison public
  key. Formspark allows exactly one autoresponder template per form and no
  hidden field overrides it per submission, so the form id decides which
  language of confirmation email the registrant receives. The templates live in
  the Formspark dashboard, not in this repo. Never collapse the two ids into one
  "to remove duplication".
- **The `<option value>` is the booking code, byte-for-byte.** It is what
  reaches the back office. Never decorate it; language hints go in the label.
- **`_data/trainings.yml` is the single source of truth** for dates, and the
  admin app writes it by pull request. Booking codes must stay unique.
- **The bilingual front-matter contract** (`lang`, `translation_url`, `locale`,
  the hreflang triple, per-language nav) is described in
  [README §Languages](/README.md#languages). `_pages/home.html` and
  `_pages/home-de.html` are structural twins — change one, change the other.
- **The `exclude:` list in `_config.yml` is load-bearing, not cosmetic.**
  Without the `admin-app` entry, the Go source is served publicly. Anything not
  excluded is copied into `_site/` and published.

## House style

Follow the surrounding file. Two habits this repo does keep:

- Comments explain **why**, especially where a constraint is external
  (Formspark, Botpoison, fly.io) and cannot be inferred from the code. The
  Liquid `{%- comment -%}` blocks in `_includes/` are the pattern.
- Design decisions are written down under `docs/superpowers/specs/`. Read the
  relevant spec before reworking a feature it covers.
