---
sidebar_position: 1
title: Which interface is which
---

Three settings in b4 take an interface name. They sit in two different places in the web
interface and they filter different things.

| Setting | Where | What it filters | Empty means |
| --- | --- | --- | --- |
| **Network interfaces** | `Settings > Core` | which packets the engine inspects | every interface |
| **Source interfaces** | `Sets > Routing` | which arriving traffic a set's rules are offered | any interface |
| **Output interface** | `Sets > Routing` | where a matched packet is sent | routing is off for that set |

The first two are filters and narrow what b4 does. Only the third sends traffic anywhere.

## Network interfaces

`Settings > Core`. The engine compares every packet the firewall hands it against this list
before anything else happens.

b4's capture rules sit in the `postrouting` and `output` hooks, where the kernel has already
chosen where the packet goes. For forwarded traffic the interface compared here is the one
the packet leaves by. Traffic captured in `prerouting`, which is the reply direction and
DNS, is matched on the interface it arrived on.

The interface a packet leaves by comes from the routing table, so another service can change
it without touching this list. A VPN client, a policy route or a transparent proxy that
moves the default route puts traffic on a different interface, and a selection made before
that stops matching.

:::warning
While the selection matches nothing, b4 inspects nothing. Packets are still queued to it and
every one is accepted unchanged. No set applies and no strategy runs.
:::

b4 counts the packets it is handed, per interface. The setting carries a warning when an
interface outside the selection is carrying outgoing traffic, and names it. An empty
selection never warns.

The same counts are in the diagnostics report as `packets_leaving` and `packets_arriving`
per interface. `packets_leaving` is the number this filter gates for outgoing traffic.

:::tip
An empty list is the usual choice. The filter runs in userspace, after the packet has been
copied out of the kernel, so a shorter list saves the analysis but not the capture.
:::

## Source interfaces

`Sets > Routing`, per set. Each name becomes an `iifname` qualifier (`-i` on iptables) on
the set's marking, TPROXY and block rules, so it selects which arriving traffic those rules
are offered.

The match is on arrival, which has two consequences:

- A WAN interface here never matches traffic from the local network. Traffic from a LAN
  client arrives on the bridge, `br0` on most consumer routers, not on the uplink.
- Traffic the router originates arrives on no interface, so a set with a source interface
  does not route, proxy or block the router's own connections. See
  [Router's own traffic](/docs/sets/routing#routers-own-traffic).

Listing source devices under `Sets > Targets` has the same effect, unless the list is an
exclude list.

:::tip
An empty list is the usual choice; a name here belongs to a set meant to cover one segment.
Empty means the set's rules are offered every packet, and the set's targets decide the rest.
:::

## Output interface

`Sets > Routing`, per set. Matched destinations are marked, and an `ip rule` sends that mark
to a private routing table whose default route points at this interface. See
[Routing](/docs/sets/routing#output-interface).

## Finding which one is wrong

| Symptom | Setting to check |
| --- | --- |
| No connections in `Connections`, or only ones from the router itself | Network interfaces |
| Sets apply to the router but never to a client | Network interfaces, then source interfaces |
| A set's marking rule shows zero packets in the firewall counters | Source interfaces |
| Traffic is marked but leaves by the wrong path | Output interface, and any other service with `ip rule` entries |

The `Connections` page answers the first two. When every row carries the router's own
address as the source, the engine is seeing only traffic the router originates.

:::info
When another service also does policy routing, a set can mark traffic correctly and still
not use its own table. [b4 with Xray or XrayUI](/docs/guides/xray) covers that case.
:::
