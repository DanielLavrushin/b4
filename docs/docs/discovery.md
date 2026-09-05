---
sidebar_position: 8
title: Discovery
---

# Discovery

Discovery finds a bypass strategy for the sites named in its input and turns the result into a [set](./sets/). A run applies b4's built-in strategy presets one after another on a separate capture queue, fetches every site through each of them, and keeps the presets that made the page load. The sets already configured keep working while a run is in progress; only the fetches the run makes itself go through the strategy under test.

The basic flow is covered in the [quickstart](./quickstart). This page describes what a run does, how its results are read, and what the set it creates contains.

## Input

The field takes a domain or a full URL, several at once when separated by commas or new lines. A domain is fetched as `https://<domain>/`; a URL is fetched as given, so a page that lives under a path can be named directly. Two entries with the same hostname count as one site.

![Discovery input and options](/img/discovery/20260905160100.png)

The options under the field change how the run probes, not what it looks for:

| Option | Effect |
| --- | --- |
| **Check DNS for tampering** | Before the strategies, each site is resolved through the DNS servers listed under Settings, Discovery, and the answers are compared with a reference. A site whose resolver lies about its address gets a DNS redirect written into its set, and a site whose addresses are all unreachable is searched for [alternative addresses](#alternative-addresses). Off, the run trusts the system resolver and does neither. |
| **Try strategies that worked before** | Presets that won an earlier run are tried before the built-in list. **Forget them** drops that list. |
| **Confirm each strategy** | How many consecutive fetches a preset has to pass during the search, 1 to 5. Independent of the confirmation pass at the end of the run, which always fetches the winner three more times. |
| **TLS version for the probes** | Limits the fetches to TLS 1.2 or 1.3, and limits the created set to that version, since some DPI treats the two differently. |
| **IP version for the probes** | Same, for IPv4 and IPv6. Offered only while the capture engine handles both. |
| **Captured ClientHello as the fake payload** | Fake packets carry a ClientHello captured under Settings, Payloads, instead of the built-in payloads. |

## A run

A run passes through six phases, shown as steps while it is in progress:

1. **DNS**. Each site is resolved and its answers checked, unless the DNS check is off. A site whose known addresses do not serve it is also scanned for alternative addresses here.
2. **Without bypass**. Each site is fetched with no strategy at all. A site that loads here through an honest answer is reported as needing no bypass and takes no further part in the search, though it is still fetched through every preset for comparison. A site that loads only once its DNS answer is corrected, or only through an alternative address, is reported as found with a set that changes no packets.
3. **Strategies**. The remembered winners, then the 18 built-in opening presets, are applied one at a time and every site is fetched through each.
4. **Tuning**. For every technique family that produced a working preset, its parameters are varied on the site that responded best to it, and the result is checked against all sites.
5. **Combining**. When two or more families worked, presets that combine them are tried.
6. **Confirming**. The winning preset of every site is rebuilt exactly as it would be saved as a set and fetched three more times. A winner that fails any of those fetches gives way to the next candidate.

![A run in progress](/img/discovery/20260905160200.png)

The panel shows the current phase, the time elapsed, the number of configurations tested so far, and the last line of the run's log; **Show log** opens the whole log. The bar under the steps measures the opening pass, where the number of tests is known in advance; from the tuning phase on it only signals that the run is still going, because how many tests those phases need depends on what the first phases found. One row per site says what has been found so far: a strategy with a one-sentence description, a count of presets tried, or that the site loads without b4.

**Stop and keep results** ends the run early. Whatever has been found is kept and written to history, marked as not confirmed, because the confirmation phase never ran for it. A run the [watchdog](./watchdog) started to heal a set is shown the same way but cannot be stopped from this page.

:::info
Only one run can be in progress at a time. The watchdog shares the same runtime, and a firewall refresh started while a run is active waits for it to finish. After a run ends, the queue and rules it used are released, which takes a few seconds; the page reports that and enables **Start** when it is done.
:::

## Results

Every site the run was given gets one verdict:

| Verdict | Meaning |
| --- | --- |
| **Strategy found** | Something made the site load: a preset, a corrected DNS answer, or an alternative address. The card names the winner, describes it in a sentence, and lists what the set would do. The badge says whether the confirmation pass succeeded. |
| **No bypass needed** | The site loaded without any strategy through the address its resolver gave. There is nothing to apply, and a set for it would only push its traffic through packet changes it does not need. |
| **Address blocked** | Connections to every known address of the site fail before any TLS is spoken, and no other region's DNS answer reached it either, so no packet strategy can help. Such a site needs a set routed through a [proxy or VPN](./sets/routing). |
| **Nothing found** | Every preset failed. The log shows how each one failed. Running again with TLS 1.2 or a captured ClientHello sometimes changes the outcome. |

Sites that share one winning preset are shown together on one card, since applying it creates a single set targeting all of them. The card is built from the set itself, with the same rows the Sets page shows: the split strategy and its parameters, the fake packets, the targets, and what happens to DNS. Below it, **N other strategies worked** lists every other preset that loaded the site, fastest first, each with **Use instead**, followed by the confirmation count and speed per site and how many of the tested configurations loaded the pages. The winner is the fastest of them on a single fetch, which is a noisy measure; a preset from that list is as valid a choice.

![Results of a run](/img/discovery/20260905160300.png)

## Applying

**Apply as a set** opens a dialog with the set previewed row by row. The suggested name is the site without its prefix and its top-level domain, `instagram` for `www.instagram.com`, with a suffix when the run was limited to a TLS or IP version. For a single site, the match can be shortened to a parent domain so that the set also covers subdomains. When a site is already listed in another enabled set, the dialog says so: a domain listed in two enabled sets is handled by whichever comes first in the list, so the site is moved into the new set. When an enabled set already uses the same strategy, the sites can be added to it instead of creating a new one.

The created set goes to the top of the list and is enabled at once. Its targets are the sites named, plus any geosite or geoip category b4 associates with them through its built-in CDN table, when the corresponding database is installed. The addresses learned during the run are not carried over, so the set matches by server name. A DNS redirect is included only for a site whose resolver was found to be lying, and a [pin](./dns#pinned-addresses) only for a site that needed an alternative address.

:::tip
A history entry keeps the set the run built, so a result can be applied later without running Discovery again. The confirmation state travels with it.
:::

## Alternative addresses

Some blocks are not about the domain name at all: the resolver hands out an address that refuses every connection, while the same site answers from addresses handed out to clients elsewhere. When every known address of a site fails, or the reference answers fail the TLS handshake, the DNS phase asks a public resolver, through EDNS Client Subnet, how clients in some five hundred other /16 networks are answered for that name. The addresses that come back are checked for a TCP connection, then the fastest of them for a TLS handshake with the site's name.

An address that completes the handshake is one the block does not cover. The site is fetched through it during the run, and the set pins the name to it, so that devices are answered with that address instead of the blocked one. When several addresses qualify, up to three are pinned. An address that accepts the connection but not the handshake is filtered by name like any other, so it is used as a probe target only when the site's own addresses refuse connections outright, and the packet strategies are searched on it.

A set with pins, or built for a site whose addresses were blocked, gets the IP block detection of its TCP tab switched on: a pinned address that stops answering is left out of DNS answers, and when all of them are gone the query is passed to the resolver. The list of addresses is fixed at the time of the run; when the block moves, the [watchdog](./watchdog) runs Discovery again for the site.

:::warning
A probe made from a machine where b4 is already running goes through b4's own sets. A TLS check that passes there measures the bypass, not the network. Discovery's own probes skip the sets, so its verdict on an address is the raw one; a third-party tool run on the router only agrees with it while every set covering the site is disabled.
:::

## History

The last hundred results are kept, one entry per site, the newest run for a site replacing the older one. Every entry shows its verdict, the winning preset with its technique family, the sentence describing it on hover, and when it ran. Sites from one run that won with the same preset share one set; each of them says so, and applying either installs the set for all of them, named after the site that was clicked. **Apply** installs the set the run built, the refresh button starts a new run for the site, and an entry can be removed on its own or the whole history cleared.

The log of the last run stays available under **Last run log** and survives a restart of the service; the log dialog can also download it as a text file, during a run or after it.

![Discovery history](/img/discovery/20260905160400.png)

:::info
The [watchdog](./watchdog) runs Discovery on its own when a monitored domain stops loading, and applies the result to the set that targets it.
:::
