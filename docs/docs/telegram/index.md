---
sidebar_position: 14
title: Telegram
slug: /mtproto
description: The three ways b4 carries Telegram traffic, and how to choose between them.
---

b4 carries Telegram traffic in three ways. They are independent and can run at the same time.

- **[MTProto proxy](./mtproto-proxy.md)**: a proxy server on b4 that Telegram connects to with a secret. Every Telegram client supports it, from anywhere its port is reachable, such as a phone on mobile data.
- **[Telegram over WebSocket](./websocket-bridge.md)**: one switch that relays Telegram from every device behind b4 and from the router, with nothing to set up on the devices.
- **[Telegram Desktop WEB proxy](./web-proxy.md)**: the MTProto proxy served as an ordinary HTTPS site, for Telegram Desktop 7.1.1 and later.

## Choosing a way

| | MTProto proxy | Telegram over WebSocket | WEB proxy |
| --- | --- | --- | --- |
| Set up in **Settings > Telegram** | **MTProto Proxy** card | **Telegram over WebSocket** card, or a set's routing mode | **Telegram Desktop WEB proxy** card |
| On the device | A proxy entry with a secret | Nothing | A proxy entry from a link |
| Devices covered | Any Telegram client, inside the network or outside it | Every device behind b4, and the router itself | Telegram Desktop 7.1.1 and later |
| Needs the MTProto proxy on | Yes | No | Yes |
| Needs a public hostname | Only for devices outside the local network | No | Yes, with a publicly trusted certificate |
| Needs TPROXY in the kernel | No | Yes | No |

On the home network, Telegram over WebSocket needs nothing on the devices. A phone away from home needs the MTProto proxy. The WEB proxy suits b4 on a VPS with a hostname and a certificate of its own.

## Shared settings

All three reach Telegram through the shared [Telegram upstream](./upstream.md) settings. A [Cloudflare Worker](./cloudflare-worker.md) is the route to add when media does not load, and a [DC Relay](./dc-relay.md) helps where the WebSocket routes are blocked as well.

[Telegram problems](./troubleshooting.md) lists symptoms and their fixes, and [Settings > Telegram](../settings/mtproto.md) describes every field.
