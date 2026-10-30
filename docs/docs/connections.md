---
sidebar_position: 12
title: Traffic
description: The connections b4 inspects, live, with the set each one matched, and the buttons that add domains, addresses and networks to sets.
---

The **Traffic** page shows the connections b4 inspects as they happen: the protocol, the domain, the destination, the device and the set that matched. It shows whether a site's connections reach a set, and adds domains, addresses and networks to sets. [DNS for sets](dns.md#reading-the-result) uses it to check DNS.

b4 sees only the first packets of each connection, up to the per-connection limits under [Settings > Core > Packet Engine](settings/core.md#queue-and-packet-processing). One connection therefore gives several entries, and the packet counts say nothing about speed or volume. DNS queries that match a set appear too, with what b4 did with each, while **Log Level** is **Info** or more detailed. A connection that a [proxy set](sets/routing.md#upstream-socks5-proxy) sends to its upstream appears once.

The page opens in **Aggregated** mode. **Raw feed** in the top right corner shows single packets instead.

![Traffic, Aggregated mode](/img/connections/20260923214001.png)

## Aggregated

Packets are grouped by device, protocol and domain, and a packet without a domain by its destination address. The groups build up while the page is open. Leaving the page, switching to **Raw feed** or clearing starts them over.

| Column | Shows |
| --- | --- |
| **Protocol** | `TCP` or `UDP` |
| **Domain** | The domain from the handshake or **(no domain)**, the TLS version (`1.2`, `1.3`) and the [labels](#labels). A domain outside every set has a **+** button, see [Adding domains to sets](#adding-domains-to-sets) |
| **Destination** | The latest destination address, `+N` when there were several, and its [ASN](#asn) |
| **Set** | The set matched by domain, and the set matched by IP when it is another one |
| **Source** | The device, see [Devices](#devices) |
| **Activity** | Packets per second over the last minute |
| **Packets** | The number of packets in the group |
| **Seen** | Time since the latest packet |

A click on a row opens the **Detail** panel: activity, packet count, the time from the first packet to the last, the source, every destination address of the group and the matched sets. For a domain outside every set it also has **Add domain to a set**.

Providers may block TLS 1.2 and TLS 1.3 differently, and a set can be limited to one TLS version on its [Targets](sets/targets.md) tab.

### Control bar

| Control | Effect |
| --- | --- |
| Filter field | Keeps the matching rows, see [Filter](#filter) |
| **30s**, **1m**, **5m**, **15m**, **All** | Shows the groups with packets within that period. **1m** is the default |
| **Unmatched only** | Keeps only the groups no set matched |
| **Domains only** / **All packets** | See [Domains only and all packets](#domains-only-and-all-packets) |
| Trash can | **Clear Packets**, see [Pausing and clearing](#pausing-and-clearing) |

The dashboard opens this page with **Unmatched only** on from **Domains not in any set** on [Activity](dashboard.md#activity), and with a device selected from [Blocked](dashboard.md#blocked), both with the period at **All**.

### Devices

The **Devices** panel on the left lists the sources seen within the period, with the time since the latest packet, the activity and the packet count. A click on a device limits the list to its groups, and **All devices** removes the selection. **Hide devices** collapses the panel. **Sort devices** orders it **By name**, the default, **By latest activity** or **By packet count**.

A device is identified by its MAC address. When the MAC is unknown, as for a device behind another router, the source shows by its IP address. Aliases from the [device filtering](settings/core.md#device-filtering) table are always shown. With device filtering or vendor lookup turned on there, the names of discovered devices are used too, and the router's own addresses show as **Router**.

## Raw feed

**Raw feed** shows the latest 1000 packets one by one, the newest at the bottom.

![Traffic, Raw feed mode](/img/connections/20260923214002.png)

| Column | Shows |
| --- | --- |
| **Time** | The time of the packet |
| **Protocol** | `TCP` or `UDP` and the [labels](#labels) |
| **Set** | The set matched by IP, or by domain when no set matched by IP |
| **Domain** | The TLS version and the domain. A domain outside every set has a **+** button |
| **Source** | The device name, or the source IP address and port |
| **Destination** | The destination address and port, the [ASN](#asn), and a **+** button for an address outside every set |

## Labels

Labels mark special cases: next to the domain in **Aggregated** mode, and next to the protocol in the raw feed and in the **Detail** panel.

| Label | Meaning |
| --- | --- |
| `dup` | The packet was duplicated, see [Packet duplication](sets/tcp/general.md#packet-duplication) |
| `ip` | The destination was found blocked by IP. An outlined label means the address was already in the cache of blocked addresses |
| `ban` | The destination is a [banned address](sets/tcp/general.md#banned-addresses), and the connection was reset at once |
| `block` | A [block set](sets/blocking.md) blocked the packet |
| `voice` | A call packet passed through untouched: by a set with **Calls** on **Pass call setup**, or by a [set that handles only calls](sets/udp.md#protocol-selection), as a later packet of a call the set took. The packet such a set takes itself carries no `voice` label and shows that set in the **Set** column |
| `proxy` | The connection came through b4's [SOCKS5 proxy](settings/core.md#socks5-proxy) |
| Telegram icon | A connection of b4's [MTProto proxy](telegram/mtproto-proxy.md), with the secret's name |
| Telegram icon with **Telegram bridge** | A connection relayed by [Telegram over WebSocket](telegram/websocket-bridge.md) |
| `doh`, `forward`, `pin`, `sinkhole` and others | What b4 did with a DNS query, see [DNS for sets](dns.md#reading-the-result) |

## Domains only and all packets

The switch in the control bar applies to both modes.

| Mode | Shows |
| --- | --- |
| **Domains only** | Packets with a recognised domain. The default |
| **All packets** | Every inspected packet, also the TCP handshake before the ClientHello, connections without SNI and traffic to addresses that have no name |

## Filter

The filter field works in both modes.

- `+` joins conditions, and all of them must hold.
- `!` before a condition excludes what it matches.
- `field:value` checks one field. Several conditions on the same field hold when any of them matches. An empty value, such as `set:`, matches an empty field.
- A word without a field is looked up in every field.

Matching ignores case and looks for a substring. The fields of both modes are `protocol`, `domain`, `destination`, `tls`, `flags`, `asn` and `device` (also `alias`). `set` works only in **Aggregated** mode, `source` only in the raw feed.

| Filter | Shows |
| --- | --- |
| `protocol:udp` | UDP packets |
| `domain:youtube` | Packets with "youtube" in the domain |
| `protocol:tcp+!domain:google.com` | TCP packets, except google.com domains |
| `asn:AS13335` | Destinations in AS13335 (Cloudflare) whose ASN has been looked up |
| `flags:dns` | DNS queries and what b4 did with them |
| `set:` | Groups that no set matched |

## Adding domains to sets

The **+** button next to a domain opens **Add Domain to Manual List**.

1. Pick the domain or one of its parent domains, listed from the most specific to the broadest. For `rr1---sn-ab5l6ne7.googlevideo.com` these are the name itself and `googlevideo.com`.
2. Under **Target Set**, choose one of the enabled sets, or **Create New Set** and enter a **Set Name**.
3. Click **Add Domain**.

A domain in a set covers its subdomains too. A service that serves content from many subdomains, such as YouTube or a CDN, is covered by its parent domain, such as `googlevideo.com`.

## Adding addresses and networks to sets

The **+** button next to a destination address, and in the raw feed a click on the address itself, opens **Add to a set**. b4 looks up which network announces the address, through [RIPEstat](https://stat.ripe.net/). The dialog then shows the address, the announced prefix that covers it and the network, such as **AS15169 Google LLC**. When several networks announce the prefix, one of them is picked in the dialog. A private address has no network. A failed lookup can be repeated with **Retry**, and the address can be added either way.

### What to add

| Choice | What goes into the set |
| --- | --- |
| **Single IP** | The address alone, as `/32`, or `/128` for IPv6. The default |
| **Announced prefix that covers this address** | The prefix, such as `142.250.0.0/15`, when it is known and wider than the address |
| A wider range | The address with a mask such as `/24` or `/16`, for IPv6 `/64` or `/48`, saved as its network: `142.250.120.139/24` becomes `142.250.120.0/24` |
| The whole network | The ASN itself, see [ASN](sets/targets.md#asn). b4 fetches its prefixes and keeps them up to date daily |

For the whole network, the dialog shows the ASN's prefix count and roughly how many IPv4 addresses it holds, and warns about a large network as the set editor does. The choice is unavailable when the set already has that ASN. An ASN whose prefixes are not known yet is added anyway, and b4 fetches them in the background.

The set is chosen under **Target Set**, as in the domain dialog, and the confirm button names what is added, such as **Add AS15169**.

With an IPInfo token on the **Settings > Integrations** tab, **Enrich with IPInfo** shows the address's hostname, organization and location from [ipinfo.io](https://ipinfo.io), and the hostname can be added to a set's domains.

The [DPI Detector](detector.md) opens the same dialog from its hosting and CDN results, with the network of the affected provider already chosen.

## ASN

The network icon next to a destination address looks up its network without opening the dialog. b4 finds the ASN that announces the address and fetches its prefixes into its ASN cache. Addresses of that network then carry a label such as **AS15169 Google LLC**, and the `asn:` filter finds them. Every browser sees the same labels, and the ASNs that sets target label their addresses too.

The cross on a label deletes that ASN from the cache. An ASN that a set still targets is not deleted: b4 names the sets that use it, and the label stays.

## Pausing and clearing

The round button in the bottom right corner pauses and resumes the stream. While it is paused, new packets are not collected. **Clear Packets**, the trash can in the control bar, removes the collected packets, groups and devices.

The number next to **Traffic** in the side menu counts the packets that matched a set. A click on **Traffic** in the menu, or clearing the packets, resets it, and it does not grow while the stream is paused.
