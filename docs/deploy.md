# Deployment

bang must be available whenever the browser uses it as the default search
engine. Use [launchd][launchd] on macOS or run the container on Linux. Finish by
following the [browser setup guide][browsers].

## Install

Install the pinned toolchain and run the install task:

```sh
mise install
mise run install
```

The task builds bang, installs it at `~/.local/bin/bang`, creates
`~/.config/bang/config.yaml` from `deploy/config.yaml` when no config exists,
and makes `~/.cache/bang` for the log. It honours `XDG_CONFIG_HOME` and
`XDG_CACHE_HOME` where they are set, and bang looks for its config in the same
place, so `bang` on its own finds what the task installed. Run `mise tasks` to
inspect the current task definitions.

## Auto-start with launchd on macOS

The `deploy/bang.plist` template contains placeholders because a launchd job
does not run through a shell:

```sh
sed -e "s|__HOME__|$HOME|g" \
  -e "s|__CONFIG__|${XDG_CONFIG_HOME:-$HOME/.config}/bang/config.yaml|" \
  -e "s|__LOG__|${XDG_CACHE_HOME:-$HOME/.cache}/bang/bang.log|" \
  deploy/bang.plist > ~/Library/LaunchAgents/bang.plist

launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/bang.plist
```

The log path must already exist — launchd does not create the parent directory
and fails the job when it is missing. `mise run install` makes it.

The job starts at login and restarts after a crash. Use these commands to
operate it:

```sh
launchctl print gui/$(id -u)/bang
launchctl kickstart -k gui/$(id -u)/bang
launchctl bootout gui/$(id -u)/bang
tail -f ~/.cache/bang/bang.log
```

After installing a rebuilt binary, run `launchctl kickstart -k` with the label
above. Config edits reload without restarting the job.

## Run a container

The image is built with [ko][ko] and uses the non-root [Chainguard static
image][chainguard-static]. A running Docker daemon is required. The image is
loaded into the daemon of the active `docker context`; set `DOCKER_HOST` to
override that.

```sh
mise run image
```

Container loopback is separate from host loopback. Set the container config to
listen on all container interfaces:

```yaml
listen: 0.0.0.0:8111
```

Publish that port on host loopback only:

```sh
docker run -d --name bang \
  --restart unless-stopped \
  -p 127.0.0.1:8111:8111 \
  -v "$HOME/.config/bang:/config:ro" \
  ko.local/bang -config /config/config.yaml
```

Mount the config directory because editors commonly save by replacing the file.
The image runs as an unprivileged user, so the config must be readable:

```sh
chmod 644 ~/.config/bang/config.yaml
```

On macOS and Windows, Docker's file-sharing layer may not forward the filesystem
events used for hot reload. Run `docker restart bang` after config changes. A
Linux bind mount normally forwards those events.

Use `docker logs -f bang` for logs and `docker ps --filter name=bang` for
status.

## Verify

The `/resolve` endpoint reports a match without redirecting:

```sh
curl -s 'http://127.0.0.1:8111/resolve?q=%23123'
```

A `403` means the request used a non-loopback host name. Connection refused
means the supervisor is not serving on the expected address.

[browsers]: ./browsers.md
[chainguard-static]:
  https://images.chainguard.dev/directory/image/static/overview
[ko]: https://ko.build/
[launchd]:
  https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html
