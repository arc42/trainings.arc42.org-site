# Admin app ↔ site integration — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the two arc42 maintainers a durable one-click path from
trainings.arc42.org to the trainings admin app, without exposing anything to the
site's readers or coupling the static site to the app.

**Architecture:** The site and the app couple through exactly one string — the
admin URL — hardcoded in one footer `<li>`. No shared session, cookie, CORS, JS
or build-time dependency. The app lives on `admin.trainings.arc42.org` (DNS
CNAME → fly.io), so the published URL survives a change of hosting provider.

**Tech Stack:** Jekyll 4 / Liquid (site, built by GitHub Pages), Go 1.x on
fly.io (app), GitHub OAuth, Docker Compose + `make` for local work.

**Source spec:** [`docs/superpowers/specs/2026-08-09-admin-app-site-integration-design.md`](../specs/2026-08-09-admin-app-site-integration-design.md)

## Global Constraints

- **The admin URL is exactly `https://admin.trainings.arc42.org`** — no trailing
  slash, no path. It appears verbatim in the footer, in `fly.toml`'s
  `PUBLIC_URL`, and (with `/auth/callback` appended) in the GitHub OAuth app.
- **The footer label is exactly `Maintainers`** — not "Login", not "Admin".
- **No `target="_blank"`** on the footer link. Sibling arc42-property links in
  that list (`arc42.org`, `Status`, `GitHub`) stay in-tab; only third-party
  links (INNOQ, CC, Kröner & Starke) open a new tab.
- **English only.** The footer is single-language by design
  (`{% include_cached footer.html %}`, `_layouts/default.html:72`). Do **not**
  add a German variant or a `_data/navigation.yml` entry.
- **No new colours, fonts or CSS.** The link inherits `.footer-links` styling
  entirely. Introducing a styled button would require a measured contrast ratio
  per meta.arc42.org ADR-0002; inheriting existing styles avoids that.
- **Task 5 (the footer link) commits LAST**, after Task 4's live sign-in check
  passes. `make check-links` runs html-proofer with `--disable-external` and
  therefore **cannot** catch a dead admin link.
- **Do not stage files you did not change.** The working tree currently holds
  unrelated in-progress admin-app work (~764 insertions across ~15 files under
  `admin-app/internal/`). Every `git add` in this plan lists explicit paths —
  never use `git add -A` or `git add .`.

## Human-only tasks

Tasks **3 and 4** cannot be executed by an agent. They need fly.io
credentials, DNS control over `arc42.org`, and the ability to create a GitHub
OAuth app. An agent reaching Task 3 must stop and hand back to the operator.

---

## File Structure

| File | Change | Responsibility |
| --- | --- | --- |
| `_config.yml` | Modify (`exclude:`) | Keeps `docs/` and `admin-app/` out of the published site |
| `docs/superpowers/specs/*.md` | Move in | Design documents, versioned with the code they describe |
| `docs/superpowers/plans/*.md` | Create | This plan |
| `admin-app/README.md` | Modify | Spec pointers (§ See also); deployment status (§ Deployment) |
| `Makefile` | Modify (`dev:` target) | Stop `make dev` from starting the admin container |
| `admin-app/fly.toml` | Modify (`PUBLIC_URL`) | The app's own idea of its public origin |
| `_includes/footer.html` | Modify | The single line of integration |
| `README.md` | Modify | Replaces the "app not deployed yet" procedure |

---

## Task 1: Keep repository documents out of the published site

Jekyll copies every unexcluded top-level directory into `_site/` verbatim. Adding
`docs/` to the repo without excluding it would serve the design specs at
`https://trainings.arc42.org/docs/…`. The same gap already applies to
`admin-app/`: the build copies `main.go`, `internal/`, `go.sum` and `fly.toml`
into `_site/`. It is not live only because `admin-app/` has never been pushed
(local `main` is ahead of `origin/main`).

> **Status: the edits below are already present in the working tree.** Verify
> them, then commit. If a step's assertion fails, apply the edit shown and
> re-run.

**Files:**
- Modify: `_config.yml` (the `exclude:` list, around line 55)
- Move in: `docs/superpowers/specs/2026-08-08-trainings-admin-design.md`,
  `docs/superpowers/specs/2026-08-09-admin-app-site-integration-design.md`
- Create: `docs/superpowers/plans/2026-08-09-admin-app-site-integration.md`
- Modify: `admin-app/README.md` (§ See also)

**Interfaces:**
- Produces: the `docs/superpowers/{specs,plans}/` convention that Tasks 2–6
  reference, and the guarantee that nothing under `docs/` is ever published.

- [ ] **Step 1: Confirm the failing behaviour is real**

Prove the exclusion is load-bearing before trusting it. Build with the current
config and check whether the two directories reach `_site/`:

```bash
make site
ls _site/docs _site/admin-app 2>/dev/null && echo "PUBLISHED — exclusion missing" || echo "excluded"
```

Expected once the edit in Step 2 is in place: `excluded`.
Without the edit: a directory listing, and `PUBLISHED — exclusion missing`.

- [ ] **Step 2: Verify the `exclude:` entries**

`_config.yml` must contain `admin-app` and `docs` in the `exclude:` list:

```yaml
# Anything not listed here is copied verbatim into _site/ and served publicly.
# Two entries below are load-bearing rather than cosmetic:
#   - admin-app: the Go source of the trainings admin app. Without this line
#     https://trainings.arc42.org/admin-app/main.go serves the app's source.
#   - docs: the design specs. Repository documents, not site content.
exclude:
  - "*.sublime-project"
  - "*.sublime-workspace"
  - admin-app
  - docs
  - vendor
```

Leave `Makefile`, `Dockerfile`, `docker-compose.yml` and `scripts/` unexcluded.
They are already live (`https://trainings.arc42.org/Makefile` answers 200),
they are harmless, and removing them is a separate cleanup that could break an
inbound link.

- [ ] **Step 3: Verify the exclusion and that the site still builds**

```bash
make site
ls _site/docs 2>/dev/null && echo "LEAK" || echo "docs excluded OK"
ls _site/admin-app 2>/dev/null && echo "LEAK" || echo "admin-app excluded OK"
ls _site/index.html _site/de/index.html _site/api/trainings.json
```

Expected: two `excluded OK` lines, then all three paths listed. The third line
matters — an over-broad `exclude:` that also dropped the home pages or the feed
would be a far worse bug than the one being fixed.

- [ ] **Step 4: Verify the README pointer**

`admin-app/README.md` § *See also* must no longer claim the spec is "not part of
this repository":

```bash
grep -n "not part of this repository" admin-app/README.md && echo "STALE" || echo "OK"
grep -n "2026-08-09-admin-app-site-integration-design" admin-app/README.md
```

Expected: `OK`, then a line number for the new spec reference.

- [ ] **Step 5: Commit**

```bash
git add _config.yml admin-app/README.md docs/
git commit -m "docs: keep design specs in the repo, out of the published site

Jekyll copies unexcluded top-level directories into _site/ verbatim, so
docs/ would be served at trainings.arc42.org/docs/. The same gap applied
to admin-app/, whose Go source was being copied into _site/ and would
have gone live on the next push.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01D42RK2ztXWqEVrDFctBSJL"
```

`git add docs/` is safe — the directory contains only the two specs and this
plan. Do not widen the other paths.

---

## Task 2: Stop `make dev` from starting the admin container

`make dev` runs `docker compose up --build` with no service argument, so Compose
starts every service in `docker-compose.yml` — including `admin`, which declares
`env_file: admin-app/.env`. A contributor who only wants the Jekyll site, and has
no `.env`, gets a Compose error instead of a dev server. Independent of the rest
of this plan; droppable without affecting any other task.

**Files:**
- Modify: `Makefile` (the `dev:` target, final line — around line 19)

**Interfaces:**
- Consumes: nothing.
- Produces: nothing. No later task depends on this.

- [ ] **Step 1: Reproduce the failure**

Temporarily hide the env file and confirm `make dev` breaks:

```bash
mv admin-app/.env admin-app/.env.bak
make dev
```

Expected: Compose fails with an error naming `admin-app/.env` (wording varies by
Compose version, e.g. `env file … not found`). If it instead starts the Jekyll
server cleanly, this bug does not reproduce on your Compose version — restore
the file, skip to Step 5, and mark this task not applicable.

Stop the run with `Ctrl-C` if it hangs.

- [ ] **Step 2: Scope the target to the jekyll service**

In `Makefile`, the last line of the `dev:` target:

```make
	docker compose up --build jekyll
```

(was: `docker compose up --build`)

Do not touch the `app:` target — `docker compose up --build admin` is already
correctly scoped.

- [ ] **Step 3: Verify with the env file still hidden**

```bash
make dev
```

Expected: the Jekyll dev server starts and serves <http://localhost:4000>, with
no mention of `admin-app/.env`. Confirm the site loads, then `Ctrl-C`.

- [ ] **Step 4: Restore the env file and confirm `make app` still works**

```bash
mv admin-app/.env.bak admin-app/.env
make app
```

Expected: the admin app starts on <http://localhost:8080>. This guards against
the scoping change accidentally breaking the app's own target. `Ctrl-C` when
confirmed.

- [ ] **Step 5: Commit**

```bash
git add Makefile
git commit -m "fix: make dev no longer starts the admin container

docker compose up with no service argument starts every service,
including admin, which requires admin-app/.env. Site-only contributors
hit a Compose error instead of a dev server.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01D42RK2ztXWqEVrDFctBSJL"
```

---

## Task 3: Deploy the app on `admin.trainings.arc42.org` — **HUMAN ONLY**

**An agent must stop here.** This task needs fly.io credentials, DNS control
over `arc42.org`, and permission to create a GitHub OAuth app. Nothing in it can
be verified from the repository.

**Files:**
- Modify: `admin-app/fly.toml` (the `PUBLIC_URL` line in `[env]`)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: a live HTTPS origin at `https://admin.trainings.arc42.org` whose
  OAuth callback is `https://admin.trainings.arc42.org/auth/callback`. Task 4
  verifies it; Task 5 links to it.

- [ ] **Step 1: Create the fly app**

```bash
cd admin-app
fly apps create arc42-trainings-admin
```

The app name must match `app = "arc42-trainings-admin"` in `fly.toml`. Do not
run `fly launch` — it would rewrite the existing, deliberately-configured
`fly.toml` (in particular it may add a `[mounts]` section, which must never
exist: the app is stateless by design).

- [ ] **Step 2: Register the production GitHub OAuth app**

At <https://github.com/settings/developers> → **New OAuth App**:

| Field | Value |
| --- | --- |
| Application name | `arc42-trainings-admin-prod` |
| Homepage URL | `https://admin.trainings.arc42.org` |
| Authorization callback URL | `https://admin.trainings.arc42.org/auth/callback` |

Keep the Client ID and generate a Client Secret. This is a **separate** OAuth
app from any development one — a dev app's callback points at
`http://localhost:8080/auth/callback` and cannot serve both.

- [ ] **Step 3: Set the fly secrets**

```bash
fly secrets set \
  GITHUB_CLIENT_ID=<client-id-from-step-2> \
  GITHUB_CLIENT_SECRET=<client-secret-from-step-2> \
  SESSION_KEY=$(openssl rand -hex 32) \
  --app arc42-trainings-admin
```

`SESSION_KEY` is not issued by anyone — it is a locally generated key that
encrypts the session cookie. `internal/config/config.go` rejects anything under
32 characters at startup, and rejects client IDs/secrets under 16 characters as
obvious placeholders.

- [ ] **Step 4: Set `PUBLIC_URL` in `fly.toml`**

```toml
  PUBLIC_URL = "https://admin.trainings.arc42.org"
```

This value and the OAuth callback from Step 2 are a **matched pair**. The app
builds its `redirect_uri` as `PUBLIC_URL + "/auth/callback"`; if the two differ
by even a character, GitHub rejects the request with an error that reads like an
app bug rather than a configuration typo. `config.go` also refuses to start in
`PRODUCTION` unless `PUBLIC_URL` begins with `https://`.

- [ ] **Step 5: Deploy**

```bash
fly deploy --app arc42-trainings-admin
fly logs --app arc42-trainings-admin
```

Expected in the logs: a clean startup line. A `missing configuration: …` line
means a secret from Step 3 did not take.

- [ ] **Step 6: Add the DNS record**

In the DNS zone for `arc42.org`:

```
admin.trainings    CNAME    arc42-trainings-admin.fly.dev
```

Then confirm it resolves before asking fly for a certificate:

```bash
dig +short admin.trainings.arc42.org
```

Expected: a CNAME/A answer pointing at fly. An empty answer means the record has
not propagated — wait and retry rather than proceeding.

- [ ] **Step 7: Issue the TLS certificate**

```bash
fly certs add admin.trainings.arc42.org --app arc42-trainings-admin
fly certs show admin.trainings.arc42.org --app arc42-trainings-admin
```

Expected: the certificate reaches a ready/issued state. Fly validates via the
DNS record from Step 6, so this fails if Step 6 has not propagated.

- [ ] **Step 8: Confirm the origin answers**

```bash
curl -sI https://admin.trainings.arc42.org/healthz | head -1
```

Expected: `HTTP/2 200`. The `/healthz` endpoint is the same one `fly.toml`
health-checks.

- [ ] **Step 9: Commit the one tracked change**

```bash
git add admin-app/fly.toml
git commit -m "chore(admin-app): serve on admin.trainings.arc42.org

PUBLIC_URL and the OAuth app's authorization callback are a matched
pair: the app builds redirect_uri as PUBLIC_URL + /auth/callback, and
GitHub rejects any mismatch.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01D42RK2ztXWqEVrDFctBSJL"
```

---

## Task 4: Verify sign-in end to end — **HUMAN ONLY**

The gate that makes Task 5 safe. Two accounts are needed: one with push access
to `arc42/trainings.arc42.org-site`, and one without. Nothing is committed here.

**Files:** none.

**Interfaces:**
- Consumes: the live origin from Task 3.
- Produces: the go/no-go signal for Task 5.

- [ ] **Step 1: Sign in as a maintainer**

Open <https://admin.trainings.arc42.org> in a browser and sign in with GitHub.

Expected: after the GitHub authorization round-trip you land on the dates list
with real data from `_data/trainings.yml`. A `redirect_uri` error here means
Task 3 Steps 2 and 4 disagree.

- [ ] **Step 2: Confirm the app reads the right repository**

Check that the dates shown match `_data/trainings.yml` on `main`. `fly.toml`
sets `GITHUB_REPO = "arc42/trainings.arc42.org-site"`; a fork's data appearing
here means a stale secret or env value.

- [ ] **Step 3: Confirm a non-maintainer is refused**

In a private window, sign in with a GitHub account that has **no** push access
to the repo.

Expected: HTTP 403 and the "No access" page
(`admin-app/internal/web/handlers_auth.go:38`), naming the account and the repo.
Expected **not**: a stack trace, a redirect loop, or any part of the dates list.

This is the check that makes a publicly-linked admin app acceptable. **If it
fails, stop — do not proceed to Task 5.**

- [ ] **Step 4: Confirm the site is independent of the app**

`fly.toml` sets `auto_stop_machines = true`, so the machine is normally stopped.
With it stopped:

```bash
fly machine list --app arc42-trainings-admin
make site && ls _site/index.html _site/de/index.html _site/api/trainings.json
```

Expected: the site builds and all three paths exist regardless of machine state.
This proves decision D1 — the static site has no dependency on the app.

---

## Task 5: Add the footer link

One line. Commits only after Task 4 passed.

**Files:**
- Modify: `_includes/footer.html` (the `.footer-links` list)

**Interfaces:**
- Consumes: the verified live origin from Tasks 3–4.
- Produces: nothing. Terminal.

- [ ] **Step 1: Write the failing check**

The site currently has no such link. Confirm:

```bash
make site
grep -c "admin.trainings.arc42.org" _site/index.html _site/de/index.html
```

Expected: `_site/index.html:0` and `_site/de/index.html:0`.

- [ ] **Step 2: Add the list item**

In `_includes/footer.html`, append to the `.footer-links` list, after the
`GitHub` item:

```html
  <ul class="footer-links">
    <li><a href="https://arc42.org">arc42.org</a></li>
    <li><a href="https://arc42.org/about/#contact">Contact</a></li>
    <li><a href="https://arc42.org/license/">License</a></li>
    <li><a href="/imprint/">Imprint &amp; Privacy</a></li>
    <li><a href="https://status.arc42.org/">Status</a></li>
    <li><a href="https://github.com/arc42">GitHub</a></li>
    <li><a href="https://admin.trainings.arc42.org">Maintainers</a></li>
  </ul>
```

No `target`, no `rel`, no `class`, no icon, no German variant.

- [ ] **Step 3: Verify it renders on both languages**

```bash
make site
grep -c "admin.trainings.arc42.org" _site/index.html _site/de/index.html
grep -o "Maintainers" _site/imprint/index.html
```

Expected: `1` for each of the two home pages, and `Maintainers` on the imprint
page too — proving the shared single-language footer reaches every page, which
is the intended behaviour.

- [ ] **Step 4: Verify the markup is valid**

```bash
make check-links
```

Expected: html-proofer passes. Note it runs with `--disable-external`, so this
confirms the markup only — the target's reachability was established in Task 4
Step 1, not here.

- [ ] **Step 5: Commit**

```bash
git add _includes/footer.html
git commit -m "feat: link the trainings admin app from the footer

Labelled 'Maintainers' rather than 'Login': the app has two users and no
signup, so a label naming who it is for beats one naming what it does.

The link is public by design. Access is gated by GitHub OAuth, a live
permissions.push check, and the PR-only boundary — not by the URL being
unguessable.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01D42RK2ztXWqEVrDFctBSJL"
```

---

## Task 6: Update the documentation that just went stale

Both READMEs state the app is not deployed. Both are wrong the moment Task 5
lands.

**Files:**
- Modify: `README.md` (§ *Updating Training Dates (requires write access)*)
- Modify: `admin-app/README.md` (§ *Deployment*)

**Interfaces:**
- Consumes: the live URL from Task 3.
- Produces: nothing. Terminal.

- [ ] **Step 1: Find the stale claims**

```bash
grep -n "In development\|not finished or deployed\|has not been created yet\|one-time \`fly launch\` away" README.md admin-app/README.md
```

Expected: hits in both files. These are the exact passages to replace.

- [ ] **Step 2: Rewrite the root README section**

Replace the `> **In development.** …` block under *Updating Training Dates
(requires write access)* with:

```markdown
There are two ways to change training dates.

**The admin app** ([`admin-app/`](/admin-app/)) at
<https://admin.trainings.arc42.org> is the supported route: sign in with
GitHub, edit the forms, and publish. It opens a single pull request against
[`_data/trainings.yml`](/_data/trainings.yml) with a minimal diff. It never
commits to `main` — merging still needs a maintainer on GitHub. Sign-in
requires push access to this repository; anyone else gets a "No access" page.

**Editing the file by hand** remains fully supported and is the permanent
fallback — the app is never in the publishing path, so nothing depends on it:

1. Edit [`/_data/trainings.yml`](/_data/trainings.yml)
2. Run `ruby scripts/validate_trainings.rb`
3. Commit and push your changes
```

Keep the existing "This automatically updates the content" list that follows —
it is true of both routes.

- [ ] **Step 3: Rewrite the admin-app README deployment section**

Replace the final sentence of § *Deployment* ("As of this writing the fly.io app
itself has not been created yet … rather than a new engineering task.") with:

```markdown
The app runs at <https://admin.trainings.arc42.org>, a DNS CNAME to the fly
app plus a fly-managed certificate. Two values must always agree: `PUBLIC_URL`
in [`fly.toml`](fly.toml), and the authorization callback registered on the
`arc42-trainings-admin-prod` GitHub OAuth app, which must be `PUBLIC_URL` +
`/auth/callback`. Changing one without the other makes GitHub reject the
`redirect_uri`. `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET` and `SESSION_KEY`
are fly secrets, not `fly.toml` values.
```

Keep the preceding sentences about `Dockerfile`, `fly.toml` and the deploy
workflow — they remain accurate.

- [ ] **Step 4: Verify no stale claim survives**

```bash
grep -n "In development\|not finished or deployed\|has not been created yet" README.md admin-app/README.md && echo "STALE REMAINS" || echo "clean"
grep -c "admin.trainings.arc42.org" README.md admin-app/README.md
make check-links
```

Expected: `clean`, then a non-zero count for each README, then html-proofer
passing (it checks the built site, catching any malformed relative link
introduced above).

- [ ] **Step 5: Commit**

```bash
git add README.md admin-app/README.md
git commit -m "docs: the admin app is live at admin.trainings.arc42.org

Hand-editing _data/trainings.yml stays documented as the permanent
fallback: the app is never in the publishing path.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01D42RK2ztXWqEVrDFctBSJL"
```

---

## Done when

- `https://admin.trainings.arc42.org` serves the app over HTTPS; a maintainer can
  sign in, a non-maintainer gets the 403 page.
- Every page of trainings.arc42.org shows a `Maintainers` item in the footer.
- `make site` emits no `_site/docs` and no `_site/admin-app`, and still emits
  `/`, `/de/` and `/api/trainings.json`.
- `make check-links` passes.
- `make dev` works without `admin-app/.env`.
- Neither README claims the app is undeployed.
