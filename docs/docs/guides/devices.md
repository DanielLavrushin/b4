---
sidebar_position: 50
title: Applying b4 to selected devices
description: Give DPI bypass only to some devices, exclude devices from it, or limit one set to some devices.
---

By default b4 handles the traffic of every device on the network. Device filtering under **Settings > Core > Devices** decides which devices get DPI bypass at all. The **Source Devices** of a set limit that one set to some devices.

## Requirements

- The devices are on the router's network, where b4 finds them in the ARP table.
- A device behind another router has a fixed or reserved IP address, since it can be added only by that address.

## Choosing the devices that get DPI bypass

1. Open **Settings > Core > Devices**.
2. Turn on **Enable Device Filtering**.
3. Under **Available Devices**, select the devices. A device missing from the list is added under **Manual Devices** by its IP address.
4. To exclude the selected devices instead of limiting DPI bypass to them, turn on **Exclude Selected Devices (Blacklist)**.
5. Click **Save Changes**, then **Restart Service**. The selection applies after a restart.

What the filter covers:

- DPI bypass matches devices by MAC address. A device added by hand does not count for it, and a selection made only of such devices leaves DPI bypass on for every device.
- Routing sets and [Telegram over WebSocket](../telegram/websocket-bridge.md) use the same selection for traffic from the network. There a device added by hand is matched by its IP address.
- The router itself is not a device. Its own connections, those of the [SOCKS5 proxy](../settings/core.md#socks5-proxy) included, get DPI bypass in both modes.
- While devices are selected, sets do not write their own [DSCP value](../sets/routing.md#dscp-modes).

The fields are described under [Devices](../settings/core.md#device-filtering).

## Applying one set to some devices

1. Open the set, then **Targets > Source Devices**.
2. Select the devices.
3. To apply the set to every device except the selected ones, turn on **All devices except selected**.
4. Save the set.

A set limited to source devices takes priority over a set without that limit. Source devices narrow whom a set applies to, but they do not select traffic. A routing set still needs a domain, IP or ASN target, and **Match any IP address** under **Bypass IPs** routes everything from those devices. See [Source devices](../sets/targets.md#source-devices).

## Checking the result

1. Open **Traffic**.
2. In the **Devices** panel, select a device.
3. Its connections show the set that matched each of them.

Devices that device filtering leaves out get no DPI bypass, and the dashboard does not count their connections.

## When it does not work

- **Nothing changed after saving.** The device filter applies after a restart of b4.
- **A device added by hand gets no DPI bypass.** DPI bypass matches MAC addresses only. Such a device can still be the source device of a set.
- **A device added by hand is not matched.** Its IP address changed, or a router in between replaces the source address of its traffic. The first needs a fixed or reserved address; the second cannot be matched at all.
- **A phone stops being matched.** Many phones use a randomized MAC address, shown as **Private**, and a new address is a new device for b4. Turning off the private address for this network on the phone keeps its MAC address fixed.
