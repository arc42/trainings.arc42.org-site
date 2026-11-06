#!/usr/bin/env bash
# Offline test for scripts/verify_consumers.sh: the expectation per site must
# follow each course's show_on. Fixtures are dated 2099 so they never expire.
set -u
here=$(cd "$(dirname "$0")" && pwd)
script="$here/../verify_consumers.sh"
fx="$here/fixtures"
fail=0

run() { # feed-file sites -> exit status, output in $out
  out=$(env -u GITHUB_EVENT_NAME -u GITHUB_ACTIONS ATTEMPTS=1 SLEEP_SECONDS=0 \
    FEED_URL="file://$1" SITES="$2" "$script" 2>&1)
}
expect_pass() { run "$2" "$3" || { echo "FAIL: $1"; echo "$out"; fail=1; return; }; echo "ok: $1"; }
expect_fail() { run "$2" "$3" && { echo "FAIL: $1 (expected failure)"; echo "$out"; fail=1; return; }; echo "ok: $1"; }

sites="faq.arc42.org|chrono|file://$fx/faq.html
arc42.org|chrono|file://$fx/arc42org.html
examples.arc42.org|per-course|file://$fx/examples.html"

expect_pass "sites expect only the courses they are listed for" "$fx/feed.json" "$sites"

# arc42.org is listed in MAS's show_on, so a page without MAS must fail.
expect_fail "a listed site missing the MAS date fails" "$fx/feed.json" \
  "arc42.org|chrono|file://$fx/faq.html"

# Courses with dates: [] or no dates key are in the feed.json already; make
# that explicit so a regression there is named.
expect_pass "courses with empty or absent dates are tolerated" "$fx/feed.json" \
  "faq.arc42.org|chrono|file://$fx/faq.html"

exit $fail
