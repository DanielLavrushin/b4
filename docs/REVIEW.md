# b4 documentation review

2026-10-15, branch `worktree-wt-docs-review`, based on `main` at `3af4af86`.

**Scope.** 63 English pages (124,000 words without the changelog) and their Russian mirror
(about 106,000 words, every page mirrored). I read the onboarding path in full (intro,
install, quickstart, dashboard, sets, settings, the section indexes) and sampled every other
page. Each page was measured: words, admonitions, screenshots, firewall and kernel terms,
second person, UI-action words, sentence length. I compared the docs tree with the web
interface's menu and tabs, and traced in git how the largest pages grew.

**Deliverables in this worktree.**

| File | What it is |
| --- | --- |
| `docs/REVIEW.md` | This review: findings, causes, the proposed structure and a rollout plan |
| `docs/STYLE.md` | A proposed style guide for people and agents, to replace the "Tone" rules of the `docs-page` skill |
| `docs/docs/dashboard.md` and its RU mirror | A pilot rewrite of one page by the new rules, for you to judge the direction |

## Summary

1. **The content is accurate and thorough, but it is written as a specification of the
   implementation, not as help for someone using the web interface.** Pages explain
   netfilter hooks, retry schedules, buffer sizes, config keys, API endpoints and version
   history next to the switch the reader came to understand.
2. **Pages grow with every change.** `sets/routing.md` went from 176 words to 10,497,
   `dashboard.md` from 311 to 5,131, `dns.md` from 2,251 to 6,025 in two months. Each
   feature PR adds its edge cases to the page of the screen it touched.
3. **The cause is written down.** The `docs-page` skill asks for an "Impersonal" voice and
   says "Explain the mechanism. The house style is dense and technical but simple [...]
   `docs/docs/dns.md` is the reference for voice and depth." Pages that follow it end up like
   `dns.md`.
4. **The structure follows the code, not the reader.** Three pages are titled "Discovery",
   two "Telegram". "Guides" holds a 9,882-word internals reference. "Advanced" is a grab
   bag. No page is named after a task a home user has, and there is no general
   troubleshooting page and no search.
5. **Proposal**: seven sections organised by what the reader wants to do (Get started,
   Guides, Web interface, Troubleshooting, Under the hood, Reference, Running a hub), the
   style guide in `docs/STYLE.md`, and a phased rewrite. The pilot cuts the Dashboard page
   from 5,131 to 1,718 words (Russian from 4,479 to 1,474) and keeps everything a user acts
   on.

## Result

The plan was carried out on this branch on 2026-10-15, in the impersonal voice the owner chose.

| | Before | After |
| --- | --- | --- |
| English pages (without the changelog) | 63 | 82 |
| English words | 124,059 | 100,992 |
| User-facing sections: Get started, Guides, Web interface, Troubleshooting | - | 68 pages, 64,676 words |
| Under the hood, Reference, Running a hub | - | 14 pages, 36,316 words |
| Average sentence on user-facing pages | 22 words, 21% over 30 | 15.6 words, 4% over 30 |
| Admonitions | 251 | 25 |
| Em-dashes, timestamp alt texts, relative image paths, English stubs | present | none |

- The total shrank by about a fifth, not by half: internals moved to Under the hood instead
  of being deleted, the Sets pages gained rows for fields the old pages never documented, and
  the new guides, troubleshooting pages and the table of config paths added text.
- Ten writer agents rewrote the pages by area. Seven fact-check agents then compared every page
  with HEAD and the source and corrected about 75 statements. Several were errors the old pages
  already had: what Hybrid splitting does, MTProto proxy restarts and secret validation, and two
  rules of set ordering.
- Search covers both locales, and the build fails on any broken link or anchor. It passes.

Still open:

- Screenshots that predate current fields: the Discovery run, the Watchdog page (it has none
  now), the DNS & Routing tab, three Telegram cards, the Sets page, the Targets tab and the TCP
  General tab.
- UI strings that contradict the code: the desync labels "Combo (RST + FIN)" and "Full (RST +
  FIN + ACK)", the SYN TTL helper (says default 3, the code uses 7), the TCP port filter helper
  ("port 443 is always included"), `addDomain.alert` on subdomain matching,
  `sets.manager.orderHint` on device-limited sets, `desync.countHelperEnabled` (Combo and Full
  ignore the count), and the RU `deviceNoDestination` string quoting the English label.
- A possible matching issue: when one set lists a more specific domain and is limited to some
  devices, other devices get no match for that name, and a less specific entry in another set
  is not tried (`src/sni/match.go:636-674`).

## What works and should stay

- **Accuracy and coverage.** Pages are current with the code, and most fields, messages and
  states are documented somewhere.
- **Bilingual parity.** Every page exists in both locales, apart from two English stubs.
  Russian runs at about 85% of the English word count, which is normal for Russian.
- **UI labels in bold, exactly as in the interface.** This is the right convention, and the
  pilot keeps it.
- **Decision tables.** `telegram/index.md` compares the three Telegram modes in one table.
  It is the best page on the site for a reader choosing what to set up.
- **Symptom-first troubleshooting.** `telegram/troubleshooting.md` is organised by what the
  reader sees ("Telegram sits at Connecting"). That pattern should spread to the whole site.
- **Install pages** are procedural, with copyable commands.
- **The changelog is separate** and has its own entry in the navbar.

## Findings

### 1. Pages describe the implementation instead of what the reader can do

The test for each sentence on a user page is: *would someone who manages b4 from the web
interface do anything differently after reading it?* A large share of the text fails it.

| Kind | Examples |
| --- | --- |
| Internal mechanism | dashboard.md: "b4 measures every time against its own uptime and converts it with the router's clock when it sends it, so the history stays in order when the router sets its clock late [...]". settings/core.md: "b4's capture rules sit in the `postrouting` and `output` hooks". dns.md: "for requests (`dport 53`) in the raw table, ahead of conntrack (priority -300 on nftables)" |
| Internal limits and timings | dashboard.md: "The browser tries again after about 1, 2, 4, 8 and 16 seconds, then every 30 seconds"; "b4 keeps the counts of up to 512 names in memory; when the list is full, the name with the fewest connections makes room, the one seen longest ago among equal counts". dns.md: "once 20 ms have passed without a reply"; "8192 DNS answers to deliver" |
| How widgets behave | dashboard.md: "Home and End go to the first and last bar, Escape closes the readout"; "A zero is drawn dimmer than other figures"; "one row each with alternating shading"; "Rows apply while the panel area is at least 960 pixels wide". settings/core.md: "The slider starts at 1, but a value below 19 is raised to 19 when the configuration is saved" |
| Config keys and API names | settings/core.md has 37 backticked config paths, such as "**Uplink interface** - `queue.tun.out_interface`". sets/discovery.md: "stored with the set as `sets[].discovery.urls`". discovery.md: "the API refuses a run for one with `routed_set`". dashboard.md: "`GET /api/metrics` returns the snapshot the page receives first over `/api/ws/metrics`" |
| Version history | dashboard.md: "Up to 1.84.0 the button was **Reset Stats** [...]". settings/mtproto.md: "Earlier versions labelled the tab **MTProto Proxy**; its address is still `/settings/mtproto`". Also in dashboard.md ("A layout saved by an earlier version"), settings/system.md, guides/marks.md ("Before 1.84") and telegram/websocket-bridge.md |
| Log line catalogues | dns.md has a table of seven log lines with their exact wording and meaning |
| Kernel and firmware specifics on user pages | dns.md: "With iptables on kernels before 4.18 (4.14 before 4.14.173), on KeeneticOS and wherever the raw table cannot queue packets [...]". settings/core.md: a whole `:::info` on TUN "without the raw table or the CT target", naming the Synology DSM kernel |

Each detail is correct. Together they bury the answer to the reader's question (what is
this, should I change it, what happens if I do) under material the reader cannot act on.
The old Dashboard page is an example: its **Needs attention** table listed when each message
appears and where its link goes, but not what the reader should do about it.

The ten largest pages:

| Page | Words | Admonitions | Firewall and kernel terms per 1,000 words |
| --- | --- | --- | --- |
| sets/routing.md | 10,497 | 25 | 23 |
| guides/marks.md | 9,882 | 1 | 66 |
| community/moderation.md | 7,354 | 12 | 0 |
| settings/core.md | 6,038 | 22 | 18 |
| dns.md | 6,025 | 10 | 7 |
| discovery.md | 5,858 | 6 | 0 |
| dashboard.md (before the pilot) | 5,131 | 1 | 6 |
| community/hosting.md | 3,825 | 13 | 0 |
| settings/mcp.md | 3,689 | 13 | 0 |
| settings/system.md | 3,335 | 9 | 5 |

### 2. The voice reads as generated

- **Descriptions where steps are needed.** The impersonal voice is the house style and stays
  (the owner confirmed it on 2026-10-15). But procedures were written as descriptions as well,
  so a reader looking for steps gets prose ("**Clear** sends every escalated host back").
  Numbered imperative steps fix that without addressing the reader anywhere else.
- **Long sentences built from clauses.** The average sentence is 22 words, and 21% of
  sentences run over 30. In dns.md it is 25 words and 30%. The English pages hold 698
  semicolons.
- **Repeated rhetorical patterns.** 65 "X, not Y" contrasts ("The selection is a filter, not a
  list of interfaces b4 attaches to") and 54 "rather than". Used this often, they read as a
  template.
- **Admonition overuse.** 251 admonitions in total, 25 on one page. Most hold ordinary body
  text, so the few real warnings do not stand out.
- **Two generations of style.** Older pages (sets/index, sets/tcp/splitting, the start of
  sets/routing, quickstart) are short, use "you" and give tips. Newer pages (dashboard, core,
  dns, discovery, watchdog, telegram, community) are exhaustive and impersonal. The site
  changes voice from page to page.
- **Russian mirrors English sentence by sentence**, so it inherits the long chains and reads as
  a translation: "b4 отсчитывает каждое время от собственного времени работы и переводит его в
  часы роутера в момент отправки, поэтому история остаётся в правильном порядке, когда роутер
  выставляет часы с опозданием [...]".

### 3. The structure follows the code, not the reader

**One feature is spread over several pages that share a title.**

| Feature | Pages |
| --- | --- |
| Discovery | `discovery.md`, `sets/discovery.md` and `settings/discovery.md`, all titled "Discovery", plus `watchdog.md`, whose settings live on the Discovery settings tab |
| Telegram | the `telegram/` section (index titled "Telegram") and `settings/mtproto.md`, also titled "Telegram" |
| DNS | `dns.md` at the top level, the DNS redirect in `sets/routing.md`, the DNS sub-tab in `settings/core.md` |
| Interfaces | `guides/interfaces.md` repeats the Capture Interfaces text of `settings/core.md` almost word for word |

**The sidebar order does not match the order a user meets things.** Settings (position 5)
comes before Sets (6) and Discovery (8). DNS is at 15, after Telegram. Community (9) sits
between Discovery and Watchdog. Positions such as 4.5 and 5.5 under Sets show pages squeezed
in after the fact, and inserting a page means renumbering its neighbours in both locales.

**"Guides" and "Advanced" are not defined by their content.**

- Guides holds "Which interface is which" (a concept), "b4 with Xray or XrayUI" (an
  integration how-to mixed with an explanation) and "Packet marks", a 9,882-word reference of
  every mark bit with 66 firewall terms per 1,000 words. None is a step-by-step guide for a
  typical user.
- Advanced holds CLI flags and the configuration file (reference), update mirrors (a how-to),
  the API (a link) and "How TSPU works" (an explanation, a stub in English). "Advanced" says
  how hard a page is, not what it is, so a reader cannot predict what is inside.

**No page is named after a task.** A home user arrives with goals: open a blocked site, send
one site through a VPN, block ads, apply b4 to the TV only, get Telegram working. The
information exists, but it lives inside screen pages: routing through a VPN is a section of
the 10,497-word `sets/routing.md`, device filtering a section of `settings/core.md`.

**No general troubleshooting.** Only Telegram has a troubleshooting page. OpenWrt, Keenetic
and MikroTik have troubleshooting sections on their install pages. Nothing covers "the site
still does not open" or "b4 does not see my traffic", and the English Logs page is a stub.

**Two audiences share the Community section.** `community/hosting.md` (3,825 words) and
`community/moderation.md` (7,354 words) are for the few people who run a hub. They sit next to
the pages every user reads.

**No search.** Docusaurus has no built-in search, and none is configured. On a site of
124,000 words in each of two languages, the sidebar is the only way to find anything.

**The docs tree differs from the web interface.**

- The UI menu is Dashboard, Sets, Discovery, Community, Watchdog, DPI Detector, Traffic, Logs,
  Settings. The docs order is different (see above).
- The Settings tabs are Core, System, Geodat Settings, Discovery, Telegram, Integrations,
  Payloads. The docs have a Security page with no tab of that name. The Integrations tab (the
  IPinfo token, the AI assistant, MCP, the Community Hub connection) has only an MCP page.
- The Traffic page is `connections.md`, while the UI route is `/traffic`.

### 4. Smaller issues

- Two English pages are stubs pointing to Russian: `logs.md` (15 words) and `advanced/tspu.md`
  (17 words). They cause the only two broken links in the build. Both Russian pages are short
  (115 and 168 words), so translating them is quick.
- 13 of 67 images use the file's timestamp as alt text (`![20260418234612](...)`), and 12 use
  relative `../../static/img/...` paths.
- `readme.md` sent English readers of Discovery and the DPI Detector to the Russian pages
  (`/ru/docs/discovery`, `/ru/docs/detector`), a leftover from when the English pages were
  stubs.
- `onBrokenLinks` is `warn`, so broken links never fail a build.
- The site tagline is "Advanced censorship bypass system", against the docs' own
  no-marketing rule.
- The writing rules exist only in the private `docs-page` skill, so a person contributing to
  the public repo never sees them. `docs/STYLE.md` puts them in the repo for everyone, and the
  skill can point to it.

## Why it happened

1. **The skill asks for it.** "Impersonal. Describe what b4 does and what happens, not what the
   reader does." "Explain the mechanism. The house style is dense and technical but simple:
   what reaches b4, what it does with it, and what the boundaries are. The reader is often not
   very technical [...] `docs/docs/dns.md` is the reference for voice and depth." "Use
   admonitions actively." The skill knows the reader is often not technical, yet it names the
   most technical page on the site as the model, and the pages follow the model.
2. **Docs are written from the diff.** The page changes in the same PR as the code: #395
   (discovery rework), #396 (core settings rework) and #399 (dashboard rework) each changed
   the pages of the screens they touched. Written from a diff, a page records what changed:
   the edge case, the limit, the old behaviour. It does not ask what the reader needs.
3. **There is only one kind of page.** Every page describes one screen. A new detail has
   exactly one place to go, so screen pages grow without limit. There is nowhere to put a
   task, and nowhere to put internals.
4. **Nothing pushes back.** No length budget, no review checklist for docs, and no failing
   build on broken links.

## Proposed structure

### Principles

- **Organise by what the reader wants to do.** This follows [Diátaxis](https://diataxis.fr/):
  learning (Get started), doing (Guides), looking up (Web interface, Reference), fixing
  (Troubleshooting) and understanding (Under the hood). Each section holds one kind of page.
- **The Web interface section mirrors the app's menu one to one**, so a reader on any screen
  finds its page in the same place.
- **Internals and operator material are separate**: available to the readers who need them,
  and out of everyone else's way.
- **One home per feature.** Other pages link to it instead of repeating it.

### Sidebar

```text
Get started                          Начало работы
  What is b4                         intro.md, rewritten
  Key concepts                       new: set, target, strategy, set kinds, Discovery, watchdog, escalation
  Install                            install/, platform pages unchanged
    Update, back up and remove       new, from install/index.md and Settings > System > Backup
  First steps                        quickstart.md, trimmed

Guides                               Руководства
  Make a blocked site open           new, from quickstart, discovery.md, detector.md
  Add sites to a set                 new, from sets/targets.md
  Keep sets working                  new, from watchdog.md, sets/escalation.md, sets/discovery.md
  Send sites through a VPN           new, from sets/routing.md "Traffic routing"
  Send sites through a proxy         new, from sets/routing.md "Upstream SOCKS5 proxy"
  Fix DNS problems                   new, from dns.md and sets/routing.md "DNS redirect"
  Block ads and sites                sets/blocking.md
  Use b4 for some devices only       new, from settings/core.md "Devices" and sets/targets.md
  Telegram                           telegram/, trimmed; the index becomes "Choose a way"
  Use and share community sets       community/browsing, applying, feedback, publishing, sets/sharing
  Move from zapret or byedpi         sets/import.md
  Open the web interface safely      settings/security.md
  Use b4 with Xray or XrayUI         guides/xray.md
  Pass sets to another router        new, from sets/routing.md "DSCP" and install/mikrotik.md
  Connect an AI assistant            settings/mcp.md, the user part
  Download b4 when GitHub is blocked advanced/update-mirrors.md

Web interface                        Веб-интерфейс
  Dashboard                          dashboard.md (pilot done)
  Sets                               sets/index.md and the editor tabs:
    Targets, TCP, UDP, DNS & Routing, Escalation, Discovery, Import/Export
  Discovery                          discovery.md, the screen only
  Community                          community/index.md, the screen only
  Watchdog                           watchdog.md
  DPI Detector                       detector.md
  Traffic                            connections.md
  Logs                               logs.md, translated
  Settings                           settings/index.md and the tabs:
    Core, System, Geodat Settings, Discovery, Telegram, Integrations (new), Payloads

Troubleshooting                      Решение проблем
  A site does not open               new
  b4 does not see traffic            new: engine failed, no packets, flow offloading, capture interfaces
  Telegram                           telegram/troubleshooting.md
  Router platforms                   the troubleshooting sections of the install pages
  Report a problem                   new: System Info, logs, diagnostics

Under the hood                       Как это устроено
  How b4 handles traffic             new, from sets/index.md "How it works" and settings/core.md
  DNS                                dns.md, internals only
  Routing sets                       sets/routing.md internals: rules, tables, firewall backend
  Which interface is which           guides/interfaces.md
  Packet marks                       guides/marks.md
  How TSPU works                     advanced/tspu.md, translated

Reference                            Справочник
  Configuration file                 advanced/config.md
  Command-line flags                 advanced/cli.md
  API and MCP tools                  the Swagger page, and the tool list from settings/mcp.md

Running a hub                        Свой хаб
  Hosting a hub or a mirror          community/hosting.md
  Moderation console                 community/moderation.md
```

### How to build it without breaking links

- **Switch to an explicit `sidebars.ts`.** With an explicit sidebar, grouping no longer depends
  on folders, so existing pages keep their paths and URLs, the ones in `readme.md` included.
  The dense `sidebar_position` renumbering in both locales goes away, and the Russian category
  labels move to `i18n/ru/docusaurus-plugin-content-docs/current.json`. Keeping paths also
  keeps CLAUDE.md's rule about `docs/docs/guides/marks.md` valid; if that file moves, the rule
  moves with it.
- **Running a hub may not belong on this site at all.** CLAUDE.md says the `b4hub` repository
  holds the operator docs. If they cover the same ground as `community/hosting.md` and
  `community/moderation.md`, those two pages can move there, and this site links to them.
- **Add `@docusaurus/plugin-client-redirects`** for the few pages that do move or merge.
- **Add search.** `@easyops-cn/docusaurus-search-local` indexes English and Russian at build
  time, with no external service.
- **Later, link the docs from the UI.** Today the web interface links only to the docs root.
  A help link on each screen or card, pointing at a stable anchor, lets every page stay short,
  because the reader lands on the right section.

## Writing rules

The full text is in `docs/STYLE.md`. In short:

- Write for the person managing b4 from the web interface, in the impersonal voice of
  official documentation. The imperative appears only inside numbered steps.
- One kind of page per page: tutorial, guide, screen reference, troubleshooting, explanation
  or technical reference, each with a template.
- Leave out what the reader cannot act on: mechanisms, internal limits, widget behaviour,
  version history, config keys and API names. Mechanisms an integrator needs go to Under the
  hood, history to the changelog, keys to Reference.
- At most two admonitions a page, and sentences of about 20 words.
- Length budgets: guides up to 1,000 words, screen pages up to 1,500, split above 2,000.
- When a feature changes, first ask whether the reader sees or does anything differently. If
  not, the change goes in the changelog only.
- Russian is written for Russian readers, not translated sentence by sentence.

## The pilot: Dashboard

`docs/docs/dashboard.md` and its Russian mirror are rewritten by these rules. The diff is the
quickest way to judge the direction.

| | Before | After |
| --- | --- | --- |
| Words, English | 5,131 | 1,718 |
| Words, Russian | 4,479 | 1,474 |
| Average sentence, English | 22 words, 24% over 30 | 15 words, 2% over 30 |
| Admonitions | 1 | 0 |
| Screenshots | 15 | 14 |

What changed:

- The page opens with what it is for: checking that b4 runs, that sets match traffic, and
  what needs attention.
- **Needs attention** is now a table of *message, meaning and action*, instead of *message,
  trigger condition, link*. The UI already shows the link.
- Removed: how times are converted, WebSocket reconnect timings, staleness thresholds, list
  capacities and eviction rules, keyboard navigation, shading and pixel widths, the version
  history of the Reset button, API endpoints, and the screenshot of interrupted updates,
  which one sentence now covers.
- Kept: what each panel shows, every **Needs attention** message, every control, and the one
  fact that explains the numbers: b4 sees only the first packets of a connection, so nothing
  on the page measures volume.
- Every anchor that other pages link to still works (`#needs-attention`,
  `#packet-engine-not-running`, `#what-a-connection-count-covers` and the Russian ones).
  `pnpm build` passes for both locales with no new broken links or anchors.
- The "what to do" advice comes from the old page, the pages it links to and the UI's own
  strings. No new claim about how b4 behaves was added. Where none of them offers a remedy,
  as for the overload messages, the pilot offers none either.

## Rollout plan

Every phase covers both locales. The phases are independent, so the work can stop after any
of them.

| Phase | Work | Risk |
| --- | --- | --- |
| 0. Foundations | Adopt `STYLE.md` and shrink the skill's "Tone" section to a pointer to it. An explicit `sidebars.ts` with the seven sections. The search and redirects plugins. Translate the two English stubs; fix alt texts, image paths and the `readme.md` links. Then make broken links fail the build | Low: no content changes |
| 1. Get started and Web interface | Rewrite intro and quickstart, add Key concepts. Trim every screen page to the budget, as in the pilot: settings, sets and the editor tabs, discovery, watchdog, traffic, detector, community. Internals that are cut wait in a holding file for phase 3 | Low: mostly cutting text that exists |
| 2. Guides | Write the guides, mostly by moving text out of `sets/routing.md`, `dns.md` and `settings/core.md`. Every new sentence about behaviour is checked against the code | Medium: new text |
| 3. Troubleshooting and Under the hood | The general troubleshooting pages. Move the internals of DNS, routing and capture there, trimmed | Low |
| 4. Running a hub | Move hosting and moderation into their own section and trim them | Low |

Expected result: roughly half the current size. Most of the cut is internals that leave the
user pages, and part of it survives in Under the hood.

## Appendix: per-page measurements (English)

`adm` admonitions, `img` screenshots, `int` firewall and kernel terms per 1,000 words, `ver`
version-history phrases, `you` and `ui` occurrences of "you/your" and of UI-action words per
1,000 words, `cfg` backticked config paths.

| Page | Words | adm | img | int | ver | you | ui | cfg |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| advanced/cli.md | 605 | 0 | 0 | 9 | 0 | 0 | 0 | 4 |
| advanced/config.md | 1,604 | 3 | 0 | 2 | 0 | 0 | 1 | 8 |
| advanced/index.md | 59 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| advanced/tspu.md | 17 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| advanced/update-mirrors.md | 1,918 | 8 | 0 | 1 | 0 | 0 | 0 | 9 |
| community/applying.md | 931 | 3 | 0 | 0 | 0 | 0 | 7 | 0 |
| community/browsing.md | 963 | 1 | 1 | 0 | 0 | 2 | 9 | 1 |
| community/feedback.md | 1,051 | 1 | 0 | 0 | 0 | 0 | 7 | 0 |
| community/hosting.md | 3,825 | 13 | 0 | 0 | 1 | 0 | 0 | 12 |
| community/index.md | 1,210 | 1 | 1 | 0 | 0 | 0 | 2 | 4 |
| community/moderation.md | 7,354 | 12 | 0 | 0 | 2 | 0 | 6 | 6 |
| community/publishing.md | 1,034 | 2 | 1 | 0 | 0 | 0 | 6 | 0 |
| connections.md | 2,431 | 5 | 2 | 1 | 0 | 0 | 11 | 3 |
| dashboard.md (before the pilot) | 5,131 | 1 | 15 | 6 | 4 | 0 | 5 | 2 |
| detector.md | 1,815 | 1 | 0 | 0 | 0 | 0 | 1 | 0 |
| discovery.md | 5,858 | 6 | 4 | 0 | 1 | 0 | 5 | 1 |
| dns.md | 6,025 | 10 | 2 | 7 | 1 | 1 | 1 | 21 |
| guides/index.md | 136 | 0 | 0 | 7 | 0 | 0 | 0 | 0 |
| guides/interfaces.md | 785 | 4 | 0 | 15 | 0 | 0 | 0 | 0 |
| guides/marks.md | 9,882 | 1 | 0 | 66 | 7 | 0 | 0 | 31 |
| guides/xray.md | 1,514 | 4 | 0 | 29 | 0 | 0 | 1 | 0 |
| install/docker.md | 187 | 0 | 0 | 15 | 0 | 0 | 0 | 1 |
| install/index.md | 298 | 0 | 0 | 3 | 1 | 0 | 0 | 0 |
| install/keenetic.md | 1,077 | 1 | 0 | 37 | 0 | 0 | 0 | 2 |
| install/linux.md | 331 | 1 | 0 | 21 | 0 | 0 | 0 | 0 |
| install/merlin.md | 179 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| install/mikrotik.md | 2,432 | 4 | 0 | 27 | 0 | 0 | 1 | 0 |
| install/openwrt.md | 774 | 3 | 0 | 45 | 0 | 0 | 0 | 2 |
| intro.md | 531 | 1 | 0 | 9 | 0 | 15 | 1 | 0 |
| logs.md | 15 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| quickstart.md | 725 | 2 | 5 | 0 | 0 | 2 | 16 | 2 |
| sets/blocking.md | 1,139 | 3 | 0 | 1 | 1 | 0 | 3 | 0 |
| sets/discovery.md | 1,358 | 2 | 0 | 0 | 0 | 0 | 7 | 0 |
| sets/escalation.md | 1,091 | 3 | 1 | 1 | 0 | 5 | 3 | 0 |
| sets/import.md | 1,528 | 3 | 0 | 3 | 0 | 1 | 3 | 0 |
| sets/index.md | 903 | 3 | 2 | 0 | 3 | 4 | 3 | 0 |
| sets/routing.md | 10,497 | 25 | 1 | 23 | 5 | 0 | 1 | 9 |
| sets/sharing.md | 1,274 | 4 | 0 | 0 | 1 | 0 | 1 | 1 |
| sets/targets.md | 1,693 | 3 | 3 | 0 | 0 | 0 | 5 | 3 |
| sets/tcp/faking.md | 1,565 | 5 | 0 | 1 | 0 | 0 | 1 | 2 |
| sets/tcp/general.md | 690 | 4 | 3 | 2 | 0 | 2 | 0 | 0 |
| sets/tcp/index.md | 47 | 0 | 0 | 0 | 0 | 0 | 20 | 0 |
| sets/tcp/rst-protection.md | 243 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| sets/tcp/splitting.md | 1,285 | 8 | 3 | 1 | 0 | 2 | 3 | 0 |
| sets/udp.md | 1,430 | 5 | 0 | 0 | 3 | 1 | 3 | 5 |
| settings/core.md | 6,038 | 22 | 4 | 18 | 2 | 0 | 7 | 37 |
| settings/discovery.md | 1,254 | 2 | 1 | 0 | 0 | 0 | 3 | 0 |
| settings/geodata.md | 2,136 | 5 | 1 | 0 | 1 | 0 | 9 | 20 |
| settings/index.md | 607 | 0 | 1 | 8 | 0 | 0 | 19 | 0 |
| settings/mcp.md | 3,689 | 13 | 1 | 0 | 0 | 0 | 2 | 9 |
| settings/mtproto.md | 2,254 | 2 | 1 | 15 | 1 | 0 | 5 | 7 |
| settings/payloads.md | 1,833 | 5 | 1 | 1 | 1 | 0 | 16 | 11 |
| settings/security.md | 2,647 | 7 | 0 | 17 | 0 | 0 | 3 | 7 |
| settings/system.md | 3,335 | 9 | 3 | 5 | 1 | 0 | 7 | 13 |
| telegram/cloudflare-worker.md | 954 | 3 | 0 | 21 | 0 | 1 | 0 | 5 |
| telegram/dc-relay.md | 543 | 5 | 1 | 11 | 0 | 0 | 14 | 0 |
| telegram/index.md | 407 | 1 | 0 | 19 | 0 | 0 | 4 | 0 |
| telegram/mtproto-proxy.md | 1,400 | 3 | 3 | 4 | 1 | 0 | 6 | 0 |
| telegram/troubleshooting.md | 2,376 | 1 | 0 | 20 | 3 | 0 | 4 | 2 |
| telegram/upstream.md | 1,464 | 4 | 1 | 15 | 0 | 0 | 0 | 2 |
| telegram/web-proxy.md | 1,998 | 7 | 2 | 2 | 0 | 0 | 4 | 1 |
| telegram/websocket-bridge.md | 2,779 | 2 | 1 | 14 | 1 | 0 | 7 | 9 |
| watchdog.md | 2,905 | 2 | 2 | 0 | 1 | 0 | 2 | 2 |
