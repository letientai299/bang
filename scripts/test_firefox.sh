#!/usr/bin/env bash
# Launch Firefox on a profile dedicated to bang, kept apart from your real one by
# --profile. Nothing outside that directory is touched.
#
# The default engine has to be picked once, by hand, in this profile's settings.
# The SearchEngines enterprise policy could do it instead, but a macOS
# configuration profile takes precedence over any policies.json, so on a managed
# machine that route is not available. The profile is kept between runs so the
# manual step happens once rather than every time.
#
# The address bar has no headless equivalent, so the typing stays manual too.
set -euo pipefail

PORT="${PORT:-8111}"
URL="http://127.0.0.1:${PORT}/"
PROFILE="${PROFILE:-${XDG_CACHE_HOME:-$HOME/.cache}/bang/firefox-profile}"

usage() {
  cat <<EOF
usage: ${0##*/} [--reset]

  --reset   delete the profile first, to redo the setup from scratch

env: PORT (default 8111), PROFILE, FIREFOX (path to the binary)
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

for candidate in \
  "${FIREFOX:-}" \
  "/Applications/Firefox.app/Contents/MacOS/firefox" \
  "/Applications/Firefox Developer Edition.app/Contents/MacOS/firefox" \
  "/Applications/Firefox Nightly.app/Contents/MacOS/firefox"; do
  [ -n "$candidate" ] && [ -x "$candidate" ] && FIREFOX="$candidate" && break
done

[ -n "${FIREFOX:-}" ] && [ -x "${FIREFOX:-}" ] || {
  echo "no Firefox binary found; set FIREFOX to its path" >&2
  exit 1
}

curl -fsS -o /dev/null --max-time 2 "$URL" || {
  echo "warning: no resolver answering at $URL — start it with 'mise run dev'" >&2
}

first_run=no
[ -d "$PROFILE" ] || first_run=yes
mkdir -p "$PROFILE"

# user.js is re-read on every start, so these hold even if the profile is
# poked at by hand. keyword.enabled is what lets bare input like `pr` reach the
# search engine instead of being tried as a hostname.
cat >"$PROFILE/user.js" <<'EOF'
user_pref("keyword.enabled", true);
user_pref("browser.shell.checkDefaultBrowser", false);
user_pref("datareporting.policy.dataSubmissionEnabled", false);
EOF

echo "browser:  $FIREFOX"
echo "profile:  $PROFILE"
echo "resolver: $URL"
echo

if [ "$first_run" = yes ]; then
  cat <<EOF
New profile — set bang as the default engine once, in the window that opens:

    1. on the resolver page, click the ⋮⋮⋮ menu in the address bar
       (or right-click the address bar) and choose Add "bang"
    2. Settings → Search → Default Search Engine → bang

See docs/browsers.md if the engine is not offered.

EOF
fi

cat <<EOF
Type these into the address bar, then check the resolver log:

    pr    #123    is    m jazz fusion    y Go concurrency

The profile survives until --reset, so the setup above is a one-time cost.
EOF

# MOZ_NO_REMOTE with --new-instance: without them a running Firefox would just
# open a tab in your real profile and this one would never start.
MOZ_NO_REMOTE=1 exec "$FIREFOX" --profile "$PROFILE" --new-instance "$URL"
