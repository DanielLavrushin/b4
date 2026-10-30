---
name: b4-setting
description: Add, rename, move, retype or change the default of a b4 config setting, global (system.*, queue.*) or per set (SetConfig), end to end - the Go field and the meaning of its zero value, defaults, validation, migrations, the UI type and control, both locales, the AI topic facts, the MCP and hub classifications, and whether it applies on save or needs a restart. Use whenever a change adds or touches a field in src/config/types.go, changes a default, or wires a new option into the web UI.
---

# Config settings

A setting touches up to a dozen files. The steps below are the whole list; skipping one gives
a field that saves but never applies, reads differently after a hub import, or cannot be
switched off.

## 1. Decide what zero means, before writing the field

The config file, `GET /api/config/download` and the set editor's Import/Export drop every value
equal to its default (`MarshalSparse`, `src/config/sparse.go`). `GET /api/config` and MCP
`b4_get_config` return the full struct. So:

- **The Go zero value should be the intended default.** Otherwise "off" may be impossible to
  express.
- **New bools default to false.** Config files and imported sets decode into structs that
  already hold the defaults (`NewConfig`, `NewSetConfig`), so a true default survives there, as
  `logging.instaflush` shows. But `ApplyDefaults` (`src/config/defaults.go`) never fills a bool,
  so a path that decodes into a zero struct and relies on it turns a missing true into false.
  The convention keeps new settings off that trap: invert the meaning (`IPOnly` rather than
  `UseDomain`) or make it a pointer.
- **Sets from elsewhere pass through `ApplySetDefaults`**: Discovery results, the zapret and
  byedpi import, hub imports and shared envelopes. There a zero int, string or float becomes
  the default. A field whose zero is a meaningful value, distinct from its default, changes
  meaning on those paths.
- **Mutually exclusive options are one string field** with named values and one select in the
  UI, not two bools. Two controls with opposite meanings on the same packets confuse users,
  and a greyed control that keeps a stale value does too.
- **Changing an existing default changes every config that never set the value**, including
  configs whose owner picked the old default on purpose: the sparse file never stored it.
  That is a CHANGED changelog bullet, not a silent tweak.

Per set or global: per-set strategy and routing options go on `SetConfig`; everything else
under `Config.System` or `Config.Queue`, all in `src/config/types.go`.

## 2. The steps

| # | Where | What |
| --- | --- | --- |
| 1 | `src/config/types.go` | The field, with a snake_case `json:` tag. |
| 2 | `src/config/config.go` | The default in `DefaultConfig` or `DefaultSetConfig`. A non-empty slice default also gets its own copy in `NewSetConfig`, the way the existing slices do: without it, JSON decoding writes into the shared default array. |
| 3 | `src/config/validation.go` | `Validate` clamps or normalizes the value. A number out of range from REST, JSON import or MCP must never reach the packet path. A rejection goes through `v.addf(path, code, ...)`, and its code needs an `errors.<code>` string in both `en.json` and `ru.json`. |
| 4 | `src/http/handler/config.go`, `config_types.go` | Only when the field is not carried by the full config JSON, which most fields are. |
| 5 | `make gen-defaults` | Set fields only: regenerates `src/http/ui/src/models/defaults.json` from `NewSetConfig()`. The file is gitignored, so the change never shows in `git status`. Global defaults reach the UI through `GET /api/config`. |
| 6 | `src/http/ui/src/models/config.ts` | The TypeScript type. |
| 7 | The control | Global: `src/http/ui/src/components/settings/<Section>.tsx`. Per set: `src/http/ui/src/components/sets/` (`tcp/`, `frags/`, `routing/`, `targets/`, `Udp.tsx`). Give it `aiTopic="<key>"`: the set path without `sets[]` (`tcp.seg2delay`) or the full global path (`system.tables.dscp.enabled`). |
| 8 | `src/http/ui/src/i18n/en.json`, `ru.json` | Label and help text in both. Russian keeps the project terms: a set is «сет», Discovery is «Дискавери»; TLS, SNI, DPI, QUIC, MSS and the like stay in English. |
| 9 | `src/ai/topics.json` | Facts for the `aiTopic` key, lower case. The same corpus answers MCP `b4_get_topic`, and models treat it as authoritative, so every sentence there is checked against the code: a false one is a bug. |
| 10 | `src/http/handler/mcp_write.go` | A field under `mcpWritableRoots` (sets, `system.mtproto`, `system.socks5`, `system.logging`) is writable over MCP automatically. Tag it `mcp:"deny"` when it holds a credential, exposes a port or must never be changed by an AI. A string with a fixed set of values goes into `mcpEnums`. |
| 11 | `src/hubwire/fields.go` | Every `SetConfig` field: `Crosses` when it is strategy that works on any router (with `Since:` set to the release that adds it), `Never` when it is local (ids, devices, interfaces, DNS servers, upstreams, marks, tables). `TestEverySetFieldIsClassified` fails until it is there. |
| 12 | Restart | Does the change apply on save? A save runs `PUT /api/config`, then `pool.UpdateConfig` and `PerformSoftRestart`, then a firewall refresh when `FirewallRefreshNeeded` says so. A value read only at start (engine mode, threads, marks, firewall backend, web port, bind, TLS, `memory_limit`) goes into its section's `restartPick` in `src/http/ui/src/components/settings/sections.ts`, so the UI asks for a restart. Read the Go apply path; never judge from the setting's name. |

## 3. Rename, move or retype: a migration

Adding a field needs no migration and no version bump. Renaming, moving or retyping one does:

- Bump `CurrentConfigVersion` in `src/config/migration.go` and add
  `migrateV<N>to<N+1>(c *Config, raw map[string]interface{}) error` to `migrationRegistry` under
  key `N`. `c` is already decoded in the new shape; the old field exists only in `raw`.
- Write literal values. A migration that assigns `DefaultSetConfig.X` applies whatever the
  default is when it runs, not what it was when the migration was written.
- Cover it in `src/config/migration_test.go`.
- A `Crosses` field also travels between routers of different versions through the hub, so a
  rename there needs the same care on the hubwire side (`/b4-hub`).

## 4. Finish

- `go -C src test ./config/ ./hubwire/ ./http/handler/ ./ai/`, then `/b4-check` for the rest.
- An ADDED changelog bullet in both changelogs (`/b4-changelog`), or CHANGED for a new default.
- The docs page for the setting, both locales, written last once the change is settled
  (`/b4-docs`).
