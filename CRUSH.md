---
layout: null
sitemap: false
---
# CRUSH.md - arc42 Trainings Site

## Build/Development Commands
- `bundle install` - Install Ruby gems
- `bundle exec jekyll serve` - Start Jekyll development server (localhost:4000)
- `docker-compose up` - Start Jekyll in Docker container (port 4000)
- `./_manage-site.sh` - Interactive Docker development script
- `npm run build:js` - Build and minify JavaScript assets
- `npm run watch:js` - Watch JS files for changes during development

## Testing
- No automated tests configured - manual testing via local server

## Code Style Guidelines

### Jekyll/Liquid Templates
- Use Jekyll front matter with `---` delimiters
- Store reusable content in `_includes/` directory
- Use semantic HTML5 elements

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
- Training dates maintained in `_includes/_subtle-ads.html`
- Use Jekyll collections and data files for structured content

### JavaScript
- Concatenate and minify JS files via npm scripts
- Store source files in `assets/js/` directory
- Use jQuery for DOM manipulation (existing pattern)