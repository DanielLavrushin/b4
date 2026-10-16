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

## What UDP Traffic to Match

| Field | What it does | Default |
| --- | --- | --- |
| **QUIC Matching** | **By SNI** matches QUIC whose ClientHello carries a server name the set targets, which is also how b4 learns the addresses of those domains. **By SNI or matched destination** also matches any QUIC packet to a destination the set already matches by IP or port, with no server name needed. | **By SNI** |
| **Port Filter** | UDP ports to match, such as `5000-6000,8000`, for VoIP, games and other UDP applications. It also limits the rest of the set: a set with `8443` here does not act on UDP port 443. Empty matches any port. | Empty |
| **Filter STUN Packets** | Lets STUN and voice media through untouched. Voice and video calls (Discord, Telegram, WhatsApp) use STUN, and interfering with it breaks them. | On |
| **Connection Packets Limit** | How many packets at the start of a UDP flow b4 processes. It cannot exceed the global limit under [Settings > Core](../settings/core.md#queue-and-packet-processing). | `8` |

With **By SNI** and no domains or GeoSite categories on the [Targets](./targets.md) tab, no QUIC ClientHello can match, and the tab shows a warning. UDP to port 53 is DNS and goes to b4's [DNS handling](../dns.md) instead.

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

When the set blocks its traffic on the [DNS & Routing](./routing.md) tab, the block action applies instead of the action mode.

Whether the rest of a dropped QUIC connection is dropped too depends on the router. [System Info](../settings/system.md#system-info) shows it under **Kernel Capabilities** in the row **QUIC drop holds for the whole connection**. Where the row reads **unavailable**, only the first packets of the connection are dropped, and the row shows what the router lacks. [Dropped QUIC connections](../internals/capture.md#dropped-quic-connections) describes the cases.

Everything on this tab applies to IPv6 only while **Enable IPv6 Support** is on under [Settings > Core](../settings/core.md#protocols). With it off, b4 sees no IPv6 packets, and a destination that answers over IPv6 keeps working over QUIC there.

## Fake & Fragment settings

Shown in **Fake & Fragment** mode.

| Field | What it does | Default |
| --- | --- | --- |
| **Fake Packet Count** | How many fakes go before the real packet, 1 to 20. | `6` |
| **Fake Packet Size** | The payload size of each fake, 32 to 1500 bytes. | `64` |
| **Fake Packet Payload** | What the fakes carry: **(zero fill)**, **(auto: QUIC Initial)** for a fresh random QUIC Initial in each fake, **QUIC preset 1**, **QUIC preset 2**, or a payload uploaded under [Settings > Payloads](../settings/payloads.md). The payload is cut or padded with zeros to **Fake Packet Size**, and 1200 bytes suits **(auto: QUIC Initial)**. | **(zero fill)** |
| **Evasion Technique** | How the fakes are made unusable to the server. **None** sends them as they are, **TTL** lets them expire on the way, **Checksum** breaks the UDP checksum. | **None** |
| **Segment 2 Delay** | Delay between the fakes and the real packet, 0 to 1000 ms. A range makes each connection pick a random value. | `0` |
