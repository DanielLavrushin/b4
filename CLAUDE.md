# b4

b4 ("Bye Bye Big Bro") is a DPI-circumvention and traffic-routing service for Linux routers.
It ships as one static binary with the React web UI embedded in it. Most targets are
busybox-based consumer routers (ASUS Merlin, Keenetic, OpenWrt, Padavan), so anything that
runs on the target must be POSIX shell, not GNU.

## Layout

The Go module root is `src/`, not the repo root. `go.mod` is at `src/go.mod`, module
`github.com/daniellavrushin/b4`. Every `go` command needs `-C src` or `cd src` first.

| Path                                          | What                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `src/main.go`                                 | Entry point.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `src/nfq/`                                    | NFQUEUE capture engine and the DPI-bypass strategies (fake, fragment, disorder, TLS ClientHello surgery). Largest package.                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `src/tables/`                                 | iptables / nftables / ipset rule emission. Routing chains, marks, MSS clamp, DNS redirect.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `src/config/`                                 | Config schema, defaults, migrations, ASN and payload data. Start here for any new setting.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `src/http/`                                   | REST API, auth, websockets, MCP server, and the embedded UI.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `src/http/handler/`                           | One file per API area. `mcp.go` and `mcp_write.go` are the MCP tool definitions.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `src/http/ui/`                                | React 19 + MUI 7 + TanStack Query + vite. Built to `dist/` and embedded.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `src/mtproto/`                                | Telegram MTProto proxy, data centre pool, Cloudflare Worker WS bridge.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `src/discovery/`                              | Automatic strategy discovery: probes a reference domain and searches strategy combinations.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `src/detector/`                               | DPI detector: classifies what kind of blocking is in front of the host.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `src/tun/`                                    | TUN ingestion engine, the alternative to NFQUEUE on kernels without it.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `src/tproxy/`, `src/socks5/`, `src/sock/`     | Transparent proxy, SOCKS5 client and server, low-level packet crafting.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `src/sni/`                                    | Domain matching, SNI and QUIC server-name extraction, set overlap checks.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `src/hubwire/`                                | Shared-set envelope: the allow-list projection of a set, its strategy fingerprint, payload attachments and the import checks. The hub service imports it.                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `hub/`                                        | Community hub service, its own Go module (`github.com/daniellavrushin/b4hub`, `replace ../src`), never part of the router binary. `cmd/b4hub` is a cobra CLI (`serve`, `build`, `moderate`, `mirror`, `keygen`). `internal/web/` serves the moderation console: a JSON API under `/admin/api` with a cookie session, and the SPA from `hub/ui/dist` embedded via `hub/ui/ui.go`. Versioned and released on its own: the `DanielLavrushin/b4hub` repository holds the release workflow (it builds a chosen b4 ref), the operator docs and the deploy bundle; the changelog is `hub/CHANGELOG.md`. |
| `hub/ui/`                                     | Moderation console SPA (vite + React 19 + MUI 7 + TanStack Query), `base: /admin/`. Same toolchain as the router UI.                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `shared/design/`                              | `@b4/design`, the shared pnpm workspace package: tokens, MUI theme, `Logo`, and the set-card pieces in `src/sets/` that the router UI and the hub console both render. Its only peers are emotion, MUI, motion and React (no icon package, no i18next), so icons use `createSvgIcon` and `t` is passed in as a prop. Both UIs import it as `@design`. Root `pnpm-workspace.yaml` ties `src/http/ui`, `hub/ui` and `shared/design` together; `docs/` is its own workspace.                                                                                                                        |
| `src/dns/`, `src/dhcp/`                       | DNS interception and verdicts, DHCP lease reading for device discovery.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `src/convert/`                                | Import of zapret and byedpi command lines into b4 sets.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `src/geodat/`                                 | GeoSite / GeoIP database loading.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `src/watchdog/`                               | Re-runs discovery and applies a healed strategy to a set that stopped working.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `src/netprobe/`, `src/iphealth/`, `src/stun/` | Reachability probing, per-IP health tracking, STUN.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `installer/`                                  | POSIX shell installer, assembled into the top-level `install.sh`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `docs/`                                       | Docusaurus site, EN under `docs/docs/`, RU mirror under `docs/i18n/ru/docusaurus-plugin-content-docs/current/`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `toolkit/`                                    | Shell helpers for tests on a real router (a deadman timer, a firewall diagnostics collector), and `toolkit/labs/`: the `lab` script and its QEMU guests with old kernels, for tests the development machine's kernel cannot run.                                                                                                                                                                                                                                                                                                                                                                 |

## Build and test

`VERSION` is required for anything that builds. `make build` runs `swagger` first, which
aborts if `VERSION` is left at its `1.0.0` default.

```sh
make build-ui                      # gen-defaults, then pnpm build in src/http/ui. Run before any Go build that must ship current UI.
make build   VERSION=1.79.0        # current platform, into out/
make arm64   VERSION=1.79.0        # cross-compile, out/linux-arm64/
make amd64   VERSION=1.79.0
make deploy-<box> VERSION=1.79.0   # build-ui + build + sftp + restart on a box from ~/.config/b4/boxes (make boxes lists them).
make test-netns                    # tables routing tests inside an unshared netns. CI never runs it: run it for any src/tables change.
go -C src test ./...               # CI runs exactly this, with working-directory: src
go -C src test ./tables/ -run TestX
toolkit/labs/lab up debian8        # QEMU guest with kernel 3.16 (debian9: 4.9). The first start downloads its image into ~/.cache/b4-lab.
toolkit/labs/lab ab debian8 TestX  # the netns tests in that guest, this checkout against origin/main. For changes to ip rules, routes or rule order.
make hub-build HUB_VERSION=1.0.0   # hub-build-ui (pnpm build in hub/ui), then the b4hub binary into out/. HUB_VERSION defaults to dev; the hub has its own version.
make hub-test                      # go -C hub test ./... (compiles even with an unbuilt console, dist/.gitkeep)
make hub-run                       # local hub on 0.0.0.0:7100; console at /admin, password from B4HUB_ADMIN_PASSWORD
make hub-types                     # regenerate hub/ui/src/models/api.gen.ts from the Go views; TestAdminTypes fails when it is stale
make hub-deploy                    # pull the latest released b4hub image on HUB_DEPLOY_HOST (.env) and restart it; the central runs released images only
```

`pnpm install` runs from the repo root (workspace). UI dependencies live in the root
`pnpm-lock.yaml`. After any lockfile change, Dependabot's included, run `pnpm dedupe`: a
partial re-resolve can give `shared/design` its own `@mui/material`, which fails the hub's
tsc and silently ships two MUI copies in the router UI.

UI checks, from `src/http/ui/`:

```sh
pnpm build          # vite build only: esbuild strips types without checking them
npx tsc --noEmit    # the type check; run it after any TS change
pnpm lint           # oxlint --type-aware --fix: rewrites files; pnpm exec oxlint --type-aware only checks
```

No workflow runs on pull requests. `go test ./...` (src and hub) and the router UI's vite build
run only in the release workflow, the docs build after a push to main, and nothing runs tsc,
oxlint or `make test-netns`, so a type error merges silently. The `b4-check` skill runs all of
it for the areas a diff touches.

`make gen-defaults` regenerates the UI's set defaults (`NewSetConfig()`) from the Go structs
(`src/tools/gendefaults.go`). Run it after changing a set default; global defaults reach the
UI through `GET /api/config`. Its output, `src/http/ui/src/models/defaults.json`, is
gitignored, so a defaults change never shows in `git status`.

Goroutine-leak guard: `src/leaktest`. A spawn-heavy package gets a `leak_test.go` with
`func TestMain(m *testing.M) { leaktest.VerifyTestMain(m) }`; the ignores in
`leaktest.Options()` are process-lifetime singletons, not leaks. goleak must stay out of the
binary: `go -C src list -deps . | grep goleak` prints nothing.

## Conventions

- **No comments in source code.** Go, TS/TSX, shell, anywhere in this repo: no line or
  block comments, doc comments, TODOs or section dividers. Names and structure carry the
  meaning; explanations go in the chat reply or the commit message. The few comments that
  exist predate the rule: leave them unless the code around them is rewritten, never strip
  them as a cleanup.
- **No em-dash** in code, docs, commit messages, changelog, or chat replies. Plain hyphen.
  Replace any em-dash in text being rewritten.
- **POSIX / busybox shell only** in `installer/` and anything that runs on a router.
  No `grep -P`, no `\s` or `\d` (use `[[:space:]]`, `[[:digit:]]`), no bashisms, no `od`,
  `hexdump` or `xxd` (Merlin's busybox has no `od`): read bytes with `_byte_at` and probe
  for tools with `command_exists` from `installer/lib/utils.sh`. The target
  has busybox, not GNU coreutils, and GNU-only syntax fails silently there. Fix portability
  in the script; never tell users to install GNU tools. Edit `installer/`, then rebuild
  `install.sh` with `make build-installer`.
- **Config omits defaults.** The config file, `GET /api/config/download` and the set
  editor's Import/Export JSON drop values equal to the default (`MarshalSparse`);
  `GET /api/config` and MCP `b4_get_config` return the full struct. Design the Go zero
  value so that it matches the intended default, otherwise the setting cannot be expressed
  as "off". New bools default to false: config files and imported sets decode into
  default-seeded structs (`NewConfig`, `NewSetConfig`), so existing true defaults such as
  `logging.instaflush` survive, but `ApplyDefaults` never fills a `bool`, so any path that
  decodes into a zero struct would lose a true default. Invert the meaning (`IPOnly` rather
  than `UseDomain`) or make it a pointer.
- **Migrations only for JSON shape changes.** Adding a field needs no migration and no
  version bump. Renaming, moving or retyping one does. See `src/config/` for the sparse
  migration registry and `CurrentConfigVersion`. A migration that assigns
  `DefaultSetConfig.X` applies whatever the default is at run time, not when the migration
  was written; write the literal value.
- **Everything user-facing is bilingual.** UI strings in `src/http/ui/src/i18n/en.json`
  and `ru.json`; docs in `docs/docs/` and `docs/i18n/ru/.../current/`; changelog in
  `changelog.md` and `changelog_ru.md`. Never land one side alone. A wording or
  terminology correction in one locale applies to the other locale too.
- **Russian word choices**: a set is "сет"; Discovery the feature is "Дискавери", while the
  act of searching stays "поиск" ("во время поиска"). Payloads, strategy family names and
  TLS, TCP, UDP, SNI, DPI, QUIC, TSPU, ASN, DoH, RTT, HTTP, CIDR, FAB, IPv6, IPInfo,
  MTProto, MSS, SOCKS5, NFQUEUE stay in English.
- **Terminology**: b4 is a "service" ("сервис"), not a "program". Docs tone is factual and
  explanatory, never promotional, and makes no unverifiable DPI-bypass claims.
- **Changelogs.** Keep a Changelog format. Router changes: `changelog.md` and `changelog_ru.md`,
  under `## [Unreleased]` until a release renames it. Anything under
  `hub/`: `hub/CHANGELOG.md`, English only, its own semver, no `[Unreleased]`. Installer-only changes: no
  entry and no version bump, because `install.sh` is served from `main` the moment it
  lands. Always write entries with the `b4-changelog` skill.
- **Marks reference.** `docs/docs/guides/marks.md` and its RU mirror list every mark bit, ip
  rule priority and jump position b4 uses. Update them in the same change as any change to
  marks, ip rules or chain order in `src/tables`, `src/tproxy` or `src/tun`.
- **Credit sources.** When b4 adapts another project's code, data lists or techniques:
  a bullet in the `readme.md` Credits section, a credit in the feature's UI if it has one,
  and a link in the changelog entry that introduces it.

## Adding a config setting

The `b4-setting` skill holds the full procedure with its traps; the short list:

1. Field on the right struct in `src/config/types.go`, with a `json:` tag, zero value equal
   to the intended default (see "Config omits defaults" for bools).
2. Default wired wherever that struct is constructed in `src/config/`. A non-empty slice
   default needs its own copy in `NewConfig` / `NewSetConfig` (`src/config/config.go`), the
   way `NewSetConfig` already copies its slices; `NewConfig` copies `DefaultConfig` by value,
   so without it JSON decoding writes into the shared default array.
3. API surface in `src/http/handler/config.go` / `config_types.go` if it is not carried
   automatically.
4. `make gen-defaults`, then the UI type in `src/http/ui/src/models/config.ts` and the
   control in `src/http/ui/src/components/settings/`.
5. Both i18n files.
6. Consider whether it belongs in the MCP writable path list in `src/http/handler/mcp_write.go`.
7. A new field on `SetConfig` must be classified in `src/hubwire/fields.go` as `Crosses` (with
   `Since` set to the release that adds it) or `Never`; `TestEverySetFieldIsClassified` fails until it is.

## MCP

b4 serves MCP itself at `/api/mcp` (`src/http/handler/mcp.go`), off by default behind
`system.web_server.mcp.enabled` and its own token. Read tools: `b4_status`, `b4_get_config`,
`b4_list_sets`, `b4_get_set`, `b4_check_domain`, `b4_recent_connections`, `b4_logs_tail`,
`b4_metrics`, `b4_diagnostics`. Writes are behind a second switch, `allow_writes`, and refuse
credentials, the web server, the capture engine, the firewall backend and the packet marks.

## Skills

Every b4 skill is named `b4-*`, so `/b4-` lists them. They are committed under
`.claude/skills/b4-*/`; the `.gitignore` re-includes only those folders, so a maintainer's
private skills can sit next to them as symlinks.

- `b4-check`: the pre-merge gate, mechanical rules plus the suites CI never runs. Run it
  before any push or PR.
- `b4-changelog`: a changelog block, the Russian mirror and `hub/CHANGELOG.md`. Load it
  before touching any changelog.
- `b4-docs`: a documentation page, its Russian mirror, its screenshots (`shoot.mjs`).
- `b4-setting`: adding or changing a config setting end to end.
- `b4-tables`: firewall and routing rules in `src/tables`, `src/tproxy`, `src/tun`.
- `b4-lab`: QEMU guests with old kernels and old iptables (`toolkit/labs`), for a change that
  has to hold where this machine's kernel cannot show it.
- `b4-installer`: the POSIX installer and `install.sh`.
- `b4-ui`: the router UI, the hub console and `shared/design`.
- `b4-mcp`: b4's MCP tools, budget, client limits and write guards.
- `b4-hub`: the b4hub module and `src/hubwire`.
- `b4-issue`, `b4-pr`, `b4-worktree`: triaging an issue, working a PR, setting up a worktree.

## Git

- Never rebase shared branches. Prefer merge for bringing main into a feature branch.

## Review comments

Back each verdict on a review comment with a probe (a test or a stress run), and give every
fix a regression test that fails without it. The rules for Fix and Skip verdicts are in the
`b4-pr` skill.

## Personal instructions

Put them in `CLAUDE.local.md` at the repository root: git ignores it and Claude Code loads it
next to this file. A worktree under `.claude/worktrees/` reads `CLAUDE.md` from its own
checkout and `CLAUDE.local.md` from the main checkout.
