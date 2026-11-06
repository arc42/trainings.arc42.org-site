#!/usr/bin/env ruby
# Checks the BUILT site: every upcoming date in _data/trainings.yml must have
# a card with id="<date id>" on both home pages (/ and /de/).
# Why: a date can validate, reach /api/trainings.json and still render as
# nothing (an unknown card template falls through to an HTML comment), which
# is how improve-apr-2027 shipped invisible. Only the output shows that.
# "Upcoming" mirrors _includes/timeline_auto.html: not cancelled, end >= today
# (build date). full and waitlist dates still render, greyed.
# Usage: ruby scripts/check_home_cards.rb [_data/trainings.yml] [_site]
# TODAY=YYYY-MM-DD overrides the date (for testing).
require "yaml"
require "date"

data = ARGV[0] || "_data/trainings.yml"
site = ARGV[1] || "_site"
today = ENV["TODAY"] || Date.today.strftime("%Y-%m-%d")

pages = { "#{site}/index.html" => nil, "#{site}/de/index.html" => nil }
pages.each_key do |p|
  abort "#{p} not found - run `make site` first" unless File.exist?(p)
  pages[p] = File.read(p)
end

courses = YAML.safe_load(File.read(data), permitted_classes: [Date])["courses"] || []
errors = []
count = 0
courses.each do |c|
  (c["dates"] || []).each do |d|
    next if d["status"] == "cancelled" || d["end"].to_s < today
    count += 1
    pages.each do |path, html|
      next if html.include?(%(id="#{d["id"]}"))
      errors << "date #{d["id"]} (#{c["short_title"]}, #{d["start"]}) is not on #{path}"
    end
  end
end

if errors.empty?
  puts "OK: #{count} dates on both home pages"
else
  puts errors
  exit 1
end
