---
sidebar_position: 2
title: Telegram over WebSocket
description: Relaying Telegram from every device behind b4 with one switch and nothing to set up on the devices.
---

Telegram over WebSocket relays Telegram from every device behind b4, and from the router itself, over Telegram's WebSocket edge, with Cloudflare routes behind it. Nothing is set up in Telegram on the devices, no VPS is involved, and the [MTProto proxy](./mtproto-proxy.md) does not need to be on.

## Requirements

- TPROXY support in the kernel: the `tproxy` and `socket` modules. On OpenWrt they come with `kmod-nft-tproxy` and `kmod-nft-socket`, and [Routing requirements](../sets/routing.md#requirements) lists the packages for other systems.
- b4 manages the firewall: **Skip IPTables/NFTables Setup** is off in [Settings > Core > Firewall](../settings/core.md#firewall).
- For Telegram over IPv6, **Enable IPv6 Support** is on in [Settings > Core > Packet Engine](../settings/core.md#protocols). Without it, Telegram over IPv6 takes the normal path.

No set and no GeoIP or GeoSite file is needed. The bridge has not been verified with the TUN engine.

## Setup

1. Open **Settings > Telegram**.
2. On the **Telegram over WebSocket** card, turn on **Enable Telegram over WebSocket**. The card shows **Save to apply**.
3. Click **Save Changes**. The switch takes effect without a restart.

![The Telegram over WebSocket card](/img/telegram/20260925200001.png)

The bridge then covers every device behind b4 and the router itself. [Device filtering](../settings/core.md#device-filtering) applies: with an allow list, the selected devices and the router are bridged; with a deny list, the selected devices keep the normal path. A set bridges only some devices or interfaces instead, see [below](#limiting-the-bridge-to-some-devices-or-interfaces).

## Checking the result

- The card's header reads **Working**. **Not working** names the missing part when the pointer rests on it.
- **Relayed sessions** in the **Bridge status** box grows while Telegram is in use.
- On the [Traffic](../connections.md) page, bridged connections appear under the set name **Telegram bridge**, with the **Telegram bridge** label. No set of that name exists in the set list.
- The **Telegram** panel of the [dashboard](../dashboard.md#telegram) shows the relayed sessions and failures.

The contents of the card are described under [Settings > Telegram](../settings/mtproto.md#telegram-over-websocket).

## When it does not work

- **A warning on the card** names the cause. Each one is explained, with its fix, under [Telegram problems](./troubleshooting.md#the-telegram-over-websocket-card-shows-a-warning).
- **Media, stickers or reactions do not load.** Telegram's own edge serves only some data centres, and a [Cloudflare Worker](./cloudflare-worker.md) covers the others.
- **Voice calls** go over UDP and are not bridged, and neither is QUIC. [Some Telegram traffic does not go through the bridge](./troubleshooting.md#some-telegram-traffic-does-not-go-through-the-bridge) lists every case.
- **A [block set](../sets/blocking.md) covers Telegram addresses.** Unless it is limited to source interfaces or an included list of source devices, it also blocks the bridge's own connections, see [A block set stops the bridge](./troubleshooting.md#a-block-set-stops-the-bridge).

## Limiting the bridge to some devices or interfaces

The routing mode **Telegram over WebSocket (built-in)** feeds the same bridge from one set, limited to the set's devices or interfaces. While the switch is on, such a set is redundant, and the card says so.

1. Open **Sets** and create a set.
2. On the **Targets** tab, add the `telegram` GeoIP category. The GeoIP database has to be configured, otherwise the set is rejected on save.
3. On the **DNS & Routing** tab, turn on **Enable Routing** and set **Routing mode** to **Telegram over WebSocket (built-in)**.
4. Select **Source Interfaces** on the same tab, or [source devices](../sets/targets.md#source-devices) on the **Targets** tab. With both empty, the set covers every device.
5. Click **Create Set**.

:::warning Source scoping leaves the router out
A set limited to source interfaces or devices does not bridge the router's own connections, Telegram Desktop on the router included. Device filtering has no such effect.
:::

Connections such a set carries appear on the Traffic page under the set's own name, with the same **Telegram bridge** label. Only the GeoIP category steers Telegram into the bridge: a GeoSite category adds nothing for it.

## How it works

The bridge takes TCP connections to Telegram's address ranges into its listener on port 13443, reads the MTProto session and relays it through the [Telegram upstream](./upstream.md). For bridged sessions the transport mode is always **Auto (WebSocket → TCP)**, and the [DC Relay](./dc-relay.md) is not used.

### Address list

The ranges always include a list built into b4. On top of it, b4 uses the first source that works: the list Telegram publishes at `core.telegram.org`, the same file from the b4 mirrors, the last downloaded copy, or the `telegram` category of the GeoIP file. The list is refreshed once a day, and **Refresh addresses** downloads it at once. **Telegram ranges** on the card shows the source in use.

### Which data centre a session goes to

Telegram Desktop names its data centre at the start of a connection. The Android app does not, so b4 takes the data centre from the address the connection was sent to. When Telegram answers that such a session belongs to another data centre, b4 sends later sessions for that address to the other data centre at the same site.

### DPI processing

Connections from devices reach the bridge untouched: no faking, fragmentation or desync applies to them. The bridge's own connections to Telegram, and those of the MTProto proxy, depend on the route:

| Route | DPI processing |
| --- | --- |
| Telegram's WebSocket edge, direct TCP to a data centre | An ordinary set whose targets match applies its DPI settings |
| Cloudflare-proxied domains, a custom WebSocket domain | None. No set applies its DPI settings |
| A Cloudflare Worker | None, unless **Let sets process Worker connections** is on |

A set that covers Cloudflare for other sites therefore leaves these routes alone, apart from the Worker while that switch is on. A set in the **Telegram over WebSocket (built-in)** mode, unless it is limited by an included source-device list, leaves the bridge's connections to its targets unmodified.

### Connections the bridge passes on

Every TCP connection to a Telegram range enters the bridge, plain HTTPS to `web.telegram.org` or `t.me` included. A connection that is not MTProto, or that b4 cannot map to a data centre, goes on unchanged to its original address, through a Cloudflare Worker when one is set and the port is 443, or directly. The card counts these as **Passed on undecoded**.

### Order among sets

The switch comes before every routing set that is not limited to devices or interfaces, so a catch-all set that sends everything through a VPN does not take Telegram away from the bridge. A set limited to devices or source interfaces still handles its own devices first, which keeps one device's Telegram on another route.
