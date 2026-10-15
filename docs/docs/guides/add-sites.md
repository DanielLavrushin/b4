---
sidebar_position: 50
title: Adding sites to a set
description: Adding domains, GeoSite categories, addresses, GeoIP categories and networks to a set, from the set editor, the Traffic page or the dashboard.
---

A set acts only on the traffic its targets name. Sites are added on the set's **Targets** tab, or with the **+** buttons on the **Traffic** page and the dashboard, which add one domain or address at a time.

## Requirements

- A set to add the sites to. The **+** buttons can create one as well.
- For GeoSite and GeoIP categories, the databases loaded under [Settings > Geodat Settings](../settings/geodata.md).

## Choosing what to add

| Target | Use it for | Sub-tab |
| --- | --- | --- |
| Domain | A site reached by name, which is most sites. A domain covers its subdomains, so a service that serves content from many subdomains is covered by its parent domain, such as `googlevideo.com` for YouTube. | **Bypass Domains** |
| GeoSite category | A ready-made list of a service's domains, such as `youtube` or `discord`. | **Bypass Domains** |
| IP address or range | Connections made straight to an address, without a name, such as those of Telegram apps, voice calls or games. | **Bypass IPs** |
| GeoIP category | The address ranges of a country or a network, from the GeoIP database. | **Bypass IPs** |
| ASN | Every address range a network announces, kept up to date by b4. A cloud, hosting or CDN network also carries many unrelated sites. | **ASN** |

## Adding on the Targets tab

1. Open **Sets** and click the set's card.
2. On the **Targets** tab, open **Bypass Domains**, type a domain into **Add Bypass Domain** and press Enter. Several domains can be pasted at once, separated by commas or new lines.
3. For a GeoSite category, pick it under **Add Bypass Category**.
4. For addresses, open **Bypass IPs** and use **Add Bypass IP/CIDR** or **Add Bypass GeoIP Category**.
5. For a network, open **ASN** and enter its number, such as `AS15169`, or an IP address of the service. For an address, b4 offers the networks that announce it.
6. Click **Save Changes**.

[Targets](../sets/targets.md) describes every field of the tab.

## Adding from the Traffic page or the dashboard

The [Traffic](../connections.md) page lists the connections b4 sees, and the dashboard's [Top domains and addresses](../dashboard.md#top-domains) panels list the names and addresses that devices use most. A **+** next to a name or an address opens a dialog that adds it to a set. On the dashboard, **+** marks the names and addresses that no set matched.

For a domain:

1. Click **+** next to the domain.
2. Pick the pattern: the domain itself or one of its parent domains. A parent domain covers every subdomain.
3. Under **Target Set**, pick a set, or **Create New Set** and enter its name.
4. Click **Add Domain**.

For an address:

1. Click **+** next to the address. The dialog looks up the network that announces it.
2. Under **What to add**, pick the address alone, the announced prefix that covers it, a broader mask, or the whole network, its ASN.
3. Under **Target Set**, pick a set or create one.
4. Click the button that names what is added, such as **Add 142.250.120.139** or **Add AS15169**.

[Adding domains to sets](../connections.md#adding-domains-to-sets) and [Adding addresses and networks to sets](../connections.md#adding-addresses-and-networks-to-sets) describe both dialogs. The [DPI Detector](../detector.md) opens the address dialog from its Hosting and CDN results.

## Checking the result

- The set's card on the **Sets** page shows the new targets. An ASN whose prefixes are not fetched yet shows as **N ASN unresolved**.
- On the Traffic page and the dashboard, the next connections to the site carry the set's badge.
- The dashboard's **Sets** panel counts the connections the set matched.

## When it does not work

**The connections carry another set's badge.** Another set targets the same site and takes its connections, for example a set limited to the device. When a domain is already in another set, the **Targets** tab names that set. [How sets match traffic](../internals/matching.md) describes how b4 chooses.

**The connections carry no set at all.** The set may be turned off, or the site may load from other domains. The Traffic page shows the names it really uses, and **Domains not in any set** on the dashboard opens it with only the connections no set matched.

**A category or an ASN adds nothing.** The GeoSite or GeoIP database is not loaded, or the ASN's prefixes could not be fetched yet. b4 retries an ASN in the background.

**The site is matched but still does not open.** The set's strategy does not work for it, see [Unblocking a site](./unblock.md).
