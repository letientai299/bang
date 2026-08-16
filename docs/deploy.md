# Deployment

bang must be available whenever the browser uses it as the default search
engine. It runs as a container that the Docker daemon restarts on startup.
Finish by following the [browser setup guide][browsers].

## Run it

```sh
mise run prod
```

The task builds the image, replaces any previous `bang` container, and starts a
new one. Re-run it after a code change to roll the container onto the rebuilt
image.

The container serves `127.0.0.1:9111`, leaving 8111 to `mise run dev`, so the
always-on instance and a development server can run side by side. Point the
browser at whichever one you mean.

The first run creates `deploy/config.local.toml` from `deploy/config.toml`. That
file is the container's config and is gitignored, so local shortcuts stay out of
the repo. Container loopback is separate from host loopback, so the seeded copy
listens on all container interfaces:

```toml
listen = '0.0.0.0:9111'
```

The published port binds host loopback only, so nothing outside the machine can
reach it. The image runs as an unprivileged user, so the config must be
world-readable:

```sh
chmod 644 deploy/config.local.toml
```

## Operate it

```sh
docker logs -f bang
docker ps --filter name=bang
docker restart bang
docker rm -f bang
```

`--restart unless-stopped` is what keeps bang available: the daemon starts the
container whenever it starts, so bang comes up with OrbStack and after a reboot.
A container stopped by hand stays stopped until the next `mise run prod`.

On macOS and Windows, Docker's file-sharing layer may not forward the filesystem
events used for hot reload. Run `docker restart bang` after editing the config.
A Linux bind mount normally forwards those events.

## Build the image alone

```sh
mise run image
```

The image is built with [ko][ko] and uses the non-root [Chainguard static
image][chainguard-static]. A running Docker daemon is required. The image is
loaded into the daemon of the active `docker context`; set `DOCKER_HOST` to
override that.

## Verify

The `/resolve` endpoint reports a match without redirecting:

```sh
curl -s 'http://127.0.0.1:9111/resolve?q=%23123'
```

A `403` means the request used a non-loopback host name. Connection refused
means the container is not running or not publishing the expected port.

[browsers]: ./browsers.md
[chainguard-static]:
  https://images.chainguard.dev/directory/image/static/overview
[ko]: https://ko.build/
