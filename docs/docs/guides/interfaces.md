---
sidebar_position: 1
title: Which interface is which
description: The three interface settings of b4, what each one filters, and how to tell which one is wrong.
---

Three settings take an interface name: the capture filter of the packet engine and two fields of a routing set. They sit in different places of the web interface and do different things.

| Setting | Where | What it filters | None selected |
| --- | --- | --- | --- |
| **Capture Interfaces** | **Settings > Core > Packet Engine** | Which packets the engine inspects | Every interface |
| **Source Interfaces** | **DNS & Routing > Traffic Routing** of a set | Which arriving traffic the set's rules are offered | Any interface |
| **Output Interface** | **DNS & Routing > Traffic Routing** of a set | Where a matched packet is sent | The set routes nothing |

The first two are filters and narrow what b4 does. Only the third sends traffic anywhere. The interface lists of [NAT Masquerade](../settings/core.md#nat-masquerade) and [DSCP](../settings/core.md#dscp), and the **Uplink interface** of the [TUN engine](../settings/core.md#tun-settings), take interface names as well.

## Capture Interfaces

The field is described under [Capture Interfaces](../settings/core.md#network-interfaces). For forwarded traffic the engine compares the interface a packet **leaves** by, because its capture rules run after the kernel has chosen the route. Only the reply direction and DNS are matched on the interface they arrive on.

The leaving interface comes from the routing table. A VPN client, a policy route or a transparent proxy that moves the default route changes it without touching this list, and a selection made before that stops matching. While the selection matches nothing, b4 inspects nothing and no set applies.

## Source Interfaces

Each name becomes an `iifname` qualifier (`-i` on iptables) on the set's marking, TPROXY and block rules, so it selects which arriving traffic those rules are offered. The match is on arrival:

- A WAN interface here never matches traffic from the local network. A LAN client's traffic arrives on the bridge, `br0` on most consumer routers, not on the uplink.
- Traffic the router originates arrives on no interface, so a set with a source interface does not route, proxy or block the router's own connections. See [Router's own traffic](../sets/routing.md#routers-own-traffic).

Source devices listed under **Targets > Source Devices** have the same effect, unless the list is an exclude list. No selection is the usual choice; a name here belongs to a set meant to cover one segment.

## Output Interface

The set's addresses are marked, and an `ip rule` sends the mark to a routing table whose default route points at this interface. See [Network interface](../sets/routing.md#output-interface) and [How routing sets work](../internals/routing.md#interface-mode).

## Finding which one is wrong

| Symptom | Setting to check |
| --- | --- |
| Nothing on the **Traffic** page, or only traffic from the router itself | Capture Interfaces |
| Sets apply to the router but never to a client | Capture Interfaces, then Source Interfaces |
| A set's marking rule shows zero packets in the firewall counters | Source Interfaces |
| Traffic is marked but leaves by the wrong path | Output Interface, and any other service with `ip rule` entries |

The **Traffic** page answers the first two. When the router is the only source (**Router** in the device list, or its own address in the raw feed), the engine sees only the traffic the router originates.

When another service also does policy routing, a set can mark traffic correctly and still not use its own table. [Using b4 with Xray or XrayUI](./xray.md) covers that case.
