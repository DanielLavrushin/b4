---
sidebar_position: 2
title: Applying a set
---

# Applying a set

**Apply** on a card or in the details dialog creates a local set from a published one. The hub is not changed; the set is created on this router and starts working at once.

## What applying does

1. The payload files the set refers to are fetched from the hub by hash. When no hub answers, the set is not applied.
2. The set is opened with the checks of a [shared set](../sets/sharing.md#what-the-import-checks) pasted into **Import**: unknown settings are dropped, values this b4 does not accept are replaced by defaults, a pin for a domain outside the set or pointing at a private address is dropped, a DNS redirect to a DoH resolver outside the public list is dropped. Each change is reported after the set is created.
3. Payload files are installed under `captures/` and appear under **Settings, Payloads**, named by protocol and server name. An existing file with the same name and different contents is kept; the new one gets a hash suffix.
4. Geosite and geoip categories are checked against the local databases. Missing categories are reported. Without a database, the categories are removed from the set; a set left with no targets is refused. ASNs whose prefixes this router has not fetched yet are reported and fetched in the background.
5. The set is created enabled, named after the published title, at the top of the **Sets** page. Domains it lists are removed from every other enabled set that listed them. The message names the domains and the sets they were removed from.
6. The change is applied without a restart.

The message after applying offers **Open set**, which opens the editor, and **Undo**, which deletes the set.

:::warning
**Undo** deletes the set and nothing else. Domains removed from other sets in step 5 stay removed.
:::

## Applied and edited

The local set records the hub set it came from: the id, the version, the strategy fingerprint and the date. The card on the Community page shows **Applied** and an **Open set** button; the card on the Sets page shows a **Community set** chip that opens the set on the Community page.

The fingerprint covers the TCP, UDP, fragmentation, fake packet, MSS clamp and DNS redirect settings. Editing any of them in the editor changes the badge to **Applied, edited** and the chip to **Community set, edited**. Targets, port filters, pins, the name, routing and device filters are outside the fingerprint, so a community set can be narrowed to a few domains or routed through a proxy and stays linked. Those local edits hold until the next **Update** or **Reapply**, which replaces the whole set with the published version.

The link is required for [works and broken reports](./feedback.md) and for the update below.

## Updating and reapplying

When the author publishes a new version, the card shows **Update to version N** in place of **Apply**. An edited set already on the newest version shows **Reapply**. Both replace the whole local set with the published one, after confirmation. The set keeps its id, its place in the list and its enabled state; every other setting comes from the published version.

:::warning
Routing to a proxy or an interface, device filters and escalation are not carried by a publication, so **Update** and **Reapply** return them to their defaults along with the edits to the strategy and the targets. The set has to be routed again afterwards.
:::

:::info
A router that applied version 1 keeps running version 1 until **Update** is pressed. The catalogue carries the newest version only, so the score on the card is that of the newest version.
:::

## Testing an applied set

**Test** on an applied set fetches one domain twice, through b4 with the set active and with b4 bypassed. The domain defaults to the one the list was filtered by, otherwise to the first domain of the set; any domain the set covers can be entered.

| Through b4 | Bypassing b4 | Verdict |
| --- | --- | --- |
| OK | OK | Nothing blocks the domain right now; the set is not needed for it. |
| OK | fails | The set does its job. |
| fails | OK | The set breaks the domain. |
| fails | fails | The address may be blocked outright, or the site is down. |

The test sends nothing to the hub.

## Discovery and the catalogue

[Discovery](../discovery.md) has the option **Try community sets first**, shown while the Community Hub is on and enabled by default. A run with the option:

1. syncs the catalogue when the local copy is older than ten minutes;
2. searches the catalogue for each site the same way the Community page does;
3. takes up to three sets per site, best rated first, and fetches their payloads; a set whose payload is unavailable is skipped;
4. tests them after the strategies remembered from earlier runs and before the built-in presets, under the names `community-<title>-v<version>`.

The run's log states how many community strategies were queued. A community strategy that wins is applied like any other Discovery result, and the set it creates keeps the link to the hub set: it shows as **Applied** on the Community page and accepts reports.
