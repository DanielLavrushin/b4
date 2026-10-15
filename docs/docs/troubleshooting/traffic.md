---
sidebar_position: 50
title: b4 does not see traffic
description: Why the traffic of the devices does not reach b4, and how to fix it.
---

When the devices' traffic does not reach b4, no set applies, and sites behave as if b4 were not installed. The signs:

- **Status** on the dashboard reads **Engine failed**.
- **Needs attention** on the dashboard shows **No packets reached b4 in the last N min**.
- While devices are in use, the **Traffic** page shows nothing, or only the router itself.

## Engine failed {#engine-failed}

The packet engine did not start. The web interface works, but traffic passes through the router untouched. The card under the status strip shows the error, see [If the packet engine did not start](../dashboard.md#packet-engine-not-running). b4 retries the start three times by itself.

| Cause | Fix |
| --- | --- |
| The kernel lacks the modules NFQUEUE needs. **Kernel Modules** and **Kernel Capabilities** in [System Info](../settings/system.md#system-info) name the missing modules and the packages that provide them. | Installing the modules, see [OpenWrt](../install/openwrt.md#kernel-modules) and [Keenetic](../install/keenetic.md#netfilter-components), or **Switch to TUN** on the card. TUN needs no queue modules. |
| TUN mode started while the WAN or the VPN was down, and found no default route. | One of the automatic retries starts the engine when the link comes up within about two minutes. |
| **TUN device name** under **Settings > Core > Packet Engine** is the name of the uplink or of another existing interface. | A name that no interface uses, such as the default `b4tun0`. |

Once the cause is fixed, **Restart b4** on the card starts the engine. On MikroTik, the error and its fix are described under [Router platforms](./platforms.md#mikrotik-nfqueue).

## No packets reached b4 {#no-packets}

**No packets reached b4 in the last N min** means that the engine runs, but no packet reaches it.

| Cause | Fix |
| --- | --- |
| **Skip IPTables/NFTables Setup** is on, and the **Firewall** cell on the dashboard reads **Set up externally**. In NFQUEUE mode b4 then installs no capture rules. | The rules are added by hand, or the switch is turned off under [Settings > Core > Firewall](../settings/core.md#firewall-rules). |
| The router's firmware removed b4's rules, and **Firewall Monitor Interval (seconds)** is `0`. In NFQUEUE mode b4 then does not put them back. | A non-zero interval, `10` by default. b4 restores missing rules at every check. |
| On MikroTik, RouterOS does not send the traffic to the container. | [Traffic does not go through the container](./platforms.md#mikrotik-traffic-not-redirected) lists the checks. |

## The Traffic page shows nothing, or only the router {#traffic-page-empty}

The **Traffic** page shows the router's own connections as **Router**, or under the router's own address. When nothing else shows while devices are in use, b4 does not see their traffic.

### Capture Interfaces match nothing {#capture-interfaces}

**Capture Interfaces** under **Settings > Core > Packet Engine** limits b4 to traffic that leaves the router through the selected interfaces. While the selection matches nothing, b4 inspects nothing, and no set applies. The **Packet Engine** card then names the interface the traffic leaves by, in a warning that starts with **Nothing B4 has seen has left by**. Clearing the selection fixes it: an empty selection covers every interface and is the usual choice. See [Capture Interfaces](../settings/core.md#network-interfaces).

### A VPN client or XrayUI moved the traffic {#another-service}

A VPN client, XrayUI or another service that moves the default route changes the interface traffic leaves by, without touching b4's settings. A **Capture Interfaces** selection made before that stops matching. When Xray routes the whole network through its tunnel, for example, a selected uplink switches b4 off for every device. The selection stays empty in such setups, see [Using b4 with Xray or XrayUI](../guides/xray.md#choosing-which-service-routes). [Which interface is which](../guides/interfaces.md#finding-which-one-is-wrong) helps find the setting at fault.

### TUN captures only the router {#tun-router-only}

For the traffic the router forwards, TUN mode needs the `iptable_raw` and `xt_CT` kernel modules. On a kernel without them, such as the Synology DSM kernel, b4 captures only the connections the router makes itself. The devices' traffic then gets no bypass strategies, and b4 does not see their DNS queries. Routing and blocking sets still apply to it. **System Info** shows **This device only** in the **Captured Traffic** row under **Engine**. Where the kernel has the modules NFQUEUE needs, NFQUEUE mode captures the devices' traffic. [Packet capture](../internals/capture.md#tun-forwarded) explains the reason.

## Flow offloading bypasses b4 {#flow-offloading}

Flow offloading moves an established connection to a fast path that skips the firewall hooks b4 works from. When the **Flow Offloading** row of [System Info](../settings/system.md#system-info) ends with **(bypasses b4)**, the router moves connections to that path before b4 has seen the packets it needs. Turning offloading off fixes it. On OpenWrt, offloading can also start after the packets b4 needs, see [Router platforms](./platforms.md#flow-offloading).

## Some devices get no set {#some-devices}

- **Device filtering.** While **Enable Device Filtering** is on under **Settings > Core > Devices**, only the selected devices get DPI bypass. With **Exclude Selected Devices (Blacklist)** on, every device except them gets it. A device left out gets no DPI bypass, and the dashboard does not count its connections. DPI bypass matches devices by MAC address, so a phone with a randomized MAC address counts as a new device. [Applying b4 to selected devices](../guides/devices.md#when-it-does-not-work) lists the other cases.
- **A VPN on the device.** Traffic that a device sends through its own VPN client leaves in a tunnel the router does not see into. It is outside b4's reach.

When none of these explains the problem, [Reporting a problem](./reporting.md) lists what to collect.
