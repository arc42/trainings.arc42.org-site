#!/usr/bin/env ruby
# Validates _data/trainings.yml - the single source of truth for training dates.
# Zero-dependency (Ruby stdlib). Prints every problem, exits 1 on any error.
require "yaml"
require "date"

DATE_RE  = /\A\d{4}-\d{2}-\d{2}\z/
STATUSES = %w[open waitlist full cancelled].freeze
FORMATS  = %w[public inhouse online].freeze
LANGS    = %w[de en].freeze
CREDIT_CATEGORIES = %w[methodical technical communication].freeze

# NO PROSE IN trainings.yml. These three keys held hand-written German
# sentences that were rendered verbatim on the English pages, and `pricing`
# additionally buried an early-bird deadline where no machine could see it, so
# the site advertised an expired price for weeks. They are now numbers and
# flags, with the wording built per language in _includes/*-label.html. The
# published feed still carries the old keys, generated, so consumers did not
# have to change; see api/trainings.json.
RETIRED_DATE_KEYS = {
  "pricing"   => "Use price: {amount:, currency:, early_bird: {amount:, until:}} - integers, not a sentence.",
  "few_seats" => "Use seats_limited: true - the wording belongs to whoever renders it."
}.freeze

path = ARGV[0] || File.expand_path("../_data/trainings.yml", __dir__)
# permitted_classes: [Date] on purpose. Unquoted YYYY-MM-DD is a Date to YAML,
# and without this safe_load raises Psych::DisallowedClass and dies with a
# stack trace - which is a worse error message than the one this script
# already has ready for exactly that mistake. Letting the Date through means
# the "must be a QUOTED string" checks below are reachable and get to explain
# themselves.
data = YAML.safe_load(File.read(path), permitted_classes: [Date])
errors = []

courses = data && data["courses"]
errors << "top-level 'courses' array missing or empty" unless courses.is_a?(Array) && !courses.empty?

ids = Hash.new(0)
codes = Hash.new(0)

(courses || []).each do |c|
  cid = c["id"] || "<missing course id>"
  %w[id short_title title url].each do |f|
    errors << "course #{cid}: missing/empty '#{f}'" unless c[f].is_a?(String) && !c[f].empty?
  end
  # url_en is optional (English detail page on trainings.arc42.org); when
  # present it must be a non-empty https URL, exactly like url.
  if c.key?("url_en") && !(c["url_en"].is_a?(String) && c["url_en"].start_with?("https://"))
    errors << "course #{cid}: 'url_en' must be an https URL when present (got #{c['url_en'].inspect})"
  end
  if c.key?("credits")
    errors << "course #{cid}: 'credits' was retired. Use credit_points: {methodical:, technical:, communication:} - the prose is built per language by _includes/credits-label.html."
  end
  if c.key?("credit_points")
    cp = c["credit_points"]
    if !cp.is_a?(Hash)
      errors << "course #{cid}: 'credit_points' must be a mapping of category => integer"
    else
      extra = cp.keys - CREDIT_CATEGORIES
      errors << "course #{cid}: unknown credit category/categories: #{extra.join(', ')} (known: #{CREDIT_CATEGORIES.join(', ')})" unless extra.empty?
      errors << "course #{cid}: 'credit_points' needs at least one category" if cp.empty?
      cp.each do |cat, n|
        errors << "course #{cid}: credit_points.#{cat} must be a non-negative integer (got #{n.inspect})" unless n.is_a?(Integer) && n >= 0
      end
    end
  end
  errors << "course #{cid}: 'trainers' must be a non-empty array" unless c["trainers"].is_a?(Array) && !c["trainers"].empty?
  errors << "course #{cid}: missing 'dates' array" unless c["dates"].is_a?(Array)

  (c["dates"] || []).each do |d|
    did = d["id"] || "<missing date id in #{cid}>"
    ids[did] += 1
    codes[d["code"]] += 1 if d["code"]
    %w[id code language format status url].each do |f|
      errors << "date #{did}: missing/empty '#{f}'" unless d[f].is_a?(String) && !d[f].empty?
    end
    %w[start end].each do |f|
      errors << "date #{did}: '#{f}' must be a QUOTED \"YYYY-MM-DD\" string (got #{d[f].inspect})" unless d[f].is_a?(String) && d[f] =~ DATE_RE
    end
    if d["start"].is_a?(String) && d["end"].is_a?(String) && d["end"] < d["start"]
      errors << "date #{did}: end #{d['end']} before start #{d['start']}"
    end
    errors << "date #{did}: language must be de|en - NO DEFAULT" unless LANGS.include?(d["language"])
    errors << "date #{did}: format must be #{FORMATS.join('|')}" unless FORMATS.include?(d["format"])
    errors << "date #{did}: status must be #{STATUSES.join('|')}" unless STATUSES.include?(d["status"])
    if d["format"] != "online" && !(d["city"].is_a?(String) && !d["city"].empty?)
      errors << "date #{did}: non-online date needs a 'city'"
    end
    if d.key?("country") && !(d["country"].is_a?(String) && d["country"] =~ /\A[A-Z]{2}\z/)
      errors << "date #{did}: country must be a quoted ISO 3166-1 alpha-2 code (got #{d['country'].inspect})"
    end

    # The retired prose fields. Rejected by name so that reintroducing one is a
    # build failure rather than a German sentence quietly appearing on the
    # English pages - which is exactly how they got here.
    RETIRED_DATE_KEYS.each do |old_key, hint|
      errors << "date #{did}: '#{old_key}' was retired. #{hint}" if d.key?(old_key)
    end

    if d.key?("price")
      pr = d["price"]
      if !pr.is_a?(Hash)
        errors << "date #{did}: 'price' must be a mapping (amount/currency/early_bird)"
      else
        errors << "date #{did}: price.amount must be a non-negative integer (got #{pr['amount'].inspect})" unless pr["amount"].is_a?(Integer) && pr["amount"] >= 0
        errors << "date #{did}: price.currency must be a quoted 3-letter code (got #{pr['currency'].inspect})" unless pr["currency"].is_a?(String) && pr["currency"] =~ /\A[A-Z]{3}\z/
        if pr.key?("alumni")
          errors << "date #{did}: price.alumni must be a non-negative integer (got #{pr['alumni'].inspect})" unless pr["alumni"].is_a?(Integer) && pr["alumni"] >= 0
          if pr["alumni"].is_a?(Integer) && pr["amount"].is_a?(Integer) && pr["alumni"] >= pr["amount"]
            errors << "date #{did}: price.alumni #{pr['alumni']} is not below the regular price #{pr['amount']}"
          end
        end
        extra = pr.keys - %w[amount currency alumni early_bird]
        errors << "date #{did}: unknown key(s) under price: #{extra.join(', ')}" unless extra.empty?

        if pr.key?("early_bird")
          eb = pr["early_bird"]
          if !eb.is_a?(Hash)
            errors << "date #{did}: price.early_bird must be a mapping (amount/until)"
          else
            errors << "date #{did}: price.early_bird.amount must be a non-negative integer (got #{eb['amount'].inspect})" unless eb["amount"].is_a?(Integer) && eb["amount"] >= 0
            unless eb["until"].is_a?(String) && eb["until"] =~ DATE_RE
              errors << "date #{did}: price.early_bird.until must be a QUOTED \"YYYY-MM-DD\" string (got #{eb['until'].inspect})"
            end
            # An offer priced at or above the regular price is a typo, not a deal.
            if eb["amount"].is_a?(Integer) && pr["amount"].is_a?(Integer) && eb["amount"] >= pr["amount"]
              errors << "date #{did}: price.early_bird.amount #{eb['amount']} is not below the regular price #{pr['amount']}"
            end
            # A deadline after the course has started cannot ever be met.
            if eb["until"].is_a?(String) && d["start"].is_a?(String) && eb["until"] > d["start"]
              errors << "date #{did}: price.early_bird.until #{eb['until']} is after the course starts (#{d['start']})"
            end
            # An expired offer is NOT an error: price-label.html and the feed
            # both drop it on their own. Deleting it by hand is optional.
          end
        end
      end
    end

    if d.key?("seats_limited") && ![true, false].include?(d["seats_limited"])
      errors << "date #{did}: 'seats_limited' must be true or false (got #{d['seats_limited'].inspect})"
    end
  end
end

ids.each   { |k, n| errors << "duplicate date id '#{k}'" if n > 1 }
codes.each { |k, n| errors << "duplicate booking code '#{k}'" if n > 1 }

if errors.empty?
  total = (courses || []).sum { |c| (c["dates"] || []).size }
  puts "OK: #{courses.size} courses, #{total} dates"
else
  errors.each { |e| warn "ERROR: #{e}" }
  exit 1
end
