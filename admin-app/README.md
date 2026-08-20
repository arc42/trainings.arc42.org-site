# trainings admin app

A small Go web app that lets the two arc42 maintainers edit training dates
through forms instead of hand-editing YAML. Sign in with GitHub, add, change
or remove a date or a course, and publish — the app turns one editing session
into a single pull request against [`_data/trainings.yml`](/_data/trainings.yml)
with a minimal, reviewable diff. It never commits to `main` itself.

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

## There is no local mode

The app runs in exactly one place: <https://trainings-admin.arc42.org>. You
change it by pushing, not by starting it on your laptop.

1. Edit the code and run the tests from this directory: `go test ./...`
   (Go 1.23, no Docker, no configuration, no network).
2. Push to `main`. [`deploy-admin-app.yml`](/.github/workflows/deploy-admin-app.yml)
   runs the same tests and then deploys — there is no manual `flyctl deploy`.
3. Try the change on the live app.

This trades a fast local loop for having one environment instead of two, and
it is affordable only because of what the app can do: it opens pull requests
and nothing else. A change that turns out wrong costs a closed PR and another
push. There is no data to corrupt and, with two users who touch this a few
times a quarter, nobody to interrupt.

A caveat that follows from it: pressing **Publish** while trying something out
opens a real pull request against this repository. That is harmless — close it
unmerged and delete its branch — but do not leave it sitting in the queue,
where the next person reads it as a proposal.

**Configuration** lives in two places, both in production. Non-secrets
(`GITHUB_REPO`, `ENVIRONMENT`, `PUBLIC_URL`) are in [`fly.toml`](fly.toml) and
are reviewable. The three secrets are set once with `flyctl secrets set … -a
arc42-trainings-admin` and never appear in the repository:

| Secret | Meaning |
| --- | --- |
| `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET` | The `arc42-trainings-admin-prod` GitHub OAuth app, which signs users in |
| `SESSION_KEY` | 32+ random bytes encrypting the session cookie. Nobody issues this — generate it with `openssl rand -hex 32`. Replacing it signs everyone out; that is its only effect |

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

Push to `main` touching `admin-app/**`. That is the whole procedure — there is no
manual `flyctl deploy`.

[`../.github/workflows/deploy-admin-app.yml`](/.github/workflows/deploy-admin-app.yml)
runs `go test`, `go vet` and a `gofmt` check, and only then runs `flyctl deploy`.
Pull requests run the same tests but never deploy. The deploy job is serialised
with a concurrency group, so two merges cannot ship over each other, and
[`fly.toml`](fly.toml)'s `/healthz` check gates the new machine before it takes
traffic.

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
