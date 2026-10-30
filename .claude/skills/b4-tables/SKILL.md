---
name: b4-tables
description: Change how b4 emits firewall and routing state - iptables and nftables rules, chains and jump positions, packet marks, ip rules and route tables, MSS clamping, NAT, TPROXY, the capture chain, the monitor that restores rules - in src/tables, src/tproxy or src/tun. Covers both backends, the rule-ordering and deletion traps that broke real routers, the neighbours b4 shares netfilter with, the netns test suite and the marks reference. Use before editing anything in those packages or when debugging rules that look present but do not match traffic.
---

# Firewall and routing rules

Most bugs here are silent: the rules look present, the tests pass, and on a real router the
traffic takes another path. Prove a change with packet counters, never with rule presence.

## Before editing

- Read `docs/docs/guides/marks.md`: every mark bit, ip rule priority and jump position b4 uses,
  and the values neighbour services use. `docs/docs/guides/xray.md` covers XrayUI.
- Find both emitters for what you change. iptables and nftables have separate code paths
  (`iptables.go` and `nftables.go`, `routing_ipt.go` and `routing_nft.go`, and more), and
  iptables itself comes as legacy and as iptables-nft. A change verified on one backend is
  unverified.
- Backend choice is `detectFirewallBackend`: it probes nft by creating a table and prefers
  `iptables-legacy` when `iptables --version` says nf_tables. Never branch on whether an `nft`
  binary exists: routers ship one without kernel support.

## Placing rules

- **Insert, do not append, into a built-in chain b4 does not own** when the rule must beat the
  host firewall: `-I` at the top, delete before insert. An appended jump can count zero
  packets while its ipset looks right.
- **A rule that must sit above another in the same chain is `-I`.** The monitor restores a
  single deleted rule with `-C` and re-adds only that one, at the chain's end for `-A` or its
  top for `-I`, so an appended guard comes back below the rule it guards. nft rebuilds whole
  tables and cannot misorder.
- **iptables MSS clamping**: rules go in with `-I` and TCPMSS does not terminate, so the last
  match wins. Emit per-set, then per-device, then global to get per-set over per-device over
  global, the nft semantics. Reordering inverts precedence with no test failing.
- **nat POSTROUTING**: any MASQUERADE above b4's per-set nat chain is terminal (the host's own,
  OpenWrt fw4's, ASUS's). New per-set nat behaviour jumps ahead: iptables `-I POSTROUTING 1`,
  nft the separate `postrouting_early` base chain. `deleteJumpRules` sweeps both.
- **The capture chain starts with the queue-mark RETURN**, so packets b4 injects are not queued
  a second time. Without it, kernels before 4.18 with NAT lose them in conntrack.
- **mangle OUTPUT**: a soft restart re-inserts the queue-mark ACCEPT at position 1, and
  `routeEnsureOutJumpPrecedence` lifts b4's jumps back above it. Every new OUTPUT jump must be
  covered by that lift.
- The 0x8000 fallback for `Queue.Mark == 0` is written separately in `iptables.go`,
  `nftables.go` and `monitor.go`: change all three together.

## Reading and deleting rules

- **Never enumerate rules with `iptables -S`.** A foreign target whose userspace extension is
  missing (common on Merlin with Skynet or Diversion) makes `-S` exit 1 with the output cut at
  that rule. Use `-L -n --line-numbers` and delete by line number, highest first
  (`iptJumpLineNumbers`, `iptDeleteListedLines`); check one rule with `-C`.
- **Never `ip rule del fwmark <mark>` without a mask.** iproute2 and busybox send the mask only
  when the argument has a `/`, and kernels up to 4.17 then delete any rule with that mark
  value, including the one just added.
- **Never mark 0 or table 0.** `ip rule del fwmark 0x0/0x0 lookup 0` deletes every rule,
  local and main included; `ip route replace default dev X table 0` replaces main's default.
- **Never emit, reuse or flush a table id that `/etc/iproute2/rt_tables` names or that holds
  routes.** On ASUS Merlin table 100 is `wan0`. `routing_tablealloc.go` consults both.
- busybox `ip` stores table ids above 255 correctly only from 1.26 (1.24 and 1.25 accept them
  and silently keep 8 bits, so 256 lands on main), which leaves ids up to 252 as the only safe
  range everywhere. A masked `fwmark V/M` in `ip rule` works only from busybox 1.33.

## Changing state safely

- **Install the new state before removing the old.** A reconfigure that removed first left a
  set disarmed for about a second and leaked requests (`TestNetnsAReconfiguredSetIsNeverDisarmed`).
- **A rollback can only undo what it snapshotted.** Every step that can fail, chain creation
  included, runs before any live chain is flushed.
- **Cached state compared to skip a rebuild carries every input.** A state without the address
  family made the IPv6 toggle half-apply.
- **Keep the checkers aligned with the emitters.** `RoutingRulesPresent` and the monitor's
  checks must match what is emitted, or the monitor restores on every tick forever. A new check
  derives from the emitted spec (`iptMarkReturn` and the like), and the canned `-S` listings in
  the monitor tests carry the new line.
- New rule-mutation entry points go through the refresh wired by `SetTablesRefreshFunc`, which
  waits for a running Discovery.

## Neighbours

b4 shares netfilter with the router's own services; their facts are in `marks.md`:

- XrayUI on Merlin uses mark 0x10000 and table 77, re-inserts its DIVERT at PREROUTING 1 on
  every restart, and its WireGuard outbound sets `src_valid_mark=1`, which drops TPROXY'd LAN
  SYNs as martians.
- ASUS Broadcom builds overwrite the whole conntrack mark, so a verdict carried in the ct mark
  can fail open there.
- KeeneticOS (NDMS) rewrites whole netfilter tables often, so "rules missing" there is normal
  and the monitor's job. Its mangle INPUT drops locally delivered 443, and it masquerades a
  forwarded connection only if the first packet passed its own FORWARD mark.

## Testing

- `make test-netns` runs the routing and conntrack suites in an unprivileged namespace. CI
  never runs it. Run it for every change here.
- `go -C src test -shuffle=on ./tables/` over a few seeds: package globals leak between tests,
  and every new routing global is reset in `familyResetGlobals`.
- netns test files run in name order; a file named `routing_netns_a*` sorts before the
  blackhole test and breaks it.
- A test goroutine that calls `Setns` switches back before `UnlockOSThread`; compare namespaces
  through `/proc/thread-self/ns/net`, not `/proc/self`.
- iptables-legacy needs the `ip_tables` module, which a user namespace cannot load, so the
  legacy subtests fail locally with "Table does not exist". Verify legacy on a real router or
  as root on a disposable VM.
- On a dev box the iptables routing backend needs `system.tables.engine: "iptables"` and the
  `ipset` binary; without ipset, routing silently uses nft.
- `runLogged` (`src/tables/common.go`) is a package variable, so a test can capture the exact
  argv; nft coverage uses a stub `nft` binary on PATH.
- Kernel-version bugs (the unmasked `ip rule del`) cannot show on dev or CI kernels. After a
  change to emitted chains, watch a real router for a quiet window: a monitor that resyncs
  every tick shows only there.

## Docs

A change to a mark bit, an ip rule priority or a jump position updates
`docs/docs/guides/marks.md` and its Russian mirror in the same pull request.
