#!/usr/bin/env ruby
# Validates _data/trainings.yml - the single source of truth for training dates.
# Zero-dependency (Ruby stdlib). Prints every problem, exits 1 on any error.
require "yaml"

DATE_RE  = /\A\d{4}-\d{2}-\d{2}\z/
STATUSES = %w[open waitlist full cancelled].freeze
FORMATS  = %w[public inhouse online].freeze
LANGS    = %w[de en].freeze

path = ARGV[0] || File.expand_path("../_data/trainings.yml", __dir__)
data = YAML.safe_load(File.read(path))
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
