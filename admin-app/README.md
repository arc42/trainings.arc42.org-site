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

## Running it locally

Everything runs in Docker via the root [`Makefile`](/Makefile), the same way
the Jekyll site does:

| Target | What it does |
| --- | --- |
| `make app` | Start the app at <http://localhost:8080> |
| `make app-test` | Run the Go test suite, including the `validate_trainings.rb` cross-check |
| `make app-stop` | Stop and remove the running container |
| `make app-logs` | Tail logs from the running container |
| `make app-shell` | Open a shell inside the container |

`make app` refuses to start if port 8080 is already held by another
container, and it refuses to start at all until `admin-app/.env` exists.
Copy the template and fill it in:

```bash
cp admin-app/.env.template admin-app/.env
```

`.env` (gitignored) holds:

| Variable | Meaning |
| --- | --- |
| `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET` | Credentials of the GitHub OAuth app used to sign users in |
| `SESSION_KEY` | 32+ bytes used to encrypt the session cookie — generate with `openssl rand -hex 32` |
| `GITHUB_REPO` | The repo the app reads from and opens PRs against. Defaults to `arc42/trainings.arc42.org-site`; point it at **your own fork** while smoke-testing so real PRs land somewhere harmless (see below) |
| `ENVIRONMENT` | `DEVELOPMENT` or `PRODUCTION` — cosmetic, logged at startup |

The container image used locally is `Dockerfile.dev`, which additionally
installs Ruby so `make app-test` can shell out to
[`scripts/validate_trainings.rb`](/scripts/validate_trainings.rb) as an
anti-drift check: the app's own validation rules must never silently diverge
from what CI already enforces. The production image
([`Dockerfile`](Dockerfile)) has no Ruby at all — the deployed binary never
runs the validator itself, it only relies on the schema and the four
cross-field rules baked into `internal/validate`.

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

## Smoke-testing against a fork

Because a real run opens a real pull request, never point a development
instance at `arc42/trainings.arc42.org-site` while you're testing a change to
the app itself. Instead:

1. Fork `arc42/trainings.arc42.org-site` on GitHub.
2. Register (or reuse) a GitHub OAuth app whose authorization callback points
   at `http://localhost:8080/auth/callback`, and put its ID/secret in
   `admin-app/.env`.
3. Set `GITHUB_REPO=<your-github-username>/trainings.arc42.org-site` in
   `admin-app/.env`.
4. `make app`, sign in, make a change, publish it, and check that the PR
   lands on your fork with the diff you expect.

Delete the fork's PR (and the branch it opened) when you're done — it never
needs to be merged, it only needs to prove the round-trip works.

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
