---
sidebar_position: 3
title: Reports and complaints
description: Telling the hub whether an applied set works, and telling the moderators what is wrong with a published set.
---

A **Works** or **Broken** report tells the hub whether an applied set does its job on this network, and it feeds the set's score. A complaint tells the moderators about a problem with the set as published, and does not change the score.

## Requirements

- For a report: the set is applied, and its strategy is unchanged since.
- For a complaint: nothing. A set can be reported without being applied.

## Steps

1. Apply the set and use the sites it covers for a while.
2. When a site does not open, click **Test** on the card, see [Testing an applied set](./applying.md#testing-an-applied-set). A site that fails both through b4 and around it is down or blocked outright, which says nothing about the set.
3. Click **Works** or **Broken** on the card.

The pressed button stays marked, and its tooltip shows the date of the report. Clicking the other button replaces the report.

## Works and broken {#works-and-broken}

The hub keeps one current report per router, strategy and network each week. A report counts for the network the hub sees the router on, so it shapes the score that routers on the same ISP and in the same country see first, see [Reading the score](./browsing.md#reading-the-score).

On an edited set the buttons are disabled, because a report would not describe the published strategy. **Reapply** restores the published strategy and enables them again, see [Updating and reapplying](./applying.md#updating-and-reapplying).

A report made while no hub answers is saved and goes out with the next sync. The status line on the **Community** page shows how many reports are waiting. They are kept for 30 days.

## Complaints {#complaints}

**Report** on a card or in **Details** opens a complaint, with a reason of up to 500 characters. Complaints are for:

- a strategy that does not do what the title says;
- targets the set has no reason to cover, such as a `regexp:` pattern under a narrow title;
- a pin that points at an address that does not belong to the site;
- a duplicate, or spam.

A set that does not work on this network gets a **Broken** report, not a complaint. Three complaints about one version, from routers on different networks, take it out of the catalogue without waiting for a moderator. A complaint made while no hub answers waits for the next sync, like a report.

## Limits {#limits}

The hub accepts a limited number of shared sets, reports and complaints from one router a day. Some limits count per network, so a router new to the hub can be refused because other new routers on the same network came first that day. The hub operator sets the limits.

A record over a limit is refused with a message that names the limit and when it resets. Reports and complaints are saved and go out with a later sync. A shared set is not saved, and has to be published again once the limit resets.

## When it does not work

| Message | Meaning |
| --- | --- |
| **Apply the set first; reports are accepted only for sets in use.** | The set is not applied on this router. |
| **Report saved; it goes out with the next sync.** | No hub answered, or a limit was reached. The report waits in the queue. |
| **Could not send the report: ...** | The report was not sent. The message names the reason, such as a key the moderators banned. |
