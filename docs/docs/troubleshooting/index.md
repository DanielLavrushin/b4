---
sidebar_position: 50
title: Troubleshooting
description: Where each problem is solved, and the checks for a site that does not open.
---

The table names the page where each problem is solved. A site that does not open is covered [on this page](#a-site-does-not-open).

| Symptom | Where it is solved |
| --- | --- |
| A site does not open, or stopped opening | [A site does not open](#a-site-does-not-open) |
| **Status** on the dashboard reads **Engine failed** | [Engine failed](./traffic.md#engine-failed) |
| **No packets reached b4 in the last N min**, or the **Traffic** page shows nothing while devices are in use | [b4 does not see traffic](./traffic.md) |
| Another message under **Needs attention** on the dashboard | [Needs attention](../dashboard.md#needs-attention) |
| Telegram does not connect, or media do not load | [Telegram problems](../telegram/troubleshooting.md) |
| The service or its container does not start, or a problem specific to OpenWrt, Keenetic or MikroTik | [Router platforms](./platforms.md) |
| None of the above, or b4 itself seems at fault | [Reporting a problem](./reporting.md) |

## A site does not open {#a-site-does-not-open}

The checks go from the quickest to the slowest. The first one that fails points to the cause.

### 1. b4 is running {#b4-is-running}

**Status** on the [dashboard](../dashboard.md) reads **Running** in normal operation. **Engine failed** means that the packet engine did not start: the web interface works, but traffic passes through the router untouched, and no set applies. The card under the status strip shows the error, and [Engine failed](./traffic.md#engine-failed) lists the fixes.

### 2. The site's connections reach a set {#reaches-a-set}

1. Open the site on a device behind the router.
2. Open **Traffic** and find the site's domain. The **Set** column shows the set it matched.

| The Traffic page shows | Cause and fix |
| --- | --- |
| The site's domains without a set | The set is off, or the site loads from domains that are not in it. The **+** next to a domain adds it to a set, see [Adding sites to a set](../guides/add-sites.md). |
| Another set | That set handles the site, and the strategy of the intended set does not apply to it. [Unblocking a site](../guides/unblock.md#when-it-does-not-work) shows how to write the strategy into the set that handles the site. |
| Nothing from the device | b4 does not see the device's traffic, see [b4 does not see traffic](./traffic.md). |

**Domains not in any set** on the dashboard's **Activity** panel opens the Traffic page with only the connections no set matched.

### 3. DNS {#dns}

b4 sees the DNS queries that devices send to port 53. A device with encrypted DNS, such as secure DNS in the browser or Android Private DNS, bypasses b4. The DNS options of the sets then do not apply to it: another resolver, pinned addresses, the [IPv4 fallback](../dns.md#the-ipv4-fallback). Turning encrypted DNS off on the device fixes it, see [What b4 sees](../dns.md#what-b4-intercepts).

`flags:dns` in the filter of the **Traffic** page shows the lookups of the sets' domains and what b4 did with each, see [Checking the result](../dns.md#reading-the-result). A device whose lookups never show there uses encrypted DNS.

When the provider's DNS answers the site with wrong addresses, a set can send its domains to another resolver, see [Sending a set's domains to another resolver](../dns.md#resolver-types). The [DPI Detector](../detector.md) shows whether DNS answers are forged. A device keeps an answer it already has until the answer expires.

### 4. IPv6 {#ipv6}

While **Enable IPv6 Support** is off, b4 handles IPv4 only, and a connection over IPv6 skips every set. When the router has IPv6, the dashboard shows **IPv6 traffic skips all sets** under **Needs attention**. **Force IPv4 for matched domains** removes the IPv6 addresses of the sets' domains from DNS answers. A device with encrypted DNS, or with an address it already has, still connects over IPv6. **Enable IPv6 Support** under [Settings > Core > Packet Engine](../settings/core.md#protocols) brings IPv6 into the sets. In TUN mode, IPv6 always skips b4.

### 5. QUIC {#quic}

YouTube, Google, Discord and other sites load over QUIC (HTTP/3), which runs over UDP. On the **Traffic** page, such connections show `UDP` next to the site's domain. b4 has only weak means against QUIC. **Block QUIC (force TCP)** on the set's [UDP](../sets/udp.md#blocking-quic) tab refuses QUIC, and the browser switches to TCP at once, where the set's strategy applies.

### 6. The strategy still works {#strategy}

A site that opened with its set and stopped later, while the **Traffic** page still shows that set, points to a strategy that no longer works. **Find a strategy** in the set card's menu, or on the set's **Discovery** tab, opens [Discovery](../discovery.md) with the set picked. [Unblocking a site](../guides/unblock.md) walks through a run. [Keeping sets working](../guides/keep-working.md) lets the watchdog search for a new strategy and write it into the set on its own.

### 7. The address is not blocked {#address-blocked}

Some blocks no strategy passes. Discovery marks them on the site's card:

- **address blocked**: connections to every address of the site fail.
- **intercepted by the gateway**: the first hop in front of b4's host answers the connection itself. When that hop is another router, b4 has to run on it, or b4's host has to be excluded from its redirect.

Such a site needs another way out: [Routing sites through a VPN](../guides/vpn.md) or [Sending sites through a SOCKS5 proxy](../guides/proxy.md). When the gateway intercepts and b4 already runs on the main router, only a proxy helps. The [DPI Detector](../detector.md) shows how the ISP blocks a site.

When every check passes and the site still does not open, [Reporting a problem](./reporting.md) lists what to collect for a bug report.
