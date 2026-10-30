---
name: b4-docs
description: Write or edit a page on b4's Docusaurus documentation site, including the Russian mirror, sidebar positions, and UI screenshots. Use whenever documenting a feature, editing anything under docs/, adding a docs screenshot, or when asked to "document this" or "add a docs page".
---

# Documentation

The site is Docusaurus in `docs/`. Every page exists twice, and a change that lands in one
locale only is incomplete.

Docs are written last. While a fix or feature is still being built, the pages stay as they
are, even where the change makes them stale; keep a list of the pages that will need an update
and report it. The docs pass comes once the change set is tested and settled, because docs
written mid-flight describe intermediate behaviour and get redone. UI strings in `en.json` and
`ru.json` are product code, not docs: they change with the code. The exception is
`docs/docs/guides/marks.md`, which changes in the same pull request as the marks, ip rules or
chain order it describes.

## Tone and content

The full guide is `docs/STYLE.md`: page types with templates, what stays off user pages,
length budgets and before/after examples. Read it before writing a page. b4 is free software,
the docs are for someone who has already installed it, and nothing here is being sold.

- **The reader manages b4 from the web interface** and is often not very technical. Pages say
  what the UI does and what changes for the reader, not what happens under the hood.
- **Impersonal, like official documentation.** Describe what b4 does and what happens, not
  what the reader does: "a set decides which resolver answers the domains it targets", not
  "you choose which resolver to use". The imperative appears only inside numbered procedure
  steps. The owner confirmed this voice on 2026-10-15.
- **One page type per page**: tutorial (Get started), guide (Guides), screen reference (Web
  interface), troubleshooting, explanation (Under the hood) or technical reference.
- **Internals stay off user pages.** Netfilter hooks, kernel versions, internal limits and
  timings, widget behaviour, config keys and API names go to Under the hood or Reference;
  version history goes to the changelog only. The test for every sentence: would a reader who
  manages b4 from the web interface do anything differently after reading it?
- **Short.** About 20 words a sentence, at most two admonitions a page, guides up to ~1,000
  words and screen pages up to ~1,500.
- **No marketing.** No superlatives, no "powerful", "advanced", "convenient",
  "удобный", "продвинутый". No landing-page feature bullets. If a sentence would fit on a
  product page, it does not belong here.
- **No unverifiable claims about DPI.** Never "works in most cases", "this is the best
  option", "reliably bypasses". Bypass behaviour depends on the ISP and changes without
  notice. Write "the strategy Discovery suggests is the first one to try" instead.
- **State facts, never announce them.** No meta-commentary flagging that something matters
  ("That has a consequence worth stating plainly", "Note that", `стоит проговорить`), no
  rhetorical counting as a device ("Two things follow from...", "Three problems compound"),
  no editorial cleverness ("very little to gain and a whole failure mode to lose"), no
  unverifiable claims about how users behave ("the most common way to..."). These read as
  generated text. Cut the framing and keep the fact.
- **No hook openers, packed sentences or justification chains** (corrected on the
  Community Hub pages). No opener that motivates the feature ("A set that works on one
  network is often the set someone else is still searching for"). No colon-packed sentence
  carrying every timing number (`через 20 секунд..., затем раз в час и повторно через 5
  минут, если...`): that is a table. No causal clause hung on the fact ("так что",
  "потому что", "which is what keeps X honest"): state the fact and stop.
- **Diagrams label nodes by their function in the flow** (Moderation, Catalogue, Reports),
  not by who signs what. An arrow label like "works / broken, signed by the author key"
  confused the reader.
- **Impersonal extends to admonitions.** A `:::tip` is not a procedural passage. Write "An
  empty list is the usual choice" / `Рекомендуется оставить список пустым`, never "Leave it
  empty" / `Оставьте пусто`.
- **A section index describes its content, not the documentation's structure.** Say what the
  pages in it are, not how they differ from the rest of the site.
- **b4 is a "service"**, not a "program" or an "app".
- **No em-dash.** Plain hyphen.

Admonitions are for what must not be missed: `:::warning` for something that breaks traffic,
exposes the router or loses data, `:::tip` for an optional shortcut. Text that belongs in the
page does not go in `:::info` or `:::note`.

## Both locales

| Locale | Path |
| --- | --- |
| English | `docs/docs/<path>.md` |
| Russian | `docs/i18n/ru/docusaurus-plugin-content-docs/current/<same path>.md` |

Both files carry their own frontmatter: a `title` and a one-sentence `description`. Neither
locale is a stub of the other.

Russian is a rewrite, not a word-for-word translation. Fixed choices: a routing set is
"сет"; b4 is a "сервис", never "программа"; Discovery the feature is "Дискавери", while the
act of searching stays "поиск" ("во время поиска"); payloads, strategy family names and
technical terms stay in English (TLS, TCP, UDP, SNI, DPI, QUIC, TSPU, ASN, DoH, MTProto,
MSS, SOCKS5, NFQUEUE).

A wording or terminology correction the user makes in one locale applies to the other: after
editing a page, check its counterpart for the same issue and fix it there too.

## Sidebar

When `docs/sidebars.ts` lists the pages explicitly, it alone orders and groups them, in seven
sections: Get started, Guides, Web interface, Troubleshooting, Under the hood, Reference,
Running a hub. A new page is added there under its section, and `sidebar_position` in
frontmatter does not matter. Category labels are translated in
`docs/i18n/ru/docusaurus-plugin-content-docs/current.json`: `pnpm docusaurus write-translations
--locale ru` adds the new keys, then the Russian values are filled in. A page that moves or
merges gets a redirect in the `@docusaurus/plugin-client-redirects` entry of
`docs/docusaurus.config.ts`, since external sites and `readme.md` link to page URLs.

If `docs/sidebars.ts` is still one autogenerated entry, order comes from `sidebar_position`,
a dense 1..N sequence across top-level pages and section `index.md` files, so inserting a page
means bumping its neighbours in both locales.

## Images

Files go to `docs/static/img/<section>/<YYYYMMDDHHMMSS>.png` and are referenced as
`/img/<section>/<file>.png`. Older pages use relative `../../static/img/...`; prefer the
absolute form. Russian pages get genuinely Russian screenshots, under
`static/img/<section>/ru/`, referenced only from the RU page.

## Screenshots

**A browser tool that returns its screenshot into the conversation cannot write a PNG to
disk.** Use such tools to drive and inspect a page; use the bundled script to produce doc
assets.

`shoot.mjs` sits next to this file. It needs no npm packages: it drives Chrome over CDP using
Node's global `WebSocket` and `fetch`, from `/usr/bin/google-chrome` or the path in
`CHROME_BIN`. It captures at `deviceScaleFactor: 1`, since no PNG optimizer is assumed to
shrink a file afterwards.

```sh
node .claude/skills/b4-docs/shoot.mjs jobs.json
node .claude/skills/b4-docs/shoot.mjs --example > jobs.json
```

A job file describes a whole page's assets, so re-shooting both locales is one command:

```json
{
  "baseUrl": "http://localhost:5173",
  "outDir": "docs/static/img/dns",
  "width": 1440,
  "height": 900,
  "jobs": [
    {
      "out": "20260820120000.png",
      "url": "/settings/dns",
      "lang": "en",
      "actions": [{ "waitFor": ".MuiPaper-root" }, { "wait": 400 }],
      "clip": { "selector": ".MuiPaper-root", "contains": "DNS over TCP", "pad": 8 }
    }
  ]
}
```

- `clip` picks the **smallest** element matching `selector` whose `textContent` includes
  `contains`, which is the reliable way to isolate one MUI card. The marker string is
  locale-dependent, so EN and RU jobs need different ones. Omit `clip` for a viewport shot,
  or set `"fullPage": true` for the whole scrollable page.
- `actions` run in order: `{"wait": ms}`, `{"waitFor": "sel"}`, `{"click": "sel"}`,
  `{"eval": "js"}`. Two clicks that build on each other, such as selecting two table rows,
  need a `wait` between them: in one `eval` the second handler still sees the old state and
  undoes the first.
- `lang` seeds `localStorage['b4-language']` before the page loads.
- A failing job reports and the run continues; the exit code is non-zero if any job failed.

The script already handles two traps, so do not re-solve them:

- **MUI opens a Select on mousedown**, not click. `{"click": ...}` dispatches
  `pointerdown`, `mousedown`, `pointerup`, `mouseup` and then `click()`.
- **Sticky and fixed elements paint over a clipped region.** Everything with a computed
  `position` of `fixed` or `sticky` is set to `visibility: hidden !important` before
  capture. Visibility rather than display, so nothing reflows and the measured bounding box
  stays valid.

Three traps the script cannot handle for you:

- **UI language on `/settings/*`.** The settings page overrides the stored language from
  the loaded config, so `lang` does not stick there. Click the language select in the Web
  Server section instead; that only stages a change, so nothing persists as long as Save is
  never pressed.
- **Shoot the set editor from `/sets/new`**, never from a real set id. A real set exposes
  private set names and can vanish mid-session when the config changes underneath.
- **The dev server shows a live router.** `src/http/ui/vite.config.ts` proxies `/api` to
  `B4_BACKEND_URL`. Full-page shots leak real LAN hostnames, IP addresses and resolved
  domains from the dashboard. Crop to the card.

The script warns when an output exceeds 250 KB. The repo norm is 10 to 150 KB.

### Full-page shots

Settings content scrolls inside an inner container, so a full settings shot needs a tall
viewport (3200 px).
`shoot.mjs` hides every fixed or sticky element, which blanks the side menu drawer and the
sticky column headers in a full-page shot (the Traffic page), and an open MUI dialog, which
is fixed too. For those, use a scratch copy that skips the hiding: sed
`const hidden = await cdp.eval(HIDE_OVERLAYS);` into
`const hidden = job.keepOverlays ? 0 : await cdp.eval(HIDE_OVERLAYS);` and give the job
`"keepOverlays": true` with a clip on `.MuiDialog-paper` (worked 2026-10-23). A card clip of a
table with a sticky header, such as the device tables, loses its header row the same way: a
copy that hides only fixed elements keeps it, with
`job.keepSticky ? HIDE_OVERLAYS.replace(" || pos === 'sticky'", "") : HIDE_OVERLAYS` and
`"keepSticky": true` on the job (2026-10-23). If Chrome is launched with
`--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1`, local URLs must use
`127.0.0.1`, not `localhost`.

### Rigs without live router data

Shots must not show real MACs, IPs, device names, set names or tokens. Two rigs worked:

1. **Real binary, no router.** Run `pnpm build` (the UI is embedded with go:embed), then
   `go -C src build -o <scratch>/b4 .` and run it as the normal user with a minimal config:
   `{"system":{"web_server":{"port":17070,"bind_address":"127.0.0.1"},"logging":{"directory":""}}}`.
   The Settings re-shoot also set the web server language (en or ru) and an empty sets list.
   iptables fails as non-root, so b4 runs degraded and still serves the full API and UI.
   Its single-instance lock tries `/var/run/b4.pid`, `/run/b4.pid`, then `/tmp/b4.pid`; run
   it under `unshare -Urm sh -c "mount --bind <scratch>/rundir /run && exec <scratch>/b4 --config ..."`
   to keep the lock out of `/tmp`. To use vite instead, set `.env` to
   `B4_BACKEND_URL=http://127.0.0.1:<port>` and start
   `BROWSER=none pnpm exec vite --port 5191 --strictPort --host 127.0.0.1`.
2. **Mock proxy for made-up data.** A small Node proxy on 127.0.0.1:17080 in front of that
   local b4: forwards only GET, HEAD, OPTIONS and WebSocket upgrades; deep-merges a made-up
   overlay into `GET /api/config` (interfaces br0/eth0/ppp0, `02:1A:2B:...` MACs,
   192.168.1.x, users admin/proxy); fully mocks `/api/devices`, `/api/system/info` (with
   `engine_failure` removed) and `/api/system/addresses`; and exposes
   `GET /api/__mock/lang/{en,ru}` to set `system.web_server.language`, because the settings
   page follows the config language, not localStorage. Run a second vite with
   `B4_BACKEND_URL=http://127.0.0.1:17080` (dotenv does not override an env variable). The
   scripts were not kept; rebuild them from this description. A rewritten body needs the
   upstream `transfer-encoding` header dropped next to the new `content-length`, or vite's
   proxy answers 502 (`Content-Length can't be present with Transfer-Encoding`). The
   Settings > Devices card takes its names from the config, so made-up devices go both into
   the config's `queue.devices.devices` and the mocked `/api/devices`.

Clipping that matched the Settings docs: a sub-tab via `#general-sectionpanel-N` or
`#system-sectionpanel-N`, pad 8, width 1440, which gives 1168 px images. For a "header plus
all cards" shot, the common ancestor is the whole page; instead an `eval` action appends an
absolute marker div covering the union of the tablist Paper and the lowest Paper below it,
then the job clips `[data-shot]`. A pure-Node lossless PNG recompressor cut 23 to 26
percent. For an MCP endpoint line, serve vite on port 7000 and map a host name with Chrome's
`--host-resolver-rules`.

## Validating

`pnpm build` in `docs/` builds both locales and the search index, and reports broken links
and anchors. When `onBrokenLinks` in `docs/docusaurus.config.ts` is `throw`, a broken link or
anchor fails the build: fix the link, never relax the setting. Mermaid `flowchart` blocks
render natively. Before removing or renaming a heading, grep both locales for links to its
anchor; keep a linked anchor alive with an explicit id (`## New heading {#old-anchor}`).

## Cross-linking

Each feature has one home page. Other pages link to it in the text instead of repeating the
explanation, and a new page that covers something an existing page touches gets a link in
both directions.
