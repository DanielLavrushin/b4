---
sidebar_position: 8
title: Discovery
description: Finds a bypass strategy for sites or for a set, and turns the result into a set.
---

Discovery searches for a bypass strategy for the sites named in its field, or for the pages of one set. The result becomes a new [set](./sets/index.md) or goes into an existing one. Configured sets keep working during a run.

[Unblocking a site](./guides/unblock.md) walks through a first run, and [Keeping sets working](./guides/keep-working.md) shows how the watchdog runs Discovery on its own. [How Discovery searches](./internals/discovery.md) explains a run in detail.

![Discovery input and options](/img/discovery/20261003230100.png)

## Input

**Site or URL** takes domains or full URLs, separated by commas or new lines. A domain is fetched as `https://<domain>/`, a URL as given. Local and private addresses are refused.

**Find a strategy for a set** runs Discovery for one set instead and fills the field with the set's [Discovery addresses](./sets/discovery.md#discovery-addresses), or with suggestions. A run for a set tests at most five addresses. A set that routes its traffic to a proxy or an interface cannot be picked. When another set handles an address in the field, a warning names that set. **Find a strategy** in a set card's menu, or on the set editor's **Discovery** tab, opens this page with the set picked.

### Options

The defaults are the usual choice.

| Option | What it does | Default |
| --- | --- | --- |
| **Check DNS for tampering** | Compares each site's address with a trusted resolver's answer, adds a DNS fix for a forged one and looks for [alternative addresses](#alternative-addresses). Turning it off shortens the run when DNS is known to be clean. | On |
| **Trusted DNS server** | The resolver the DNS check trusts, for this run only. Empty uses the [one in Settings](./settings/discovery.md#trusted-dns-server). | Empty |
| **Pinned addresses** | Hosts-file lines, the address and then its names. Those names are tested through these addresses, and the set keeps them as [pinned addresses](./dns.md#pinned-addresses). | Empty |
| **Stop at the first strategy that works for every address** | Shown while a set is picked. Turned off, the search tests every strategy, which takes longer and can find a faster one. | On |
| **Try community sets first** | Shown while the [Community Hub](./community/index.md) is on. Tests up to three published sets per site before the built-in strategies. | On |
| **Try strategies that worked before** | Winners of earlier runs go first. **Forget them** clears that list. | On |
| **Confirm each strategy** | Fetches in a row a strategy has to pass during the search, 1 to 5. | 1 |
| **TLS version for the probes** | Limits the probes, and the created set, to TLS 1.2 or 1.3. | Auto |
| **IP version for the probes** | The same for IPv4 and IPv6. Shown while the capture engine handles both. | Auto |
| **Captured ClientHello as the fake payload** | Fake packets carry a ClientHello captured under [Settings > Payloads](./settings/payloads.md). | None |

## A run

**Start** begins a run through six steps: **DNS**, **Without bypass**, **Strategies**, **Tuning**, **Combining** and **Confirming**. A row per site shows what has been found so far, and **Show log** opens the log. A run takes from one to ten minutes, depending on the network.

![A run in progress](/img/discovery/20260905160200.png)

**Stop and confirm** ends the search and confirms what was found, without tuning it. **Stop** ends a run that has found nothing. **Stop now**, during the confirmation, ends the run at once and keeps the results unconfirmed.

One run is in progress at a time, the searches of the [watchdog](./watchdog.md) included. A watchdog search shows on this page but cannot be stopped here.

## A run for a set

A run for a set ends with one verdict for the whole set, above **Results per address**. The set's own strategy is tested first, as **Current strategy of the set**, and a strategy is confirmed on all addresses together.

| Verdict | Meaning and what the card offers |
| --- | --- |
| **one strategy covers every address** | **Apply to &lt;set&gt;** writes the strategy into the set, see [Writing into an existing set](#writing-into-an-existing-set). |
| **current strategy works** | The set's strategy loads every address. For sites that still fail in a browser, the card names the addresses another set handles. It also notes that the browser may use QUIC (HTTP/3), which Discovery does not test, or another IP version than the run, such as IPv6. **IP version for the probes** sets the version a run tests. |
| **covers some addresses** | **Apply to &lt;set&gt; anyway** writes the best strategy. **Remove them from this set's Discovery addresses** drops the uncovered addresses. |
| **no bypass needed** | Every address loads without b4, so these pages cannot show whether the set works. |
| **nothing worked** | The cards below show what happened to each address. |
| **stopped before a result** | Nothing to apply. |

The set's [Discovery tab](./sets/discovery.md#last-search) shows the last run for it.

## Results

Every site gets one card, under **Results per address** in a run for a set.

![Results of a run](/img/discovery/20260905160300.png)

| Badge | Meaning and next step |
| --- | --- |
| **confirmed 3 of 3**, **not confirmed** | A strategy, a DNS fix, an [alternative address](#alternative-addresses) or removing an [ECH key](#sites-with-an-ech-key) made the site load. The card describes the set it creates. |
| **no bypass needed** | The site loads without b4. |
| **address blocked** | Connections to every known address fail, so no strategy can help. The site needs a [VPN](./guides/vpn.md) or a [proxy](./guides/proxy.md). |
| **intercepted by the gateway** | The first hop in front of this host answers the connection itself. b4 has to run on that gateway, or this host has to be excluded from its redirect. When b4 is the router itself, only a proxy helps. |
| **does not resolve** | The name has no address, as with a misspelled domain. |
| **link answers 400** | The link answers HTTP 400 even without b4. A link to a page or a file the site serves is needed. |
| **nothing found** | No strategy worked. A new run with **TLS 1.2** or a captured ClientHello sometimes helps. |

Sites that won with the same strategy share one card and one set. **N other strategies worked** lists the other strategies that loaded the site, fastest first, each with **Use instead**. The winner is the fastest on a single fetch, which is a noisy measure: a strategy from the list is as valid a choice.

## Applying

**Apply as a set**, **Apply** in the history and **Use instead** open one dialog. It previews the set and offers:

- **Create a new set**, then **Create set**: see [Creating a set](#creating-a-set).
- **Write the strategy into an existing set**, then **Replace strategy**: see below. This is the default when the run was for a set or a set lists the site exactly.
- **Add to &lt;set&gt;, which already uses the same strategy**, then **Add to set**: adds the sites to that set.

**Copy JSON** copies the set without creating it, in the **Import/Export** format.

### Writing into an existing set

**Which set to change** lists the run's set and the enabled sets without routing that already match the site. **Another set** picks any other set without routing. Only the bypass strategy is written: TCP, fragmentation, fake packets and, when the result uses them, a DNS redirect, pinned addresses and removing ECH keys. The port filter, RST protection, UDP, targets, routing and escalation stay as they are. **Use these addresses as the set's discovery URLs** also replaces the set's Discovery addresses.

When the set does not match the sites, **Add &lt;sites&gt; to this set** adds them and moves them out of other enabled sets. The dialog warns when another set actually handles a site, since the new strategy has no effect there.

### Creating a set

The suggested **Set name** is the site without its prefix and top-level domain. **Match** can shorten a single site to a parent domain, so the set also covers its subdomains. A site listed in another enabled set moves into the new one.

The new set goes to the top of the list, enabled. It gets a DNS redirect or pinned addresses only for a site that needed them. The tested addresses become its [Discovery addresses](./sets/discovery.md), so the set can be searched again or watched.

## Alternative addresses

Some blocks hand out an address that refuses every connection, while clients in other regions get addresses that work. With **Check DNS for tampering** on, Discovery looks for such addresses when every known address of a site fails. The set pins the site's name to up to three addresses that complete a TLS handshake, so devices get those in DNS answers.

The pins are fixed at the time of the run. When the block moves, a new run with the DNS check on finds new ones. Watchdog searches skip the DNS check and do not refresh pins.

## Sites with an ECH key

A site that publishes an ECH key is probed the way a browser with the key connects, so the ClientHello names the key's public name, such as `cloudflare-ech.com`. The set targets that name as well.

When the network drops ClientHellos with ECH and the site loads without it, the card is marked **ECH blocked**. The result is **ECH keys removed**: a set that turns on [Remove ECH keys from DNS answers](./dns.md#removing-ech-keys) and changes no packets. A browser with its own secure DNS still sends ECH, so it needs one of the other strategies listed under the result.

## History

The history keeps the latest result of each site, newest first.

![Discovery history](/img/discovery/20260905160400.png)

- **Apply** creates the set the run built, without a new run.
- **Run again** repeats the run for the site, or for the set the row came from.
- **Remove from history** and **Clear history** remove one row or all of them.
- A row expands to the other strategies that worked, each with **Use instead**.
- **Tried** marks a strategy that has been applied as a set, also after the set is deleted.
- **via watchdog** and **via MCP** mark runs that the watchdog or an AI assistant over [MCP](./settings/mcp.md) started.

**Last run log** opens the log of the last run, also after a restart, and can download it as a text file.
