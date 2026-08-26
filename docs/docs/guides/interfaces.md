---
sidebar_position: 1
title: Which interface is which
---

b4 has three settings that take an interface name, in two different places in the web
interface, and they mean three different things. Picking the wrong one is the most common way to end up with a
configuration that looks correct and does nothing.

| Setting | Where | What it filters | Empty means |
| --- | --- | --- | --- |
| **Network interfaces** | `Settings > Core` | which packets the engine inspects at all | every interface |
| **Source interfaces** | `Sets > Routing` | which arriving traffic a set's rules are offered | any interface |
| **Output interface** | `Sets > Routing` | where a matched packet is sent | routing is off for that set |

Only the third one sends traffic anywhere. The first two are filters, and both of them
narrow what b4 does rather than widen it.

## Network interfaces

`Settings > Core`. This is the engine's own filter, applied to every packet the firewall
hands to b4 before anything else happens.

It is not a list of interfaces b4 attaches to. b4's capture rules sit in the `postrouting`
and `output` hooks, where the kernel has already decided where the packet is going, so for
forwarded traffic the interface being compared is **the one the packet leaves by**, not the
one it arrived on. Only traffic captured in `prerouting`, which is the reply direction and
DNS, is matched on the arriving interface.

That has a consequence worth stating plainly: the interface a packet leaves by is decided
by the routing table, which another service can change without touching this list. A VPN
client, a policy route or a transparent proxy that moves the default route puts every
packet on a different interface, and a selection made before that happened silently stops
matching.

:::warning
While the selection matches nothing, b4 inspects nothing. Packets are still queued to it,
so the cost is still paid, and every one of them is accepted unchanged. No set applies, no
strategy runs, and nothing in the rules looks wrong.
:::

Since b4 1.80.0 the web interface says so. b4 counts the packets it is handed per
interface, and the setting carries a warning whenever an interface it is not watching is
carrying outgoing traffic, naming that interface and how much of the total it accounts for.
An empty selection never warns, because it covers everything.

The same counts are in `b4_diagnostics` and in the diagnostics report, as `packets_leaving`
and `packets_arriving` per interface. `packets_leaving` is the one the filter gates for
outgoing traffic; an interface with a high `packets_leaving` that is not selected is traffic
b4 is not inspecting.

:::tip
Leave it empty. The filter runs in userspace, after the packet has already been copied out
of the kernel, so a narrower list saves the analysis but not the capture. There is very
little to gain and a whole failure mode to lose.
:::

## Source interfaces

`Sets > Routing`, per set. This one is a real firewall match: each name becomes an
`iifname` qualifier (`-i` on iptables) on the set's marking, TPROXY and block rules, so it
decides which **arriving** traffic is offered to that set's rules.

Two things follow from it being an ingress match:

- A WAN interface here can never match traffic from the local network. Traffic from a LAN
  client arrives on the bridge, `br0` on most consumer routers, not on the uplink. Naming
  the uplink produces a set whose rules match nothing.
- Traffic the router originates arrives on no interface at all, so a set with a source
  interface never routes, proxies or blocks the router's own connections. See
  [Router's own traffic](/docs/sets/routing#routers-own-traffic).

Listing source devices under `Sets > Targets` has the same effect, unless the list is an
exclude list.

:::tip
Leave it empty unless the set is genuinely meant for one segment. Empty means the set's
rules are offered every packet, and the set's targets decide the rest.
:::

## Output interface

`Sets > Routing`, per set. The only one of the three that moves traffic: matched
destinations are marked, and an `ip rule` sends that mark to a private routing table whose
default route points here. This is covered in full under
[Routing](/docs/sets/routing#output-interface).

## Telling which one is wrong

| Symptom | Look at |
| --- | --- |
| No connections at all in `Connections`, or only ones from the router itself | Network interfaces |
| Sets apply to the router but never to a client | Network interfaces, then source interfaces |
| A set's marking rule shows zero packets in the firewall counters | Source interfaces |
| Traffic is marked but leaves by the wrong path | Output interface, and any other service with `ip rule` entries |

`Connections` is the quickest of these. If every row has the router's own address as the
source, the engine is only seeing traffic the router originates, and the network interface
filter is why.

:::info
When another service also does policy routing, a set can mark traffic correctly and still
not use its own table. [b4 with Xray or XrayUI](/docs/guides/xray) walks through that case.
:::
