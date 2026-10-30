---
name: b4-issue
description: Triage a b4 GitHub issue or bug report - read it with its comments, pin down the platform, engine, firewall backend and version from the System Info JSON and the trace, rule out the usual false leads (a corrupted binary, an unmeasured "it worked in version N", rules that are present but match nothing, a misread Discovery verdict), check the known platform traps, and draft what to ask the reporter or what to fix. Use when given an issue number or link, a pasted bug report, a b4 log trace or System Info JSON, or a user's "b4 stopped working" description.
argument-hint: "<issue-number>"
---

# Issue triage

## 1. Set up and read

- Work in a worktree named `wt-issue-<n>` on `origin/main` (`/b4-worktree`).
- `gh issue view <n> --comments`, and download the attachments: the System Info JSON and the
  `b4-trace-*.log` file are usually the whole case.
- Never post a comment, label or close an issue without being asked. Draft the reply in the
  reporter's language and show it first.

## 2. Pin down the box

From System Info (Settings, System, Service, System Info, Copy JSON) and the trace header:

| Fact | Why it matters |
| --- | --- |
| b4 version | Compare with the latest release and the changelog before reading code: the bug may already be fixed. |
| Platform and firmware | Merlin, Keenetic, OpenWrt, MikroTik container, Xiaomi, Padavan and plain Linux each have their own traps, below. |
| Kernel version | Before 4.18, conntrack loses the second of two queued UDP DNS queries on one socket, and `ip rule del` without a mask deletes any rule with that mark. |
| Engine | NFQUEUE or TUN. Discovery and the watchdog's healing need NFQUEUE. |
| Firewall backend | iptables-legacy, iptables-nft or nftables: separate code paths (`/b4-tables`). |
| Sets involved | Their targets, routing mode and strategy, from the trace's set summary or the set's Copy JSON. |

If the report lacks these, ask for them with the reporting page:
`https://docs.b4core.app/docs/troubleshooting/reporting` (Russian:
`https://docs.b4core.app/ru/docs/troubleshooting/reporting`). It explains the trace, the System
Info copy and what in them is private.

## 3. Rule out the false leads first

- **Standard-library panics, or a "regression" nobody else sees**: compare the installed binary
  with the one in the release archive for that version and architecture (`SHA256SUMS` covers
  the archives). Routers on USB flash have lost writes after a reboot and run a corrupted
  binary.
- **"It worked in version N"**: measure N before bisecting. Reporters' memory of a working
  version has been wrong.
- **Rules present is not traffic matched**: ask for counters (`iptables -vnL -t mangle`,
  `nft list ruleset` with counters) rather than trusting a listing.
- **A Discovery verdict can mislead**: a Cloudflare WAF page counts as a reachable site, a
  site's own 451 looks like an ISP block, and a strategy that Discovery found but the browser
  "ignores" can be the browser going to `http://` on port 80 instead.
- **An address-level block** (TLS dropped at an address for any name) is beaten by no packet
  strategy. Routing the set through an interface or proxy is the answer, not a better strategy.
- **Third-party egress**: where the reporter tests from, and where the agent's own `curl`
  goes out, are different networks. Neither is a neutral vantage point.

## 4. Platform traps

- **ASUS Merlin**: BusyBox 1.25.1 without `od`; Broadcom overwrites the whole conntrack mark;
  XrayUI uses mark 0x10000, table 77 and PREROUTING position 1; table 100 is `wan0`. The service
  script sends b4's stdout to `/dev/null`, so logs come from the trace.
- **KeeneticOS**: no `modules.dep`, so `modprobe` fails and only `insmod <path>` loads a module;
  NDMS rewrites whole netfilter tables often ("rules missing" in the log is normal); its raw
  table runs after conntrack; its mangle INPUT drops locally delivered 443, which breaks proxy
  sets' TPROXY.
- **OpenWrt**: fw4 is nftables; 64 MB boxes run `/tmp` out of space during a self-update.
- **MikroTik**: the community container image; RouterOS 7.21.2 and later no longer mount a
  tmpfs on `/tmp`.
- **Xiaomi stock firmware**: firewall restarts wipe b4's TUN rules; NFQUEUE and hashlimit
  arrive only in later firmware; `br_netfilter` breaks TPROXY.
- **OpenRC (Alpine)**: the init script is sourced by the runner, so it must never replace
  `PATH`.
- **32-bit routers on kernels before 5.2**: binaries built with Go 1.26 or later crash at start,
  which is why b4 stays on Go 1.25.

## 5. Read the trace

- It starts with a System Info snapshot and the enabled sets, then the log lines at the level
  the reporter chose. Debug is the useful one; at Info, ask for a new trace.
- Rebuild windows are marked by `resyncing routing rules`, `Routing rules missing`,
  `reinstalled policy routes for interface` and `IPTABLES: adding rules`. "Slow after
  switching sets" reports usually fall inside one.
- `engine did not start` means degraded mode: the UI is up but no packets are processed.

## 6. Conclude

Say which of these it is, with the evidence (log line, config field, code location):

- **A b4 bug**: reproduce it in a test or a netns rig before fixing, then follow the area skill
  (`/b4-tables`, `/b4-setting`, `/b4-installer`, `/b4-ui`, `/b4-mcp`).
- **Configuration**: the reply names the setting by its UI label and location.
- **A network that blocks differently** (an address block, a block no strategy beats): say so
  plainly, without promising that a strategy will work.
- **Not enough data**: the reply asks for exactly the missing item and links the reporting
  page.
