---
sidebar_position: 1
title: Finding a community set
description: Searching the Community catalogue for a site and judging a set by its score.
---

Other b4 users may already have published a set that opens a blocked site. The search on the **Community** page finds the sets that cover a domain, and the score shows how they do on networks near this one.

## Requirements

- The Community Hub is on, see [Switching it on](./index.md#switching-it-on).
- The catalogue has synced at least once. The status line at the top of the **Community** page shows its date.

## Steps

1. Open **Community**.
2. Type the domain, or paste a URL, into **Domain**.
3. Compare the sets in the list. The closest match with reports from the nearest network comes first.
4. Click **Details** on a set to see all its targets and the full strategy.
5. Click **Apply** on the set chosen, see [Applying a community set](./applying.md).

The fields of the page are described under [Community](./index.md).

## Reading the score {#reading-the-score}

**Reports** on a card shows the share of reports that say the set works, such as **75% works**. The card uses the nearest network with enough reports:

1. **on your ISP**: routers on the same ASN as this one;
2. **in your country**: routers in the same country;
3. **worldwide**: all routers.

A set without enough reports in any of them shows **no reports yet**. Until the router knows its own network, only worldwide reports are shown. The hub tells the router its network at the first sync, and before that the last [DPI Detector](../detector.md) run supplies it.

Under the percentage, the card shows the weight of the reports, written as a number of reports, the number of devices that sent them and the date of the last one. Older reports count for less, so that number shrinks as they age, and the score follows recent reports. A set that stopped working falls within a couple of weeks of the first **Broken** reports, whatever it collected before. Few reports give a cautious figure: two **Works** reports and nothing else show 75%.

The score describes other networks. DPI differs between regions of one ISP and changes without notice. A high score makes a set worth trying, and **Test** on an applied set checks it on this connection, see [Testing an applied set](./applying.md#testing-an-applied-set).

## When it does not work

| What happens | Cause and next step |
| --- | --- |
| **No published set covers ...** | Nobody has published a set for this domain yet. [Discovery](../discovery.md) searches for a strategy on this connection. |
| **Community** is missing from the menu | The Community Hub is off, see [Switching it on](./index.md#switching-it-on). |
| A warning takes the place of the list | See the messages under [Status line](./index.md#status-line). |
| A link opens **Set details** with a note that the set is not in the catalogue | The set is still waiting for a moderator, or the catalogue has not synced since it was approved. **Sync now** fetches the newest catalogue. |
