---
sidebar_position: 1
title: Targets
description: The Targets tab of the set editor, which decides the domains, addresses, networks and devices a set applies to.
---

The **Targets** tab decides which traffic the set applies to. It has four sub-tabs: **Bypass Domains**, **Bypass IPs**, **ASN** and **Source Devices**.

[Adding sites to a set](../guides/add-sites.md) shows the ways to fill this tab, including from the Traffic page and the dashboard. [How sets match traffic](../internals/matching.md) describes how b4 chooses between sets that target the same site.

![The Bypass Domains sub-tab with a GeoSite category](/img/targets/20260418234818.png)

## Domains {#domains}

The **Bypass Domains** sub-tab holds domain names and GeoSite categories, and the filters that apply to the whole set.

| Field | What it does | Default |
| --- | --- | --- |
| **Match any domain** | Adds `regexp:.*`, which matches every domain. A domain that another set lists is still handled by that set, so this set takes only what no other set claims. For routing, it covers only the addresses this set has resolved, not every address. | Off |
| **Domain-only matching** | Matches the domains only by the server name in the handshake, never by their IP address. Useful on shared CDN addresses, where a neighbouring domain on the same IP would otherwise get this set's strategy. A routing set with this on routes only the addresses listed explicitly. | Off |
| **TLS Version Filter** | **Any**, **TLS 1.2** or **TLS 1.3**, compared with the highest version the client offers. For a match by domain it is a preference: among the sets that list the domain, one whose filter fits comes first. For a match by address, a set whose filter does not fit does not take the connection. | **Any** |
| **IP Version Filter** | **Any**, **IPv4** or **IPv6**, a preference for matches by domain and by address alike: among the matching sets, one whose filter fits comes first. Shown while both IPv4 and IPv6 support are on under [Settings > Core](../settings/core.md#protocols). An ASN target then loads only the prefixes of the chosen family. | **Any** |
| **Manual Bypass Domains** | Domain names, added with Enter. A domain covers its subdomains, so a leading `*.` is removed. Several domains can be pasted at once, separated by commas or new lines. A `regexp:` entry takes a regular expression. | Empty |
| **Bypass GeoSite Categories** | Ready-made lists of a service's domains from the GeoSite database, such as `youtube` or `discord`. Needs the database loaded under [Settings > Geodat Settings](../settings/geodata.md). A click on a category shows its domains. | Empty |

**Edit List** opens all manual entries as text, one per line. When a domain is already in another set, a warning names that set.

## IP addresses {#ip-addresses}

The **Bypass IPs** sub-tab holds addresses and GeoIP categories.

![The Bypass IPs sub-tab](/img/targets/20261101084918.png)

| Field | What it does | Default |
| --- | --- | --- |
| **Match any IP address** | Adds `0.0.0.0/0`, and `::/0` while IPv6 support is on, which match every address. For DPI bypass, a narrower range in another set is handled by that set, so this set takes only what no other set claims. For routing, a narrower range in another routing set gets no such priority, so the two are best not combined. | Off |
| **IP priority** | A connection to an address the set lists, by IP, GeoIP category or ASN, stays with this set even when a set lower in the list matches its domain. Shown while **TLS Version Filter** on **Bypass Domains** is **Any**. | Off |
| **Manual Bypass IPs** | Addresses and CIDR ranges, such as `192.168.1.100` or `10.0.0.0/8`, entered the same way as domains. | Empty |
| **Bypass GeoIP Categories** | Address ranges from the GeoIP database, by country or by network. Needs the database loaded under **Settings > Geodat Settings**. | Empty |

**IP priority** suits servers that carry only this set's sites, such as the Google Global Cache nodes in a provider's own network. Shared CDN ranges and large networks carry many sites, and the switch is best left off there. With the Google network `AS15169` as a target, for example, the set would also keep `youtube.com` and every other Google service from the sets below it. Sets that rank above this one still take connections by name. A set limited to source devices ranks above every set without them. Among sets of the same kind, the one higher in the list ranks first. **IP priority** does not apply to a connection that matches the set only through **Match any IP address**. [IP priority](../internals/matching.md#ip-priority) describes the exact rules.

## ASN {#asn}

An ASN (autonomous system number) identifies the network of one operator: `AS15169` is Google, `AS13335` is Cloudflare. An ASN target makes the set match every address range, or prefix, the network announces. b4 fetches the prefixes from RIPEstat, refreshes them daily and keeps the last good copy when a fetch fails.

| Field | What it does |
| --- | --- |
| **Add ASN or IP address** | Takes a number (`15169`), the number with its prefix (`AS15169`) or an IP address. For an address, b4 lists the networks that announce it, and a button such as **Add AS15169** adds one. |
| ASN rows | Each ASN in the set with its holder, the number of IPv4 and IPv6 prefixes, the approximate number of IPv4 addresses and the time of the last update. **Refresh from RIPEstat** fetches the prefixes again, **Show prefixes** lists them, and **Remove from the set** removes the ASN. |

When a lookup fails, the error is shown, and **Add anyway, resolve later** still adds the ASN. b4 retries in the background. Until a fetch succeeds, the ASN adds no addresses to the set, and the set's card shows **N ASN unresolved**. Private and reserved numbers are refused, since no public network announces them.

For routing, where two sets of the same kind cover the same addresses, the set higher in the list wins, and sets limited to selected devices or source interfaces come before the others. A set with ASN targets and no domains cannot be kept working by the [watchdog](../watchdog.md), which identifies a set's sites by their names. [How sets match traffic](../internals/matching.md#asn-targets) describes where the prefixes come from and how they are refreshed.

:::warning Large and shared networks
A large network expands into thousands of prefixes, which costs memory on a small router. The tab warns above 2,000 prefixes or 16,777,216 IPv4 addresses. A cloud, hosting or CDN network such as AWS, Google Cloud, Cloudflare, Akamai or Hetzner carries many unrelated services. Its ASN also catches every other site hosted there, public DNS resolvers such as `8.8.8.8` and `1.1.1.1` included. A domain target, or a narrower set placed above, keeps the set to the service meant.
:::

## Source devices {#source-devices}

The **Source Devices** sub-tab limits the set to traffic from selected devices. [Applying b4 to selected devices](../guides/devices.md) shows the setup.

![The Source Devices sub-tab with two devices selected, one of them offline](/img/targets/20261023183500.png)

| Field | What it does | Default |
| --- | --- | --- |
| Device table | The devices with their MAC address, IP address and name. **offline** marks a device not on the network now. The set applies to the traffic of the selected ones. | None selected |
| **All devices except selected** | Applies the set to every device except the selected ones. | Off |

With no device selected, the set applies to all traffic. A set limited to devices takes priority over a set without devices that targets the same site.

A device that has left the network for good is removed from this set by clearing its checkbox, or from every set by [forgetting it](../settings/core.md#device-table) under **Settings > Core > Devices**.

Devices found in the router's ARP table are matched by their MAC address. A device added by hand has no MAC address on the network, so it is matched by the IP address entered for it. Such a device needs a fixed or reserved address, and it cannot be matched when a router between it and b4 replaces its source address. Without a working ARP table, the tab says so, and filtering by source device does not work.

:::warning Source devices do not select traffic on their own
Source devices decide whose traffic the set applies to, not which traffic. A routing set whose only target is a source device routes nothing. To route everything from a device, **Match any IP address** is turned on in the **Bypass IPs** sub-tab. Traffic the router itself sends never comes from a source device, so such a set leaves it alone.
:::
