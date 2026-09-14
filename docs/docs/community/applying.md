---
sidebar_position: 2
title: Applying a set
---

# Applying a set

**Apply** on a card or in the details dialog turns a published set into a local one. Nothing is changed on the hub; the set is created on this router and starts working at once.

## What applying does

1. The payload files the set refers to are fetched from the hub by their hash. When no hub answers, the set is not applied, since a fake packet without its payload is not the strategy that was published.
2. The set is opened with the same checks as a [shared set](../sets/sharing.md#what-the-import-checks) pasted into **Import**: settings this b4 does not know are dropped, values it does not accept are replaced by defaults, a pin for a domain outside the set or pointing at a private address is dropped, and a DNS redirect to a DoH resolver outside the known public list is dropped. Each of these is reported after the set is created.
3. Payload files are installed under `captures/` and appear under **Settings, Payloads**, named by protocol and server name; an existing file with the same name and different contents is kept, and the new one gets a hash suffix.
4. Geosite and geoip categories the set names are checked against the local databases. Categories missing from a database are reported; with no database installed at all, the categories are removed from the set, and a set left with no targets this router can match is refused.
5. The set is created enabled, named after the published title, and placed at the top of the **Sets** page. Domains it lists are removed from every other enabled set that listed them, so that one domain is handled by one set; the message names what moved and from where.
6. The change is applied without a restart, as a set saved from the editor would be.

The message after applying offers **Open set**, which opens it in the editor, and **Undo**, which deletes the set again.

:::warning
Domains moved out of other sets are not put back by **Undo**. A domain that was deliberately kept in another set ends up in the new one once a community set listing it is applied, and goes back only by hand.
:::

## Applied and edited

The local set remembers which hub set it came from: the id, the version, the strategy fingerprint and the date. The card on the Community page shows **Applied** and gains an **Open set** button; the card on the Sets page shows a **Community set** chip, which opens the set on the Community page.

The fingerprint covers the packet-altering settings and the DNS redirect settings. Changing them in the editor turns the badge into **Applied, edited** and the chip into **Community set, edited**. Changing the targets, the port filters, the pins, the name, the routing or the device filters does not count as an edit, so a community set can be limited to the domains that matter on this network, or routed through a proxy, without losing the link.

The link matters for two things: the [works and broken reports](./feedback.md), which are accepted only while the strategy is unmodified, and the update path below.

## Updating and reapplying

When the author publishes a new version, the card of the applied set shows **Update to version N** in place of **Apply**. An edited set shows **Reapply** instead. Both replace the strategy and the targets of the local set with the published ones, after confirmation; the set keeps its id, its position in the list and its enabled state, and everything else about it, including routing and device filters, stays as it was. Edits to the strategy or the targets are lost, which the confirmation says.

:::info
Superseded versions stay usable. A router that applied version 1 keeps running version 1 until **Update** is pressed; the catalogue only carries the newest version, so the reports score shown on the card is that of the newest one.
:::

## Testing an applied set

**Test** on an applied set fetches one domain twice, once through b4 with the set active and once with b4 bypassed, and reports both outcomes. The domain defaults to the one the list was filtered by, otherwise to the first domain the set lists; any domain the set covers can be typed in.

| Through b4 | Bypassing b4 | Verdict |
| --- | --- | --- |
| OK | OK | Nothing blocks the domain right now, so the set is not needed for it. |
| OK | fails | The set does its job. |
| fails | OK | The set breaks the domain. |
| fails | fails | The address may be blocked outright, or the site is down. |

A test result is the fetch of one page at one moment, not a report; nothing is sent to the hub. It is the check to make before pressing **Works** or **Broken**.

## Discovery and the catalogue

[Discovery](../discovery.md) has an option, **Try community sets first**, shown while the Community Hub is on and enabled by default. With it, a run refreshes the catalogue when the copy is older than ten minutes, searches it for each site the same way the Community page does, and takes up to three published sets per site, best rated first. They are tested after the strategies remembered from earlier runs and before the built-in presets, under the names `community-<title>-v<version>`, and the run's log says how many were queued. A set whose payload cannot be fetched is skipped.

A community strategy that wins the run is applied as a Discovery result like any other, and the set it creates keeps the link to the hub set, so it shows as **Applied** on the Community page and can be reported on.
