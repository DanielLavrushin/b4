---
sidebar_position: 50
title: Unblocking a site
description: Find a bypass strategy for a blocked site with Discovery, apply it as a set, and check that it works.
---

[Discovery](../discovery.md) searches for a bypass strategy that makes a blocked site load and turns it into a [set](../sets/index.md). When no strategy helps, the site can go through a VPN or a proxy instead.

## Requirements

- b4 is running: **Status** on the [dashboard](../dashboard.md) reads **Running**.
- The devices' traffic reaches b4: the dashboard's **Activity** chart shows connections while they are in use.
- The address of the site, or of a page on it that does not open.

## Setup

1. Open **Discovery**.
2. Enter the site in **Site or URL**, for example `youtube.com`, and press Enter. Sites of one service, such as `youtube.com` and `googlevideo.com`, can go in together.
3. Click **Start**. A run takes from one to ten minutes.
4. On a card with a strategy, click **Apply as a set**.
5. Choose **Create a new set**, check the name under **Set name**, and click **Create set**.

The new set is enabled at once. The defaults under **Options** are the usual choice for a first run; the fields are described on the [Discovery](../discovery.md#options) page.

## Checking the result

1. Open the site on a device behind the router.
2. Open the dashboard. The **Sets** panel lists the new set with the connections it matched in the last hour.
3. Open **Traffic**. The site's connections show the new set in the **Set** column.

When several strategies worked, **Use instead** under **N other strategies worked**, or in the Discovery [history](../discovery.md#history), tries another one. Its dialog offers to write the strategy into the set that already handles the site, so no second set is needed.

## When it does not work

The card's badge says why no strategy was found:

| Badge | Cause and fix |
| --- | --- |
| **address blocked** | Connections to every address of the site fail, and no bypass strategy can help. The site needs a [VPN](./vpn.md) or a [proxy](./proxy.md). |
| **intercepted by the gateway** | The first hop in front of b4's host answers the connection itself. When that hop is another router, b4 has to run on it, or b4's host has to be excluded from its redirect. When b4 runs on the main router, only a proxy helps. |
| **does not resolve** | The name has no address, as with a misspelled domain. |
| **link answers 400** | The link answers HTTP 400 even without b4. The link of a page or a file the site serves is needed. |
| **nothing found** | A new run with **TLS version for the probes** set to **TLS 1.2**, or with a captured ClientHello, sometimes helps. Otherwise a [VPN](./vpn.md) or a [proxy](./proxy.md) is left. |

The [DPI Detector](../detector.md) shows how the ISP blocks a site: by resetting connections, dropping them, forging DNS answers or cutting long transfers. It checks many sites at once, and **Fix N with Discovery** opens Discovery with the sites still blocked filled in.

When the set exists but the site still does not open:

- **Traffic** shows another set in the **Set** column: that set handles the site, and the new strategy has no effect there. Applying the result with **Write the strategy into an existing set** and that set puts the strategy where it applies.
- The browser can reach the site over QUIC (HTTP/3), which Discovery does not test. **Block QUIC (force TCP)** on the set's [UDP](../sets/udp.md) tab makes the browser use TCP.
- The browser can connect over another IP version than the run, for example over IPv6 while the run tested IPv4. A new run with **IP version for the probes** set to the browser's version tests the site the way the browser reaches it.
- The site opened before and stopped later: [Keeping sets working](./keep-working.md) shows how the watchdog searches for a new strategy on its own.

The full list of checks, from the quickest to the slowest, is under [A site does not open](../troubleshooting/index.md#a-site-does-not-open).
