#!/usr/bin/env bash
# Launch Chrome on a disposable profile with bang pre-set as the default search
# engine. Touches nothing belonging to your real Chrome: separate --user-data-dir,
# thrown away on exit.
#
# The omnibox has no headless equivalent, so the actual typing is manual. This
# script only removes the setup and teardown from that loop.
set -euo pipefail

CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
PROFILE="$(mktemp -d /tmp/bang-chrome-XXXXXX)"
PORT="${PORT:-8111}"

[ -x "$CHROME" ] || {
  echo "Chrome not found at $CHROME" >&2
  exit 1
}

cleanup() {
  rm -rf "$PROFILE"
  echo "removed $PROFILE"
}
trap cleanup EXIT

mkdir -p "$PROFILE/Default"

# Chrome's internal template syntax is {searchTerms}; the %s in the settings UI
# is only a front-end convenience.
cat >"$PROFILE/Default/Preferences" <<EOF
{
  "default_search_provider_data": {
    "template_url_data": {
      "short_name": "bang",
      "keyword": "bang",
      "url": "http://127.0.0.1:${PORT}/?q={searchTerms}",
      "favicon_url": "http://127.0.0.1:${PORT}/favicon.ico",
      "id": 1,
      "prepopulate_id": 0,
      "sync_guid": "bang-throwaway-profile",
      "safe_for_autoreplace": false,
      "created_by_policy": 0
    }
  },
  "browser": { "has_seen_welcome_page": true }
}
EOF

echo "profile:  $PROFILE"
echo "resolver: http://127.0.0.1:${PORT}/"
echo
echo "Type these into the address bar, then check the resolver log:"
echo "    pr    #123    is    m jazz fusion    y Go concurrency"
echo
echo "The profile ships with bang already set as the default engine, so this"
echo "skips the Activate / Make default dance and tests the rules themselves."
echo "To rehearse that setup instead, see docs/browsers.md."
echo
echo "Close Chrome to delete the profile."

# Not exec: that would replace this shell and discard the cleanup trap, leaving
# the profile behind.
"$CHROME" \
  --user-data-dir="$PROFILE" \
  --no-first-run \
  --no-default-browser-check \
  --disable-sync \
  "http://127.0.0.1:${PORT}/"
