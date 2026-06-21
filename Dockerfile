# Build & serve the arc42 trainings site (Jekyll).
#
# Tracks the GitHub Pages-supported stack (github-pages 232 -> jekyll 3.10.0 +
# liquid 4.0.4), which runs on modern Ruby. Pinned to Ruby 3.3 to match
# arc42/quality.arc42.org-site. (Older jekyll 3.9.0 / liquid 4.0.3 broke here on
# newer Ruby via the removed `csv` default gem and `Object#tainted?`.)

FROM ruby:3.3

LABEL description="develop and generate the arc42 trainings site"
LABEL vendor="arc42 (Gernot Starke)"

WORKDIR /srv/jekyll

RUN gem install bundler:2.5.4

COPY Gemfile Gemfile.lock ./
RUN bundle install --jobs 4 --retry 3

EXPOSE 4000
CMD ["bundle", "exec", "jekyll", "serve", "--host", "0.0.0.0", "--force_polling"]
