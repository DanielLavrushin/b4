---
sidebar_position: 11
title: DPI Detector
---

# DPI Detector

The detector reports what the ISP does to traffic leaving the router: which sites are blocked and how, whether plain DNS is hijacked or answers are substituted, whether long connections to hosting and CDN networks are cut, and whether Telegram is reachable. It is built on the detection methods of the [dpi-detector](https://github.com/Runnin4ik/dpi-detector) project and reuses its target lists.

Every site is fetched twice: once directly, with b4's processing bypassed, and once through b4 with the configured sets in effect. The through-b4 fetch takes its address the way a client of that set would: a pinned address from the set first, then the answer of the set's DoH or DNS server when it has a DNS redirect, and the system resolver otherwise. Up to three addresses are tried on each side, since a site with several addresses is often blocked on only some of them; the row then reads OK with the blocked addresses listed. When the direct fetch fails, the address the DoH resolvers return is tried as well, and a row whose DoH address loads says so. The first fetch shows the ISP; the second shows whether b4 fixes it. A check made only through the service would report the bypassed view and hide the blocking it is meant to show.

## Input

The field takes a domain or a full URL, several at once when separated by spaces, commas or new lines. A domain is fetched as `https://<domain>/`; a URL with a path is fetched at that path, which matters for sites that are only blocked on some endpoints. When no site is added, the built-in list is checked. The list can also be filled from the enabled sets, so the check covers the sites the router is configured for. The last list is remembered by the browser.

Four scopes are offered, each with an estimate of how long it takes:

| Scope | What it answers |
| --- | --- |
| **Sites** | Whether each site loads, how it is blocked, and whether b4 fixes it. Blocked sites are also tried over TLS 1.2 and over plain HTTP to tell an SNI block from an address block and to catch an ISP block page. |
| **DNS** | For each public resolver: latency over UDP, DoH and DoT, whether its answers for blocked names match the answers of encrypted resolvers, and which network really answered a port 53 query. The resolvers in `/etc/resolv.conf` of the host b4 runs on are listed first. |
| **Hosting and CDN** | Whether a keep-alive connection to Hetzner, Cloudflare, Akamai, AWS and other networks is cut after the first 12 to 40 KB, grouped by network with the drop point. With the SNI search on, whitelisted names are tried against each affected network and the ones that get through are listed. |
| **Telegram** | Reachability of the datacenters, and download and upload throughput. |

Sites and DNS are on by default. Under **Advanced**: the IP family, offered only while the capture engine handles both IPv4 and IPv6 (with both selected, a site that has an IPv6 address gets a second row), the number of parallel checks (lower is kinder to a small router), whether to fetch through b4 at all, the TLS 1.2 retry, and the SNI search.

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
| No address from the resolver | This host's resolver gives no address for the site, DoH does, and the site loads at the DoH address. |

The status words are: *Reset* (a TCP reset on the TLS handshake), *Dropped* (the handshake is never answered), *Unreachable* (the connection is never established), *Throttled* (the transfer stalls inside the 12 to 69 KB window), *Fake DNS* (the resolver answers with a stub: an address from a reserved range, such as a private or loopback one, or one address given for several sites that DoH resolves elsewhere; the DoH address is shown next to it), *No address* (described below), *ISP page* (a redirect or a page from the ISP), *Refused*, *Timeout*, *Fake certificate*, *Gateway* (the first hop in front of this host answers the TCP handshake itself: a SYN sent with a hop limit of one gets a reply, so a transparent proxy on that gateway carries the connection and nothing b4 does on this host reaches past it; behind another router that means running b4 there, and when b4 is the router itself only a proxy set helps). Gateway sites have a counter of their own, *answered by the gateway*: they count neither as blocked by the ISP nor as still blocked, and **Fix with Discovery** leaves them out, since no strategy from this host reaches past that gateway. The technical detail is in the last column.

:::info The resolver behind the Sites scope
The direct fetch resolves names with the resolver of the host b4 runs on: the first three nameservers in its `/etc/resolv.conf`, or 127.0.0.1 and ::1 when it lists none, as the system resolver does. The through-b4 fetch uses the same resolver unless a set supplies an address. On a container install that is the container's own DNS setting, for example `dns=` of a RouterOS container. LAN clients may use a different resolver, so what the table shows about DNS applies to b4's host rather than to the devices behind it. The line above the table names the resolvers used.
:::

A name that resolver gives no address for while DoH resolves it is asked once more, one name at a time, unless the resolver answered that the name has no address; after three retries in a row time out, the remaining names keep their first answer. A name still without an address is fetched directly at the DoH address. When that fetch fails, the row shows its result, for example *Dropped*, and the detail adds that the resolver gave no address. When the site loads, the row reads *No address* with the outcome *No address from the resolver*, and the detail gives the reason: no answer, an answer that the name has no address (NXDOMAIN or an empty answer), a server failure or refusal, or another query error. Through b4 the row reads *No address* as well unless an enabled set supplies an address through a DNS redirect or a pin; when that address is blocked, the outcome is *Broken by b4*. Sites with the outcome *No address from the resolver* have a counter of their own, *no address from the resolver*: they count neither as blocked by the ISP nor as still blocked, and **Fix with Discovery** leaves them out, since no strategy changes what this host's resolver answers.

The **DNS** table has a row per provider. The first rows are the same resolvers the Sites scope uses, loopback addresses included; these are the resolvers of b4's host and not necessarily the router's upstream resolvers. Latency cells show the fastest answer for names that are not censored. *Honest* compares the provider's answers for known blocked names with the answers of the encrypted resolvers, and a name whose query failed is asked once more, until three retries in a row time out. *Substituted* means a stub or reserved-range address where encrypted resolvers have a real one. *No answer* means the resolver answers ordinary names but times out, fails, or returns an error code such as SERVFAIL or REFUSED for blocked names, also on the retry when one was made. *Filters* means NXDOMAIN or an empty answer for those names: the provider declines to resolve them without forging an address. When one transport's answers show several of these, it gets the first in that order. When the UDP, DoH and DoT verdicts disagree, the column shows one chip per verdict, labelled with the transports it applies to. The summary above the table names each provider that substitutes or does not answer once, however many of its transports or queries were affected.

*Port 53 answered by* is the network that handled an identity query sent to that resolver over UDP. When that network is neither the provider's own nor a known public resolver, the row is marked as hijacked. A redirect to a known public resolver is not marked on the row: when a router forwards all port 53 traffic from b4's host to AdGuard Home, which uses Google as its upstream, the column shows Google without the mark. When at least five public resolvers answered the identity query and all of them from one such network, the summary says that port 53 is redirected on this network or by the ISP; the UDP 53 column then describes that one resolver, not the listed providers. A DoH server that answered honestly can be copied to paste into a set's DNS redirect.

The **Hosting and CDN** table has a row per network with the number of targets that were cut and the byte count at which it happened. Working SNI names, when the search was on, can be copied to use as a set's fake SNI. **Details** lists every target of the network.

## Target lists

The lists of sites, hosting targets, whitelisted SNI names and resolvers are embedded in the service and dated; the line under the Run button says where they come from and how old they are. **Fetch the project's current lists** downloads them from the dpi-detector repository and keeps them next to the config, so sites and addresses that have died since the release are replaced without a new b4 version. The built-in copy can be restored from the same line.

## History

Every run is kept with its verdict, up to fifty. A run can be reopened, copied as a report, or deleted.
