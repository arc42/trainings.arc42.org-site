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
- *(deprecated)* A generated HTML fragment
  ([`/_includes/_subtle-ads.html`](/_includes/_subtle-ads.html)) served by a
  Vercel backend, kept only until the last consumers' migration to the JSON
  feed is live — see [Deprecated: Vercel Fragment Backend](#deprecated-vercel-fragment-backend)

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

Everything runs in Docker — no local Ruby/bundler needed. `make` (or `make help`)
lists all targets; the ones you need day to day:

| Target | What it does |
| --- | --- |
| `make dev` | Start the dev server with live reload at <http://localhost:4000> (not `0.0.0.0:4000`) |
| `make site` | Build the static site into `_site/` |
| `make check-links` | Run html-proofer over the built `_site` (internal links, images, HTML) |
| `make stop` | Stop and remove the running dev container |
| `make clean` | Remove `_site/`, Jekyll caches and the Docker volumes |
| `make install` | Re-resolve gems after editing the `Gemfile` (rewrites `Gemfile.lock`) |

`make dev` refuses to start if another container already holds port 4000 — that
is usually a dev server from a sibling arc42 site repo; stop that one first.

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

Two rules that bite in practice:

- `<meta name="theme-color">` in [`_includes/head/custom.html`](/_includes/head/custom.html)
  and `theme_color` in [`site.webmanifest`](/site.webmanifest) must both equal
  the masthead fill `#a04c5e`.
- Contrast is measured, never eyeballed
  ([ADR-0002](https://github.com/arc42/meta.arc42.org/blob/main/adr/0002-measured-accessibility.md)).
  Every new foreground/background pair gets its measured ratio stated before it
  ships; body text ≥ 4.5:1, and the on-band secondary tint is `#f6edef`
  (4.95:1) — not `rgba(255,255,255,.82)`, which fails at 4.41:1.

Run `make check-links` before opening a PR: it builds the site and runs
html-proofer over `_site/`, which also catches missing `alt` attributes and
dead in-page anchors.

## Updating Training Dates (requires write access)

To change or add training dates:

1. Edit [`/_data/trainings.yml`](/_data/trainings.yml)
2. Run `ruby scripts/validate_trainings.rb` and `ruby scripts/generate_subtle_ads.rb`
   (the latter regenerates the deprecated fragment — still required while it
   exists, because CI checks its freshness)
3. Commit and push your changes

This automatically updates the content:

- On trainings.arc42.org (both home-page timelines render `_data/trainings.yml` directly)
- Across the consumer sites (GitHub Pages republishes `/api/trainings.json`,
  which the consumers pull weekly — see [Consumers](#consumers))

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

## Deprecated: Vercel Fragment Backend

> **Deprecated.** The HTML fragment backend described below is superseded by
> the JSON feed above. It is kept only until the last consumers' migration to
> the feed is live; the removal steps are tracked in the workspace rollout
> plan, Phase B:
> `docs/superpowers/plans/2026-08-04-trainings-consumers-rollout.md`.

The legacy backend is deployed on Vercel as a simple serverless function, written in the format Vercel expects for [Next.js API routes](https://nextjs.org/docs/api-routes/introduction).

It reads the contents of `_subtle-ads.html` from the filesystem and serves it as raw HTML via this endpoint:

```
https://arc42-subtle-ads-backend.vercel.app/api
```

The endpoint returns the HTML with appropriate CORS and caching headers. The backend is automatically redeployed on each push to this repository, so any site still fetching the fragment sees updated training data.

If the backend is unreachable or blocked (e.g. by browser settings), sites
still on the fragment fall back to a plain link to
[trainings.arc42.org](https://trainings.arc42.org), which always reflects the
latest content via its build-time timeline.

### Further Details (deprecated fragment)

The function for the deprecated backend is located in [`/api/index.js`](/api/index.js).  
Here’s the full implementation:

```
const fs = require('fs').promises;
const path = require('path');

// Enable CORS headers for browser access
const allowCors = fn => async (req, res) => {
  res.setHeader('Access-Control-Allow-Credentials', true);
  res.setHeader('Access-Control-Allow-Origin', '*');
  res.setHeader('Access-Control-Allow-Methods', 'GET,OPTIONS,PATCH,DELETE,POST,PUT');
  res.setHeader(
    'Access-Control-Allow-Headers',
    'X-CSRF-Token, X-Requested-With, Accept, Accept-Version, Content-Length, Content-MD5, Content-Type, Date, X-Api-Version, hx-target, hx-current-url, hx-request, hx-trigger'
  );
  if (req.method === 'OPTIONS') {
    res.status(200).end();
    return;
  }
  return await fn(req, res);
};

const handler = async (req, res) => {
  try {
    const delay = (duration) => new Promise(resolve => setTimeout(resolve, duration));
    await delay(6000); // artificial delay for testing

    const filePath = path.join(__dirname, '..', '_includes', '_subtle-ads.html');
    const htmlContent = await fs.readFile(filePath, 'utf8');

    res.setHeader('Content-Type', 'text/html');
    res.setHeader('Cache-Control', 'public, max-age=3600');
    res.status(200).end(htmlContent);
  } catch (error) {
    res.status(500).end('Error loading the HTML file.');
  }
};

module.exports = allowCors(handler);
```

Because the backend is part of the same repository as the `_subtle-ads.html` file, it can access the training data at runtime using a relative path:

`const filePath = path.join(__dirname, '..', '_includes', '_subtle-ads.html');`

The fragment itself is generated from `_data/trainings.yml` by
[`scripts/generate_subtle_ads.rb`](/scripts/generate_subtle_ads.rb) — run it
after every date change while the fragment still exists; CI fails if the
committed fragment is stale.

### How Deployment Works

When you commit and push changes to the repo:

- **GitHub** rebuilds the Jekyll frontend (`trainings.arc42.org`), which also
  republishes the JSON feed at `/api/trainings.json`
- **Vercel** *(deprecated path)* detects the push and automatically re-deploys
  the serverless backend; the repository contents (including
  `_subtle-ads.html`) are bundled into the function’s read-only filesystem

## Frontend Integration

- **trainings.arc42.org** renders its own home-page timelines (`_includes/timeline_auto.html`, once per language) straight from `_data/trainings.yml` at build time and uses neither the JSON feed nor the deprecated backend. This ensures availability even if everything else fails.
- **The consumer sites** (see [Consumers](#consumers)) render the dates at build time from their committed, expiry-filtered copy of the JSON feed. The former runtime HTMX fetch against the Vercel fragment backend is deprecated; any site still on it should migrate per the rollout plan referenced above.

## Maintaining training dates

All training dates for ALL arc42 sites live in `_data/trainings.yml` (single
source of truth). To change dates:

1. Edit `_data/trainings.yml` on a branch (every date needs `language: de|en` —
   validation fails loudly otherwise).
2. Run `ruby scripts/validate_trainings.rb` and `ruby scripts/generate_subtle_ads.rb`.
3. Open a PR. CI re-validates and checks the generated fragment is fresh.
4. On merge: GitHub Pages republishes `/api/trainings.json`; Vercel redeploys the
   deprecated htmx fragment; the [consumer sites](#consumers) pull the JSON
   weekly (Monday mornings UTC, staggered crons), immediately if the
   `CONSUMER_DISPATCH_TOKEN` secret is set
   (fine-grained PAT, Contents read/write on the consumer repos), or on demand
   via their `Refresh training dates` workflow_dispatch button.

## Created with [OneFlow Jekyl Theme](https://oneflow-jekyll-theme.github.io/)
