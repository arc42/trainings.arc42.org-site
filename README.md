# trainings.arc42.org-site

This repository powers [trainings.arc42.org](https://trainings.arc42.org), which displays a list of upcoming arc42 training dates, and includes backend functionality to dynamically serve these dates on other arc42-related sites.

# Overview

This project includes both frontend and backend functionality, used by multiple arc42-related sites to show consistent, up-to-date training info.

## Key Process

All training dates are maintained in a single data file ([`/_data/trainings.yml`](/_data/trainings.yml)) and distributed across sites via:

- The timeline on this site's two home pages — the English `/` and the German
  `/de/`, both rendered from the same `_data/trainings.yml`
- A generated HTML fragment ([`/_includes/_subtle-ads.html`](/_includes/_subtle-ads.html)) served by the backend API to other sites (via Vercel)

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
- German pages additionally declare `locale: de_DE`, which is the key
  `jekyll-seo-tag` reads for `og:locale` (it does not look at `lang`).
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
3. Commit and push your changes

This automatically updates the content:

- On trainings.arc42.org (both home-page timelines render `_data/trainings.yml` directly)
- Across other arc42 sites (via the backend API, which serves the generated
  `_subtle-ads.html` fragment)

See [Maintaining training dates](#maintaining-training-dates) for the full
branch/PR workflow and how consumer sites pick the changes up.

## Backend API

The backend is deployed on Vercel as a simple serverless function, written in the format Vercel expects for [Next.js API routes](https://nextjs.org/docs/api-routes/introduction).

It reads the contents of `_subtle-ads.html` from the filesystem and serves it as raw HTML via this endpoint:

```
https://arc42-subtle-ads-backend.vercel.app/api
```

The endpoint returns the HTML with appropriate CORS and caching headers. The backend is automatically redeployed on each push to this repository, ensuring that updates to the training data are reflected across all consuming sites.

### Further Details

The function for the backend is located in [`/api/index.js`](/api/index.js).  
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

Because the backend is part of the same repository as the `_subtle-ads.html` file, we can access the training data at runtime using a relative path:

`const filePath = path.join(__dirname, '..', '_includes', '_subtle-ads.html');`

### How Deployment Works

When you commit and push changes to the repo:

- **GitHub** rebuilds the Jekyll frontend (`trainings.arc42.org`)
- **Vercel** detects the push and automatically re-deploys the serverless backend

During that deployment, the contents of the repository (including `_subtle-ads.html`) are bundled and made available in the serverless function’s read-only filesystem. This ensures the backend API always serves the latest training data—without any additional steps.


## Frontend Integration

- **trainings.arc42.org** renders its own home-page timelines (`_includes/timeline_auto.html`, once per language) straight from `_data/trainings.yml` at build time and does *not* use the backend, nor the generated `_subtle-ads.html`. This ensures availability even if the backend fails.
- **All other arc42 sites** load the training data dynamically using HTMX, which fetches the HTML from the backend API and replaces a placeholder div. On these sites, the HTMX snippet is contained in a Jekyll include as well, and can be inserted via `{% include subtle-ads/subtle-ads.html %}`.

## Fallback Behavior

If the backend is unreachable or blocked (e.g. by browser settings), users are directed to [trainings.arc42.org](https://trainings.arc42.org), which always reflects the latest content via its build-time timeline.

## Maintaining training dates

All training dates for ALL arc42 sites live in `_data/trainings.yml` (single
source of truth). To change dates:

1. Edit `_data/trainings.yml` on a branch (every date needs `language: de|en` —
   validation fails loudly otherwise).
2. Run `ruby scripts/validate_trainings.rb` and `ruby scripts/generate_subtle_ads.rb`.
3. Open a PR. CI re-validates and checks the generated fragment is fresh.
4. On merge: GitHub Pages republishes `/api/trainings.json`; Vercel redeploys the
   legacy htmx fragment; consumer sites (arc42.de, …) pull the JSON weekly
   (Mon 04:17 UTC), immediately if the `CONSUMER_DISPATCH_TOKEN` secret is set
   (fine-grained PAT, Contents read/write on the consumer repos), or on demand
   via their `Refresh training dates` workflow_dispatch button.

## Created with [OneFlow Jekyl Theme](https://oneflow-jekyll-theme.github.io/)
