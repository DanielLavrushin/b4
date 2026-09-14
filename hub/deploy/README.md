# Running a hub

`b4hub` is the service behind community sets. b4 routers share, rate and download sets
through it. The central hub is `https://hub.b4core.app`; its signing key is compiled into
b4, so a router with community sets enabled talks to it without any configuration.

The same binary runs anywhere else in two roles:

- **Central hub** (`b4hub serve`): owns the store, moderates shares, signs and publishes
  the catalogue. There is one publisher key per hub network; routers trust exactly one key.
- **Child hub** (`b4hub mirror`): keeps a byte-identical copy of a parent's signed
  catalogue and relays signed records (shares, votes, reports) to the parent. Routers
  pointed at a child see the same catalogue as routers pointed at the parent, verified
  with the parent's key. A child announces itself with `--announce`; once the parent's
  operator approves it and its health checks pass, the parent lists it in the manifest
  and routers learn it as an alternative address.

An operator who wants an independent network (own moderation, own key, no relation to
`hub.b4core.app`) runs `b4hub serve` and gives routers the hub address and key id in
`system.hub` settings. An operator who wants to serve their routers locally while staying
part of the community runs `b4hub mirror`.

This directory holds the Docker Compose bundle ([docker/](docker/)), the systemd unit
([b4hub.service](b4hub.service)), the nginx vhost ([nginx-hub.conf](nginx-hub.conf)) and the
environment template ([env.example](env.example)).

## Releases

`b4hub` is released from the b4 release workflow, under the b4 version number. A [b4 release](https://github.com/DanielLavrushin/b4/releases)
that publishes a hub carries `b4hub-linux-amd64.tar.gz` and `b4hub-linux-arm64.tar.gz` next to
the router binaries, listed in the same `SHA256SUMS`, and pushes `lavrushin/b4hub:<version>`
(also `ghcr.io/daniellavrushin/b4hub`) for linux/amd64 and linux/arm64. A release without those
assets says in its notes which earlier version the hub is unchanged since; hub versions therefore
have gaps, and `lavrushin/b4hub:latest` is the newest published one. Every release still compiles
and tests the hub against its b4 tree, published or not.

The hub module depends on the router source tree (`replace ../src` in `hub/go.mod`) and the wire
format lives in `src/hubwire`, so a hub build is always a build of one b4 commit, and the b4
version number is the honest name for it. For an operator the rule is: run the newest published
hub.

## Run with Docker

The quickest way to run a hub of either role. [docker/](docker/) holds a Compose file with
`b4hub` and a Caddy front that obtains and renews the certificate on its own; the only
requirement is a host with ports 80 and 443 reachable under a DNS name.

```sh
mkdir b4hub && cd b4hub
curl -fsSLO https://raw.githubusercontent.com/DanielLavrushin/b4/main/hub/deploy/docker/docker-compose.yml
curl -fsSLO https://raw.githubusercontent.com/DanielLavrushin/b4/main/hub/deploy/docker/Caddyfile
curl -fsSL -o .env https://raw.githubusercontent.com/DanielLavrushin/b4/main/hub/deploy/docker/.env.example
```

`.env` selects the role. A central hub keeps `B4HUB_COMMAND=serve` and needs
`B4HUB_ADMIN_PASSWORD`; a child of `hub.b4core.app` sets `B4HUB_COMMAND=mirror --announce`
and `B4HUB_UPSTREAM=https://hub.b4core.app` instead. `HUB_DOMAIN` is the public name in both
cases; it becomes `B4HUB_PUBLIC_URL` and the Caddy site.

```sh
docker compose run --rm b4hub keygen     # once; prints the key id, writes hub.key into the volume
docker compose up -d
docker compose logs -f b4hub
```

The signing key sits in the `b4hub-data` volume; `docker compose down -v` deletes it together
with the store, so back up `hub.key` (`docker compose cp b4hub:/var/lib/b4hub/hub.key .`) before
anything that removes volumes. The image runs as uid 7100; a bind mount in place of the named
volume has to be owned by that uid. Updating is `docker compose pull && docker compose up -d`;
the store migrates on start.

The image without Compose:

```sh
docker run --rm -v b4hub:/var/lib/b4hub lavrushin/b4hub keygen
docker run -d --name b4hub -v b4hub:/var/lib/b4hub -p 127.0.0.1:7100:7100 \
  -e B4HUB_PUBLIC_URL=https://hub.example.net -e B4HUB_ADMIN_PASSWORD=... lavrushin/b4hub
```

behind any TLS-terminating proxy that forwards `X-Forwarded-Proto: https`.

## Build

From the repo root:

```sh
make hub-build VERSION=1.82.0          # current platform, out/b4hub
make hub-linux-amd64 VERSION=1.82.0    # linux/amd64, out/assets/b4hub-linux-amd64.tar.gz (+ .sha256)
make hub-linux-arm64 VERSION=1.82.0    # linux/arm64
make hub-linux-all VERSION=1.82.0      # both
make hub-docker VERSION=1.82.0         # lavrushin/b4hub:1.82.0 from hub/Dockerfile, context = repo root
```

All of them build the moderation console first (`pnpm build` in `hub/ui`) and embed it. A plain
`go build` produces a binary whose `/admin` answers 503 "not built into this binary".
`VERSION` is the string the console and the CLI report.

## Install a central hub (systemd)

This is how `hub.b4core.app` runs.

1. Copy the binary: `sudo install -m0755 b4hub-linux-amd64 /usr/local/bin/b4hub`.
2. Service account and data directory:

   ```sh
   sudo useradd --system --home /var/lib/b4hub --shell /usr/sbin/nologin b4hub
   sudo install -d -o b4hub -g b4hub -m 0750 /var/lib/b4hub
   ```

3. Signing identity, once:

   ```sh
   sudo -u b4hub b4hub keygen --data /var/lib/b4hub
   ```

   The command prints the key id. Back up `/var/lib/b4hub/hub.key` offline: routers trust
   the hub by this key, and a lost key means every router has to be reconfigured with a new
   one. Routers of a self-hosted hub set the address and this key id under `system.hub`.

4. Environment: `sudo install -d -m 0750 /etc/b4hub`, then `/etc/b4hub/env` from
   [env.example](env.example) with a real `B4HUB_ADMIN_PASSWORD`, owned `root:b4hub`,
   mode `0640`. The password is the only credential of the console at `/admin`; changing
   it ends every signed-in session.
5. Unit: copy [b4hub.service](b4hub.service) to `/etc/systemd/system/`, set
   `B4HUB_PUBLIC_URL` to the public address, then
   `sudo systemctl daemon-reload && sudo systemctl enable --now b4hub`.
   The unit listens on `127.0.0.1:7100`; nothing else is exposed directly.
6. nginx: copy [nginx-hub.conf](nginx-hub.conf) to `/etc/nginx/conf.d/`, set
   `server_name` and the certificate paths, `sudo nginx -t && sudo systemctl reload nginx`.
   The vhost proxies the router endpoints (`/b4/hub/...`), the console (`/admin`) and the
   console's hashed assets (`/admin/assets/`, cached as immutable). `X-Forwarded-Proto`
   must be `https`: it makes the session cookie `Secure`.
7. Geo databases download on the first start and daily after that. The first catalogue is
   built once a share is approved, or on demand from the console's Catalogue page
   (`sudo -u b4hub b4hub build --data /var/lib/b4hub` does the same).

## Install a child hub (systemd)

Same binary, own data directory and identity, `mirror` instead of `serve`:

```sh
sudo -u b4hub b4hub keygen --data /var/lib/b4hub
b4hub mirror --data /var/lib/b4hub --upstream https://hub.b4core.app \
  --listen 127.0.0.1:7100 --public-url https://hub.example.net --announce
```

`--upstream-key` pins the parent's key id; without it the key compiled into b4 is trusted,
which is right for a child of `hub.b4core.app`. In the unit, replace `serve` in `ExecStart`
and put `B4HUB_UPSTREAM` in `/etc/b4hub/env`. A child has no moderation console and no
admin password; the nginx vhost is the same minus the `/admin` locations.

## Update

```sh
make hub-deploy VERSION=1.82.1
```

`hub-deploy` cross-compiles for linux/amd64, copies the binary to `HUB_DEPLOY_HOST` (a
`user@host` with passwordless `sudo`, optionally `HUB_DEPLOY_KEY` for the SSH key; both
belong in the gitignored `.env`, see `.env.example`), installs it as `/usr/local/bin/b4hub`,
restarts the unit and prints `systemctl is-active` and `b4hub version`. By hand:

```sh
scp out/b4hub-linux-amd64 user@host:/tmp/
ssh user@host 'sudo install -m0755 /tmp/b4hub-linux-amd64 /usr/local/bin/b4hub && sudo systemctl restart b4hub'
```

Data under `/var/lib/b4hub` is untouched by an update; schema migrations run on start, the
key never changes. When [nginx-hub.conf](nginx-hub.conf) changed, the vhost on the box has
to be updated by hand, since the deployed copy carries the box's certificate paths.

## Checks

```sh
curl -s https://hub.example.net/admin/api/session     # {"configured":true,"authenticated":false,"version":"1.82.0"}
curl -s -o /dev/null -w '%{http_code}\n' https://hub.example.net/b4/hub/manifest.json
sudo -u b4hub b4hub moderate --data /var/lib/b4hub list
```

`configured:false` means `B4HUB_ADMIN_PASSWORD` is missing from the environment. The
`moderate` subcommands (`list`, `approve`, `reject`, `hide`, `ban`, `mirrors`,
`approve-mirror`, `reject-mirror`) act on the same store as the console.
