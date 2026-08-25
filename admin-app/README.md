# trainings admin app

A small Go web app that lets the two arc42 maintainers edit training dates
through forms instead of hand-editing YAML. Sign in with GitHub, add, change
or remove a date or a course, and publish — the app turns one editing session
into a single pull request against [`_data/trainings.yml`](/_data/trainings.yml)
with a minimal, reviewable diff. It never commits to `main` itself.

## How it works

One Go binary, no database, no build step for the frontend. It renders HTML
server-side with `html/template`, uses htmx in exactly one place (the draft
bar's keepalive ping), and treats **the GitHub REST API as its only backend**:
it has no storage of its own that could become a second source of truth about
training dates.

### The pieces

| Package | What it does |
| --- | --- |
| [`internal/config`](internal/config/config.go) | Reads the environment at startup and refuses to boot on anything missing or obviously a placeholder |
| [`internal/gh`](internal/gh) | Everything that talks to GitHub: OAuth exchange, permission check, read file, create branch, commit, open PR |
| [`internal/model`](internal/model) | The `Trainings` → `Course` → `Date` structs, the flat date list the UI shows, and the values derived from others (booking `code`, registration `url`) |
| [`internal/yamldoc`](internal/yamldoc) | Parses `_data/trainings.yml` into a node tree and edits **only the touched nodes**, so a generated PR is three changed lines and not a reformat |
| [`internal/validate`](internal/validate) | The repository's own JSON schema plus the cross-field rules, run before anything can be published |
| [`internal/web`](internal/web) | Routes, handlers, templates, the session cookie and the draft store |
| [`internal/ghfake`](internal/ghfake) | A stand-in for the GitHub API — used by the tests, and by the offline demo. Never part of the deployed binary |

Only `internal/web` knows about HTTP; the rest are plain libraries whose tests
need neither a network nor a configuration file.

### What happens during one editing session

1. **Sign in.** `GET /auth/github` sets a short-lived random `state` cookie and
   redirects to GitHub. GitHub comes back to `/auth/callback`, where the app
   checks the `state` (CSRF), trades the `code` for a token using the client
   secret, and asks GitHub two questions: *who is this* (`GET /user`) and *may
   they push to this repository* (`GET /repos/{owner}/{repo}` →
   `permissions.push`). No push permission, no app — see
   [`handlers_auth.go`](internal/web/handlers_auth.go). The scope requested is
   `public_repo`, not `repo`, so the token cannot reach anybody's private work.

2. **Load.** The first page after sign-in fetches `_data/trainings.yml` and the
   current head of `main`, and keeps three things: the parsed document, the
   file's **blob SHA** and the **head SHA**. Those two SHAs are what lets step 5
   notice that somebody else changed the file meanwhile.

3. **Edit.** Every form submit is validated first and only then applied — to the
   in-memory draft, never to GitHub. Each change also appends a one-line summary
   ("added", "updated", "removed"), and repeated edits of the same date collapse
   into a single entry, which is what later becomes the PR body. The draft bar
   polls `/keepalive` every 60 seconds while a draft is dirty, so fly's idle
   timer cannot stop the machine out from under an editing session.

4. **Review.** `GET /propose` re-reads the file from GitHub, renders a unified
   diff of what you are about to propose, and validates the result against the
   schema **fetched live from the repository** — so the app cannot drift from
   what CI will enforce on the pull request.

5. **Publish.** `POST /propose` re-reads the blob SHA and compares it with the
   one from step 2. Different means somebody else edited the file: you get a
   conflict screen and keep your draft, and nothing is written. Otherwise three
   GitHub calls, in this order ([`gh/pr.go`](internal/gh/pr.go)):
   `POST /git/refs` cuts a branch `trainings-admin/<date>-<slug>-<random>` from
   the head SHA, `PUT /contents/_data/trainings.yml` commits the new content
   onto it, and `POST /pulls` opens the pull request against `main`. Then the
   draft is discarded and you get the PR link.

### Where state lives

| What | Where | Survives a restart? |
| --- | --- | --- |
| Who you are, and your GitHub token | An encrypted cookie in *your browser*, valid 8 hours — the server keeps no copy | Yes |
| Your unpublished draft | Server memory, keyed by session id | **No**, by design |
| Everything else | The Git repository | Yes — it is the only source of truth |

The middle row is the one to remember; see *Drafts are in-memory, on purpose*
below.

### Routes

| Route | Purpose |
| --- | --- |
| `GET /healthz` | Liveness probe for fly's health check |
| `GET /auth/github`, `GET /auth/callback`, `POST /auth/logout` | Sign in and out |
| `GET /` | The flat date list |
| `GET /dates/new`, `GET /dates/{id}`, `POST /dates/{id}` | Add, duplicate (`?from=`) and edit a date |
| `GET /dates/{id}/delete`, `POST /dates/{id}/delete` | The confirmation page, then the removal — the GET deliberately changes nothing |
| `GET /courses`, `GET /courses/new`, `GET /courses/{id}`, `POST /courses/…` | The courses. There is no course delete: a course owns its dates, so removing one would silently take them along |
| `GET /propose`, `POST /propose` | The diff-and-publish screen, and the publish itself |
| `POST /discard` | Throw the draft away |
| `GET /keepalive` | The draft bar's 60-second htmx ping |

Everything except `/healthz`, `/static/*` and `/auth/*` goes through one wrapper that
requires a valid session and builds a GitHub client **from the signed-in user's
own token** — the app holds no credential of its own that can write to this
repository.

## Why it opens a PR instead of committing

The app can only *propose* a change, never publish one. Merging still requires
an authenticated maintainer clicking the button on GitHub. That is the
app's primary security boundary: if the app's session key, OAuth secret or
hosting were ever fully compromised, the worst an attacker gets is an
unmerged pull request sitting in the queue — not a change on the live site.
Nothing downstream (the site's own timelines, the JSON feed, the four
consumer sites) reacts to anything short of a merge, so a bad PR is inert
until a human approves it.

This is also why the app keeps no allowlist of its own. Authorization is a
live `permissions.push` check against the repository on every request that
needs it — access is granted and revoked entirely on GitHub's side, and the
app has nothing locally that could go stale or leak.

## Working on the app

The app is deployed in exactly one place: <https://trainings-admin.arc42.org>.
You change it by pushing, not by starting it on your laptop — but you can run
the whole thing offline against a stand-in GitHub, which is what the demo below
is for.

1. Edit the code, then run `make app-check` from the repository root — the Go
   suite, `go vet` and a `gofmt` check, which is exactly what CI runs before it
   deploys (Go 1.23, no Docker, no configuration, no network).
2. Push to `main`. [`deploy-admin-app.yml`](/.github/workflows/deploy-admin-app.yml)
   runs the same three checks and then deploys.
3. Try the change on the live app.

Every target below is run from the **repository root**, not from this directory:

| Target | What it does |
| --- | --- |
| `make app-check` | Tests, `go vet` and `gofmt` — the same three checks CI gates the deploy on |
| `make app-test` | Just the Go test suite, for a tighter loop |
| `make app-build` | Compile to `admin-app/admin`; a fast compile check, never the deployed binary |
| `make app-demo` | The offline demo: the real app on <http://localhost:8080> against a fake GitHub — see below |
| `make app-stop` | Stop a demo left running in the background |
| `make app-preview` | Render every page to `preview-out/` as plain HTML files, no server involved |
| `make fly-deploy` | Deploy **your current working tree** to fly — the manual path, see below |
| `make fly-status` | The app, its machines and their health checks (`stopped` is the normal resting state) |
| `make fly-logs` | Tail production logs |
| `make fly-releases` | What has actually been deployed, newest first |
| `make fly-secrets` | The *names* of the three fly secrets; values are never readable, by design |

### The offline demo

`make app-demo` runs **the real app** on <http://localhost:8080>. Only GitHub is
different: instead of api.github.com it talks to
[`internal/ghfake`](internal/ghfake), a stand-in running in the same process.

- **Sign in is one click** and involves no GitHub account. The app still runs
  its real OAuth flow — state cookie, code, token exchange, permission check —
  the fake just answers the other end. There is no "skip sign-in" branch in the
  app, because there does not need to be.
- **It reads your checkout's `_data/trainings.yml`** and the real schema, so the
  courses and dates on screen are the actual ones. Neither file is ever written.
- **Publishing opens nothing.** No branch, no commit, no pull request. The
  proposed file is written to `demo-out/<branch>.yml` for you to diff, the
  terminal prints the `diff -u` command, and the "Pull request opened" link
  leads to a page showing exactly what would have been sent.
- **It cannot exist in production.** [`cmd/demo`](cmd/demo) and the fake are a
  separate package that the deployed binary never links — `Dockerfile` builds
  the root package alone, and `main_test.go` fails the build if that ever stops
  being true. A demo mode that could be switched on in a real deployment is
  precisely the thing worth not having.

Use it to click through a change, to check a form, or to show somebody what the
app does with no network at all.

Ctrl-C ends it. From the background, `make app-stop` does — the top-level `make
stop` is `docker compose down` and the demo is not a container, and killing the
`go run` leaves the binary it compiled still listening on 8080.

**What it cannot tell you:** whether GitHub accepts the commit, whether the
OAuth app's callback still matches `PUBLIC_URL`, whether fly's health check
passes. The fake is written to refuse what GitHub refuses — a duplicate branch
ref, a commit over a moved file — and every one of those refusals is a test, but
a stand-in only ever models what somebody thought to model. Sign-in against the
real GitHub, and a real pull request, are still only exercised in production.
That is what `make fly-deploy` from a branch is for; read *How a change reaches
production* before using it, because it ships unreviewed code.

For looking at a page rather than using it, `make app-preview` dumps every
screen to `preview-out/` as static HTML — the same rendering the tests use, no
server, no browser session.

### There is still no second environment

This trades a fast local loop against real GitHub for having one environment
instead of two, and it is affordable only because of what the app can do: it
opens pull requests and nothing else. A change that turns out wrong costs a
closed PR and another push. There is no data to corrupt and, with two users who
touch this a few times a quarter, nobody to interrupt.

A caveat that follows from it: pressing **Publish** *on the deployed app* while
trying something out opens a real pull request against this repository. That is
harmless — close it unmerged and delete its branch — but do not leave it sitting
in the queue, where the next person reads it as a proposal. In the demo, the
same button costs nothing.

### Configuration

Configuration lives in two places, both in production (the demo supplies its own
obviously-fake values and needs none of this). Non-secrets
(`GITHUB_REPO`, `ENVIRONMENT`, `PUBLIC_URL`) are in [`fly.toml`](fly.toml) and
are reviewable. The three secrets are set once with `flyctl secrets set … -a
arc42-trainings-admin` and never appear in the repository:

| Secret | Meaning |
| --- | --- |
| `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET` | The `arc42-trainings-admin-prod` GitHub OAuth app, which signs users in |
| `SESSION_KEY` | 32+ random bytes encrypting the session cookie. Nobody issues this — generate it with `openssl rand -hex 32`. Replacing it signs everyone out; that is its only effect |

### The Ruby cross-check

The test suite includes an anti-drift check that shells out to
[`scripts/validate_trainings.rb`](/scripts/validate_trainings.rb) and requires
the Go and Ruby validators to reach the same verdict on every fixture — the
duplication would otherwise rot silently. It skips itself when Ruby is absent,
so it is optional locally and mandatory in CI, which pins Ruby 3.3. The
production image ([`Dockerfile`](Dockerfile)) has no Ruby at all: the deployed
binary never runs the validator, only the schema and the four cross-field
rules in `internal/validate`.

## Drafts are in-memory, on purpose

Everything you edit before you publish — added dates, changed statuses,
in-progress course edits — lives only in server memory, keyed to your
session. There is no database and no fly volume. Restarting the app, or the
machine getting recycled after being idle, throws away every open draft.
This is deliberate, not a limitation to work around: the only thing that is
allowed to persist is what is already in the repository, or what is on its
way there as a PR. A draft that outlived a restart would be a second, hidden
source of truth for training dates, which is exactly what this app exists to
avoid.

## Deployment: why fly.io, and what we use it for

### Why there is a server at all

The site itself is static and served by GitHub Pages. This app cannot be,
because three things it does need code running somewhere:

- **The OAuth exchange.** Signing in with GitHub means trading a `code` for a
  token using the OAuth app's *client secret*. A secret shipped to the browser
  is not a secret, so the trade has to happen server-side.
- **The session.** The user's GitHub token lives in an encrypted cookie that
  only the server can read, and every write re-checks `permissions.push`
  against the repository.
- **The draft.** Edits accumulate in server memory until you publish, so the
  whole session becomes one pull request with one minimal diff.

So the choice was never "server or no server", only *where the smallest
possible one runs*.

### Why fly.io

It fits the shape of this workload, which is unusual: two maintainers, a few
times a quarter, and idle the rest of the year.

- **Scale to zero.** `min_machines_running = 0` with `auto_stop_machines` and
  `auto_start_machines`: the machine stops when nobody is editing and cold-starts
  on the next request. We pay for minutes of use, not months of uptime.
- **A container, not a runtime.** The [`Dockerfile`](Dockerfile) builds a
  from-scratch image around one static Go binary. No buildpack guessing, no
  language runtime to keep patched, nothing in the image but the app.
- **Custom domain and TLS** for `trainings-admin.arc42.org` without a proxy in
  front, and **secrets** that live in the platform rather than the repository.
- **One region** (`ams`). Latency is irrelevant for a form used from Germany a
  handful of times a quarter, and a single machine keeps the failure modes few.

A serverless function would also have worked for the OAuth exchange, but not for
the draft: drafts are per-session state held between requests, and that is
exactly what a function does not have.

### What we deliberately do *not* use it for

- **No database, no volume.** [`fly.toml`](fly.toml) has no `[mounts]` section
  and must never grow one. Drafts are in memory on purpose (see *Drafts are
  in-memory, on purpose* above); the only thing allowed to persist is what is in
  the repository or on its way there as a pull request.
- **No serving of the public site.** trainings.arc42.org stays on GitHub Pages.
  Fly hosts the editor, never the content.
- **No production data.** The worst thing on that machine is an unmerged pull
  request and a draft nobody minds losing.

Scale-to-zero and disposable drafts are the same decision seen from two sides: a
machine that stops when idle *will* lose your draft, and that is acceptable only
because a lost draft costs a few re-typed fields.

### How a change reaches production

**The normal way is a push to `main`** that touches `admin-app/**` — that is the
whole procedure.
[`../.github/workflows/deploy-admin-app.yml`](/.github/workflows/deploy-admin-app.yml)
runs `go test`, `go vet` and a `gofmt` check, and only then runs `flyctl deploy`.
Pull requests run the same checks but never deploy. The deploy job is serialised
with a concurrency group, so two merges cannot ship over each other, and
[`fly.toml`](fly.toml)'s `/healthz` check gates the new machine before it takes
traffic.

**The manual way is `make fly-deploy`**, which runs the same three checks and
then `flyctl deploy --remote-only` from your machine. It exists for the two
cases CI cannot serve: GitHub Actions is unavailable, or you want to try a
branch on the real thing — which, since there is no local mode, is the only way
to exercise GitHub OAuth and a real pull request end to end. It needs `flyctl`
installed, `flyctl auth login` done, and access to the `arc42-trainings-admin`
app; the target checks all three before it does anything.

Three consequences worth knowing before you use it, which is why the target
names the branch it is about to ship and makes you type `deploy` to go ahead
(`YES=1 make fly-deploy` skips the prompt, for when you already know):

- **It builds from your working tree, not from `main`** — uncommitted changes
  ship too. What runs in production is then something no reviewer has seen and
  no commit records.
- **The next push to `main` silently replaces it.** A manual deploy is never the
  way to *keep* something in production, only to look at it.
- **The deploy restarts the machine, so every open draft is discarded** —
  including the one belonging to the other maintainer, if they are editing right
  now. `make fly-status` shows whether a machine is currently awake.

### The one configuration trap

`PUBLIC_URL` in [`fly.toml`](fly.toml) and the `arc42-trainings-admin-prod`
OAuth app's registered callback URL are a matched pair: the callback must be
exactly `PUBLIC_URL` + `/auth/callback`. Change one without the other and GitHub
rejects the `redirect_uri`, which surfaces as an error page that reads like an
app bug rather than a configuration mismatch. `main.go` logs the callback it
computed at startup for exactly this reason.

## See also

- Design spec: [`docs/superpowers/specs/2026-08-08-trainings-admin-design.md`](/docs/superpowers/specs/2026-08-08-trainings-admin-design.md)
  — how the app itself is designed, and
  [`…/2026-08-09-admin-app-site-integration-design.md`](/docs/superpowers/specs/2026-08-09-admin-app-site-integration-design.md)
  — how the site links to it. `docs/` is excluded from the Jekyll build
  (`_config.yml`), so these are repository documents and are never published.
- [meta.arc42.org/training-dates.md](https://github.com/arc42/meta.arc42.org/blob/main/training-dates.md) —
  the training-dates contract shared by this site and its four consumers,
  including §6 on how dates are edited.
