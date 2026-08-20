# Trainings admin app — design

**Date:** 2026-08-08
**Status:** approved (brainstorming complete)
**Scope:** a CRUD web app for maintaining arc42 training dates, plus a freshness
pass on `meta.arc42.org/training-dates.md`.

Normative context: [ADR-0004](https://github.com/arc42/meta.arc42.org/blob/main/adr/0004-trainings-feed-is-a-contract.md)
(the feed is a contract), [ADR-0006](https://github.com/arc42/meta.arc42.org/blob/main/adr/0006-training-dates-single-source.md)
(single source of truth). Where this spec conflicts with an ADR, the ADR wins.

---

## 1. Problem

Training dates live in `trainings.arc42.org-site/_data/trainings.yml`. Changing a
date today means hand-editing YAML, running two Ruby scripts, and opening a PR.
That is error-prone (the file has load-bearing quoting conventions and a
`language:` field with no default) and it is the only step in an otherwise fully
automated pipeline that requires knowing the file's internals.

Two people maintain it — Gernot Starke and Peter Hruschka — roughly once or twice
a quarter.

## 2. What this is not

- **Not** a new source of truth. `_data/trainings.yml` stays authoritative.
- **Not** a replacement for the feed. `/api/trainings.json` keeps being generated
  by Jekyll at build time and served statically by GitHub Pages (ADR-0004).
- **Not** a general CMS. Four screens, two editable entities.

## 3. Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | The repo file stays the source of truth; the app is an editor for it | The whole publishing chain (CI → Pages → feed → four consumers) hangs off commits to that file. Nothing downstream changes. If the app dies, hand-editing works exactly as today. |
| D2 | Edits reach `main` as a **pull request**, never a direct commit | Merging requires an authenticated arc42 maintainer. Even a full compromise of the app yields, at worst, an unmerged PR. This is the primary security boundary — the app's own login is the second line of defence, not the only one. |
| D3 | Authentication is **GitHub OAuth** | Dissolves the user-zero problem: the accounts already exist. Authorization is a live `permissions.push` check against the repo, so there is no allowlist to maintain and revocation is a GitHub org action. Inherits GitHub's own 2FA/passkeys. The PR is opened *as the signed-in user* with their own short-lived token — no long-lived shared PAT in fly secrets, and a real audit trail. |
| D4 | The overview list is **dates, flat across all courses** | ~12 dates are live at a time and dates are what actually change. Course metadata changes maybe twice a year and gets its own rarely-visited screen. |
| D5 | Edits accumulate in a draft; **one editing session → one PR** | Keeps PR history meaningful and puts a diff preview between a typo and GitHub. |
| D6 | The draft lives **in server memory**, with no persistence | No database, no fly volume, no schema, no migrations, no backup story. A session is minutes long and the source of truth is untouched in GitHub throughout, so the worst case is re-entering a few fields. |
| D7 | The app is a **server-rendered Go web app**, not a desktop app | Wails builds a desktop binary and cannot be the fly.io production version; one codebase would become two. Go + `html/template` + htmx is one static binary deployed to fly.io, and matches the existing `status.arc42.org-site/go-app` precedent. |
| D8 | Validation reuses the repo's own schema; it is not re-implemented | The app fetches `api/trainings.schema.json` live from the repo, so it cannot drift. Only the four cross-field rules JSON Schema cannot express are hand-written. |
| D9 | YAML is edited **surgically**, node by node | A naive unmarshal→marshal round-trip destroys the comment header, reorders keys, drops deliberate quoting, and turns a one-line change into a 400-line diff — defeating the review gate D2 exists for. |

## 4. Prerequisite: retire the deprecated fragment — DONE (2026-08-08)

`.github/workflows/validate-trainings.yml` used to run
`ruby scripts/generate_subtle_ads.rb --check`, so **any** PR touching
`_data/trainings.yml` failed CI unless `_includes/_subtle-ads.html` was
regenerated in the same commit. The app cannot run Ruby on fly.io, so this
blocked app-generated PRs entirely.

Verified before removal: nothing anywhere still fetched
`arc42-subtle-ads-backend.vercel.app`. The four consumers render dates at build
time from their committed `_data/trainings.json`, with no htmx and no runtime
request, and the fragment was not included by any page in the trainings site
itself — a genuinely dead file.

**Resolution (executed, commit `0b78e51`):** Phase B is done — removed
`_includes/_subtle-ads.html`, `scripts/generate_subtle_ads.rb`, `api/index.js`,
and the `--check` step and path filters that referenced them. Deleting the
Vercel *project* is dashboard-only and remains open as a Todoist item.

One consequence worth recording: `weekly-refresh.yml` was kept, not deleted. Its
bot commit was never really about the fragment — it was what triggered the Pages
rebuild that re-evaluates the build-time `today` filters on the home-page
timeline and the registration forms. It now requests that build directly through
the Pages API, preserving the guarantee without a generated artifact.

## 5. Architecture

Location: `trainings.arc42.org-site/admin-app/`, mirroring
`status.arc42.org-site/go-app/`. Living in the repo it edits lets tests run the
real schema and the real Ruby validator against fixtures.

Stack: Go, `html/template` (`.gohtml`), htmx. No JS build step, no database,
single static binary.

### 5.1 Packages

| Package | Responsibility | Depends on |
|---|---|---|
| `internal/model` | `Trainings`/`Course`/`Date` structs; flatten to the date list; sort | — |
| `internal/yamldoc` | Parse `trainings.yml` into a `yaml.Node` tree; apply edits surgically; serialise | `model` |
| `internal/validate` | Schema check + cross-field rules | `model` |
| `internal/gh` | OAuth flow; permission check; read file; create branch, commit, PR | — |
| `internal/web` | Handlers, templates, session, draft store | all of the above |

Each package is independently testable and has no knowledge of `web`.

### 5.2 `yamldoc` — the load-bearing package

`_data/trainings.yml` carries meaning in its formatting:

- A nine-line comment header stating the single-source rule.
- Deliberate quoting: `start: "2026-09-29"`, `country: "NO"` (unquoted `NO` is
  the boolean `false` in YAML 1.1), `code: "26-12 MSA"`.
- Course and date ordering that humans rely on when scanning the file.

`yamldoc` therefore mutates only the touched nodes in the parsed `yaml.Node`
tree and leaves every untouched byte alone. New scalars are emitted with
`Style: yaml.DoubleQuotedStyle` for `start`, `end`, `code`, `country`.

**Consequence that matters:** a maintainer reviewing an app-generated PR sees
three changed lines, not a reformat. Minimal diffs are what make D2's review
gate real rather than ceremonial.

### 5.3 Data model

Two levels, mirroring the file: ~5 `courses`, each owning a list of `dates`.
The UI flattens dates for display (D4) but a date always belongs to exactly one
course, and moving a date between courses is an explicit edit.

Fields are exactly those in `api/trainings.schema.json`:

- **Course** — `id`, `short_title`, `title`, `url`, `trainers[]` (required);
  `blurb`, `certification`, `credits` (optional).
- **Date** — `id`, `code`, `start`, `end`, `language` (`de|en`), `format`
  (`public|inhouse|online`), `status` (`open|waitlist|full|cancelled`), `url`
  (required); `city` (required unless `format: online`), `country`, `trainers[]`,
  `pricing`, `few_seats` (optional).

`code` and `url` stay in the file — they serve different readers (`code` is the
Formspark option value that reaches the back office verbatim; `url` is the
`arc42.de/termine` anchor) — but the form no longer asks for either by hand.
`url` is derived from `id` on save, and `code` is prefilled from course, start
and language and remains editable, because a course starting 30 November can
still be booked as `27-12 MSA`. See `internal/model/derive.go`.

## 6. Screens

| Route | Purpose |
|---|---|
| `/` | Flat date list sorted by `start`. Upcoming first; past dates behind a "show past" toggle. Columns: code · course · dates · city/online · lang · status badge. Row actions: edit, duplicate, delete. |
| `/dates/{id}` | Detail form. Course selector. `city` required-ness toggles live off `format`. Inline validation. |
| `/dates/new` | Same form. **Duplicate** pre-fills from an existing date — the common real action (next year's MSA) — clearing `id`, `code`, `url`, `start` and `end`, which are what distinguish one run from the next. |
| `/courses`, `/courses/{id}` | Rare. Course-level fields. |
| `/propose` | Unified YAML diff, validation result, editable PR title/body, **Open PR**. |
| `/auth/github`, `/auth/callback`, `/auth/logout` | OAuth. |

### Form assistance

Both detail forms assist in three ways, none of which the operator has to
accept:

- **Help.** Every field carrying a rule has a server-rendered hint. `form.js`
  collapses each behind a `?` at the end of its label, named for what it
  explains. Without scripting the hints are simply visible.
- **Defaults.** A new date starts from its course's most recent one — city,
  country, pricing, trainers — and switching the course dropdown re-applies
  them. `code`, `id` and `url` derive from course, start and language.
  Derivation stops the moment the operator types in a field.
- **Warnings.** `validate.Rules` returns only what must block. `DateWarnings`
  and `CourseWarnings` return advisory findings — a past start, a nine-day
  course, `online` with a city, a status that hides the date from booking.
  The first submit shows them and relabels the button **Save anyway**; the
  second goes through. Errors are checked first and separately, so an
  acknowledgement can never carry an invalid entry past the blocking rules,
  and no warning reaches the pull-request gate.

A sticky bar appears whenever the draft is dirty:
*"3 unpublished changes — Review & propose · Discard"*. That bar carries
`hx-trigger="every 60s"` as a keepalive, so it pings fly's auto-stop only while
there is something to lose.

Visual style follows the family design system (`BRAND.md`, `DESIGN.md`): the
trainings signature hue `#a04c5e`, Libre Caslon Text headings, Atkinson
Hyperlegible Next body. Contrast is measured, never eyeballed (ADR-0002).
This is an internal tool, so the bar is "consistent and legible", not "designed".

## 7. Authentication and authorization

- GitHub OAuth web flow, scope **`public_repo`** — the repo is public, so full
  `repo` scope is unnecessary and would over-grant.
- On callback: `GET /repos/{owner}/{repo}` and require `permissions.push == true`.
  Otherwise render a 403 explaining that push access is required.
- **No allowlist anywhere.** Access is whatever GitHub says it is.
- Session: encrypted cookie, `Secure`, `HttpOnly`, `SameSite=Lax`, 8h TTL,
  holding login + access token. Encrypted with `SESSION_KEY`.
- One OAuth app, `arc42-trainings-admin-prod`, whose callback is
  `https://trainings-admin.arc42.org/auth/callback`. The app runs in one place,
  so there is one credential pair.

## 8. Publish flow

1. **Validate** the draft (schema + cross-field). Errors block; the diff screen
   shows them against the offending field.
2. **Re-read** `main`'s `_data/trainings.yml` blob SHA and compare it to the one
   loaded at session start. If it changed, show a conflict screen — do not
   silently overwrite. (Optimistic concurrency: if Peter merged something while
   you were editing, you find out here.)
3. **Serialise** via `yamldoc`.
4. **Create branch** `trainings-admin/YYYY-MM-DD-<slug>` from `main`'s SHA.
5. **Commit** the new content (Contents API `PUT`, with `branch` and base SHA).
6. **Open PR** against `main`. Title e.g. `Training dates: 3 changes`; body a
   per-change bullet list (added / updated / removed, by date `code`).
7. Show the PR link and clear the draft.

CI then runs `validate-trainings.yml` on the PR as the authoritative gate, and a
human maintainer merges.

## 9. Testing concept

D6 (no draft persistence) makes the test suite load-bearing rather than
decorative.

| Level | Test | Why it exists |
|---|---|---|
| Unit | **`yamldoc` identity test** — load the real `_data/trainings.yml`, serialise with no edits, assert **byte-identical** output | One test that catches every comment/quoting/ordering regression, forever |
| Unit | **`yamldoc` golden tests** — apply a known edit, diff against a golden file | The golden files also *prove* diffs stay minimal (§5.2) |
| Unit | **`validate` anti-drift test** — fixtures in `testdata/invalid/`, each run through both the Go validator and the real `scripts/validate_trainings.rb`, asserting they agree on accept/reject | The only guard against D8's duplication drifting. Skipped with `t.Skip` if Ruby is absent |
| Unit | `model` — flatten, sort, course membership | — |
| Integration | **`gh`** — against `httptest`, asserting the exact API call sequence and payloads | No network in tests; pins the publish flow's contract |
| Handler | **`web`** — `httptest` + a fake `gh`: list → edit → propose, plus the conflict path and the 403 path | Covers the flows a user actually performs |
| Manual | Sign in to the deployed app and publish, then close the PR unmerged | The one flow no test can stand in for: real GitHub, real OAuth, a real PR. Also where draft loss is deliberately exercised, by letting the machine idle out mid-session |

No browser/e2e tests — four screens do not justify the maintenance.

## 10. Deployment — the only environment

The app runs in exactly one place: fly.io, at
<https://trainings-admin.arc42.org>. It is developed by pushing, not by being
started somewhere else. `go test ./...` runs the suite anywhere Go 1.23 is
installed; a push to `main` touching `admin-app/**` runs the same tests in CI
and then deploys.

Deployment is CI's job, but not CI's alone: `make fly-deploy` (root `Makefile`)
runs the same checks and then `flyctl deploy --remote-only` from a maintainer's
machine, for when Actions is unavailable or a branch needs trying on the real
app — the only way to exercise GitHub OAuth and a real PR, since there is no
local mode. It builds from the working tree, so it ships unreviewed code and the
next push to `main` replaces it; the target says so and asks before proceeding.
`make fly-status`, `fly-logs`, `fly-releases` and `fly-secrets` cover the
read-only side. See `admin-app/README.md`.

Multi-stage Dockerfile → scratch. `fly.toml`: app `arc42-trainings-admin`,
primary region `ams`, `internal_port = 8080`, `force_https`,
`auto_stop_machines`/`auto_start_machines`, `min_machines_running = 0` — so it
costs nothing between quarters.

Configuration is `fly.toml` for the non-secrets (`GITHUB_REPO`, `ENVIRONMENT`,
`PUBLIC_URL`) plus three values set once with `fly secrets set`:
`GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `SESSION_KEY`. `PUBLIC_URL` and the
OAuth app's registered callback are a matched pair and change together.

## 11. Backup

There is nothing to back up, by design.

Every published state is a commit: `git log -- _data/trainings.yml` **is** the
audit log (who, what, when, and the PR that carried it). The draft is
deliberately disposable (D6). There is no volume, no database, no uploaded
files.

The one exception is the OAuth client secret and `SESSION_KEY` — the only state
a machine loss would destroy. They belong in 1Password.

## 12. Failure modes

| Failure | Behaviour |
|---|---|
| App is down | Hand-edit `trainings.yml` exactly as today. The app is never in the publishing path. |
| Draft lost (redeploy, auto-stop, crash) | Re-enter the edits. Source of truth untouched. |
| Concurrent edit by the other maintainer | Detected at publish via blob-SHA comparison; conflict screen (§8.2). |
| Invalid data reaches a PR | CI's `validate-trainings.yml` fails the PR. Nothing publishes. |
| App compromised | Attacker can open a PR. Merging still requires an authenticated arc42 maintainer (D2). |
| GitHub OAuth outage | Nobody can sign in. Hand-editing still works. |

## 13. Out of scope

- Editing the German/English site copy (lives in the Jekyll templates).
- Registration/booking data (Formspark).
- Any change to the feed contract, the consumers, or their refresh workflows.
- Notifications, reminders, or calendar integration.

## 14. Companion work item

`meta.arc42.org/training-dates.md` needs a freshness pass, independent of the
app:

- §3's contract table omits `pricing`, `country` and per-date `trainers`.
- The four-way `notify-consumers` fan-out and its `workflow_dispatch` manual test
  landed after the document was written.
- Once §4 is executed, all fragment/Vercel references become historical.
- A new section describing this app as the *supported editing path*, with the
  hand-edit route retained as the fallback.
