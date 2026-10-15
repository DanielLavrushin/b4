---
sidebar_position: 11
title: DPI Detector
description: What the ISP does to traffic leaving the router, and whether b4's sets fix it.
---

The DPI Detector shows what the ISP does to traffic leaving the router. It finds which sites are blocked and how, whether DNS answers are forged, whether long connections to hosting networks are cut, and whether Telegram is reachable. Every site is fetched twice, directly and through b4, so the result shows both the block and whether a set fixes it. [Unblocking a site](./guides/unblock.md) shows where the detector helps.

The checks and the target lists come from the [dpi-detector](https://github.com/Runnin4ik/dpi-detector) project.

## Setting up a check

**Sites to check** takes domains or URLs, separated by spaces or new lines. A URL with a path is checked at that path. Without sites, the built-in list is checked. **Fill from:** fills the field with the built-in list, or with the domains listed by hand under **Targets** in the enabled sets.

**What to check** offers four scopes:

| Scope | What it answers | Default |
| --- | --- | --- |
| **Sites** | Whether each site loads, how it is blocked, and whether b4 fixes it. | On |
| **DNS** | Whether resolvers answer blocked names honestly, whether port 53 is hijacked, and which DoH and DoT servers work. | On |
| **Hosting and CDN** | Whether long connections to Hetzner, Cloudflare, Akamai and other networks are cut after the first kilobytes. | Off |
| **Telegram** | Whether the datacenters are reachable, and the download and upload speed. | Off |

**Advanced** holds the rest:

| Option | What it does | Default |
| --- | --- | --- |
| **IP family** | IPv4, IPv6 or both. Shown while the capture engine handles both. With both, a site with an IPv6 address gets a second row. | both |
| **Parallel checks** | How many checks run at once. A lower number is easier on a small router. | 5 |
| **Fetch mode** | **direct and through b4**, or **direct only**. | direct and through b4 |
| **Also try TLS 1.2** | When a site is blocked over TLS 1.3, checks whether TLS 1.2 gets through. | On |
| **Look for a working SNI on blocked hosting** | For each network that cuts long connections, tries known whitelisted names as the SNI. | Off |

**Run check** starts the check, and **Stop and keep results** ends it early.

## Reading the results

The verdict comes first, with the counts of sites blocked by the ISP, fixed by b4 and still blocked. **Fix N with Discovery** opens [Discovery](./discovery.md) with the still-blocked sites filled in. **Copy report** copies a plain-text report for an issue or a forum post.

### Sites

The table has a row per site, with the result **Direct**, the result **Through b4** and the **Outcome**:

| Outcome | Meaning |
| --- | --- |
| **Works** | Loads both ways. |
| **Fixed by b4** | Blocked directly, loads through b4. The row names the set. |
| **Still blocked** | Blocked both ways. The row says whether no set targets the site, the set is disabled, or the set does not help. |
| **Broken by b4** | Loads directly but not through b4. The matching set does harm. |
| **Site-side error** | The site itself answered with an error, both ways. |
| **No address from the resolver** | The resolver of b4's host gives no address for the site, while DoH does and the site loads at it. |

The direct result says how a site is blocked:

| Status | Meaning |
| --- | --- |
| **Reset** | A TCP reset on the TLS handshake. |
| **Dropped** | The TLS handshake is never answered. |
| **Unreachable** | The connection is never established. |
| **Throttled** | The transfer stops after the first kilobytes. |
| **Fake DNS** | The resolver answers with a stub address. The DoH address is shown next to it. |
| **No address** | The resolver gives no address for the name. |
| **ISP page** | A redirect to, or a page from, the ISP. |
| **Fake certificate** | The certificate is substituted. |
| **TLS alert**, **Garbage reply** | A forged TLS alert, or injected garbage in place of TLS. |
| **Refused**, **Timeout** | The connection is refused, or nothing answers in time. |
| **Gateway** | The first hop in front of this host answers the connection itself, as a transparent proxy does. Nothing b4 does on this host reaches past it. |

Sites with **Gateway** or **No address from the resolver** have counters of their own and are left out of **Fix with Discovery**, since no strategy helps them.

Names are resolved with the resolver of the host b4 runs on, from its `/etc/resolv.conf`. Devices on the network may use another resolver, so what the table shows about DNS applies to b4's host. The line above the table names the resolvers used.

### DNS

The table has a row per resolver, with the resolvers of b4's host first. Latency cells show the fastest answer over UDP, DoH and DoT. **Honest** compares the resolver's answers for blocked names with the answers of encrypted resolvers:

| Value | Meaning |
| --- | --- |
| **Honest** | The answers match. |
| **Substituted** | A stub or reserved address where encrypted resolvers give a real one. |
| **No answer** | Ordinary names resolve, while blocked names time out or return an error. |
| **Filters** | NXDOMAIN or an empty answer for blocked names, without a forged address. |

**Port 53 answered by** is the network that answered a query sent to the resolver. A network that is neither the provider's own nor a known public resolver marks the row as **hijacked**. **copy DoH URL** copies a working DoH server, for a set's DNS redirect.

### Hosting and CDN

The table has a row per network, with the number of targets cut and the point of the cut (**Drops at**). **Working SNI**, when the search was on, lists names that got through, for a set's fake SNI. **Add &lt;ASN&gt; to a set** opens the add dialog of the [Traffic](./connections.md#adding-addresses-and-networks-to-sets) page with that network chosen.

### Telegram

The panel shows how many datacenters are reachable, and the download and upload speed.

## Target lists

The lists of sites, hosting targets and resolvers are built into b4 and dated. **Fetch the project's current lists** downloads the current lists from the dpi-detector repository and keeps them next to the configuration. Dead sites and addresses are then replaced without a b4 update. **Use the built-in lists** goes back to the built-in copy.

## History

Up to fifty runs are kept with their verdict. A run can be opened again, copied as a report or deleted.
