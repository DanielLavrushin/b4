# b4 documentation style guide

This guide is for everyone who writes or edits pages under `docs/`, people and AI agents alike.
It covers who the pages are for, the kinds of pages, voice and what to leave out. The
`docs-page` skill keeps the mechanics (locales, screenshots, building) and points here for
everything else.

## Who reads the docs

The reader has installed b4 on a router or a Linux box and manages it from the web interface.
They can follow install instructions and use SSH, but they are not network engineers. They
arrive with a goal (make a blocked site open, send one site through a VPN) or a problem
(Telegram stopped working).

Pages are written for that reader first. Readers who run b4 next to Xray, a VPN client or a
second router have their own section, **Under the hood**. Hub operators have theirs,
**Running a hub**.

## Page types

Every page is one type. The type is decided before writing, and types are not mixed on one
page.

| Type | Answers | Section | Shape |
| --- | --- | --- | --- |
| Tutorial | "How do I get from zero to a working setup?" | Get started | Numbered steps that end in a visible result |
| Guide | "How is X done?" | Guides | Purpose, requirements, steps, checking the result, troubleshooting |
| Screen reference | "What is this screen or field for?" | Web interface | One page per screen of the web interface: purpose, then fields in tables |
| Troubleshooting | "X is broken" | Troubleshooting | Symptom as the heading, then cause and fix |
| Explanation | "How does this work?" | Under the hood | Prose and diagrams, no steps |
| Technical reference | "What exactly is the format?" | Reference | Configuration file, command-line flags, API |

This is the [Diátaxis](https://diataxis.fr/) model, plus a screen reference, because the web
interface is where b4 is used.

### Guide template

```md
---
title: Routing sites through a VPN
description: One sentence on what the guide achieves.
---

One or two sentences: what the setup does and when it is needed.

## Requirements

- What must already exist, such as a VPN interface that is up on the router.

## Setup

1. Open **Sets** and click **Create set**.
2. ...

## Checking the result

Where the result shows, and what it looks like when the setup works.

## When it does not work

The two or three likely causes, each with its fix, or a link to Troubleshooting.
```

### Screen reference template

```md
---
title: The screen's name exactly as in the menu
description: One sentence.
---

What the screen is for, in one or two sentences, with links to the guides that use it.

![What the screenshot shows](/img/<section>/<file>.png)

## Card or section name as in the UI

| Field | What it does | Default |
| --- | --- | --- |
| **Label** | The effect, and when a change is useful. One or two sentences. | `value` |
```

## Voice

- **Impersonal, like official documentation.** Pages describe what b4 does and what the
  interface shows. No "you", "your" or "we". Write "a set decides which resolver answers the
  domains it targets", not "you choose which resolver to use".
- **Procedures are the one exception.** Numbered steps use the imperative ("Open **Sets** and
  click **Create set**."), still without "you". Everything outside the steps stays
  impersonal.
- **Recommendations are impersonal too**, admonitions included: "An empty list is the usual
  choice", not "Leave it empty".
- **Open with the purpose.** The first sentence of a page or a section says what it is for:
  "The tab sends a set's traffic through a VPN or another interface."
- **Present tense, active voice.** "b4 restarts the proxy", not "the proxy is restarted".
- **One idea per sentence.** Aim for 20 words or fewer. Split at "and", "which", "so" and
  semicolons.
- **Condition first.** "When the site still does not open, a new Discovery run is the next
  step."
- **Plain words.** "use", not "utilize"; "turn on", not "enable the toggle".
- **State facts, do not announce them.** No "Note that", "It is worth mentioning", "Two things
  follow from this".
- **No hook openers, packed sentences or justification chains.** No opening line that sells
  the feature. A sentence that carries several numbers or conditions is a table. No "so that",
  "because" or "which is what keeps X honest" hung on a fact, unless the reason changes what
  the reader does: state the fact and stop.
- **b4 is a "service"**, not a program or an app.
- **No marketing**: no superlatives, no "powerful", "advanced", "convenient".
- **No unverifiable claims about DPI.** Never "works in most cases" or "reliably bypasses".
  Write "the strategy Discovery suggests is the first one to try".
- **No em-dash.** A plain hyphen.

## What to leave out

The test for every sentence: **would a reader who manages b4 from the web interface do
anything differently after reading it?** If not, it does not belong on a Get started, Guides
or Web interface page.

| Leave out | Put it here instead |
| --- | --- |
| How b4 implements something: hooks, chains, conntrack, kernel versions, sockets | Under the hood, if an integrator needs it. Otherwise nowhere: the code is the record |
| Internal limits, buffer sizes, retry schedules, refresh intervals | Nowhere, unless the number shows in the UI or explains something the reader sees |
| How widgets behave: hover, keyboard, drag details, colours, shading | Nowhere. The UI shows it |
| What changed in which version ("Up to 1.84.0 the button was...") | The changelog |
| JSON config keys, API endpoints, MCP tool names | Reference: Configuration file, API and MCP |
| Catalogues of log lines | Troubleshooting, only lines a reader will search for |
| Edge cases of rare setups | Troubleshooting, under the symptom the reader sees |

What the UI already says is not repeated. If a field shows "Applies after a restart", the
docs need no table of the fields that need a restart.

## Formatting

- **UI labels** in bold, exactly as in the UI of that locale. Paths use `>`:
  **Settings > Core > Packet Engine**.
- **Steps**: a numbered list, one action per step. The result of a step follows it when the
  next step depends on it.
- **Field tables**: Field, What it does, Default. One or two sentences a row. A longer
  explanation goes in a section under the table.
- **Admonitions**: at most two a page. `:::warning` only for something that breaks traffic,
  exposes the router or loses data. `:::tip` for an optional shortcut. Text that belongs in
  the page does not go in `:::info` or `:::note`.
- **Screenshots**: where the reader acts, cropped to the card. The alt text says what the
  image shows, never a file name. Paths are absolute: `/img/<section>/<file>.png`.
- **Diagrams**: in explanations. A guide needs steps, not a flowchart. Each node is labelled
  by its function in the flow (Moderation, Catalogue, Reports), not by implementation details.
- **Headings**: sentence case. Guide titles name the task as a gerund or noun phrase
  ("Routing sites through a VPN"). Screen reference headings use the UI's own names.
- **One home per feature.** A feature is explained on its home page. Other pages link to it
  instead of repeating it.

## Page length

| Type | Aim for | Split when over |
| --- | --- | --- |
| Guide | 400-1,000 words | 1,500 |
| Screen reference | 500-1,500 words | 2,000 |
| Explanation | any | 3,000, by topic |

A page over the limit usually holds two page types or two features.

## When a feature changes

1. Does the change alter what the reader sees or does? If not, it goes in the changelog only.
2. A new field: one row in the screen reference table.
3. A new capability: a new guide, or a section of the guide for that goal.
4. A mechanism an integrator needs: Under the hood, not the screen page.
5. A fixed bug: the changelog. The docs describe how b4 works now, not how it used to.
6. Both locales in the same change. A wording or terminology fix in one locale is checked and
   made in the other too.

## Russian

- Russian is written for Russian readers, with the same structure and facts as the English
  page, not translated sentence by sentence.
- Безличный стиль, как в официальной документации: «Сет определяет, какой резолвер отвечает
  на его домены», а не «вы выбираете резолвер». Без «вы» и «ваш» вне шагов инструкций.
- Шаги инструкций - в повелительном наклонении: «Откройте **Сеты** и нажмите **Создать
  сет**». Рекомендации безличные: «Рекомендуется оставить список пустым», а не «Оставьте
  список пустым».
- Short sentences, no bureaucratic constructions: not «осуществляется перезапуск прокси» but
  «b4 перезапускает прокси». Avoid «осуществляется», «производится», «является», «данный»,
  «в случае если».
- Fixed terms: a set is «сет»; b4 is a «сервис», never «программа»; Discovery the feature is
  «Дискавери», while the act of searching stays «поиск»; the watchdog is «мониторинг», as in
  the UI. Payloads, strategy family names and TLS, TCP, UDP, SNI, DPI, QUIC, TSPU, ASN, DoH,
  RTT, HTTP, CIDR, FAB, IPv6, IPInfo, MTProto, MSS, SOCKS5 and NFQUEUE stay in English.
- UI labels in bold, exactly as in the Russian UI.

## Examples

**An internal mechanism on a screen page** (dashboard.md):

> Times on the dashboard are in the browser's time zone. b4 measures every time against its
> own uptime and converts it with the router's clock when it sends it, so the history stays
> in order when the router sets its clock late, as a router without a battery-backed clock
> does when it syncs over NTP after boot.

Removed. Times appear in the browser's time zone, and the reader has nothing to do.

**A field described by its implementation** (settings/core.md):

> **Capture Interfaces** (`queue.interfaces`), shown in NFQUEUE mode, decides which packets
> the engine inspects at all. [...] b4's capture rules sit in the `postrouting` and `output`
> hooks, after the kernel has decided where the packet goes, and for forwarded traffic the
> interface compared here is **the one the packet leaves by**.

Rewritten for the reader:

> **Capture Interfaces** limits b4 to traffic that leaves the router through the selected
> interfaces. An empty selection, the default, covers every interface and is the usual
> choice. When nothing works after an interface is selected, clearing the selection is the
> first check.

**A rule of the kernel on a user page** (dns.md):

> With iptables on kernels before 4.18 (4.14 before 4.14.173), on KeeneticOS and wherever the
> raw table cannot queue packets, requests are not queued: a rule at the top of mangle
> `INPUT`, `FORWARD` and `OUTPUT` sends b4 a copy of each one with NFLOG [...]

Moved to Under the hood. The user page keeps what the reader acts on:

> b4 sees DNS that goes to port 53. A device that uses encrypted DNS (secure DNS in the
> browser, Android Private DNS) bypasses it, and the sets do not apply to its lookups until
> encrypted DNS is turned off on that device.
