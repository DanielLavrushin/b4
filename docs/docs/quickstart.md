---
sidebar_position: 3
title: First steps
description: From opening the web interface to a first set that opens a blocked site.
---

This tutorial goes from opening the web interface to a set that opens a blocked site. It starts once b4 is [installed](install/index.md) and running.

## Opening the web interface {#open-the-web-interface}

1. Open `http://<address>:7000` in a browser. `<address>` is the address of the router or computer b4 runs on, for example `http://192.168.1.1:7000`. On that computer itself, `http://localhost:7000` works too.
2. When a username and password were set during installation, sign in with them.

When HTTPS was turned on during installation or under **Settings > System > Web Server**, the address starts with `https://`. A browser warns about a self-signed certificate, and the warning can be accepted.

The dashboard opens. With no sets yet, the **Sets** panel reads **No sets yet: b4 changes nothing until a set targets sites**. The [Dashboard](dashboard.md) page describes every panel.

![The dashboard on a first launch, with no sets](/img/quickstart/20261003220200.png)

## Community sets (optional) {#community-sets}

Sets that other b4 users published can be tested before b4's own strategies. Discovery does this while the Community Hub is on.

1. Open **Settings > Integrations** and turn on **Enable Community Hub** in the **Community Hub** card.
2. Click **Save Changes**.

**Community** then appears in the side menu, and Discovery shows **Try community sets first**, on by default: it tests up to three published sets for each site before the built-in strategies. With the hub on, b4 syncs the catalogue from `hub.b4core.app`. [What leaves the router](community/index.md#what-leaves-the-router) lists what the hub receives.

## Finding a strategy {#run-discovery}

1. Click **Find a strategy with Discovery** on the **Sets** panel, or open **Discovery** in the side menu.
2. Type the address of a site that does not open into **Site or URL** and press Enter. Several sites can go in at once, separated by commas.
3. Click **Start**.

![Two sites added to Discovery](/img/quickstart/20260905160500.png)

b4 tries its strategies on the sites. A run takes from one to ten minutes, depending on the provider. When it ends, every site gets a verdict:

| Verdict | Meaning and next step |
| --- | --- |
| **confirmed 3 of 3**, **not confirmed** | A strategy made the site load. The next section applies it |
| **no bypass needed** | The site loads without b4. There is nothing to apply |
| **address blocked** | The provider blocks the site's addresses, and no strategy can help. The site needs a set that sends it through a [VPN](guides/vpn.md) or a [proxy](guides/proxy.md) |
| **intercepted by the gateway** | A device in front of b4 answers for the site. b4 has to run on that device. When b4 runs on the router itself, only a proxy set helps |
| **does not resolve** | The name has no address, as with a typo |
| **nothing found** | No strategy loaded the site. [Discovery](discovery.md#results) describes what to try next |

![The results of a run](/img/quickstart/20260905160700.png)

## Applying the result {#apply-the-configuration}

1. Click **Apply as a set** on the card of a site with a strategy found.
2. Check the rows under **The set will**. They describe what the set does.
3. Keep the suggested **Set name** or type another one.
4. Click **Create set**.

## Checking the result {#verify-it-works}

1. Open the site in a browser on a device in the network.
2. Open **Traffic** in the side menu. The **Set** column shows the name of the new set next to the site's domain.

![The Traffic page, with set names in the Set column](/img/connections/20260923214001.png)

The **Sets** panel of the dashboard counts the connections the set matched as well. When the site still does not open, [Unblocking a site](guides/unblock.md) lists the next steps.

## Next steps {#whats-next}

- [Adding sites to a set](guides/add-sites.md): more domains, or whole GeoSite categories.
- [Keeping sets working](guides/keep-working.md): the watchdog and escalation.
- [Securing the web interface](settings/security.md): it has no password until one is set.
- [Key concepts](concepts.md): sets, targets, strategies and the set kinds.
