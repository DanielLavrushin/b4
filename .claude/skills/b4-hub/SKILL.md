---
name: b4-hub
description: Change the b4hub community hub - the separate Go module in hub/ (the b4hub CLI, the router API, the moderation console's API and its SPA in hub/ui) and the shared-set envelope in src/hubwire that routers and the hub both speak. Covers the module and release split, the trust-model decisions not to reopen, the console's design rules, a local dev hub, the generated TypeScript types and the hub's own changelog. Use before editing anything under hub/ or src/hubwire, or when a router's Community page and a hub disagree.
---

# b4hub

## What it is

`hub/` is its own Go module (`github.com/daniellavrushin/b4hub`, with `replace ../src`) and is
never part of the router binary. It has its own version and its own release: the
`DanielLavrushin/b4hub` repository holds the release workflow (it builds a chosen b4 ref), the
operator docs and the deploy bundle.

| Path | What |
| --- | --- |
| `hub/cmd/b4hub` | The cobra CLI: `serve`, `build`, `moderate`, `mirror`, `keygen`. |
| `hub/internal/api` | The router-facing API: manifest, catalogue files, blobs, signed messages, mirror network. Its paths are constants in `src/hubwire`. |
| `hub/internal/web` | The console's JSON API under `/admin/api`, with a cookie session, and the SPA. |
| `hub/internal/moderation` | Moderation actions shared by the console and the CLI. It must not import `ingest`. A state change and its audit row share one transaction. |
| `hub/ui` | The console SPA (`base: /admin/`), embedded through `hub/ui/ui.go`. |
| `src/hubwire` | The shared-set envelope: which set fields travel (`fields.go`), the strategy fingerprint, payload attachments and the import checks. Both sides import it. |

`b4hub moderate` and `b4hub build` run beside a live `serve` on the same SQLite database (WAL,
a busy timeout, immediate transactions); the CLI records a build request that the server's
builder picks up.

## Trust model (owner decisions)

Do not reopen these without new evidence:

- One publisher. The central hub signs the catalogue with one ed25519 key, and a router trusts
  exactly one key: the built-in one (`hubwire.BuiltinHubKeys`) or `system.hub.public_key`.
- Routers download the signed catalogue and search it locally, so the domains a router asks
  about never leave it.
- Mirrors serve the catalogue byte-identical and relay signed records; a router learns a mirror
  only after operator approval. No multi-master, no peer-to-peer between routers, no per-hub
  subscriptions.
- Every share is moderated. Votes and reports queue (the router's outbox, a mirror's spool);
  shares never queue.
- The hub has no public web pages: it serves the console and the router API, and `/`
  redirects to `/admin`.

The central hub's addresses leave a router's hub list only when both a custom public key and
at least one hub URL are set (`Service.BaseURLs` in `src/hub/service.go`). With the key alone
the central stays in the list, but the router trusts only the configured key, so it accepts
nothing the central signed.

## Console rules

- Naming differs between the two sides: the console's **vote** is the router's works/broken
  **report**, and the console's **report** is the router's **complaint**.
- Standard MUI Alerts, a contained default for the primary button, no success-coloured buttons,
  statuses as a dot plus a word (`StatusChip`).
- Removed on request, not to be re-added: keyboard shortcuts, the "Published x ago" badge, the
  lineage banner, a separate Sets table. Queue and Sets both use the set card in
  `hub/ui/src/features/sets/card/`, whose pieces live in `shared/design/src/sets/` and are the
  router's own.
- The UI rules of `/b4-ui` apply here too, both locales included.

## Generated types

`make hub-types` regenerates `hub/ui/src/models/api.gen.ts` from the Go views.
`TestAdminTypes` fails while it is stale, so run it after changing any console API struct.

## Local dev hub

```sh
make hub-keygen
B4HUB_ADMIN_PASSWORD=test make hub-run      # 0.0.0.0:7100, console at /admin
make hub-seed                               # fill the database with generated activity
```

Point a router at `http://<this machine's LAN address>:7100` with the generated public key;
`127.0.0.1` on a router is the router itself. The console allows 10 sign-ins per 15 minutes
per address, so a test script signs in once and reuses the cookie.

## Checks and changelog

- `make hub-test` (`go -C hub test ./...`), `pnpm -C hub/ui build` (`tsc`, then vite, the
  build `make hub-build` embeds), and `/b4-check`.
- Every change under `hub/` gets a bullet in `hub/CHANGELOG.md`: English only, its own semver,
  newest block on top. The b4hub release workflow refuses a version without a block.
- A change in `src/hubwire`, or one that touches the router's hub client and the hub together,
  gets a bullet in each changelog, each written for its reader (`/b4-changelog`).
- A new `SetConfig` field is classified in `src/hubwire/fields.go`: `Crosses` with `Since` set
  to the release that adds it, or `Never` (`/b4-setting`).
- The central hub runs released images only; deploying it is an operator task outside this
  repository.
