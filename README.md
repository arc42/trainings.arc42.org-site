# trainings.arc42.org-site

This repository powers [trainings.arc42.org](https://trainings.arc42.org), which displays a list of upcoming arc42 training dates, and includes backend functionality to dynamically serve these dates on other arc42-related sites.

# Overview

This project includes both frontend and backend functionality, used by multiple arc42-related sites to show consistent, up-to-date training info.

## Key Process

All training dates are maintained in a single HTML file ([`/_includes/_subtle-ads.html`](/_includes/_subtle-ads.html)) and distributed across sites via:

- A static Jekyll include on trainings.arc42.org
- A backend API used by other sites (served via Vercel)

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

## Updating Training Dates (requires write access)

To change or add training dates:

1. Edit [`/_includes/_subtle-ads.html`](/_includes/_subtle-ads.html)
2. Commit and push your changes

This automatically updates the content:

- On trainings.arc42.org (via Jekyll include)
- Across other arc42 sites (via the backend API)

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

- **trainings.arc42.org** includes `_subtle-ads.html` directly via Jekyll and does *not* use the backend. This ensures availability even if the backend fails.
- **All other arc42 sites** load the training data dynamically using HTMX, which fetches the HTML from the backend API and replaces a placeholder div. On these sites, the HTMX snippet is contained in a Jekyll include as well, and can be inserted via `{% include subtle-ads/subtle-ads.html %}`.

## Fallback Behavior

If the backend is unreachable or blocked (e.g. by browser settings), users are directed to [trainings.arc42.org](https://trainings.arc42.org), which always reflects the latest content via the static include.

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
