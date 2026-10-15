---
sidebar_position: 5.5
title: Discovery
description: The pages Discovery tests for a set, the last search for it, and the watchdog switch.
---

The **Discovery** tab of the set editor holds the pages [Discovery](../discovery.md) tests for the set, the last search for it, and the switch that puts the set under the [watchdog](../watchdog.md). [Keeping sets working](../guides/keep-working.md) walks through it.

A set that routes its traffic to a proxy or an interface applies no bypass strategy, so the tab is read-only for it.

## Discovery addresses

**Address** adds a page that Discovery fetches when it searches a strategy for the set. The watchdog checks the same pages.

| Rule | Effect |
| --- | --- |
| Up to 5 addresses, one per host | The field is disabled once the list is full. |
| A domain | Becomes `https://<domain>/`. |
| A full URL | Kept with its path. |
| Only `http` and `https` addresses of public hosts | Local and private addresses are refused. |

A strategy found on these pages applies to every domain the set targets, although only these pages were tested. For a set built on a GeoSite category, that is every entry of the category. Useful addresses are pages that represent the set, such as the main page of each service it covers, and pages that do not open without b4. A page that loads without b4 cannot show whether the set works.

A set created by [Discovery](../discovery.md#creating-a-set) keeps the addresses it was found on. **Use these addresses as the set's discovery URLs**, in the Discovery apply dialog, stores the tested addresses in an existing set. **Watch through &lt;set&gt;** on the watchdog's [older per-domain list](../watchdog.md#older-per-domain-list) adds the address of a domain.

Discovery addresses describe the local network and stay on the router. A set shared or published to the Community Hub carries none, and an update from the hub keeps the local ones. The **Import/Export** JSON carries the addresses, but not the watchdog switch.

## Suggestions

**Suggest** loads up to ten candidate addresses, one per host. Each one names its source:

- **Saved for this set**;
- **From an earlier search for this set**;
- **From the DPI Detector site list**, for sites of the [DPI Detector](../detector.md) list that the set matches;
- **A domain listed in the set**;
- **A known page of a service in the set's geosite categories**.

**Add** puts a suggestion in the list. A suggestion for a host that another set handles names that set: a strategy written into this set does not affect it. A set whose targets are only large lists, such as `ru-blocked`, usually gets no suggestion, and a page has to be added by hand.

**Suggest** and **Find a strategy** need a saved set.

## Finding a strategy

**Find a strategy** opens [Discovery](../discovery.md#a-run-for-a-set) with this set picked and its addresses in the field. Unsaved changes to the set are lost when Discovery opens. The **Find a strategy** item of the set card's menu on the **Sets** page does the same.

## Watchdog

**Keep this set working with the watchdog** puts the set under the [watchdog](../watchdog.md). The set's Discovery addresses are then checked on a schedule. When they keep failing, the watchdog searches for a new strategy, writes it into the set, checks the addresses again, and undoes the change if they still fail.

**Enable Watchdog** under [Settings > Discovery](../settings/discovery.md#watchdog) has to be on as well. While it is off, the set's switch can stay on, but nothing is checked.

| Condition | When it does not hold |
| --- | --- |
| The set is enabled | The switch stays on, and the set is not checked until it is enabled. |
| The set has Discovery addresses | The switch stays on, and nothing is checked until an address is added. |
| The set routes no traffic | The switch turns off when the set is saved. |
| The set is not limited to specific devices by an include list | The switch turns off when the set is saved. The router's own check carries no device MAC and never matches such a set. |
| The set targets domains or GeoSite categories | The switch stays on, but the set is not checked. A set of only IP addresses, GeoIP categories or ASNs cannot be checked by name. |

A caption under the switch names the condition that fails.

Once the set is saved with the switch on and **Enable Watchdog** is on, the tab shows its watchdog status and the time of the last check. It also shows the last heal with the strategy it wrote, and **Open the watchdog**. On the **Sets** page, the card of a watched set carries a **Watchdog** badge coloured by the status.

A heal can change the set while the editor is open on an older copy. Saving that copy is refused with an offer to reload, which discards the unsaved changes.

## Last search

**Last search for this set** shows the most recent Discovery run for the set: its verdict, when it ended, the strategy it found, and which addresses it covered. **Open in Discovery** opens Discovery with the set picked.
