# bang

Address-bar shortcuts. Type `!313` and land on merge request 313; type anything
else and land on Google as usual.

Runs as a loopback-only HTTP service set as the browser's **default search
engine**, which is the only hook that sees input with no keyword delimiter —
browser keyword shortcuts all require a Tab or Space, so they cannot express
`!313`. Because it is the default engine it sees everything typed into the
address bar, which is why it binds `127.0.0.1` and nothing leaves the machine.

## Install

```sh
go build -o ~/.local/bin/bang .
mkdir -p ~/.config/bang && cp config.yaml ~/.config/bang/config.yaml

sed -e "s|__HOME__|$HOME|g" -e "s|__CONFIG__|$HOME/.config/bang/config.yaml|" \
  com.taile.bang.plist > ~/Library/LaunchAgents/com.taile.bang.plist
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.taile.bang.plist
```

Keep `config.yaml` in a git repo and symlink it — the watcher resolves the real
path on each save, so edits through the symlink still trigger a reload.

## Browser setup

Open <http://127.0.0.1:8111/> in the browser you want to set up. The page
detects which browser you are using and shows only the steps that apply, lists
the loaded rules, and has a box for trying a shortcut without navigating.

The page advertises an [OpenSearch][opensearch] descriptor, so both browsers
discover the engine on their own — but **neither makes it the default**, and
that last step is the one that makes bare input like `!313` work.

- **Chrome** adds it to Site search automatically, **as inactive**. Go to
  `chrome://settings/searchEngines`, click Activate, then ⋮ → Make default.
- **Firefox** offers _Add “bang”_ from the address bar menu. Then Settings →
  Search → Default Search Engine.
- **Safari** is out of scope: Apple restricts the default engine to a fixed
  list, and the workaround is a third-party extension that would see every
  query.

## Checking rules without a browser

`/resolve` is a dry run — it reports where a query would land instead of
redirecting, so it is safe to script:

```sh
curl -s 'http://127.0.0.1:8111/resolve?q=p%21313'
# {"matched":true,"query":"p!313","rule":"p!(\\d+)","target":"https://gitlab.com/..."}
```

## Config

See [config.yaml](./config.yaml). Rules are tried in order, first match wins.

- Patterns are **anchored implicitly** — `mr` matches only the exact string
  `mr`, never `mr robot`. Without this, ordinary searches get hijacked.
- Capture groups are `$1`..`$9` and are percent-encoded on substitution.
- `{{name}}` expands the `vars` block. Group and var expansion happen in one
  pass, so a captured `{{gl}}` stays literal.
- `{{q}}` is the whole query and is valid only in `fallback`.

Saving the file reloads it. **A config that fails to parse is rejected and the
running one stays live**, so a typo mid-edit cannot break the address bar. If
the config is already broken at startup the service still starts and forwards
everything to the fallback engine.

## Troubleshooting

**A bare word like `mr` opens a hostname prompt instead of searching.** Chrome
guesses single-token input might be an intranet host. Adding a trailing space
before Enter forces a search; otherwise use a two-character shortcut.

**`gh#12` is safe** — once Chrome treats input as a search query the `#` is
percent-encoded to `%23`, so the fragment is never stripped.

**Nothing resolves.** `tail -f ~/Library/Logs/bang.log`. A rejected reload is
logged with the offending rule number.

[opensearch]: https://developer.mozilla.org/en-US/docs/Web/XML/Guides/OpenSearch
