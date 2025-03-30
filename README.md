# trainings.arc42.org-site

This repository powers [trainings.arc42.org](https://trainings.arc42.org), which displays a list of upcoming arc42 training dates, and includes backend functionality to dynamically serve these dates on other arc42-related sites.

# Overview

This project includes both frontend and backend functionality, used by multiple arc42-related sites to show consistent, up-to-date training info.

## Key Process

All training dates are maintained in a single HTML include ([`/_includes/_subtle-ads.html`](/_includes/_subtle-ads.html)) and distributed across sites via:

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

The backend is deployed on Vercel.

It serves the contents of `_subtle-ads.html` as raw HTML via this endpoint:

```
https://arc42-subtle-ads-backend.vercel.app/api
```

The endpoint returns the HTML with appropriate CORS and caching headers. The backend is automatically redeployed on each push to this repository.

## Frontend Integration

- **trainings.arc42.org** includes `_subtle-ads.html` directly via Jekyll and does *not* use the backend. This ensures availability even if the backend fails.
- **All other arc42 sites** load the training data dynamically using HTMX, which fetches the HTML from the backend API and replaces a placeholder div on page load. On these sites, the HTMX snippet is contained in a Jekyll include as well, and can be inserted via `{% include subtle-ads/subtle-ads.html %}`.

## Fallback Behavior

If the backend is unreachable or blocked (e.g. by browser settings), users are directed to [trainings.arc42.org](https://trainings.arc42.org), which always reflects the latest content via the static include.

## Created with [OneFlow Jekyl Theme](https://oneflow-jekyll-theme.github.io/)
