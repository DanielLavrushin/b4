---
sidebar_position: 5
title: Hosting a hub or a mirror
description: Running b4hub as a hub with its own catalogue, or as a mirror of another hub.
---

`b4hub` is the service behind `hub.b4core.app`. It keeps the catalogue, accepts what routers send, signs what it publishes and serves the [moderation console](./moderation.md). It is a separate binary from b4, released on its own, and never runs on a router.

| | A hub of its own | A mirror |
| --- | --- | --- |
| Command | `b4hub serve` | `b4hub mirror` |
| Catalogue | Built from the sets its moderators approve, signed with its own key | Copied from an upstream hub, with the upstream signature |
| Records from routers | Stored and counted, with a database and the console | Checked and passed on upstream |
| What routers need | Its address and key id, entered by hand | Nothing, once the upstream hub approves the mirror |

A hub of its own is a second catalogue, with its own sets and scores. A mirror serves the same catalogue at another address, for networks where the hub does not answer. **Hub mirrors** on the router has nothing to do with [update mirrors](../advanced/update-mirrors.md).

## The service

One static binary with the console embedded. Every command takes `--data`, the directory that holds everything the service owns.

| Command | What it does |
| --- | --- |
| `b4hub keygen` | Creates the signing key and prints its key id. Refuses to overwrite an existing one |
| `b4hub serve` | Runs a hub: the catalogue builder, the endpoints routers use and the console |
| `b4hub mirror` | Runs a mirror of another hub |
| `b4hub build` | Builds and signs the catalogue once, then exits |
| `b4hub moderate` | Moderates from the command line, see [Without the console](./moderation.md#without-the-console) |
| `b4hub version` | Prints the hub version and the b4 source it was built from |

| Flag | Variable | Default |
| --- | --- | --- |
| `--data` | `B4HUB_DATA` | `data` |
| `--listen` | `B4HUB_LISTEN` | `127.0.0.1:7100` |
| `--public-url` | `B4HUB_PUBLIC_URL` | empty |
| `--geosite-url`, `--geoip-url` | `B4HUB_GEOSITE_URL`, `B4HUB_GEOIP_URL` | the v2ray-rules-dat and b4geoip releases |
| `--upstream` | `B4HUB_UPSTREAM` | empty, required by `mirror` |
| `--upstream-key` | `B4HUB_UPSTREAM_KEY` | empty: the key built into b4 |
| `--trusted-proxies` | `B4HUB_TRUSTED_PROXIES` | empty |
| none | `B4HUB_ADMIN_PASSWORD` | empty: the console is closed |
| none | `B4HUB_TELEGRAM_TOKEN`, `B4HUB_TELEGRAM_CHAT`, `B4HUB_WEBHOOK_URL`, `B4HUB_WEBHOOK_SECRET` | empty, see [Notifications](./moderation.md#notifications) |

`--new-epoch`, `--revoke`, `--new-database`, `--announce`, `--refresh` and `--queue-limit` exist on the command line only.

The data directory holds `hub.key` (the signing key), `hub.db` (the SQLite store), `secret`, `blobs/` (payload files), `public/` (the signed files routers download) and `geo/`.

:::warning
b4hub speaks plain HTTP and expects a reverse proxy for TLS, so it listens on loopback by default. The [b4hub repository](https://github.com/DanielLavrushin/b4hub) publishes a deployment bundle with an nginx vhost, a systemd unit and a Compose file.
:::

Routers use `/b4/health` and the paths under `/b4/hub/`. The console is at `/admin`.

## A hub of its own {#a-hub-of-its-own}

```sh
b4hub keygen --data /var/lib/b4hub

B4HUB_ADMIN_PASSWORD=... b4hub serve --data /var/lib/b4hub \
  --listen 127.0.0.1:7100 --public-url https://hub.example.org
```

`keygen` prints the key id, the only thing that identifies the hub to a router. The key cannot be rotated. `--public-url` is the address routers reach, which the hub announces in its manifest.

The console password comes from the environment only. Without it, the console refuses every sign-in, and `b4hub moderate` is the only way to moderate. A new hub starts with an empty catalogue: nothing is published until a moderator approves it.

### Pointing a router at it {#pointing-a-router-at-it}

On the router, under **Settings > Integrations > Community Hub**:

1. Enter the hub's base URL in **Hub mirrors**.
2. Enter the key id in **Hub public key**.
3. Save the settings, then click **Sync now**.

Both fields are needed. With the key alone, the router keeps syncing from `hub.b4core.app` and refuses its catalogue for the wrong signature.

The address must be `https://`. Plain `http://` is accepted only for `localhost` and for loopback, private and link-local addresses, and an address b4 does not accept is dropped without a message. A wrong key fails every sync with "manifest is not signed by a trusted key".

### The catalogue it publishes

A build runs within about 10 seconds of a moderation action, and at least once a day. The signed manifest is valid for 14 days. Routers that hear from no hub for that long mark their copy expired and keep running the sets they applied.

Routers accept only a manifest newer than theirs: a higher epoch, or the same epoch and a higher sequence number. The hub refuses a build that would not be newer than the catalogue in `public/` or the one an approved mirror serves, and the build history gives the reason.

A database that has never published refuses to publish over an existing catalogue, or to sign an empty one with a key built into b4. That is what `serve` with the production key on the wrong data directory looks like. `--new-database` on `serve` or `build` confirms that a new database is intended.

### The network a contribution is attributed to {#the-network-a-contribution-is-attributed-to}

A record counts for the ASN and country of the address it comes from. The hub looks them up over DNS, so it needs working outbound DNS.

Behind a reverse proxy, the hub sees the proxy's address, unless the proxy is on loopback or listed in `--trusted-proxies` as an address or a CIDR. With an untrusted proxy, every record counts for the proxy, and all routers share the per-network limits.

A mirror with its own key, on b4hub 1.3.0 or newer, signs the records it forwards. When the mirror is approved and its clock is within 10 minutes of the hub's, such a record is stored without a network. A vote then counts only worldwide, at reduced weight, and a complaint never counts toward the automatic hide. The mirror's address gets 10 times the per-network request limit and 20 times the new-key limit. Any other record from a mirror counts for the mirror's own address.

## A mirror of another hub {#a-mirror-of-another-hub}

```sh
b4hub mirror --data /var/lib/b4hub-mirror \
  --upstream https://hub.b4core.app \
  --listen 127.0.0.1:7101
```

The mirror checks the upstream manifest every five minutes, or at the interval set with `--refresh`. It copies a newer catalogue and the payload files it lacks, verifies the signature, sizes and hashes, and serves them unchanged. A failed check keeps the previous copy. With the upstream down, the mirror keeps serving the last catalogue it copied.

A mirror of a self-hosted hub needs `--upstream-key` with that hub's key id. Without it, the mirror trusts only the built-in key of `hub.b4core.app`. The page at `/` of a mirror shows what it holds and when it last checked the upstream.

A mirror cannot alter what it serves. Routers verify the hub's signature and every hash and reject a changed byte. A manifest or a catalogue that fails the check sends them to their next address.

### Announcing a mirror {#announcing-a-mirror}

Routers learn mirrors from the signed manifest of the hub, up to 32 of them. They try them after the addresses entered by hand and the built-in one. A mirror gets into the manifest by announcing itself:

```sh
b4hub keygen --data /var/lib/b4hub-mirror

b4hub mirror --data /var/lib/b4hub-mirror \
  --upstream https://hub.b4core.app \
  --public-url https://mirror.example.org --announce
```

`--announce` needs the mirror's own key and `--public-url`, the address being announced. That address follows the rules of a router's hub address. The announcement is sent at start and daily, and arrives at the hub as **pending**.

The mirror is advertised only after a moderator approves it on the [Mirrors page](./moderation.md#mirrors). Announcing again never changes a decision. An announcement signed by a key other than the one the decision was made for is refused with `mirror_key_mismatch`.

The hub checks every approved mirror every 10 minutes: `/b4/health` must answer `ok`, and the manifest must verify against the hub's key. A mirror stays listed while a check passed in the last 24 hours.

### Records sent through a mirror {#records-sent-through-a-mirror}

A mirror passes shared sets, reports and complaints on to the hub and returns the hub's answer. It refuses a record with a bad signature itself. One client network, a /24 for IPv4 or a /48 for IPv6, may send up to 600 records an hour.

While the upstream does not answer, reports and complaints wait in the mirror's queue, and a shared set is refused with `502 hub_unreachable`. The queue holds up to `--queue-limit` records, 5,000 by default, with at most 200 a day from one client network and 40 a day from one key. A record over a limit stays in the router's own queue.

Behind a proxy that `--trusted-proxies` does not name, all routers share the limits of one client, and the mirror logs a warning. A mirror does not serve `/b4/hub/v1/network`, so a router that syncs only through mirrors takes its network from the [DPI Detector](../detector.md).

## A hub and a mirror on one host

They are separate processes and deployments: nothing merges two catalogues, and a router trusts one hub key at a time. On one machine they need two data directories and two listen addresses.

## Keeping the data {#keeping-the-data}

The data directory moves as a unit: `hub.key`, `hub.db`, `secret`, `blobs/` and `public/` belong together.

:::danger
`secret` keys the HMAC that stands for every contributor's key in the database, and it is created silently when it is missing. A database restored without its original `secret` recognises nobody: bans, trust marks, the authorship of sets and the de-duplication of reports all come apart.
:::

Restoring an older database rolls the epoch and sequence number back. Routers refuse a catalogue older than theirs, and the hub refuses to build one while `public/` or an approved mirror holds a newer one. **Start a new epoch** on the console's Catalogue page, or `b4hub build --new-epoch --data <dir>`, publishes the restored database.

A lost `hub.key` cannot be replaced from the hub's side. Every router pointed at the hub then needs a new key id entered by hand.
