---
sidebar_position: 50
title: Real TCP window rewriting
description: Which packets the Real TCP window (wssize) option of a set rewrites, where the rewrite stops, how long b4 follows a connection, and where the option does not apply.
---

[Real TCP window (wssize)](../sets/tcp/general.md#wssize) writes a small TCP window into the first packets of a connection and can lower the Window Scale option of its SYN. b4 follows each connection it rewrites from the SYN to the point where the rewrite stops. Which set a connection gets at its SYN is described in [How sets match traffic](./matching.md#wssize).

At the **Trace** log level, the [Logs](../logs.md) page shows `WSSize: opened for ...` when b4 starts to follow a connection, with the window and the Window Scale before and after, and `WSSize: ended for ...` with the reason when a packet or the 5-second limit stops the rewrite. No `ended` line follows when the connection goes idle or the configuration is saved.

## What is rewritten

| Packet of the connection | What changes |
| --- | --- |
| The SYN and its retransmissions | The window field, when **Window** is not `0`. The Window Scale option, when **Window Scale** is not `0` and the option holds a larger value |
| Packets without data before the stop | The window field |
| Packets with data up to the stop, the last one included | The window field |
| Packets after the stop | Nothing |
| Packets with the RST flag | Nothing |

A SYN without a Window Scale option stays without one. A packet in which nothing has to change is left as it is. b4 changes the packet before the set's other techniques split it or build fake packets from it. Packets from the server are not looked at.

## Where the rewrite stops

With **After the ClientHello**, b4 follows the bytes the device sends, counted from the SYN, however they are split into TCP segments and TLS records. Segments that arrive out of order or twice are handled. The packet that completes the ClientHello is rewritten too, even when it carries data after the ClientHello. A part of the ClientHello sent again is still rewritten until 5 seconds have passed since the connection's first data byte.

With **After the first data packet**, the rewrite stops after the first packet with data that is not a SYN.

Data that is not a TLS ClientHello, such as an HTTP request, stops the rewrite with its first packet in both modes. Data inside a Fast Open SYN is followed as the start of the ClientHello and starts the 5 seconds, but it is not the first data packet.

| Limit with **After the ClientHello** | Value | When it is reached |
| --- | --- | --- |
| Time from the first data byte | 5 seconds | A later packet passes unchanged, and the rewrite is over |
| Size of the ClientHello | 64 KB | The packet is still rewritten, then the rewrite stops |
| Packets with data | 128 | The same |
| Buffers of all followed connections together | 4 MB | The same, and b4 logs a warning |

## How long a connection is followed

| Connection | b4 stops following it |
| --- | --- |
| Only SYNs so far | 35 seconds after the last SYN |
| Handshake done, no data yet | After 2 minutes without a packet. Until then every packet b4 sees is rewritten |
| The rewrite stopped | At once. After a complete ClientHello, 5 seconds after the connection's first data byte |
| The device sends a FIN | At once |
| The configuration is saved | At once, for every connection |

b4 follows at most 20,000 connections at a time. A new connection beyond that passes unchanged, and b4 logs a warning at most once in 10 seconds. Neither a reset nor a packet from the server makes b4 stop following a connection.

## Where it does not apply

- With **TUN interface** as the packet engine. The set keeps the option, and b4 logs a warning. A Discovery run then tests the set without the rewrite as well.
- To packets that leave through a tunnel or by another service's route, see [Packets that leave by another route](./capture.md#tunnel-egress).
- To packets of a connection that a routing set hands to a proxy or to another interface.
- To ports b4 does not capture and to interfaces outside **Capture Interfaces**.
- To IPv4 fragments and to IPv6 packets with extension headers.
- To a connection whose SYN b4 did not see, or for whose SYN no set matched.

b4 sees only the first packets of a connection, 19 by default (**TCP Connection Packets Limit**). The SYN and the packets up to the stop have to be among them, and a device that retransmits many times before it sends data can use them up.

## A lowered Window Scale lasts

TCP fixes the Window Scale in the SYN. The device keeps writing its windows for its own scale, the server reads them with the lowered one, and b4 does not rescale later packets. With a scale of 7 lowered to 1, the server sees every later window as 1/64 of what the device meant, until the connection closes. **Window** alone has no such effect: packets after the stop carry the device's own window.
