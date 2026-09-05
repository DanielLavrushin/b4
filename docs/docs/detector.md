---
sidebar_position: 10
title: DPI Detector
---

# DPI Detector

The detector reports what the ISP does to traffic leaving the router: which sites are blocked and how, whether plain DNS is hijacked or answers are substituted, whether long connections to hosting and CDN networks are cut, and whether Telegram is reachable. It is built on the detection methods of the [dpi-detector](https://github.com/Runnin4ik/dpi-detector) project and reuses its target lists.

Every site is fetched twice: once directly, with b4's processing bypassed, and once through b4 with the configured sets in effect. The first fetch shows the ISP; the second shows whether b4 fixes it. A check made only through the service would report the bypassed view and hide the blocking it is meant to show.

## Input

The field takes a domain or a full URL, several at once when separated by spaces, commas or new lines. A domain is fetched as `https://<domain>/`; a URL with a path is fetched at that path, which matters for sites that are only blocked on some endpoints. When no site is added, the built-in list is checked. The list can also be filled from the enabled sets, so the check covers the sites the router is configured for. The last list is remembered by the browser.

Four scopes are offered, each with an estimate of how long it takes:

| Scope | What it answers |
| --- | --- |
| **Sites** | Whether each site loads, how it is blocked, and whether b4 fixes it. Blocked sites are also tried over TLS 1.2 and over plain HTTP to tell an SNI block from an address block and to catch an ISP block page. |
| **DNS** | For each public resolver: latency over UDP, DoH and DoT, whether its answers for blocked names match the answers of encrypted resolvers, and which network really answered a port 53 query. The router's own upstream resolvers are listed first. |
| **Hosting and CDN** | Whether a keep-alive connection to Hetzner, Cloudflare, Akamai, AWS and other networks is cut after the first 12 to 40 KB, grouped by network with the drop point. With the SNI search on, whitelisted names are tried against each affected network and the ones that get through are listed. |
| **Telegram** | Reachability of the datacenters, and download and upload throughput. |

Sites and DNS are on by default. Under **Advanced**: the IP family, the number of parallel checks (lower is kinder to a small router), whether to fetch through b4 at all, the TLS 1.2 retry, and the SNI search.

## Reading the results

The verdict comes first: a sentence naming the block types seen, with the counts of sites blocked by the ISP, fixed by b4 and still blocked. **Fix with Discovery** opens [Discovery](./discovery) with the still-blocked sites filled in. **Copy report** puts a plain-text report on the clipboard for an issue or a forum post.

The **Sites** table has a row per site with the direct result, the result through b4, and an outcome:

| Outcome | Meaning |
| --- | --- |
| Works | Loads both ways. |
| Fixed by b4 | Blocked directly, loads through b4. The row names the set that handles it. |
| Still blocked | Blocked both ways. The row says whether no set targets the site, the set is disabled, or the set does not help. |
| Broken by b4 | Loads directly but not through b4; the matching set does harm. |
| Site-side error | The site itself answered with an error, both ways; not the ISP. |

The status words are: *Reset* (a TCP reset on the TLS handshake), *Dropped* (the handshake is never answered), *Unreachable* (the connection is never established), *Throttled* (the transfer stalls inside the 12 to 69 KB window), *Fake DNS* (the resolver answers with a stub address, the honest address is shown next to it), *ISP page* (a redirect or a page from the ISP), *Refused*, *Timeout*, *Fake certificate*. The technical detail is in the last column.

The **DNS** table has a row per provider. Latency cells show the fastest answer for names that are not censored. *Honest* compares the provider's answers for known blocked names with the answers of the encrypted resolvers: *Substituted* means a stub address, silence or an empty answer where encrypted resolvers have an address; *Filters* means the provider itself answers NXDOMAIN for those names, which is its own policy rather than interference. *Port 53 answered by* is the network that handled an identity query sent to that resolver; when it is not the provider's own network and not a known public resolver, the row is marked as hijacked. A DoH server that answered honestly can be copied to paste into a set's DNS redirect.

The **Hosting and CDN** table has a row per network with the number of targets that were cut and the byte count at which it happened. Working SNI names, when the search was on, can be copied to use as a set's fake SNI. **Details** lists every target of the network.

## Target lists

The lists of sites, hosting targets, whitelisted SNI names and resolvers are embedded in the service and dated. **Update target lists** downloads the current lists from the dpi-detector project and keeps them next to the config; the date under the button says which lists are in use, and the built-in copy can be restored from there.

## History

Every run is kept with its verdict, up to fifty. A run can be reopened, copied as a report, or deleted.
