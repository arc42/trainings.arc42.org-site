# trainings.arc42.org-site

This repository powers [trainings.arc42.org](https://trainings.arc42.org), which displays a list of upcoming arc42 training dates, and publishes those dates as a JSON feed that the other arc42-related sites consume.

# Overview

This project includes both the frontend and the machine-readable feed used by multiple arc42-related sites to show consistent, up-to-date training info.

## Key Process

All training dates are maintained in a single data file ([`/_data/trainings.yml`](/_data/trainings.yml)) and distributed across sites via:

- The timeline on this site's two home pages — the English `/` and the German
  `/de/`, both rendered from the same `_data/trainings.yml`
- The JSON feed ([`/api/trainings.json`](/api/trainings.json)), served by GitHub
  Pages — the supported machine interface for all other arc42 sites

## Languages

The site is bilingual: English at `/` ([`_pages/home.html`](/_pages/home.html)),
German at `/de/` ([`_pages/home-de.html`](/_pages/home-de.html)). The two home
pages are structural twins — same layout, same section ids
(`training-dates`, `contact`, `license`) — and differ only in language and in
which registration form they point at (`/registration/` vs `/anmeldung/`).

- Every page declares `lang: en|de` in its front matter. The eight pages that
  have a twin also declare `translation_url:` (the twin's URL); `/imprint/` and
  `404.html` have no twin and therefore no `translation_url`. The masthead
  renders a **DE | EN switch** only where `translation_url` is present (there is
  no site search; the theme's search toggle was removed and the switch sits in
  its slot).
- The masthead nav is per language: [`_data/navigation.yml`](/_data/navigation.yml)
  holds `main` (English) and `main_de` (German); `page.lang == "de"` selects the
  latter.
- **The registration form posts to a different Formspark form per language**, and
  that is what decides the language of the confirmation email the registrant
  gets. Formspark allows exactly one autoresponder template per form and no
  hidden field overrides it per submission, so the form id *is* the language
  choice: `AIKiYyJP` (German) and `Tq1M7LqmX` / "registration-EN" (English), both
  declared at the top of
  [`_includes/registration-form.html`](/_includes/registration-form.html). The
  two autoresponder templates live in the Formspark dashboard, not in this repo —
  editing the confirmation wording is dashboard work, and it has to be done
  twice. Each form also carries its own Botpoison project (public key next to the
  id here, secret key in that form's Formspark spam-protection settings). Adding
  a language means adding a Formspark form, its autoresponder and its Botpoison
  secret before touching this file.
- [`_includes/head.html`](/_includes/head.html) emits the `hreflang` triple
  (`en`, `de`, `x-default`) from the same front matter. (Not
  `_includes/head/custom.html` — that one holds favicons, `theme-color` and
  font preloads.)
- German pages additionally declare `locale: de_DE`; the vendored
  `_includes/seo.html` (there is no jekyll-seo-tag plugin) emits it as
  `og:locale` via `page.locale | default: site.locale`.
- Timeline cards are bilingual: `_includes/timeline_auto.html` takes a
  `page_lang` parameter (`"en"` / `"de"`) and renders all card copy, buttons and
  date labels in the **page's** language,
  while the language a training is actually *held* in (`language:` in
  `_data/trainings.yml`) travels separately and shows as a small per-card note.

## Local development

This repository holds two programs with two lifecycles: **the site** (Jekyll,
static, GitHub Pages) and **the admin app** (Go, a container on fly.io). `make`
(or `make help`) lists every target; the ones you need day to day:

### The site

Everything runs in Docker — no local Ruby/bundler needed.

| Target | What it does |
| --- | --- |
| `make dev` | Start the dev server with live reload at <http://localhost:4040> (not `0.0.0.0:4040`) |
| `make site` | Build the static site into `_site/` |
| `make check-links` | Run html-proofer over the built `_site` (internal links, images, HTML) |
| `make stop` | Stop and remove the Jekyll dev container — it does not touch the admin app demo, which is not a container (see `make app-stop`) |
| `make clean` | Remove `_site/`, Jekyll caches, the Docker volumes and the admin binary |
| `make install` | Re-resolve gems after editing the `Gemfile` (rewrites `Gemfile.lock`) |

`make dev` serves on port **4040**, this repo's fixed slot in the port
assignment across arc42 sites, so it does not collide with the sibling site
repos — see `raw/port-assignment.md` in meta.arc42.org for the full list.
Jekyll binds 4040 inside the container as well as on the host, so the
"Server address:" banner it prints on startup names the real port. The number
lives in three places that must stay in step: `SITE_PORT` in the `Makefile`,
the mapping in `docker-compose.yml`, and `EXPOSE`/`CMD` in the `Dockerfile`.
`make dev` still refuses to start if another container already holds 4040;
stop that one first.

### The admin app

Needs Go 1.23; `flyctl` only for the `fly-*` targets.
[`admin-app/README.md`](/admin-app/README.md#how-it-works) explains how the app
works and why it is deployed in exactly one place.

| Target | What it does |
| --- | --- |
| `make app-demo` | Run the real app offline on <http://localhost:8080> against a fake GitHub — nothing is published |
| `make app-stop` | Stop a demo left running in the background |
| `make app-check` | Tests, `go vet` and `gofmt` — the same three checks CI gates the deploy on |
| `make app-test` | Just the Go test suite |
| `make app-preview` | Render every page to `preview-out/` as static HTML, no server |
| `make app-build` | Compile to `admin-app/admin` as a fast compile check (never the deployed binary) |
| `make fly-deploy` | Deploy your **current working tree** to fly.io — the manual path; it asks first |
| `make fly-status` | The fly app, its machines and their health checks (`stopped` is normal: it scales to zero) |
| `make fly-logs` | Tail production logs |
| `make fly-releases` | What has actually been deployed, newest first |
| `make fly-secrets` | The *names* of the fly secrets; values are never readable |

`make app-demo` is how you try a change, or show the app to somebody, without a
GitHub account or a network: it reads this checkout's `_data/trainings.yml`,
never writes it, and "publishing" writes the proposed file to `demo-out/` and
opens no pull request. See
[The offline demo](/admin-app/README.md#the-offline-demo) for what it does and
does not prove.

It runs in the foreground, so Ctrl-C ends it. If you started it in the background
instead, `make app-stop` is what ends it: `make stop` only stops the Jekyll
container, and the demo is a Go process. Killing the `go run` by hand is not
enough either — that leaves the binary it compiled still holding port 8080.

Normally you never need `make fly-deploy`: pushing to `main` with changes under
`admin-app/**` runs the same checks in CI and deploys. It is the escape hatch for
when Actions is down, or for trying a branch on the real app — with the caveats
in [How a change reaches production](/admin-app/README.md#how-a-change-reaches-production).

## Design

How this site looks is **not** decided in this repository. It follows the arc42
family design system, which lives in
[meta.arc42.org](https://github.com/arc42/meta.arc42.org):

- [`BRAND.md`](https://github.com/arc42/meta.arc42.org/blob/main/BRAND.md) — the
  hue registry. The signature hue of trainings.arc42.org is the **softened dusty
  rose `#a04c5e`** (deep variant `#743442`), a low-chroma relative of the family
  coral `#ff5c7c`. White on that band measures 5.69:1. It is deliberately *not*
  the shared error token `#c22b47`: a colour sized for a button and an error
  message reads as an alarm when it is stretched across a full-width masthead.
- [`DESIGN.md`](https://github.com/arc42/meta.arc42.org/blob/main/DESIGN.md) —
  the family constants (§2): Libre Caslon Text for headings and Atkinson
  Hyperlegible Next for body/UI, both self-hosted with no third typeface; a
  solid masthead band in the signature hue with light text; flat near-white
  paper behind long prose; and the pinned-note shadow
  (`box-shadow: 3px 3px 0 0 #743442`, zero blur, annotation-style elements only).

Three rules that bite in practice:

- `<meta name="theme-color">` in [`_includes/head/custom.html`](/_includes/head/custom.html)
  and `theme_color` in [`site.webmanifest`](/site.webmanifest) must both equal
  the masthead fill `#a04c5e`.
- The favicons carry the same hue: they are arc42.org's round "42" mark with the
  navy circle recoloured to `#a04c5e`, so the family mark stays recognisable and
  the site still reads as its own. Regenerate all six files together
  (`favicon.ico` 16/32/48, `favicon-16x16`, `favicon-32x32`, `apple-touch-icon`,
  `android-chrome-192x192`, `android-chrome-512x512`) — a half-updated set shows
  the old icon in whichever slot was missed.
- Contrast is measured, never eyeballed
  ([ADR-0002](https://github.com/arc42/meta.arc42.org/blob/main/adr/0002-measured-accessibility.md)).
  Every new foreground/background pair gets its measured ratio stated before it
  ships; body text ≥ 4.5:1, and the on-band secondary tint is `#f6edef`
  (4.95:1) — not `rgba(255,255,255,.82)`, which fails at 4.41:1.

Run `make check-links` before opening a PR: it builds the site and runs
html-proofer over `_site/`, which also catches missing `alt` attributes and
dead in-page anchors.

## Updating Training Dates (requires write access)

Use the **trainings admin app** at [https://trainings-admin.arc42.org](https://trainings-admin.arc42.org) (also linked as **Maintainers** in the site footer): sign in with GitHub, edit courses or dates via forms, and publish — the app turns your editing session into a single pull request against `_data/trainings.yml` with a minimal diff.

The two halves of this repository are hosted differently, which is worth knowing
before you go looking for a server: **the site is static on GitHub Pages**, while
**the admin app runs as a container on fly.io** — it needs somewhere to hold the
GitHub OAuth client secret, the signed-in session and your unpublished draft,
none of which a static site can do. The fly machine scales to zero between
editing sessions, keeps no database and no volume, and never serves any page of
trainings.arc42.org. It only ever opens pull requests.
[`admin-app/README.md`](/admin-app/README.md#deployment-why-flyio-and-what-we-use-it-for)
has the full reasoning, the config split between `fly.toml` and Fly secrets, and
the deploy path.

Manual editing remains the permanent fallback, as the app is never in the publishing path:

1. Edit [`/_data/trainings.yml`](/_data/trainings.yml)
2. Run `ruby scripts/validate_trainings.rb`
3. Commit and push your changes

This automatically updates the content:

- On trainings.arc42.org (both home-page timelines render `_data/trainings.yml` directly)
- Across the consumer sites (GitHub Pages republishes `/api/trainings.json`,
  which the consumers pull weekly and on dispatch — see [Consumers](#consumers))

See [Maintaining training dates](#maintaining-training-dates) for the full
branch/PR workflow and how consumer sites pick the changes up.

## JSON Feed (the supported machine interface)

The supported way for other sites to consume training dates is the JSON feed:

```
https://trainings.arc42.org/api/trainings.json
```

[`/api/trainings.json`](/api/trainings.json) is a Jekyll template (JSON with
front matter) rendered from `_data/trainings.yml` at build time and served by
GitHub Pages as a static file — no serverless function involved. On every
relevant change, CI validates the built feed against
[`/api/trainings.schema.json`](/api/trainings.schema.json)
(see [`validate-trainings.yml`](/.github/workflows/validate-trainings.yml)).

Every course carries a German `url` (its detail page on arc42.de) and may
carry an optional `url_en` — an English detail page on this site,
`https://trainings.arc42.org/courses/<id>/`. English consumers render
`course.url_en | default: course.url`; arc42.de ignores `url_en`.

### Prices, credits and seats: read the structured fields

Three fields exist twice in the feed, and new consumers should read the second
of each pair:

| deprecated (German prose) | read this instead | shape |
| --- | --- | --- |
| `date.pricing` | `date.price` | `{ amount, currency, alumni?, early_bird? }` |
| `course.credits` | `course.credit_points` | `{ methodical?, technical?, communication? }` |
| `date.few_seats` | `date.seats_limited` | `true` |

The three deprecated fields were hand-written German sentences. They were
rendered verbatim on English pages, and `pricing` additionally buried its
early-bird deadline inside the prose, where no machine could see it — so the
site advertised an expired early-bird price for weeks before anyone noticed.

They are still published, unchanged in name and type, because four consumers
render them verbatim and [ADR-0004](https://github.com/arc42/meta.arc42.org)
makes the feed a contract: adding optional fields is free, changing an existing
one is not. But they are now **generated** on every build rather than stored,
so they can no longer go stale. Nothing downstream had to change; consumers
that migrate simply gain the ability to render prices and credits in their own
language.

Two guarantees worth relying on:

- **Amounts are integers** in whole currency units (`2890` means EUR 2890). The
  currency symbol, the thousands separator and the surrounding wording belong
  to whoever renders it.
- **An expired `early_bird` is never published.** The producer compares
  `early_bird.until` against the build date and omits the offer from both
  `price` and `pricing`. Consumers do not need a calendar. `alumni`, the
  reduced rate for former participants, never expires and is always published
  when present.
- **Every published date carries a price.** Four card templates used to hold a
  hardcoded bilingual price sentence, which meant the price never reached this
  feed at all and consumers could not quote those courses. A test
  (`TestEveryPublishedDateHasAPrice`) keeps it that way.

docs.arc42.org and faq.arc42.org render no prices and are unaffected by any of
this; arc42.de and arc42.org are the consumers that show them.

### Consumers

Four sites render the training dates at build time from this feed. Each pulls
it weekly via its own `.github/workflows/refresh-trainings.yml` and commits an
expiry-filtered `_data/trainings.json` into its own repository:

- arc42.de-site
- docs.arc42.org-site
- faq.arc42.org-site
- arc42.org-site

Because the dates are baked into the consumers' HTML at build time, a failing
refresh workflow means "dates at most one week stale" — never a broken page.

In addition, every push to `main` that touches `_data/trainings.yml` triggers
[`notify-consumers.yml`](/.github/workflows/notify-consumers.yml): it waits for
GitHub Pages to republish the feed, then sends a `repository_dispatch` event
(`trainings-updated`) to all four consumer repos, so they refresh within
minutes instead of waiting for their weekly cron. It authenticates with the
repo secret `CONSUMER_DISPATCH_TOKEN` — a fine-grained PAT that needs
**Contents: read and write** on each of the four consumer repos. If the secret
is absent the workflow exits gracefully; if a dispatch to one repo fails, the
others are still notified, a warning names the failed repo, and the run ends
red as a signal to fix the token's access. Test the fan-out manually (without
touching the data) via `gh workflow run notify-consumers.yml` — a manual run
skips the Pages poll.

The dispatch is an accelerator, never a dependency
([ADR-0006](https://github.com/arc42/meta.arc42.org/blob/main/adr/0006-training-dates-single-source.md)):
the weekly pull alone keeps consumers correct, so worst-case staleness without
any dispatch is one week.

## Frontend Integration

- **trainings.arc42.org** renders its own home-page timelines (`_includes/timeline_auto.html`, once per language) straight from `_data/trainings.yml` at build time, without going through its own JSON feed. This ensures availability even if everything else fails.

  Because those timelines — and the registration-form `<select>`s — filter
  against `today` **at build time**, and a date passing its `end` touches no git
  history, [`weekly-refresh.yml`](/.github/workflows/weekly-refresh.yml) asks
  GitHub for a Pages build every Monday. Without it this site alone would keep
  advertising ended courses until an unrelated commit landed. The consumer sites
  are unaffected either way: they filter expired dates themselves.
- **The consumer sites** (see [Consumers](#consumers)) render the dates at build time from their committed, expiry-filtered copy of the JSON feed. Nothing is fetched in the reader's browser: the former runtime HTMX fetch against a Vercel-hosted HTML fragment was retired in 2026-08 along with the fragment itself.

## Maintaining training dates

All training dates for ALL arc42 sites live in `_data/trainings.yml` (single
source of truth). To change dates:

**No prose in `_data/trainings.yml`.** Everything in that file is rendered on
both the German and the English pages, so a hand-written sentence is wrong on
half of them. Sentences live in `_includes/*-label.html`, which branch on the
page language; the data file holds numbers and flags:

```yaml
price:                     credit_points:        seats_limited: true
  amount: 2890               methodical: 20
  currency: EUR              technical: 10       # also: communication
  alumni: 2050             # optional, never expires
  early_bird:
    amount: 2690
    until: "2026-11-02"    # QUOTED, like start/end
```

Every bookable date needs a `price`. There is no template fallback any more.

`price-label.html`, `credits-label.html` and `money.html` turn those into
`Frühbucherpreis bei Anmeldung bis 2. November 2026: 2.690 €, Normalpreis:
2.890 €` or `Early bird until November 2, 2026: €2,690, regular: €2,890`.
`scripts/validate_trainings.rb` rejects the retired `pricing`, `credits` and
`few_seats` keys by name, so reintroducing one fails CI rather than quietly
shipping German to English readers.

An `early_bird` whose `until` has passed does **not** need deleting. Both the
site and the feed stop rendering it on their own, and the weekly rebuild
([`weekly-refresh.yml`](/.github/workflows/weekly-refresh.yml)) bounds the
staleness to one Monday — the same guarantee that already covers dates that
have passed.

1. Edit `_data/trainings.yml` on a branch (every date needs `language: de|en` —
   validation fails loudly otherwise).
2. Run `ruby scripts/validate_trainings.rb`.
3. Open a PR. CI re-validates against the schema.
4. On merge: GitHub Pages republishes `/api/trainings.json`; the
   [consumer sites](#consumers) pull the JSON
   weekly (Monday mornings UTC, staggered crons), immediately when
   `notify-consumers.yml` dispatches to all four repos (see
   [Consumers](#consumers) for the token requirement), or on demand via their
   `Refresh training dates` workflow_dispatch button.

### Adding an English course page

Course descriptions in German live on arc42.de (`/info-<id>/`). English
descriptions live here, one page per course, and are maintained by hand —
there is deliberately no DE↔EN sync. To add one (today only `msa` has one):

1. Create `_pages/courses/<id>.md` (`<id>` = the course id in
   `_data/trainings.yml`). Copy the front matter of `_pages/courses/msa.md`;
   set `translation_url:` to the German page on arc42.de so the masthead
   DE | EN switch and `hreflang` point there. Buttons use the site-wide
   `btn btn--primary` / `btn btn--inverse` classes.
2. Add `url_en: "https://trainings.arc42.org/courses/<id>/"` to that course in
   `_data/trainings.yml`.

The consumer sites already prefer `url_en`, so nothing else changes.

## Created with [OneFlow Jekyl Theme](https://oneflow-jekyll-theme.github.io/)
