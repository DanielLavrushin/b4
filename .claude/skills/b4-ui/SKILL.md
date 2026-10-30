---
name: b4-ui
description: Change b4's web interfaces - the router UI in src/http/ui, the hub moderation console in hub/ui and the shared @design package in shared/design (React 19, MUI 7, TanStack Query, vite) - with the dev loop against a router or a local b4 without one, the shared components, both locales, the owner's UI decisions, the React and MUI traps already hit, and the type-check, lint and build steps CI never runs. Use before editing any .tsx or .ts under those folders, en.json or ru.json, or when the UI misbehaves.
---

# Web UI

| Package | What |
| --- | --- |
| `src/http/ui/` | The router UI. `pnpm build` writes `dist/`, which the Go binary embeds, so a UI change ships only in a binary built after it. |
| `hub/ui/` | The hub's moderation console, served under `/admin/`. Same toolchain. |
| `shared/design/` | `@b4/design`, imported as `@design` by both: tokens, the MUI theme, `Logo`, the set-card pieces. Its only peers are emotion, MUI, motion and React: no icon package (icons use `createSvgIcon`) and no i18next (`t` is passed in as a prop). |

## Dev loop

- `pnpm install` runs at the repo root (one workspace, one `pnpm-lock.yaml`). After any
  lockfile change, Dependabot's included, run `pnpm dedupe`: a partial re-resolve can give
  `shared/design` its own `@mui/material`, which fails the hub's `tsc` and ships two MUI copies
  in the router UI.
- `make gen-defaults` writes `src/http/ui/src/models/defaults.json`, the set defaults, from the
  Go structs. It is gitignored, so a fresh checkout needs it before `tsc` or vite.
- `pnpm -C src/http/ui dev` proxies `/api` to `B4_BACKEND_URL` from `src/http/ui/.env`
  (gitignored), else `http://192.168.1.1:7000`. A b4 serving HTTPS needs `https://` there; the
  proxy accepts a self-signed certificate. A wrong scheme shows as endless
  `ws proxy error: ECONNRESET`.
- **Without a router**: `go -C src build -o <scratch>/b4 .` and run it as a normal user with the
  config
  `{"system":{"web_server":{"port":17070,"bind_address":"127.0.0.1"},"logging":{"directory":""}}}`.
  iptables fails without root, so b4 runs degraded but serves the whole API. Point
  `B4_BACKEND_URL` at `http://127.0.0.1:17070`. A seeded set with geosite categories stops the
  start-up, so leave them out. A dev server shows live router data: never put its screens in
  public places without cropping out addresses and names.

## Building blocks

- Reuse `src/http/ui/src/components/common/` (`B4Select`, `B4Dialog`, `B4TextField`,
  `B4NumberField`, `B4Switch`, `B4Slider`, `B4Tabs`, `B4FormHeader`, `B4Alert`, ...) before
  writing a new control, and import through the path aliases in `tsconfig.json` (`@common/*`,
  `@b4.sets`, ...).
- A setting's control carries `aiTopic="<key>"`, which feeds "Ask AI to explain" from
  `src/ai/topics.json` (`/b4-setting`).
- Every string goes into both `en.json` and `ru.json` of the package. Russian terms: a set is
  «сет», Discovery is «Дискавери» while the act of searching stays «поиск», b4 is a «сервис»;
  TLS, SNI, DPI, QUIC, ASN, MSS, SOCKS5, NFQUEUE and strategy names stay in English. Units follow
  `ru.json`'s own usage: «байт», «КБ», «мс», «мкс», «мин», «с», never «Б». Plurals need
  `_one`, `_few` and `_many` in Russian.
- No code comments, no em-dash, in TSX as everywhere.

## Owner decisions

- **Dialog footers**: Cancel is the leftmost button. Every action sits on the right, secondary
  ones such as Copy JSON included: Cancel, a `<Box sx={{ flex: 1 }} />` spacer, then the
  actions.
- **Mutually exclusive options** are one select whose options are the states, each with a short
  `note` line in the menu (`B4Select` options take `note` and `disabled`). Controls that do
  nothing in the chosen state are hidden, not greyed out with a stale value.
- **Devices are labelled by name or IP address**: the alias, the DHCP host name, or
  "Vendor (IP)", else the IP. A MAC only as a last resort, on hover or in a details pane.

## Traps already hit

- A WebSocket that reconnects in `onclose` needs an unmount guard, or every page visit leaves
  an orphaned socket. That leak once grew a tab until the browser killed it.
- `handler/spa.go` answers every missing path, an old hashed chunk included, with `index.html`
  and 200. A `React.lazy` route needs an error boundary with a reload, or a tab left open across
  an upgrade shows react-router's error page.
- MUI 7 `Slider` commits `lastChangedValue ?? newValue` from a ref that is never reset, so a
  slider that commits on release can re-apply its last drag after an outside change (Discard, a
  typed value) when a thumb is merely clicked.
- The set editor's sync from `initialSet` must stay: it brings back the server-normalized
  fields and a hub publish's new revision. Without it the draft shows phantom changes and the
  next save gets a 409.
- `B4FormHeader`'s divider label: `.MuiDivider-wrapper`'s `white-space: nowrap` is
  load-bearing. To let a label wrap on a phone, set `whiteSpace: "normal"` with `flexShrink: 0`,
  `minWidth: 0` and `maxWidth: "100%"`; `whiteSpace` alone collapses every label to its longest
  word.
- `flex: 1` with `minWidth: 0` on a row's left group hides its content from the wrap decision,
  so fields overflow under the buttons on the right. Do not copy `settings/Page.tsx`'s action
  bar wholesale: its `alignItems="center"` makes Save 2 px shorter than Cancel elsewhere.
- Check a changed screen at phone width too. MUI breakpoints follow the viewport, so shrink the
  window or use device emulation; narrowing one element proves nothing.

## Checks

CI runs only the router UI's vite build, and only at release. Before calling a UI change done:

```sh
make gen-defaults && pnpm -C src/http/ui exec tsc -p tsconfig.json
pnpm -C src/http/ui exec oxlint --type-aware --report-unused-disable-directives-severity=warn
pnpm -C src/http/ui build
pnpm -C hub/ui typecheck        # when hub/ui or shared/design changed
```

`pnpm lint` in either package runs oxlint with `--fix` and rewrites files; the command above
only checks, and prints nothing when clean. `/b4-check` runs the rest.
