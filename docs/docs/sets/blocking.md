---
sidebar_position: 4.5
title: Blocking ads and sites
description: Blocking ads, trackers or any site for every device on the network with a block set.
---

A block set stops the traffic it matches instead of sending it anywhere. It blocks ad and tracker domains, unwanted sites or addresses for every device on the network and for the router itself, and it needs no interface or proxy.

## Requirements

- The devices resolve names through the router. A device with encrypted DNS of its own is still blocked by the server name in its connections, see [When it does not work](#when-it-does-not-work).
- For GeoSite categories such as `category-ads-all`, the GeoSite database loaded under [Settings > Geodat Settings](../settings/geodata.md).
- For IPv6, **Enable IPv6 Support** turned on under [Settings > Core](../settings/core.md#protocols). Without it, blocking covers IPv4 only, see [IPv6](#ipv6).

## Setup

1. Open **Sets** and click **Create Set**.
2. On the **Targets** tab, add what to block: domains or `regexp:` patterns, IP addresses and CIDR ranges, GeoSite categories such as `category-ads-all`, or GeoIP categories. [Adding sites to a set](../guides/add-sites.md) lists the ways.
3. On the **DNS & Routing** tab, open **Traffic Routing** and turn on **Enable Routing**.
4. Set **Routing mode** to **Block (blackhole)**.
5. Pick the **Block action**, see [Block options](#block-options). **Reject (fail fast)** is the usual choice for ads.
6. Click **Create Set**. Changes apply without a restart.

### Blocking on some interfaces only {#source-interfaces}

**Source Interfaces** in the same **Traffic Routing** section limit the set to traffic that arrives on the selected interfaces, such as the bridge of a guest network. Devices on other interfaces and the router's own connections are then not blocked, and the other sets handle their traffic. [Which interface is which](../guides/interfaces.md) explains the interface names.

### Ready-made ad blocker

A set with a broad list of ad and tracker domains and the `category-ads-all` category, already in block mode. To use it, create a set, open its **Import/Export** tab, paste the JSON below, click **Import** and save the set. The category needs the GeoSite database.

```json
{"b4_version":"1.65.0","name":"adblock","fragmentation":{"strategy":"none"},"faking":{"sni":false},"targets":{"sni_domains":["ad.mail.ru","advertronic.io","vu.okcdn.ru","adkernel.com","adfox.ru","nr-data.net","rubiconproject.com","adition.com","mc.yandex.com","log.strm.yandex.ru","analytics.yahoo.com","criteo.com","taboola.com","ad4m.at","1rx.io","hbx.media.net","media.net","adform.net","analyticsengine.s3.amazonaws.com","analytics.s3.amazonaws.com","ad.doubleclick.net","analytics.google.com","api.bugsnag.com","app.bugsnag.com","browser.sentry-cdn.com","app.getsentry.com","ads-api.twitter.com","log.byteoversea.com","log.fc.yahoo.com","adtech.yahooinc.com","appmetrica.yandex.ru","metrika.yandex.ru","fingerprintjs.com","px.srvcs.tumblr.com","device-metrics-us.amazon.com","cdn.cookielaw.org","consent.cookiebot.com","sdk.privacy-center.org","cdn.privacy-mgmt.com","api.impact.com","cdn.dynamicyield.com","widget.intercom.io","bnc.lt","bingads.microsoft.com","ads.microsoft.com","snap.licdn.com","ct.pinterest.com","geolocation.onetrust.com","consent.trustarc.com","app.usercentrics.eu","advertising-api-eu.amazon.com","fls-na.amazon.com","advertising.yandex.ru","ironsource.mobi","is.com","pangleglobal.com","posthog.com","o0.ingest.sentry.io","lr-ingest.com","mineralt.io","pixel.quora.com","qevents.quora.com","ads.vk.com","advertising.apple.com","ads.huawei.com","ngfts.lge.com","xp.apple.com","cmp.osano.com","consentcdn.cookiebot.com","zenaps.com","clientstream.launchdarkly.com","match.adsrvr.org","smartyads.com","adcolony.com","mouseflow.com","edge.fullstory.com","stats.wp.com","adsrvr.org","c.bing.com","app-measurement.com","cdn.segment.com","quantcast.com","rudderstack.com","snowplowanalytics.com","adjust.com","singular.net","wzrkt.com","ads.pinterest.com","ads.x.com","ads-sg.tiktok.com","tracking.rus.miui.com","cookiebot.com"],"ip":["5.255.255.77/32"],"geosite_categories":["category-ads-all"]},"enabled":true,"routing":{"enabled":true,"mode":"block"}}
```

## Block options {#block-options}

The **Block action** decides what a device sees. **Reject (fail fast)** is the default.

| Option | What the device sees |
| --- | --- |
| **Reject (fail fast)** | The connection fails at once: a TCP reset, or ICMP unreachable for UDP and QUIC. A page gives up on an ad slot right away. |
| **Drop (silent blackhole)** | Nothing comes back: the connection times out, and a lookup of a blocked name gets no answer. |

b4 blocks a site in three ways:

1. **DNS.** A lookup of a blocked domain gets the answer that the name does not exist (NXDOMAIN), or no answer with **Drop (silent blackhole)**. This covers every device whose DNS goes through the router.
2. **Server name.** A device that already has an address, from encrypted DNS or a cache, is still stopped: b4 reads the server name in the TLS or QUIC handshake and fails the connection.
3. **Address.** IP addresses, CIDR ranges and GeoIP categories are blocked in the firewall.

The first two work by name, so other sites on the same servers as a blocked host keep working. Blocking by address on a shared CDN blocks every site behind that address.

## Checking the result

- On a device, the blocked site or ad does not load, and the rest of the page does.
- The dashboard shows the [Blocked](../dashboard.md#blocked) panel while a block set is enabled: **Domains** lists what was blocked, **Devices** the devices that tried to reach it. Connections that the firewall drops once a blocked site's addresses are known are not counted there. The set's row in the **Sets** panel shows how many DNS lookups it blocked.
- On the [Traffic](../connections.md) page, blocked packets carry the `block` label, and answers the set replaced carry the `sinkhole` DNS label.

Ad-blocking test pages show the result in a browser. With the browser's own ad blocker turned off, they show what the block set catches:

- [superadblocktest.com](https://superadblocktest.com/)
- [adblock.turtlecute.org](https://adblock.turtlecute.org/)
- [getblockify.com](https://getblockify.com/adblock-test)
- [obfusgated.com](https://obfusgated.com/tools/ad-block-test)
- [paileactivist.github.io](https://paileactivist.github.io/toolz/adblock.html)
- [adblocktester.pages.dev](https://adblocktester.pages.dev/)

A block set stops lookups and connections and does not change pages, so test items that check hidden page elements pass only with a browser extension.

## When it does not work

**A device still opens a blocked site.** The device may use encrypted DNS of its own (secure DNS in the browser, Android Private DNS) or a cached address. b4 still stops it by the server name, but not over IPv6 while IPv6 support is off, see [IPv6](#ipv6). Turning encrypted DNS off on the device, or IPv6 support on in b4, closes the gap. Over QUIC, the block holds for the whole connection only while the row **QUIC drop holds for the whole connection** under **Kernel Capabilities** in [System Info](../settings/system.md#system-info) reads **available**. Where it reads **unavailable**, the row names what the router lacks.

**A device with a VPN of its own is not blocked.** Traffic inside a tunnel the router cannot see is out of b4's reach.

**A GeoSite category blocks nothing.** The GeoSite database is not loaded, or does not contain the category.

**Too much is blocked.** A `regexp:` pattern can catch far more than intended, and so can an address on a shared CDN. A domain target keeps the block narrow.

### IPv6 {#ipv6}

Block rules are written only for the address families b4 handles. **Enable IPv6 Support** is off by default, and a block set then writes no IPv6 rules:

- DNS still hides the name from devices that resolve through the router, for A and AAAA lookups alike. A lookup that reaches the router over IPv6 itself passes unchanged.
- The server name of IPv6 connections is not checked.
- Address targets are blocked for IPv4 only.

A device that already holds an IPv6 address of a blocked host therefore reaches it over IPv6. With **Enable IPv6 Support** on under [Settings > Core](../settings/core.md#protocols) and the service restarted, blocking covers both families. This is not the same as turning IPv6 off for the network, which is a router setting outside b4.
