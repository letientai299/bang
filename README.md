# bang

Address-bar shortcuts. Type `!313` and land on merge request 313; type anything
else and land on Google as usual.

Runs as a loopback-only HTTP service set as the browser's **default search
engine**, which is the only hook that sees input with no keyword delimiter —
browser keyword shortcuts all require a Tab or Space, so they cannot express
`!313`. Because it is the default engine it sees everything typed into the
address bar, which is why it binds `127.0.0.1` and nothing leaves the machine.

## Quick start

```sh
mise install
mise run install                    # -> ~/.local/bin/bang, ~/.config/bang/config.yaml
bang -config ~/.config/bang/config.yaml
```

Then open <http://127.0.0.1:8111/> and follow the on-page instructions.

- [docs/deployment.md][deployment] — keeping it running: launchd on macOS, or a
  container image built with [ko][ko].
- [docs/browsers.md][browsers] — registering it in Chrome and Firefox, and the
  quirks of each.

## Config

See [deploy/config.yaml](./deploy/config.yaml). Rules are tried in order, first
match wins.

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

Keep `config.yaml` in a git repo and symlink it — the watcher resolves the real
path on each save, so edits through the symlink still trigger a reload.

`listen` is read once, at startup. Changing it logs a warning and takes effect
on the next restart.

## Checking rules without a browser

`/resolve` is a dry run — it reports where a query would land instead of
redirecting, so it is safe to script:

```sh
curl -s 'http://127.0.0.1:8111/resolve?q=p%21313'
# {"matched":true,"query":"p!313","rule":"p!(\\d+)","target":"https://gitlab.com/..."}
```

## Scope

Requests are answered only when the `Host` header is a loopback name —
`127.0.0.1`, `::1`, or `localhost`. Binding loopback is not enough on its own: a
remote page can reach a loopback service by DNS rebinding, and would then be
same-origin enough to read the whole rule set. Reaching bang under a LAN
hostname gets a `403`.

Only `GET /`, `GET /opensearch.xml`, and `GET /resolve` are served. Everything
else is a `404`.

## Development

`main.go` is wiring only — flags, the initial load, and the listener. The two
halves live under `internal`: `config` parses the rule set, watches the file,
and decides where a query goes; `web` is the HTTP surface and knows nothing
about how a rule is matched. Everything else at the top level is toolchain
config, with the example config and launchd template under `deploy`.

Tooling is pinned in `mise.toml`; `mise install` fetches it.

| Task             | What it does                                           |
| ---------------- | ------------------------------------------------------ |
| `mise run build` | Builds `bin/bang`                                      |
| `mise run test`  | `go test` with race, shuffle, and coverage             |
| `mise run lint`  | [golangci-lint][golangci] and [shellcheck][shellcheck] |
| `mise run fmt`   | Formats Go, shell, and prose                           |
| `mise run serve` | Runs against `deploy/config.yaml`, verbose             |
| `mise run image` | Builds the container image with ko                     |

Use `mise run <task>`, not `mise <task>` — mise has built-in `fmt` and `run`
subcommands that shadow tasks of the same name.

[prek][prek] runs `fmt`, `lint`, and `test` before each commit; the hook
installs itself the first time you enter the directory with mise active.

Go formatting is `gofumpt` plus `golines` at 80 columns, enforced by
`golangci-lint fmt`. Markdown, YAML, and JSON go through [prettier][prettier].

## Troubleshooting

**A bare word like `mr` opens a hostname prompt instead of searching.** Chrome
guesses single-token input might be an intranet host. Adding a trailing space
before Enter forces a search; otherwise use a two-character shortcut.

**`gh#12` is safe** — once Chrome treats input as a search query the `#` is
percent-encoded to `%23`, so the fragment is never stripped.

**Nothing resolves.** `tail -f ~/Library/Logs/bang.log`. A rejected reload is
logged with the offending rule number.

[browsers]: ./docs/browsers.md
[deployment]: ./docs/deployment.md
[golangci]: https://golangci-lint.run/
[ko]: https://ko.build/
[prek]: https://github.com/j178/prek
[prettier]: https://prettier.io/
[shellcheck]: https://github.com/koalaman/shellcheck
