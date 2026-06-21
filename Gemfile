source "https://rubygems.org"

# jekyll itself is provided and version-pinned by the github-pages gem, so we do
# not pin it directly here. This keeps us on the GitHub Pages-supported stack
# (currently jekyll 3.10.x + liquid 4.0.4), matching arc42/quality.arc42.org-site
# and avoiding the old liquid `tainted?` / `csv` breakage on modern Ruby.
group :jekyll_plugins do
  gem "github-pages"
  gem "jekyll-include-cache"
  gem "jekyll-github-metadata"
  gem "jekyll-sitemap"
  gem "jekyll-seo-tag"
  gem "webrick"
end
