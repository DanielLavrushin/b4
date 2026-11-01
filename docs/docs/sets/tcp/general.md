---
sidebar_position: 1
title: General
description: The General sub-tab of a set's TCP tab, with the segment delay, port filter, packet duplication, IP block detection, RST injection protection and MSS clamping.
---

The **General** sub-tab of the [TCP](./index.md) tab holds the settings that apply whatever splitting or faking method the set uses.

![The top of the General sub-tab](/img/general/20260418235044.png)

## Delay, ports and headers

| Field | What it does | Default |
| --- | --- | --- |
| **Segment 2 Delay** | Delay between the pieces b4 sends, in milliseconds. A range makes each connection pick a random value from it. | `0` |
| **Port Filter** | The TCP ports the set acts on, such as `443` or `80,5222,8000-9000`, and a list without `443` leaves HTTPS out. Empty means every port b4 captures: 443 and the ports any set lists. | Empty |
| **Drop SACK Options** | Removes the Selective Acknowledgment option from TCP headers. Some DPI uses it to put fragments back in order. | Off |
| **Empty line before HTTP method** | For plain HTTP on port 80: puts an empty line before the request and shortens the User-Agent by two characters to keep the length. Inspection that expects the method at the start of the request no longer finds the Host header. Needs a User-Agent header, and suits nginx servers. | Off |

## Packet Duplication {#packet-duplication}

For providers that slow traffic down by dropping some outgoing packets to certain address ranges, such as Telegram's. b4 drops the original packet and sends several copies instead.

![The Packet Duplication section](/img/general/20260418235123.png)

| Field | What it does | Default |
| --- | --- | --- |
| **Enable Packet Duplication** | Sends each packet of the set's TCP connections as several copies. The duplicated packets get none of the set's other bypass techniques. | Off |
| **Copy Count** | How many copies to send, 1 to 10. | `3` |

The [Traffic](../../connections.md) page labels duplicated packets `dup`.

## IP Block Detection

Some server addresses are blocked at the IP level: nothing reaches the server, so no bypass strategy helps. A CDN often answers DNS with several addresses of which only some are blocked, and the site then works or hangs depending on the address a device picked. The detection finds such addresses, and the [Traffic](../../connections.md) page labels their connections `ip`. The other fields appear once it is on.

| Field | What it does | Default |
| --- | --- | --- |
| **Enable IP Block Detection** | Detects destinations that never answer and acts on them. | Off |
| **Heal DNS answers** | Also removes unreachable addresses from DNS answers. When every address is unreachable, the last one that worked for the domain is answered instead. | Off |
| **Detect unanswered SYNs** | Catches blocks at the address level, where the connection never opens. | On |
| **SYN Threshold** | How many unanswered connection attempts before the router tests the address itself. Nothing happens until that test fails too. | `3` |
| **Retransmit Threshold** | How many unanswered copies of the ClientHello, the first one included, mark the destination blocked. This covers a block that lets the connection open and then swallows the handshake. A low value can mistake a slow server for a block. | `3` |
| **Detection Timeout** | How long, in milliseconds, before a repeated and still unanswered SYN or ClientHello counts as reaching its threshold. With **Cache Blocked IPs** on, a server that sends nothing back for this long after the ClientHello gets its address checked for a [ban](#banned-addresses). | `3000` |
| **Healed Answer TTL** | The TTL written into healed DNS answers, in seconds. A short value lets devices pick up a recovered address quickly. | `60` |
| **Cache Blocked IPs** | Remembers blocked addresses, so new connections to them are reset at once instead of waiting. An address blocked for every site name is [banned](#banned-addresses). | On |

How long an address stays marked is set by **Retest interval (seconds)** under [Settings > Core](../../settings/core.md#ip-block-detection).

### Banned addresses {#banned-addresses}

Some providers block an address for every site name: the connection opens, but no site on that address answers. In a set with **Cache Blocked IPs** on, b4 tests whether an address answers for other site names when:

- IP block detection marks the address blocked (see **Retransmit Threshold** and **Detection Timeout**);
- the server sends nothing back for the **Detection Timeout** after a device's ClientHello.

An address that answers none of those names is banned. A connection still waiting on it is then reset. An address that answers them, even with a refusal, is not banned. An address that IP block detection marked blocked stays in the cache of blocked addresses all the same. Until the **Retest interval (seconds)** runs out, new connections to it are reset, whatever the site.

On some routers b4 cannot see whether a server has answered while the connection is open. There it tests a silent server only after the device gives up on the connection, see [Kernel requirements](../../internals/capture.md#kernel-requirements).

While an address is banned:

- New connections to the address and port are reset at once in every set with IP block detection and **Cache Blocked IPs** on. QUIC to that address and port is refused. A device can then move on to the site's next address, if the site has one.
- A set that routes through a proxy, or through a TUN, TAP or WireGuard interface that is up, leaves its connections to the address alone. A connection that another service sends into a tunnel is left alone too, see [Packets that leave by another route](../../internals/capture.md#tunnel-egress). A set routed to another interface, such as a second uplink, still resets or refuses its connections.
- DNS answers for the domains of every set leave it out. When every address of an answer is banned, b4 answers with an address that worked for the domain before. When it knows none, the answer passes unchanged.
- The [Traffic](../../connections.md) page labels the connections b4 resets or refuses `ban`.

A ban lasts 24 hours by default. An address banned again within 30 days after its previous ban ended is banned for 7 days. b4 tests a banned address again at every **Retest interval (seconds)** and lifts the ban early after three successful tests in a row. Bans stay across restarts and set changes.

The [Banned addresses](../../dashboard.md#banned-addresses) panel on the dashboard lists the bans, and **Unban** lifts one at once. The switch and the ban lengths are on the **IP Block Detection** card under [Settings > Core](../../settings/core.md#ip-block-detection).

When the set's [Escalation](../escalation.md) trigger fires first, the host moves to the backup set, and this set does not check or ban its address. A backup set with IP block detection and **Cache Blocked IPs** on can still ban it.

## RST Injection Protection

| Field | What it does | Default |
| --- | --- | --- |
| **Enable RST Protection** | Drops connection resets that look injected by DPI rather than sent by the server. | Off |
| **TTL Tolerance** | The largest allowed difference between the TTL of a reset and the TTL of the server's replies. | `3` |

[RST Injection Protection](./rst-protection.md) describes the checks.

## MSS Clamping {#mss-clamping}

Limits the TCP maximum segment size (MSS) for the set's connections. A smaller MSS makes the client send its ClientHello in several segments, which some DPI cannot put back together.

![The MSS Clamping section](/img/general/20260815225810.png)

| Field | What it does | Default |
| --- | --- | --- |
| **Enable per-set MSS** | Turns the clamp on for the set. Available once the set has an IP, GeoIP, ASN or source device target. | Off |
| **MSS size (bytes)** | The MSS, 10 to 1460. `88` is filled in when the switch is turned on. | `88` |

The MSS is set on the first packet of a connection, before the server name is known. Only IP, GeoIP, ASN and source device targets therefore decide where it applies. Domains, GeoSite categories and the TLS version filter play no part.

| The set targets | The clamp applies to |
| --- | --- |
| IP addresses, GeoIP categories or ASNs | Connections to those addresses |
| Source devices only | Every port 443 connection from those devices, wherever it goes |
| Both | Connections from those devices to those addresses |

A set whose IP, GeoIP and ASN targets resolve to no addresses yet installs no clamp until they do. A device added by hand needs a fixed address to scope a clamp.

`88` is the value suggested for smart TVs on YouTube. Every segment then carries at most 88 bytes, so large downloads are slow, and when a connection works but crawls, a larger value is the next step. To slow down one device's HTTPS without a set, the [per-device MSS](../../settings/core.md#device-table) does the same. A per-set MSS overrides the [per-device and global MSS](../../settings/core.md#global-mss-clamping) for the connections it covers.
