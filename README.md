# bang

bang turns address-bar input into shortcuts. The included config opens Go pull
requests and issues, YouTube, YouTube Music, and Hacker News. Anything else
falls through to Google Search.

It runs as a loopback-only HTTP service and acts as the browser's default search
engine. This is the only browser hook that receives input without a keyword
delimiter. The service accepts requests only for loopback host names, and query
logging is disabled by default.

## Quick start

Install the toolchain with [mise][mise], then install and run bang:

```sh
mise install
mise run install
bang -config ~/.config/bang/config.yaml
```

Open <http://127.0.0.1:8111/> and follow the browser-specific instructions. See
the [documentation index][docs] for deployment, browser setup, and development
guides.

## Configure shortcuts

The installed config starts as a copy of [deploy/config.yaml][config]. Edit
`~/.config/bang/config.yaml` to add personal shortcuts. Rules are tried in
order, and the first match wins.

- Patterns are anchored implicitly. A pattern such as `pr` matches the complete
  query and cannot capture an ordinary search containing those letters.
- Capture groups use `$1` through `$9` and are percent-encoded during
  substitution.
- `{{name}}` expands a value from `vars`. Variables may refer to other
  variables; quote YAML values that begin with `{{...}}`.
- `{{q}}` represents the complete query and is valid only in `fallback`.

Saved changes reload automatically. An invalid change is rejected while the last
valid config keeps running. Undefined variable references and variable cycles
are invalid. The `listen` setting takes effect after a restart.

Use `GET /resolve` to inspect a shortcut without navigating:

```sh
curl -s 'http://127.0.0.1:8111/resolve?q=%23123'
```

## Project tasks

Run `mise tasks` for the current task list. Run `mise run` to choose a task
interactively. Task definitions and tool versions live in [mise.toml][tasks].

[config]: ./deploy/config.yaml
[docs]: ./docs/readme.md
[mise]: https://mise.jdx.dev/
[tasks]: ./mise.toml
