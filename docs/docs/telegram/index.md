---
sidebar_position: 13
title: Telegram
slug: /mtproto
---

# Telegram

b4 carries Telegram traffic three ways. They are independent, they can run at the same time, and the one that fits depends on what can be changed on the client.

- **[MTProto proxy](./mtproto-proxy.md)** - a proxy server clients connect to with a secret entered in Telegram's own proxy settings. Every Telegram client supports it, and it reaches b4 from anywhere its port is open, which is what makes it the option for a phone on a cellular network.
- **[Telegram over WebSocket](./websocket-bridge.md)** - a per-set routing mode. b4 intercepts Telegram sessions from devices behind it and relays them, so nothing is configured on the device.
- **[Telegram Desktop WEB proxy](./web-proxy.md)** - the same stream served over ordinary HTTPS on a hostname of its own. Telegram Desktop 7.1.1 and later loads that hostname in a hidden WebView, so the client opens no MTProto socket at all.

| | MTProto proxy | WebSocket bridge | WEB proxy |
| --- | --- | --- | --- |
| Configured in | Settings, MTProto Proxy | A set's routing mode | Settings, MTProto Proxy |
| On the client | A proxy entry with a secret | Nothing | A proxy entry from a link |
| Clients covered | Any Telegram client | Every device behind b4 | Telegram Desktop 7.1.1 and later |
| Needs the proxy server enabled | Yes | No | Yes |
| Needs a public hostname | Only for clients outside the LAN | No | Yes, with a publicly trusted certificate |

All three reach Telegram's data centres through the same [Telegram upstream](./upstream.md) settings, which apply even when the proxy server itself is off. Two upstream routes have enough setup of their own to sit on separate pages: the [Cloudflare Worker relay](./cloudflare-worker.md), and the [DC Relay](./dc-relay.md) for a network where the WebSocket transport is blocked as well.

Symptoms and the log lines that identify them are collected in [Troubleshooting](./troubleshooting.md).

:::info
The per-setting reference for the MTProto tab, including the fields none of these pages walk through, is [Settings, MTProto](../settings/mtproto.md).
:::
