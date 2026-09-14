---
sidebar_position: 1
title: Browsing the catalogue
---

# Browsing the catalogue

The **Community** page appears in the navigation once the Community Hub is switched on. It shows the catalogue the router holds, one card per published set, best rated first.

## Status line

The line at the top says which catalogue is shown: its build date, how many sets it holds, when it was last synced, the network the hub sees the router on, and how many reports are waiting to be sent. **Sync now** fetches the catalogue at once and delivers any waiting reports.

Two warnings can replace the list. Without a trusted hub key the catalogue cannot be verified, which happens only when a custom key in the settings is malformed. An expired catalogue, one that could not be refreshed for two weeks, is still shown, with a note that the sets are the last ones seen.

## Search

The **Domain** field filters the list to the sets that cover a domain. A URL pasted into it is reduced to its hostname. Matching works the way [set targets](../sets/targets.md) match: an exact domain entry, a suffix entry that covers the domain, a pattern, or a geosite category that contains it. The category lookup uses the geosite database installed on this router, so a category this router does not have cannot match. Each matching card names the entry it matched through, such as "covers it through the category youtube".

With the field empty, every set in the catalogue is listed. Either way the search runs on the router against the local copy; the domain is not sent to the hub.

The order is: the closer match first (an exact domain over a suffix over a category), then the set with reports from the nearer network (this ISP over this country over the world), then the higher score, then the larger number of reports, then the more recently updated set.

## The set card

![A community set card](/img/community/20260914230100.png)

The header carries the title, the author's key id, the version, the technique family, the b4 version the set needs, and a summary of its targets: the first domains, the geosite and geoip categories, and how many domains and addresses there are in total. Badges to the right say what kind of set it is:

| Badge | Meaning |
| --- | --- |
| **Applied** | A local set was created from this hub set, and its strategy is still the published one. Clicking opens the local set. |
| **Applied, edited** | The local set exists but its strategy was changed after it was applied. |
| **needs payload** | The fake packets use a captured ClientHello that travels with the set. Applying fetches the file from the hub. |
| **block** | A [blocking](../sets/blocking.md) set: routing in block mode, which blackholes its targets rather than bypassing anything. |
| **pins** | The set pins domains to fixed addresses. The addresses are listed under **Details** and, after applying, in the set's **DNS** tab. |
| **blanket** | The set targets geosite categories only, with no domains or addresses of its own. It is listed unfiltered, since what it matches depends on the geosite database it is looked up in. |
| **catch-all** | At least one domain entry is a pattern, so the set may cover far more than the listed names suggest. |

Below the description, the strategy summary describes what the set does to packets in one sentence per technique, the same summary Discovery uses for its results. Beside it, the **Reports** block shows the score.

## Reading the score

The score is the share of reports that say the set works, shown as "N% works", with the count of reports it is based on, how many routers sent them and when the last one arrived. It is shown for the nearest network that has enough reports:

1. **on this ISP** (shown as "your ISP"): reports from routers the hub saw on the same ASN as this router;
2. **in this country**: reports from the same country;
3. **worldwide**: all reports.

A network counts as having enough reports once at least two different routers have reported from it and their reports carry enough weight between them. Until the hub has told the router which network it is on, which happens at the first sync, only worldwide reports are shown. A set whose reports do not reach that threshold anywhere shows "no reports yet".

The score is not a plain average. A report loses half its weight every two weeks, so a set that stopped working sinks as new broken reports arrive even when it collected many works reports last month. A report from a router whose key the hub first saw less than a week ago counts a quarter. A report the hub could not place on any network counts a quarter and only in the worldwide figure. The publication itself counts as one works report from the author. One router has one report per set per network per week; a newer report replaces the older one. The figure starts from an even prior rather than from the first report, so a set with two works reports and nothing else shows 75%, not 100%.

:::info
The score describes other people's networks. A set at 90% on the ISP was reported working by routers the hub saw on the same ASN, which is the strongest signal the catalogue has, but the DPI on that ISP can differ by region and change without notice. **Test** on the card, described under [Applying a set](./applying.md#testing-an-applied-set), checks the set on this connection.
:::

## Details

**Details** opens everything the catalogue holds about the set: the score with its network, the full author key id, the version with the b4 version it needs and the version it was built on, the capture engine the author runs, the creation and update dates, the strategy fingerprint, the complete list of domains, categories and addresses, the strategy summary, the payload files with their sizes, and the set JSON as it would be imported, with a copy button. The dialog has the same **Apply** and **Report** actions as the card.

A link to a set that is not in the local catalogue, for example one that was just published and is still waiting for a moderator, opens the dialog with a note saying so.
