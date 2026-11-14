---
sidebar_position: 2
title: Applying a community set
description: Turning a published set into a local set, keeping it up to date and checking that it works.
---

**Apply** creates a local set from a published one. The hub is not changed: the set is created on this router and starts working at once, without a restart.

## Requirements

- The Community Hub is on, and the set is listed on the **Community** page, see [Finding a community set](./browsing.md).
- A hub answers, when the set has the **needs payload** badge: applying fetches the payload file from it.

## Setup

1. Click **Apply** on the set's card, or in **Details**.
2. Read the message that follows. It names the domains moved out of other sets.
3. When the **Other sets affected** window opens, decide what happens to the sets it lists, see [Other sets affected](#other-sets-affected).
4. Click **Open set** to review the new set in the editor, or **Undo** to delete it.
5. Open the sites the set covers. When one does not open, run the [test](#testing-an-applied-set).

## What applying does {#what-applying-does}

- The set goes through the checks of a [shared set](../sets/sharing.md#what-the-import-checks) pasted into **Import**. Unknown settings are dropped, values this b4 does not accept are replaced by defaults, and unsafe pins and DNS redirects are dropped.
- Payload files are installed and appear under **Settings > Payloads**.
- Geosite and geoip categories missing from this router's databases are named. Without a database, the categories are removed. A set left with no targets is refused, unless it [handles only calls](../sets/udp.md#protocol-selection).
- The set is created enabled, at the top of the **Sets** page, named after the published title.
- Domains the set lists are removed from every other enabled set that listed them.
- Geosite categories, geoip categories and ASNs stay in the other sets that list them. The new set is first in the list and handles those targets, see [Other sets affected](#other-sets-affected).

:::warning
**Undo** deletes the new set. Domains removed from other sets stay removed.
:::

## Other sets affected {#other-sets-affected}

Between sets that list the same target, the one higher in the list handles it, see [Set order](../sets/index.md#set-order). The applied set is first, so it handles every geosite category, geoip category and ASN it shares with an enabled set below it. The **Other sets affected** window opens after applying and names each such set with the targets it lost.

A set that has other targets keeps handling them. A set whose every target is also in the applied set, and that is not in use in another way, handles nothing, and the window offers two actions for it:

| Action | Result |
| --- | --- |
| **Disable** | Both sets stay. The local set is switched off and keeps its settings. |
| **Replace** | The applied set takes the local set's place in the list, as in [Updating and reapplying](#updating-and-reapplying). The copy added at the top is deleted. |

**Close** keeps the sets as they are. **Undo** deletes the applied set and enables the sets disabled in this window again.

A set limited to [source devices](../sets/targets.md#source-devices) keeps the traffic of its devices and is not listed. A set that another enabled set uses as its [escalation](../sets/escalation.md) backup stays in use. A set that [routes](../sets/routing.md) its addresses, clamps their MSS or writes a [DSCP value](../sets/routing.md#dscp) stays in use as well: these work by address. The window offers no action for such sets.

## Applied and edited {#applied-and-edited}

The local set stays linked to the published one, and the link is what allows reports and updates. The card on the Community page shows **Applied**, and the set's card on the **Sets** page shows a **Community set** chip.

Editing the strategy changes the badge to **Applied, edited** and the chip to **Community set, edited**. The strategy here means the TCP, UDP, fragmentation, fake packet, MSS clamp and DNS redirect settings. **Works** and **Broken** are disabled on an edited set.

Targets, port filters, pins, the name, routing and device filters are not part of the strategy. A community set can be narrowed to a few domains or sent through a proxy and stays linked and unedited. **Calls** and **Which calls** on the UDP tab are part of the strategy, see [Sets for calls](../sets/udp.md#protocol-selection): changing either changes the badge to **Applied, edited**.

## Updating and reapplying {#updating-and-reapplying}

When the author publishes a new version, the card of an applied set shows **Update to version N**. An edited set on the newest version shows **Reapply**. Both replace the local set with the published version, after a confirmation.

The set keeps its place in the list, its enabled state, its [Discovery addresses](../sets/discovery.md), the watchdog switch and its own [DSCP value](../sets/routing.md#dscp). Every other setting comes from the published version, and local edits are lost. The [IP priority](../sets/targets.md#ip-addresses) switch is turned off.

:::warning
Routing to a proxy or an interface, device filters and escalation are not part of a publication. **Update to version N** and **Reapply** return them to their defaults, so the set has to be routed again afterwards.
:::

When the published version lists targets the local set did not have, the [Other sets affected](#other-sets-affected) window opens for the sets below it, with **Disable** as its only action.

A router keeps running the version it applied until **Update to version N** is clicked. The score on the card belongs to the newest version.

## Testing an applied set {#testing-an-applied-set}

**Test** on the card of an applied set fetches one domain twice: through b4 with the set active, and with b4 bypassed. The domain defaults to the one the list was filtered by, otherwise to the first domain of the set. Any domain the set covers can be entered. The test sends nothing to the hub.

| Through b4 | Bypassing b4 | Verdict |
| --- | --- | --- |
| OK | OK | Nothing blocks the domain right now, so the set is not needed for it. |
| OK | FAIL | The set does its job. |
| FAIL | OK | The set breaks the domain. |
| FAIL | FAIL | The address may be blocked outright, or the site is down. |

## Discovery and the catalogue {#discovery-and-the-catalogue}

[Discovery](../discovery.md) has the option **Try community sets first**, shown while the Community Hub is on and enabled by default. A run with it first syncs the catalogue when the router's copy is older than ten minutes. It then takes up to three sets per site, best rated first, and tests them after the strategies remembered from earlier runs and before the built-in presets.

Community strategies appear in the run as `community-<title>-v<version>`. One that wins is applied like any other Discovery result. The set it creates stays linked to the hub set: it shows as **Applied** on the Community page and accepts reports.

## When it does not work

When applying fails, the message names the reason. When the set applies but a site does not open, the [test](#testing-an-applied-set) shows whether the set or the site is at fault. A set that breaks the domain gets a **Broken** report, see [Reports and complaints](./feedback.md), and [Discovery](../discovery.md) searches for a strategy on this connection.
