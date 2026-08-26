---
sidebar_position: 2
title: Telegram over WebSocket
---

# Telegram over WebSocket

A per-set routing mode. When a device behind b4 opens a connection to a Telegram data centre, b4 intercepts it, reads the session and relays it over Telegram's WebSocket edge, with the Cloudflare routes behind it. Nothing is configured inside Telegram on the device, and no VPS is involved.

The mode runs on its own. The MTProto proxy under Settings, MTProto does not have to be enabled: the bridge is built at start-up either way.

## Requirements

The bridge rides the TPROXY path, which needs the `tproxy` and `socket` kernel modules. Without them b4 logs that transparent redirect is unavailable and the mode silently steers nothing. On OpenWrt the packages are `kmod-nft-tproxy` and `kmod-nft-socket`; the same requirement is described under [Routing](../sets/routing.md).

Matching Telegram by geo category also needs the GeoSite and GeoIP databases to be configured. A set that names a category while the corresponding database path is empty is rejected when the configuration is saved.

## Setting it up

1. Create a set and give it the `telegram` **GeoIP** category. Addresses are what the interception rule matches, so the GeoIP half is the part that does the steering. Adding the `telegram` GeoSite category as well brings in addresses learned from DNS answers b4 observes, which does nothing for a device that resolves over DoH or DoT.
2. On the set's **DNS & Routing** tab, enable routing and set **Routing mode** to *Telegram over WebSocket (built-in)*.
3. Choose the **source interfaces** whose devices should be bridged, or leave the list empty.

```json
{
  "name": "telegram-ws",
  "targets": {
    "geosite_categories": ["telegram"],
    "geoip_categories": ["telegram"]
  },
  "enabled": true,
  "routing": { "enabled": true, "mode": "mtproto-ws" }
}
```

:::warning Source scoping does more than narrow the LAN side
The rules that send the router's own Telegram traffic into the bridge are installed only while the set is not scoped to source interfaces or devices. Selecting a source interface therefore excludes the router itself, not just the devices on other interfaces.
:::

## What the bridge uses upstream

The [Telegram upstream](./upstream.md) settings apply, with two overrides the bridge makes for every session it carries:

- The transport mode is forced to **Auto**, whatever the dropdown says.
- The **DC Relay** is cleared. A relay configured for the proxy server is not used here, so a network where the WebSocket transport is blocked cannot be rescued by a relay in this mode.

The bridge also keeps no warm WebSocket pool, only a Worker pool, so every bridged session dials Telegram cold inside the same budget the proxy server has warm spares for. A first connection through the bridge is slower to come up than the same connection through the proxy server.

Configure a [Cloudflare Worker domain](./cloudflare-worker.md) if media fails to load: the data centres Telegram's own edge does not serve are reached through the Cloudflare routes.

## Boundaries

Only TCP MTProto sessions are bridged.

- A connection b4 cannot decode or cannot map to a data centre is offered to the configured Cloudflare Worker first, and only then dialled directly.
- Voice calls are not diverted at all. UDP handling is disabled for a set in this mode and no UDP listener is started, so calls take the ordinary path.
- No QUIC rejection rule is installed, so a client that prefers QUIC to a matched address bypasses the bridge without a log line saying so.
- The interception rule carries no destination-port filter. Every TCP connection to a matched Telegram address enters the bridge listener, including plain HTTPS to `web.telegram.org` or `t.me`, which is read, recognised as not MTProto and relayed on.

:::info DPI bypass is off for a bridged set
A set routed through TPROXY has its packet manipulation disabled: matched packets are accepted unmodified, so faking, fragmentation and desync do not apply to them. The bridge replaces the bypass rather than adding to it.
:::

A connection that reaches the listener and then sends nothing occupies the listener for the **Bridge handshake wait**, 180 seconds by default. Setting that field to `-1` means waiting indefinitely, not disabling the wait.
