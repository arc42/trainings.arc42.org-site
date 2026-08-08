---
layout: null
sitemap: false
---
# CRUSH.md - arc42 Trainings Site

## Build/Development Commands
Everything runs in Docker via the Makefile - no local Ruby/bundler needed.
- `make help` - List all targets
- `make dev` - Start the dev server with live reload (http://localhost:4000)
- `make stop` - Stop and remove the running dev container
- `make site` - Build the static site into `_site/`
- `make check-links` - Build, then run html-proofer over `_site/`
- `make install` - Re-resolve gems after editing the `Gemfile`
- `make clean` - Remove `_site/`, Jekyll caches and the Docker volumes
- `npm run build:js` - Build and minify JavaScript assets
- `npm run watch:js` - Watch JS files for changes during development

## Testing
- `ruby scripts/validate_trainings.rb` - Validate `_data/trainings.yml`
- `make check-links` - html-proofer: internal links, images, `alt` attributes, in-page anchors
- No unit-test suite; everything else is manual testing via `make dev`

## Code Style Guidelines

### Jekyll/Liquid Templates
- Use Jekyll front matter with `---` delimiters
- Store reusable content in `_includes/` directory
- Use semantic HTML5 elements

### Bilingual site (EN `/`, DE `/de/`)
- Every page front matter carries `lang: en|de`; the eight pages that have a twin
  also carry `translation_url:` (the twin's URL). `/imprint/` and `404.html` have
  no twin, so no `translation_url` and no switch. The masthead builds the DE | EN
  switch from those two fields. There is no site search — the theme's search
  toggle was removed and the switch occupies its slot
- German pages also carry `locale: de_DE` — the vendored `_includes/seo.html`
  (no jekyll-seo-tag plugin) emits `og:locale` from `page.locale`
- Masthead nav is per language: `_data/navigation.yml` holds `main` (EN) and
  `main_de` (DE); `page.lang == "de"` selects `main_de`
- `_includes/head.html` emits the `hreflang` triple (en, de, x-default) — *not*
  `_includes/head/custom.html`, which holds favicons, theme-color, font preloads
- `_pages/home.html` and `_pages/home-de.html` are structural twins: same layout,
  same section ids (`training-dates`, `contact`, `license`), same classes. Only
  the language and the form URL differ (`/registration/` vs `/anmeldung/`).
  Change one, change the other
- Timeline cards follow the *page* language via the `page_lang` parameter of
  `_includes/timeline_auto.html`; the language a training is *held* in comes from
  `language:` in `_data/trainings.yml` and renders as a small per-card note

### SCSS/CSS
- Follow BEM-like naming conventions for CSS classes
- Use SCSS imports from `_sass/oneflow/` directory
- Compressed output style in production
- Organize styles by component in separate files

### HTML Structure
- Use semantic HTML5 elements (`<section>`, `<article>`, `<nav>`)
- Include proper meta tags and SEO elements
- Maintain accessibility with proper ARIA labels
- Use `.webp` format for images when possible

### Configuration
- Site settings in `_config.yml` - restart server after changes
- Training dates maintained in `_data/trainings.yml` (single source of truth);
- Use Jekyll collections and data files for structured content

### JavaScript
- Concatenate and minify JS files via npm scripts
- Store source files in `assets/js/` directory
- Use jQuery for DOM manipulation (existing pattern)