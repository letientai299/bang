# Development

## Set up the toolchain

[mise][mise] installs the Go and support tools declared in `mise.toml`:

```sh
mise install
```

Run `mise tasks` for the current task list and descriptions. Run `mise run` to
open the interactive task picker. These commands keep this guide independent of
the task catalog as the project evolves.

## Develop

Use the development task to rebuild and restart bang when Go files change:

```sh
mise run dev
```

It loads `deploy/config.toml` and enables verbose query logging. Config changes
reload in the running process, so they do not trigger a restart.

Open <http://127.0.0.1:8111/> to inspect loaded rules. Use `/resolve` for a dry
run:

```sh
curl -s 'http://127.0.0.1:8111/resolve?q=m%20jazz%20fusion'
```

To exercise browser integration without changing a browser profile you rely on,
run `scripts/test_chrome.sh` or `scripts/test_firefox.sh` after starting bang.
Each launches its browser on a profile of its own, reused across runs and
discarded by `--reset`. Set bang as the default engine there once — see
[browsers.md][browsers], which also covers why that step cannot be scripted.

## Test changes

Before handing off a change, format, lint, and test it:

```sh
mise run fmt
mise run lint
mise run test
```

The task definitions in `mise.toml` are the source of truth for the checks.
[prek][prek] invokes the same checks before a commit when its hook is active.

## Troubleshoot

Start with the resolver, then move outward to the browser:

1. Run `go run . check <config>` to validate the rule set on its own, then
   `mise run serve` and check startup or reload errors in the terminal.
2. Call `/resolve` with the failing query. The response reports whether a rule
   matched, the selected pattern, and the target URL. `/suggest` shows what the
   address bar would offer for the same input.
3. Inspect the rule order and regular expression in `deploy/config.toml`
4. Follow the browser-specific checks in [browsers.md][browsers]

An invalid config at startup leaves bang in fallback-only mode. An invalid hot
reload keeps the previous valid config. The error identifies the rejected rule.

If a changed `listen` address appears to have no effect, restart bang. The
listener is created once at startup.

For the deployed container, inspect `docker logs bang` and confirm the port is
published only on `127.0.0.1`. The status commands are in [deploy.md][deploy].

[browsers]: ./browsers.md
[deploy]: ./deploy.md
[mise]: https://mise.jdx.dev/
[prek]: https://github.com/j178/prek
