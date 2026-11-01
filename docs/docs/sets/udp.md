---
sidebar_position: 3
title: UDP
description: The UDP tab of the set editor, which decides the UDP and QUIC traffic a set matches and what b4 does with it.
---

The **UDP** tab decides which UDP traffic the set matches, QUIC above all, and what b4 does with it. Its most common use is not a bypass over UDP but refusing QUIC, so that the browser falls back to TCP, where the set's [TCP](./tcp/index.md) strategies apply.

## Blocking QUIC

| Field | What it does | Default |
| --- | --- | --- |
| **Block QUIC (force TCP)** | Matches every QUIC packet to the set's targets and answers it with ICMP port unreachable. It sets **QUIC Matching** to **By SNI or matched destination** and **Action Mode** to **Reject**. | Off |

QUIC carries HTTP/3 for YouTube, Google, Discord and other sites. Its packets are encrypted end to end, so b4 cannot desync the handshake the way it does with TLS over TCP: only decoys and a fragmented Initial packet are left, and both are weak. When a provider silently drops UDP to port 443, a video stalls without an error, and nothing tells the browser to stop trying QUIC. Browsers also remember that a host offered HTTP/3 and keep trying QUIC with it. A refusal from the router makes the browser switch to TCP at once.

On a set sent through an [Upstream SOCKS5 proxy](./routing.md#upstream-socks5-proxy) with **Route UDP through upstream** on, the switch refuses only the set's QUIC, and the rest of its UDP goes through the proxy. The other settings on this tab do not apply to UDP that goes through the proxy.

## What UDP Traffic to Match

| Field | What it does | Default |
| --- | --- | --- |
| **QUIC Matching** | **By SNI** matches QUIC whose ClientHello carries a server name the set targets, which is also how b4 learns the addresses of those domains. **By SNI or matched destination** also matches any QUIC packet to a destination the set already matches by IP or port, with no server name needed. | **By SNI** |
| **Port Filter** | UDP ports to match, such as `5000-6000,8000`, for VoIP, games and other UDP applications. It also limits the rest of the set: a set with `8443` here does not act on UDP port 443. Empty matches any port, except the ports that only a [set for calls](#protocol-selection) lists. | Empty |
| **Calls** | How the set treats calls, see the table below. | **Pass call setup** |
| **Which calls** | Shown with **Handle only calls**: the calls the set takes, see [Sets for calls](#protocol-selection). | **Discord app and browsers** |

| Calls | What happens |
| --- | --- |
| **Pass call setup** | STUN requests, which start calls in browsers and apps, and the Discord app's voice packets pass unchanged. The set's **Action Mode** still applies to the rest of a call, such as browser audio and video. |
| **Handle only calls** | The set takes no targets and acts only on calls, see [Sets for calls](#protocol-selection). A set with targets cannot choose it. |
| **Handle like other UDP** | STUN requests and the Discord app's voice packets get the set's action like the rest of its UDP. With **Drop** or **Reject**, the set blocks calls too. |

With **By SNI** and no domains or GeoSite categories on the [Targets](./targets.md) tab, no QUIC ClientHello can match, and the tab shows a warning. UDP to port 53 is DNS and goes to b4's [DNS handling](../dns.md) instead.

### Sets for calls {#protocol-selection}

With **Calls** on **Handle only calls**, the set applies its **Action Mode**, such as fakes, only to the packet that opens each call on the ports in its **Port Filter**, whatever the address. **Which calls** decides which calls it takes:

| Which calls | The set takes |
| --- | --- |
| **Discord app and browsers** | Calls in the Discord app, in browsers and in other apps that start calls with STUN. |
| **Discord app** | Calls and voice channels in the Discord app. |
| **Browsers** | Calls in browsers, Discord in a browser included, and in other apps that start calls with STUN. |

The rest of a call the set takes belongs to the set: its later packets pass untouched, or are blocked when the set blocks. Fakes go only before the packet that opens the call. Every other packet on those ports is left to the other sets, or passes untouched. A set with an empty UDP **Port Filter** leaves those ports alone. A call the set takes belongs to it even when another set matches its destination address, such as a GeoIP set that covers the same servers. The same holds when that address belongs to a domain another set handles.

A set for calls has three requirements. When it breaks one, b4 refuses to save it, and a warning on the tab names the cause.

- No targets: no domains, IPs, GeoSite or GeoIP categories and no ASNs. Source devices are allowed.
- A **Port Filter** on this tab with the ports the calls arrive on. While it is empty, the warning offers **Use Discord ports**, which enters `19294-19344,50000-50100`.
- An empty **Port Filter** on the [TCP](./tcp/general.md) tab. Without targets, a TCP port filter would make the set act on every TCP connection on those ports. The **TCP** tab shows a warning too.

While **Handle only calls** is chosen, the tab hides **Blocking QUIC** and **QUIC Matching**. With **Action Mode** on **Drop** or **Reject**, or with **Block (blackhole)** routing on the [DNS & Routing](./routing.md) tab, the set blocks the packet that opens each call, and a warning says so. The **IP Version Filter** on the [Targets](./targets.md) tab limits such a set as well.

Choosing **Handle only calls** changes **Fake Packet Payload** from **(zero fill)** or **(auto: QUIC Initial)** to **(auto: STUN request)**, and any other payload stays. On some networks zero-filled fakes cut Discord calls short. Leaving **Handle only calls** in the same edit restores the previous payload. When a set without targets leaves **Handle only calls** and keeps its UDP **Port Filter**, it acts on all UDP on those ports, and a hint on the tab says so. [Discord voice calls](../guides/discord-voice.md) sets up such a set step by step.

## How to Handle Matched Traffic

| Field | What it does | Default |
| --- | --- | --- |
| **Action Mode** | What b4 does with the UDP the set matched, see the table below. | **Fake & Fragment** |

| Mode | What happens |
| --- | --- |
| **Off** | Matched UDP passes unchanged. b4 still reads the QUIC ClientHello, so a set with domains keeps learning their addresses, for example for routing. |
| **Fake & Fragment** | Decoy packets go before the real one, and the Initial packet is fragmented. Weak against QUIC. |
| **Drop** | The packet is dropped without an answer, and for QUIC so is the rest of its connection. The client falls back to TCP once it stops waiting, usually after several seconds. |
| **Reject** | The packet is dropped, and for QUIC so is the rest of its connection. An ICMP port unreachable goes back to the client, which switches to TCP at once. The recommended way to refuse QUIC. |
| **Coalesce QUIC Initial** | Sends the handshake packet that carries the server name behind a padding packet in the same datagram. Inspection that reads only the first packet of a datagram no longer sees the name. The rest of the connection goes out untouched, and the handshake itself is unchanged. |

When the set blocks its traffic on the [DNS & Routing](./routing.md) tab, the block action applies instead of the action mode. In a set with IP block detection and **Cache Blocked IPs** on, QUIC to a [banned address](./tcp/general.md#banned-addresses) is refused whatever the action mode.

Whether the rest of a dropped QUIC connection is dropped too depends on the router. [System Info](../settings/system.md#system-info) shows it under **Kernel Capabilities** in the row **QUIC drop holds for the whole connection**. Where the row reads **unavailable**, only the first packets of the connection are dropped, and the row shows what the router lacks. [Dropped QUIC connections](../internals/capture.md#dropped-quic-connections) describes the cases.

Everything on this tab applies to IPv6 only while **Enable IPv6 Support** is on under [Settings > Core](../settings/core.md#protocols). With it off, b4 sees no IPv6 packets, and a destination that answers over IPv6 keeps working over QUIC there.

## Fake & Fragment settings

Shown in **Fake & Fragment** mode.

| Field | What it does | Default |
| --- | --- | --- |
| **Fake Packet Count** | How many fakes go before the real packet, 1 to 20. | `6` |
| **Fake Packet Size** | The payload size of each fake, 32 to 1500 bytes. | `64` |
| **Fake Packet Payload** | What the fakes carry: **(zero fill)**, **(auto: QUIC Initial)** for a fresh random QUIC Initial in each fake, **(auto: STUN request)** for a fresh STUN request in each fake, **QUIC preset 1**, **QUIC preset 2**, or a payload uploaded under [Settings > Payloads](../settings/payloads.md). Presets and uploaded payloads are cut or padded with zeros to **Fake Packet Size**, **(auto: STUN request)** is padded with random bytes, and 1200 bytes suits **(auto: QUIC Initial)**. | **(zero fill)** |
| **Evasion Technique** | How the fakes are made unusable to the server. UDP fakes always use the set's **Fake TTL** from [TCP > Faking](./tcp/faking.md): **None** sends them without extra corruption, **TTL** is the same as **None**, **Checksum** also breaks the UDP checksum. | **None** |
| **Segment 2 Delay** | Delay between the fakes and the real packet, 0 to 1000 ms. A range makes each connection pick a random value. | `0` |
