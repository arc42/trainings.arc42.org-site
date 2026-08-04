#!/usr/bin/env ruby
# Regenerates _includes/_subtle-ads.html from _data/trainings.yml.
# The file is served (a) as a Jekyll include on the home page and (b) verbatim
# by api/index.js on Vercel (legacy htmx feed for docs/faq). NEVER hand-edit it.
# Usage: generate_subtle_ads.rb [--date YYYY-MM-DD] [--check]
require "yaml"
require "date"

root   = File.expand_path("..", __dir__)
target = File.join(root, "_includes", "_subtle-ads.html")
check  = ARGV.delete("--check")
run_date = if (i = ARGV.index("--date")) then Date.parse(ARGV[i + 1]) else Date.today end
# --check must be reproducible: reuse the date stamped in the committed file
if check && File.exist?(target) && (m = File.read(target).match(/Dates updated: (\d{4}-\d{2}-\d{2})/))
  run_date = Date.parse(m[1])
end

MONTHS_DE = %w[Januar Februar März April Mai Juni Juli August September Oktober November Dezember].freeze
MONTHS_EN = %w[Jan Feb Mar Apr May Jun Jul Aug Sep Oct Nov Dec].freeze

def label(d, lang)
  s = Date.parse(d["start"]); e = Date.parse(d["end"])
  if lang == "de"
    if s.month == e.month && s.year == e.year
      "#{s.day}.-#{e.day}. #{MONTHS_DE[s.month - 1]} #{s.year}"
    elsif s.year == e.year
      "#{s.day}. #{MONTHS_DE[s.month - 1]} - #{e.day}. #{MONTHS_DE[e.month - 1]} #{s.year}"
    else
      "#{s.day}. #{MONTHS_DE[s.month - 1]} #{s.year} - #{e.day}. #{MONTHS_DE[e.month - 1]} #{e.year}"
    end
  else
    if s.month == e.month && s.year == e.year
      "#{MONTHS_EN[s.month - 1]} #{s.day}-#{e.day}, #{s.year}"
    elsif s.year == e.year
      "#{MONTHS_EN[s.month - 1]} #{s.day} - #{MONTHS_EN[e.month - 1]} #{e.day}, #{s.year}"
    else
      "#{MONTHS_EN[s.month - 1]} #{s.day}, #{s.year} - #{MONTHS_EN[e.month - 1]} #{e.day}, #{e.year}"
    end
  end
end

def li(d, course, lang)
  trainers = d["trainers"] || course["trainers"] || []
  joiner = lang == "de" ? " und " : " and "
  place = d["format"] == "online" ? "online" : d["city"]
  text = [label(d, lang), place, trainers.join(joiner)].reject { |p| p.nil? || p.empty? }.join(", ")
  %(        <li><a href="#{d['url']}">#{text}</a></li>)
end

data    = YAML.safe_load(File.read(File.join(root, "_data", "trainings.yml")))
courses = data["courses"]
live    = ->(d) { d["status"] != "cancelled" && Date.parse(d["end"]) >= run_date }

msa      = courses.find { |c| c["id"] == "msa" }
advanced = courses.reject { |c| c["id"] == "msa" }
msa_en = msa["dates"].select { |d| live.(d) && d["language"] == "en" }
msa_de = msa["dates"].select { |d| live.(d) && d["language"] == "de" }

out = +"<!-- GENERATED FILE - do not edit. Source: _data/trainings.yml -->\n"
out << "<!-- Regenerate: ruby scripts/generate_subtle_ads.rb -->\n"
out << "<!-- Served via api/index.js (Vercel htmx feed for docs/faq); no longer included on this site's pages. -->\n"
out << "<!-- Heading levels start at h3: this block is injected below an h2 on embedding pages. -->\n\n"
out << "<div class=\"subtle-ad\">\n"
out << "    <h3>arc42 offers architecture training.</h3>\n"
out << "    #{msa['blurb'].strip.gsub("\n", ' ')}\n"
out << "\n    <h4>Next available dates (in <strong>English</strong>)</h4>\n"
if msa_en.empty?
  # ADR-0006 mandates an empty state: never render a heading with nothing under it.
  out << "    <p>No English-language dates are currently scheduled - see the German dates below or <a href=\"https://arc42.org/about/#contact\">contact us</a> for inhouse training.</p>\n"
else
  out << "    <ul>\n"
  msa_en.each { |d| out << li(d, msa, "en") << "\n" }
  out << "    </ul>\n"
end
unless msa_de.empty?
  out << "\n    <h4>Next available dates (in <strong>German</strong>)</h4>\n    <ul lang=\"de\">\n"
  msa_de.each { |d| out << li(d, msa, "de") << "\n" }
  out << "    </ul>\n"
end
out << "\n    <h3>iSAQB Advanced Topics (in German)</h3>\n"
advanced.each do |c|
  dates = c["dates"].select { |d| live.(d) }
  next if dates.empty?
  out << "    <h4>#{c['title']}</h4>\n"
  out << "    #{c['blurb'].strip.gsub("\n", ' ')}\n" if c["blurb"]
  out << "    <ul lang=\"de\">\n"
  dates.each { |d| out << li(d, c, "de") << "\n" }
  out << "    </ul>\n\n"
end
out << "    <p>Interested in inhouse training? <a href=\"https://arc42.org/about/#contact\">Contact us</a>.</p>\n\n"
out << "    <p class=\"subtle-ad__updated\" style=\"text-align: right; font-size: smaller;\">\n"
out << "        Dates updated: #{run_date.strftime('%Y-%m-%d')}\n    </p>\n</div>\n"

if check
  if File.read(target) == out
    puts "OK: _subtle-ads.html is up to date"
  else
    warn "STALE: _includes/_subtle-ads.html does not match _data/trainings.yml"
    warn "Run: ruby scripts/generate_subtle_ads.rb"
    exit 1
  end
else
  File.write(target, out)
  puts "Wrote #{target}"
end
