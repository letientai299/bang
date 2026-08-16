#!/usr/bin/env bash
# Launch Chrome on a profile dedicated to bang, kept apart from your real one by
# --user-data-dir. Nothing outside that directory is touched.
#
# The default engine has to be picked once, by hand, in this profile's settings:
# Chrome signs its Default Search Engine setting and reverts any copy written
# from outside the browser ("Chrome reset these settings"). The profile is kept
# between runs so that click happens once rather than every time.
#
# The omnibox has no headless equivalent, so the typing stays manual too.
set -euo pipefail

PORT="${PORT:-8111}"
URL="http://127.0.0.1:${PORT}/"
PROFILE="${PROFILE:-${XDG_CACHE_HOME:-$HOME/.cache}/bang/chrome-profile}"

usage() {
  cat <<EOF
usage: ${0##*/} [--reset]

  --reset   delete the profile first, to redo the setup from scratch

env: PORT (default 8111), PROFILE, CHROME (path to the binary)
EOF
}

case "${1:-}" in
  --reset)
    rm -rf "$PROFILE"
    echo "removed $PROFILE"
    ;;
  -h | --help)
    usage
    exit 0
    ;;
  "") ;;
  *)
    usage >&2
    exit 2
    ;;
esac

# Canary and Beta install alongside stable, so any of them can host the profile.
for candidate in \
  "${CHROME:-}" \
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
  "/Applications/Google Chrome Beta.app/Contents/MacOS/Google Chrome Beta" \
  "/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary"; do
  [ -n "$candidate" ] && [ -x "$candidate" ] && CHROME="$candidate" && break
done

[ -n "${CHROME:-}" ] && [ -x "${CHROME:-}" ] || {
  echo "no Chrome binary found; set CHROME to its path" >&2
  exit 1
}

curl -fsS -o /dev/null --max-time 2 "$URL" || {
  echo "warning: no resolver answering at $URL — start it with 'mise run dev'" >&2
}

first_run=no
[ -d "$PROFILE" ] || first_run=yes
mkdir -p "$PROFILE"

echo "browser:  $CHROME"
echo "profile:  $PROFILE"
echo "resolver: $URL"
echo

if [ "$first_run" = yes ]; then
  cat <<EOF
New profile — set bang as the default engine once, in the window that opens:

    1. chrome://settings/searchEngines
    2. find bang under Site search, click Activate
    3. its ⋮ menu → Make default

The resolver page is opened for you, which is what makes Chrome offer bang
there at all. See docs/browsers.md if it does not show up.

EOF
fi

cat <<EOF
Type these into the address bar, then check the resolver log:

    pr    #123    is    m jazz fusion    y Go concurrency

The profile survives until --reset, so the setup above is a one-time cost.
EOF

exec "$CHROME" \
  --user-data-dir="$PROFILE" \
  --no-first-run \
  --no-default-browser-check \
  --disable-sync \
  "$URL"
