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

## Deployment

The app runs on fly.io at [https://trainings-admin.arc42.org](https://trainings-admin.arc42.org).
[`Dockerfile`](Dockerfile) builds a from-scratch production image, [`fly.toml`](fly.toml) configures it (single process, no volumes, a `/healthz` check), and [`../.github/workflows/deploy-admin-app.yml`](/.github/workflows/deploy-admin-app.yml) tests and deploys on every push to `main` that touches `admin-app/`.

`PUBLIC_URL` in `fly.toml` (or Fly app secrets) and the GitHub OAuth app authorization callback URL (`https://trainings-admin.arc42.org/auth/callback`) are a matched pair and must always change together.

## See also

- Design spec: [`docs/superpowers/specs/2026-08-08-trainings-admin-design.md`](/docs/superpowers/specs/2026-08-08-trainings-admin-design.md)
  — how the app itself is designed, and
  [`…/2026-08-09-admin-app-site-integration-design.md`](/docs/superpowers/specs/2026-08-09-admin-app-site-integration-design.md)
  — how the site links to it. `docs/` is excluded from the Jekyll build
  (`_config.yml`), so these are repository documents and are never published.
- [meta.arc42.org/training-dates.md](https://github.com/arc42/meta.arc42.org/blob/main/training-dates.md) —
  the training-dates contract shared by this site and its four consumers,
  including §6 on how dates are edited.
