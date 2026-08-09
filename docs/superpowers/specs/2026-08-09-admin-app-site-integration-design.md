# Admin app ↔ site integration — design

**Date:** 2026-08-09
**Status:** approved (brainstorming complete)
**Scope:** how a maintainer gets from trainings.arc42.org to the trainings admin
app, and what has to exist for that link to work.

Predecessor: [`2026-08-08-trainings-admin-design.md`](2026-08-08-trainings-admin-design.md)
— that spec designed the app; this one connects it to the site. Where the two
conflict, the predecessor wins.

---

## 1. Problem

The trainings admin app (`admin-app/`) is built and tested but reachable from
nowhere. It has no public hostname, and nothing on trainings.arc42.org points at
it. Two maintainers need a durable way to find it that does not depend on
remembering a `.fly.dev` URL.

The site is static GitHub Pages on the apex `trainings.arc42.org`. It cannot
proxy or serve `/admin`. Whatever the entry point is, it is a **link to another
origin**.

## 2. What this is not

- **Not** shared authentication. The site has no sessions, no cookies, no JS
  touching the app. It never learns whether anyone is signed in.
- **Not** a build-time dependency. The site renders identically if the app is
  down, deleted, or never deployed. This is load-bearing: the app is never in
  the publishing path, and this link must not put it there.
- **Not** a user-facing feature. Zero of the site's readers are users of the app.

## 3. Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | The two systems couple through **exactly one string**: the admin URL | No session, cookie, CORS, JS or build-time coupling. The site stays a pure static artifact whose correctness is independent of the app's existence. |
| D2 | Entry point is **one quiet link in the existing footer**, not a masthead item or homepage button | The app has two users; every other visitor is not one. A prominent "Login" promises an account that cannot be obtained. The footer is where site-infrastructure links (Status, GitHub, Imprint) already live. |
| D3 | The label is **"Maintainers"**, not "Login" | A label that names *who it is for* reads as "not me" to every visitor and "that's me" to the two maintainers. "Login" reads as an invitation and provokes "where do I sign up?". |
| D4 | The app gets the custom hostname **`trainings-admin.arc42.org`** *(revised 2026-08-09; originally `admin.trainings.arc42.org`)* | The public footer of an arc42 site should not advertise a hosting provider. More importantly it decouples the published URL from fly.io: moving hosts later becomes a DNS change, not a dead bookmark plus a re-registered OAuth callback. |
| D5 | The URL is **hardcoded to production** in the footer; no `jekyll.environment` branch | Neither dev loop clicks that link — site work does not care where it points, and app work reaches `localhost:8080` from the terminal. An env branch would be this repo's *first* dev/prod split (there is no `url:`, `baseurl:` or `jekyll.environment` anywhere today) and would sit inside an `include_cached` block. Not worth it for a link nobody clicks in dev. |
| D6 | The link is **public**, and that is accepted deliberately | Access is gated by GitHub OAuth, a live `permissions.push` check, and the PR-only boundary. Security rests on those, not on the URL being unguessable. Obscurity here would buy nothing real and cost daily usability. |
| D4a | The hostname is a **single label**, not `admin.trainings` | The DNS panel for `arc42.org` accepts only one label in the host field, so a two-label name cannot be entered at all. It also sidesteps a real grey area: `trainings.arc42.org` is itself a CNAME, and RFC 1034 says a name carrying a CNAME should carry no other data. `trainings-admin` sorts beside `trainings` and reads as "the trainings admin". |
| D7 | Design specs live in **this repository** under `docs/superpowers/specs/`, and `docs` is added to Jekyll's `exclude:` | Keeps the documentation next to the thing it documents. The exclusion is mandatory, not tidy-up: Jekyll copies every unexcluded directory into `_site/` verbatim, so without it the specs would be served at `https://trainings.arc42.org/docs/…`. |

## 4. The change

### Site side — one line

`_includes/footer.html`, appended to the `.footer-links` list after `GitHub`:

```html
<li><a href="https://trainings-admin.arc42.org">Maintainers</a></li>
```

- **English only.** The footer is single-language by design
  (`{% include_cached footer.html %}` in `_layouts/default.html:72`, matching
  arc42.org). No DE/EN duplication, no `_data/navigation.yml` entry.
- **No `target="_blank"`.** Consistent with the sibling arc42-property links
  (`arc42.org`, `Status`), which also stay in-tab. The external-looking targets
  in this footer that *do* open a new tab are third parties (INNOQ, CC, K&S).

### Infra side — one-time, no code change

| Step | Value |
| --- | --- |
| Create the fly app | `fly launch` / `fly apps create arc42-trainings-admin` (`fly.toml` already exists) |
| Set fly secrets | `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `SESSION_KEY` |
| DNS | `trainings-admin` **A** → `66.241.124.107` and **AAAA** → `2a09:8280:1::165:17ca:0` — *not* a CNAME, see below (GoDaddy, which now owns Host Europe; `arc42.org` answers from `ns29/ns30.domaincontrol.com`) |
| TLS | `fly certs add trainings-admin.arc42.org` |
| `admin-app/fly.toml` | `PUBLIC_URL = "https://trainings-admin.arc42.org"` |
| GitHub OAuth app (`arc42-trainings-admin-prod`) | Authorization callback → `https://trainings-admin.arc42.org/auth/callback` |


### DNS: A/AAAA, not CNAME

The design's CNAME to `arc42-trainings-admin.fly.dev` **cannot be entered** in
the `arc42.org` DNS panel (GoDaddy, which now owns Host Europe). The panel
rejects any target ending in `.dev` as *"syntaktisch nicht korrekt"* — a stale
TLD allow-list that predates the 2019 `.dev` gTLD. A trailing dot and other
`.dev` hostnames fail identically, so it is the extension, not the syntax.

Address records avoid the question entirely:

| Type | Name | Value |
| --- | --- | --- |
| `A` | `trainings-admin` | `66.241.124.107` (fly **shared** IPv4) |
| `AAAA` | `trainings-admin` | `2a09:8280:1::165:17ca:0` (fly **dedicated** IPv6) |

`fly certs` validates identically for A/AAAA and CNAME, so nothing else changes.

**The trade-off, recorded because it will not announce itself:** a CNAME follows
fly automatically if the app's address changes; an A record does not. The IPv6
address is dedicated and therefore stable. The IPv4 is *shared* — stable in
practice, not contractually ours. If it is ever reassigned, the hostname breaks
silently, with no failing build and no alert. `fly ips allocate-v4` (about
$2/month) removes that risk; for a tool used once or twice a quarter the shared
address was judged an acceptable exposure. Dual-stack clients prefer the
dedicated IPv6 anyway.

`PUBLIC_URL` was made configurable in `e0f54cd` for exactly this. The last two
rows **must change together** — a mismatch makes GitHub reject the
`redirect_uri`, and the resulting error reads like an app bug rather than a
configuration typo.

## 5. Rollout order

The footer commit lands **last**. `make check-links` runs html-proofer with
`--disable-external`, so CI will *not* catch a dead admin link — shipping the
link early would point every page on the site at a 404, silently.

1. Create the fly app, set secrets, deploy.
2. Add the A and AAAA records and the fly cert; confirm HTTPS resolves.
3. Set `PUBLIC_URL` and the OAuth callback together; redeploy.
4. **Verify a real sign-in end to end** at `https://trainings-admin.arc42.org`,
   including that a non-maintainer account gets the 403 "No access" page
   (`internal/web/handlers_auth.go:38`).
5. Only then: the footer commit, together with the doc updates in §6.

## 6. Documentation updates (same PR as the footer commit)

Both READMEs currently state the app is not deployed, and both become wrong the
moment the link is live:

- **`README.md`**, *"Updating Training Dates (requires write access)"* — replace
  the "In development / not finished or deployed yet" block with the live URL
  and the sign-in flow. The manual `_data/trainings.yml` procedure **stays
  documented** as the permanent fallback: the app is never in the publishing
  path, and hand-editing must remain a first-class path.
- **`admin-app/README.md`**, *"Deployment"* — replace "the fly.io app itself has
  not been created yet … deploying is a one-time `fly launch` away" with the
  actual hostname, and record that `PUBLIC_URL` and the OAuth callback are a
  matched pair.

## 7. Adjacent fixes (separate commits, same PR)

### 7.1 `admin-app/` is published as static files

Jekyll's `exclude:` in `_config.yml` did not list `admin-app`, so the build
copies the whole Go source tree into `_site/` — `main.go`, `internal/`,
`go.sum`, `fly.toml`. It is not live today only because `admin-app/` has never
been pushed (local `main` is 19 commits ahead of `origin/main`); the first push
would publish it. `Makefile` and `docker-compose.yml`, which *have* been on
`origin` for a while, are already served — `https://trainings.arc42.org/Makefile`
answers 200 — so this is a demonstrated behaviour, not a theoretical one.

No credential is exposed (`admin-app/.env` is gitignored and has never been
committed), and the source is public on GitHub anyway. The cost is a confusing
second copy of the code on a documentation domain, crawlable and unversioned.

Fixed together with the `docs` entry this design requires, since both are the
same one-line-per-entry change to the same list. **Still unexcluded and
knowingly left alone:** `Makefile`, `Dockerfile`, `docker-compose.yml`,
`scripts/`. They are already live, harmless, and removing them is a separate
cleanup with its own (small) risk of breaking an inbound link.

### 7.2 `make dev` starts the admin container

`make dev` runs `docker compose up --build` with no service argument, so it
starts the `admin` service too — which declares `env_file: admin-app/.env`.
Anyone without that file gets a compose error on `make dev`, even when they only
want to work on the site. Pre-existing and unrelated to this design, but it is a
one-line fix in the same neighbourhood and it gets more likely to bite once the
app is publicly advertised:

```make
docker compose up --build jekyll
```

Kept as its own commit so it can be dropped without touching the rest.

## 8. Verification

- `make site && make check-links` passes (proves the footer markup is well-formed;
  does **not** validate the external target — see §5).
- The rendered footer contains the `Maintainers` item on both `/` and `/de/`,
  confirming the shared single-language footer behaves as expected.
- Clicking it from the live site reaches the sign-in screen; a maintainer signs
  in and lands on the dates list; a non-maintainer gets the 403 page.
- The site builds and renders correctly with the fly machine stopped
  (`auto_stop_machines = true` means it usually *is* stopped) — proving D1.
