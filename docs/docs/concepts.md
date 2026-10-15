---
sidebar_position: 50
title: Key concepts
description: The terms the web interface and the documentation use, each with a link to its page.
---

The terms below appear throughout the web interface. Each section links to the page that covers it in full.

## Set {#set}

A set ties a list of targets to what b4 does with their traffic. b4 changes nothing until a set targets sites. Sets are created on the **Sets** page or from a Discovery result, see [Sets](sets/index.md).

## Target {#target}

Targets decide which traffic a set applies to: domains, IP addresses and ranges, GeoSite and GeoIP categories, ASNs and source devices. They are edited on the set's **Targets** tab, see [Targets](sets/targets.md). [How sets match traffic](internals/matching.md) explains which set handles a connection that several sets match.

## Strategy {#strategy}

A strategy is the set of packet changes a bypass set makes, such as splitting a request into pieces or sending fake packets ahead of it. It is configured on the set's **TCP** and **UDP** tabs, see [TCP](sets/tcp/index.md). Discovery tests strategies and suggests the one that loaded the sites.

## Set kinds {#set-kinds}

The dashboard shows each set's kind. **Enable Routing** and **Routing mode** on the set's **DNS & Routing** tab decide it.

| Kind | Setting | What the set does |
| --- | --- | --- |
| **Bypass** | **Enable Routing** off | Applies its strategy to its traffic. See [Unblocking a site](guides/unblock.md) |
| **Route** | **Network interface** | Sends its traffic out through another interface, such as a VPN. See [Routing sites through a VPN or another interface](guides/vpn.md) |
| **Proxy** | **Upstream SOCKS5 proxy** or **Telegram over WebSocket (built-in)** | Hands its traffic to a SOCKS5 proxy, or to b4's Telegram bridge. See [Sending sites through a SOCKS5 proxy](guides/proxy.md) |
| **Block** | **Block (blackhole)** | Stops its traffic, for every device and for the router itself. See [Blocking ads and sites](sets/blocking.md) |

A proxy set applies no strategy, and neither does a route set whose interface is a tunnel such as a VPN: the tunnel or the proxy carries the traffic to the site. A set routed to a plain interface, such as a second uplink, keeps its strategy, see [Network interface](sets/routing.md#output-interface).

## Discovery {#discovery}

Discovery tests b4's strategies against sites that do not open. It turns the strategy that loaded them into a new set, or writes it into an existing one. It is the usual way to make a first set, see [First steps](quickstart.md) and [Discovery](discovery.md).

## Watchdog {#watchdog}

The watchdog fetches a set's pages on a schedule. When they keep failing, it runs Discovery for the set and writes the new strategy into it. It is turned on under **Settings > Discovery**, and for each set with **Keep this set working with the watchdog** on the set's **Discovery** tab. See [Watchdog](watchdog.md).

## Escalation {#escalation}

Escalation moves a site to a backup set when its own set keeps failing to reach it, and tries the original set again after a while. The backup can use another strategy, a VPN or a proxy. The watchdog repairs a whole set on a schedule, while escalation reacts to the failing connections of one site. It is set up on the set's **Escalation** tab, see [Escalation](sets/escalation.md).

## Community sets {#community-sets}

Community sets are sets that other b4 users published to the Community Hub. The **Community** page lists them, applies one in one step, and collects reports on whether it works. An applied set is a normal set marked **Community set**. The page appears once **Enable Community Hub** is on under **Settings > Integrations**, see [Community](community/index.md).

## Packet engine {#packet-engine}

The packet engine is how traffic reaches b4:

- **NFQUEUE**, the default, has firewall rules hand packets to b4.
- **TUN interface** routes the traffic through a virtual interface instead. It is the alternative on kernels without the NFQUEUE modules.

Either way, b4 reads only the first packets of each connection. The engine is chosen under **Settings > Core > Packet Engine** and changes after a restart, see [Packet engine](settings/core.md#packet-engine). When the engine does not start, the [dashboard](dashboard.md#packet-engine-not-running) shows the error and offers the other engine.
