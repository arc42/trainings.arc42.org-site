#!/usr/bin/env bash
#
# Are the four consumer sites actually SHOWING the current feed?
#
# Every other check in this chain stops at "the data is committed". That is not
# the same as "the page says so", and the gap is silent: in September 2026
# faq.arc42.org advertised a withdrawn Req4Arc date for a week while its
# _data/trainings.json was already correct, every workflow involved was green,
# and nothing anywhere was red. The cause then was that a push made with
# GITHUB_TOKEN fires no `on: push` trigger, so a workflow-built consumer never
# rebuilt. This script does not care about the cause: it reads the four
# published pages and compares them against the feed, so any reason a site stops
# republishing shows up as a failure here.
#
# How a page is checked, without needing to know how it is built: each consumer
# bakes the feed's date ids into its HTML, either as links to the arc42.de
# anchors (`termine#msa-dez-2026`) or as the anchors themselves (`id="..."`).
# So for every consumer:
#
#   · every date id on the page must still be published by the feed
#     -> catches a page serving a withdrawn, expired or edited date
#   · the earliest few published dates must appear on the page
#     -> catches a page that never picked up a newly added date
#
# The second check is deliberately shallow. Three of the four consumers cap the
# block at the next 8 dates, so only the earliest handful are guaranteed to be
# on every page; $LEAD stays well under that cap.
#
# Usage:  scripts/verify_consumers.sh          one pass, for a local check
#         ATTEMPTS=8 scripts/verify_consumers.sh   retry, for CI right after a change
set -euo pipefail

FEED_URL="${FEED_URL:-https://trainings.arc42.org/api/trainings.json}"
ATTEMPTS="${ATTEMPTS:-1}"
SLEEP_SECONDS="${SLEEP_SECONDS:-60}"
LEAD="${LEAD:-3}"

# One page per consumer that renders the block. Not the home pages: faq's and
# docs' are redirect stubs, and docs only carries the block on content pages.
SITES="faq.arc42.org|https://faq.arc42.org/questions/A-3/
docs.arc42.org|https://docs.arc42.org/section-1/
arc42.org|https://arc42.org/
www.arc42.de|https://www.arc42.de/termine"

# Ids that begin with a course id but are not dates. Empty today: every such id
# on all four sites is a date. If a consumer ever adds one (say id="msa-intro"),
# put it here rather than weakening the check for everything else.
ALLOWLIST="${ALLOWLIST:-}"

note() { printf '%s\n' "$*" >&2; }
problem() {
  if [ -n "${GITHUB_ACTIONS:-}" ]; then printf '::error::%s\n' "$*"; else printf 'FAIL  %s\n' "$*"; fi
}

feed=$(curl --fail --silent --show-error --location "$FEED_URL")
today=$(date -u +%Y-%m-%d)

# Published and bookable. Cancelled and full dates are dropped by the consumers
# themselves, so requiring them on a page would be a false alarm.
published=$(printf '%s' "$feed" | jq -r --arg t "$today" '
  [.courses[].dates[] | select(.end >= $t and .status != "cancelled" and .status != "full")]
  | sort_by(.start) | .[].id')
course_ids=$(printf '%s' "$feed" | jq -r '.courses[].id')

if [ -z "$published" ]; then
  problem "the feed publishes no bookable dates at all - checking the consumers would be meaningless"
  exit 1
fi

lead=$(printf '%s\n' "$published" | head -"$LEAD")
note "feed: $(printf '%s\n' "$published" | wc -l | tr -d ' ') bookable dates, earliest $(printf '%s' "$lead" | tr '\n' ' ')"

# Every id on the page that looks like a date id, i.e. starts with a course id.
page_date_ids() {
  { printf '%s' "$1" | grep -oE 'termine#[a-z0-9-]+' | sed 's/.*#//'
    printf '%s' "$1" | grep -oE 'id="[a-z0-9-]+"' | sed 's/^id="//; s/"$//'
  } | sort -u | while read -r id; do
        [ -n "$id" ] || continue
        printf '%s\n' "$course_ids" | while read -r c; do
          case "$id" in "$c"-*) printf '%s\n' "$id" ;; esac
        done
      done | sort -u
}

check_all() {
  failures=0
  printf '%s\n' "$SITES" | while IFS='|' read -r name url; do
    [ -n "$name" ] || continue
    if ! html=$(curl --fail --silent --show-error --location "$url"); then
      problem "$name: could not fetch $url"
      printf 'x' >> "$tally"; continue
    fi
    ids=$(page_date_ids "$html")
    if [ -z "$ids" ]; then
      problem "$name: $url renders no training dates at all"
      printf 'x' >> "$tally"; continue
    fi

    bad=""
    for id in $ids; do
      printf '%s\n' "$ALLOWLIST" | grep -Fxq "$id" && continue
      printf '%s\n' "$published" | grep -Fxq "$id" || bad="$bad $id"
    done
    gone=""
    for id in $lead; do
      printf '%s\n' "$ids" | grep -Fxq "$id" || gone="$gone $id"
    done

    if [ -n "$bad" ]; then
      problem "$name: shows date(s) the feed no longer publishes:$bad - the site has not rebuilt since they changed ($url)"
      printf 'x' >> "$tally"
    fi
    if [ -n "$gone" ]; then
      problem "$name: missing the earliest published date(s):$gone - the site has not picked up the current feed ($url)"
      printf 'x' >> "$tally"
    fi
    if [ -z "$bad" ] && [ -z "$gone" ]; then
      note "ok    $name ($(printf '%s\n' "$ids" | wc -l | tr -d ' ') dates shown)"
    fi
  done
  failures=$(wc -c < "$tally" | tr -d ' ')
  [ "$failures" -eq 0 ]
}

tally=$(mktemp)
trap 'rm -f "$tally"' EXIT

attempt=1
while : ; do
  : > "$tally"
  if check_all; then
    note "all four consumer sites are serving the current feed"
    exit 0
  fi
  if [ "$attempt" -ge "$ATTEMPTS" ]; then
    note "still failing after $attempt attempt(s)"
    exit 1
  fi
  note "attempt $attempt/$ATTEMPTS failed, retrying in ${SLEEP_SECONDS}s (a consumer may still be rebuilding)"
  attempt=$((attempt + 1))
  sleep "$SLEEP_SECONDS"
done
