# bang

bang turns address-bar input into shortcuts. The included config opens Go pull
requests and issues, YouTube, YouTube Music, and Hacker News. Anything else
falls through to Google Search.

It runs as a loopback-only HTTP service and acts as the browser's default search
engine. This is the only browser hook that receives input without a keyword
delimiter. The service accepts requests only for loopback host names, and query
logging is disabled by default.

## Quick start

Install the toolchain with [mise][mise], then build and start the container:

```sh
mise install
mise run prod
```

Open <http://127.0.0.1:9111/> and follow the browser-specific instructions. See
the [documentation index][docs] for deployment, browser setup, and development
guides.

## Configure shortcuts

The first run creates `deploy/config.local.yaml` as a copy of
[deploy/config.yaml][config]. Edit that file to add personal shortcuts; it is
gitignored, so they stay out of the repo. Rules are tried in order, and the
first match wins.

- Patterns are anchored implicitly. A pattern such as `pr` matches the complete
  query and cannot capture an ordinary search containing those letters.
- Capture groups use `$1` through `$9` and are percent-encoded for a query
  parameter during substitution. `${1:path}` escapes for a path segment instead,
  and `${1:raw}` substitutes the capture unchanged, which is what a rule needs
  when the query supplies something like `owner/repo`. A `raw` capture can carry
  `?`, `#`, or a host of its own into the target, so keep the pattern feeding it
  tight.
- `{{name}}` expands a value from `vars`. Variables may refer to other
  variables; quote YAML values that begin with `{{...}}`.
- `{{q}}` represents the complete query and is valid only in `fallback`.

Saved changes reload automatically. An invalid change is rejected while the last
valid config keeps running. Undefined variable references, variable cycles, a
capture reference that cannot resolve, and a pattern padded with space are all
invalid. The `listen` setting takes effect after a restart.

Use `GET /resolve` to inspect a shortcut without navigating:

```sh
curl -s 'http://127.0.0.1:9111/resolve?q=%23123'
```

`bang check` reports the same problems as a reload without needing a running
service, which is what a config kept in git can run from a hook or a CI job:

```sh
mise run build
bin/bang check deploy/config.local.yaml
```

Typing the start of a shortcut offers the shortcuts that begin with it. A rule
is offered by its literal prefix, so a pattern that does not start with literal
text is never suggested. See [browser setup][browsers] for what each browser
does with them.

## Project tasks

Run `mise tasks` for the current task list. Run `mise run` to choose a task
interactively. Task definitions and tool versions live in [mise.toml][tasks].

[browsers]: ./docs/browsers.md
[config]: ./deploy/config.yaml
[docs]: ./docs/readme.md
[mise]: https://mise.jdx.dev/
[tasks]: ./mise.toml
