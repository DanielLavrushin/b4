---
sidebar_position: 12
title: Traffic
---

The Traffic page shows the packets b4 takes from the queue and inspects: the protocol, the domain from the TLS ClientHello or QUIC, the destination, the source device and the set that matched. The data arrives in real time over its own WebSocket stream and does not depend on the log level.

b4 does not receive every packet of a connection, only the first ones, as many as the **TCP per-connection packet limit** and **UDP per-connection packet limit** in the [queue settings](./settings/core#queue-and-packet-processing) allow. One connection therefore produces several entries in a row, and the packet counts on this page say nothing about download speed or volume. DNS queries b4 intercepts are part of the stream too, each with the decision b4 made about it.

The page opens in **Aggregated** mode. The choice between **Aggregated** and **Raw feed** in the top right corner is remembered by the browser.

![Traffic, Aggregated mode](/img/connections/20260923214001.png)

## Aggregated

Packets are grouped by device, protocol and domain; a packet without a domain is grouped by its destination address instead. Groups build up while the page is open: on opening they receive the packets already held in the web interface buffer, then the new ones. Leaving the page, switching to the raw feed and clearing reset them. At most 2000 groups and 512 devices are kept, and the ones idle the longest are dropped first.

| Column | Description |
| --- | --- |
| **Protocol** | `TCP` or `UDP` |
| **Domain** | The domain from the handshake or "(no domain)", the TLS version (`1.2`, `1.3`) and the [labels](#labels) for special cases. A domain outside every set gets a **+** button, see [Adding domains to sets](#adding-domains-to-sets) |
| **Destination** | The latest destination address, a `+N` label when there were several, and the [ASN](#asn) |
| **Set** | The set matched by domain, and the set matched by IP when it is a different one |
| **Source** | The device, see [Devices](#devices) |
| **Activity** | Packets per second over the last minute |
| **Packets** | Number of packets in the group |
| **Seen** | Time since the latest packet |

Clicking a column header, except **Activity**, sorts the list; further clicks reverse the order and then remove the sort. Unsorted, the group with the most recent packet comes first.

Clicking a row opens the **Detail** panel: activity, packet count, the duration from the first packet to the last, the source, every destination address of the group (up to 24) and the matched sets. For a domain outside every set it also has an **Add domain to a set** button.

### Control bar

| Control | Effect |
| --- | --- |
| Filter field | See [Filter](#filter) |
| **30s**, **1m**, **5m**, **15m**, **All** | Show the groups that had packets within the period, **1m** by default. The period is counted back from the latest packet received, not from the computer clock |
| **Unmatched only** | Keep only the groups no set matched |
| **Domains only** / **All packets** | See [Domains only and all packets](#domains-only-and-all-packets) |
| Trash can | Clear the packets, see [Pausing and clearing](#pausing-and-clearing) |

### Devices

The **Devices** panel on the left lists the traffic sources seen within the period, each with the time since its latest packet, its activity over the last minute and its packet count. Clicking a device limits the list to its groups, **All devices** removes the selection. The button in the panel header collapses it; on a narrow screen it starts collapsed.

A device is identified by its MAC address. When the MAC is unknown, for example for a device behind another router, the source is grouped by its IP address. Aliases from the [device filtering](./settings/core#device-filtering) table are always shown. With device filtering or vendor lookup turned on there, the names of discovered devices are used as well, and the router's own addresses are labelled **Router**.

## Raw feed

**Raw feed** shows packets one by one, the latest 1000. New ones are added at the bottom; while the table is scrolled to the end it stays on the latest entries.

![Traffic, Raw feed mode](/img/connections/20260923214002.png)

| Column | Description |
| --- | --- |
| **Time** | Time of the packet (HH:MM:SS) |
| **Protocol** | `TCP` or `UDP` and the [labels](#labels) |
| **Set** | The set matched by IP, or by domain when no set matched by IP |
| **Domain** | The TLS version and the domain. A domain outside every set gets a **+** button |
| **Source** | The device name, or the source IP address and port |
| **Destination** | The destination IP address and port, the [ASN](#asn), and a **+** button for an address outside every set |

Clicking a column header sorts the table. The **Sorted by …** chip next to the filter shows the current sort, and its cross removes it. While a filter is active, the number of matching packets is shown there too.

## Labels

Special cases are marked with labels: next to the domain in **Aggregated** mode, next to the protocol in the raw feed and in the detail panel. Hovering a label explains it.

| Label | Meaning |
| --- | --- |
| `dup` | The packet was duplicated, see [Packet duplication](./sets/tcp/general#packet-duplication) |
| `ip` | The destination was found blocked by IP. An outlined label means the address was already in the cache of blocked addresses |
| `block` | The packet was blocked by a [blocking set](./sets/blocking) |
| `proxy` | The connection came through b4's SOCKS5 proxy |
| Telegram icon | A connection of b4's MTProto proxy, with the secret's name |
| Telegram icon with **Telegram bridge** | A connection relayed by the [Telegram over WebSocket](./telegram/websocket-bridge.md) bridge |
| `doh`, `forward`, `pin`, `sinkhole` and others | The decision about a DNS query, see [DNS](./dns#reading-the-result) |

:::info TLS version
The **1.2** and **1.3** labels next to a domain are the TLS version. Providers may block TLS 1.2 and TLS 1.3 by different methods, so each may need its own bypass strategy. A set can be limited to one TLS version on its [Targets](./sets/targets) tab.
:::

## Domains only and all packets

The switch in the control bar applies to both modes and is remembered by the browser.

| Mode | What is shown |
| --- | --- |
| **Domains only** | Packets with a recognised domain. The default |
| **All packets** | Every inspected packet, including those without a domain |

:::tip
**All packets** adds the packets in which b4 found no domain: the TCP handshake before the ClientHello, connections without SNI, traffic to addresses with no name.
:::

## Filter

The filter field is shared by both modes and is kept in the browser.

- `+` joins conditions, and all of them must hold.
- `!` before a condition excludes what it matches.
- `field:value` checks a single field; several conditions on the same field hold when any one of them matches. An empty value, such as `set:`, matches an empty field.
- A word without a field is looked up in every field.

Matching ignores case and looks for a substring. Fields available in both modes: `protocol`, `domain`, `destination`, `tls`, `flags`, `asn`, `device` (also `alias`). `set` exists only in **Aggregated** mode, `source` only in the raw feed.

| Filter | Shows |
| --- | --- |
| `protocol:udp` | UDP packets |
| `domain:youtube` | Packets with "youtube" in the domain |
| `protocol:tcp+!domain:google.com` | TCP packets, except google.com domains |
| `protocol:udp+domain:discord` | UDP packets to Discord domains |
| `asn:AS13335` | Destinations in AS13335 (Cloudflare) whose ASN has been looked up |
| `device:iPhone` | Packets from the device named iPhone |
| `flags:dup` | Packets with the `dup` label |
| `set:` | Groups without a set |

## Adding domains to sets

The **+** button next to a domain opens the add dialog.

1. **Domain pattern** - the domain and its parent domains, from the most specific to the broadest. For `rr1---sn-ab5l6ne7.googlevideo.com` these are `rr1---sn-ab5l6ne7.googlevideo.com` and `googlevideo.com`. A domain in a set matches the name itself and all its subdomains, so a parent domain covers every subdomain.
2. **Set** - one of the enabled sets, or **Create New Set** with the name entered.
3. **Add Domain**.

:::tip
Services that serve content from many subdomains (YouTube, CDNs) are covered by the parent domain, such as `googlevideo.com`. Otherwise every subdomain has to be added separately.
:::

## Adding IP/CIDR to sets

The **+** button next to a destination address (and, in the raw feed, a click on the address itself) opens the add IP dialog.

### Address details

| Button | Source | What it shows |
| --- | --- | --- |
| **Load Network Info** | RIPE NCC | The ASN and the prefixes it announces |
| **Enrich with IPInfo** | ipinfo.io | Organization, hostname, location |

:::info IPInfo
The IPInfo button appears once an API token is set on the **Settings → Integrations** tab. A free token is available from [ipinfo.io](https://ipinfo.io).
:::

### What to add

**When an ASN is found**, the options are:

- **Add 142.250.74.110 only** - that single address
- **Add all N prefixes** - every address range of that ASN

**Without an ASN** - the address with a mask of choice:

| Mask | Description |
| --- | --- |
| `/32` | A single IP address |
| `/24` | ~256 addresses - a local subnet |
| `/16` | ~65K addresses - a network block |
| `/8` | ~16M addresses - a class A |

For IPv6 the masks are `/128`, `/64`, `/48` and `/32`. The set is chosen the same way as for a domain.

## ASN

The network icon next to a destination address looks up its ASN without opening the dialog: through IPInfo when a token is set, otherwise through RIPE NCC. Addresses of that ASN then carry a label such as **AS15169 Google LLC** (just the number without IPInfo), and the `asn:` filter starts finding them. ASN data is stored in b4 and shared by every browser; the cross on the label removes the ASN.

## Pausing and clearing

The round button in the bottom right corner, or the **P** key, stops and resumes the stream. While paused, new packets are not collected and the page frame is highlighted.

The trash can in the control bar (**Clear Packets**) removes the collected packets, groups and devices.

| Key | Action |
| --- | --- |
| **P** or **Pause** | Pause and resume the stream |
| **Ctrl+X** or **Delete** | Clear the packets |

## Menu counter

The **Traffic** item in the side menu shows the number of packets that matched a set since the counter was last reset (`999+` above 999). Clicking the **Traffic** item and clearing the packets reset it. It does not grow while the stream is paused.
