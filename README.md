# trainings.arc42.org-site

This repository powers [trainings.arc42.org](https://trainings.arc42.org), which displays a list of upcoming arc42 training dates, and includes backend functionality to dynamically serve these dates on other arc42-related sites.

# Overview

This project includes both frontend and backend functionality, used by multiple arc42-related sites to show consistent, up-to-date training info.

## Key Process

All training dates are maintained in a single HTML file ([`/_includes/_subtle-ads.html`](/_includes/_subtle-ads.html)) and distributed across sites via:

- A static Jekyll include on trainings.arc42.org
- A backend API used by other sites (served via Vercel)
 
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

## Created with [OneFlow Jekyl Theme](https://oneflow-jekyll-theme.github.io/)
