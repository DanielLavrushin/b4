---
sidebar_position: 3
title: Reports and complaints
---

# Reports and complaints

Two kinds of feedback go from a router to the hub. A **works** or **broken** report says whether an applied set does its job on this network, and feeds the score everyone sees. A **complaint** goes to the moderators and is about the set as published: a strategy that never worked, a misleading title, targets it should not cover.

## Works and broken

The **Works** and **Broken** buttons appear on the card of an applied set. A report is accepted only while the local set is in use and its strategy is unmodified; the buttons are disabled on an edited set, because a report would then describe a strategy nobody else can apply. Reapplying the set, described under [Applying a set](./applying.md#updating-and-reapplying), restores the published strategy and enables them again.

A report carries the hub set id and version, the fingerprint of the strategy it describes, the kind of report, the domain the list was filtered by when it was made, if it was, the network the router believes it is on, the capture engine and the b4 version. It is signed with the router's author key. The hub places the report on the network it sees the request coming from, not on the network the router claims, which is what keeps the "on your ISP" figure honest.

One router has one current report per set per network per week; pressing the other button replaces it. The card remembers which button was pressed and when, in the tooltip of the button. A report sent while no hub answers is saved in the outbox and delivered with the next sync; the status line on the Community page and the settings block count the reports waiting there, and the outbox keeps them for 30 days.

:::tip
**Test** on the same card fetches a domain through and around b4 before anything is reported. A set that fails the test because the site is down or blocked outright is not a broken set.
:::

## How reports become the score

The hub aggregates reports per set three times over: for every ASN, for every country, and worldwide. A router's report lands in the ASN and country cells the hub saw the request from, and in the worldwide cell; a report the hub could not place on any network lands worldwide only. The Community page shows the nearest cell that has reports from at least two routers with enough weight between them, in that order, and marks which one it is showing. Each cell keeps a count of reports, a count of distinct routers and the date of the newest report, which is what the card prints under the percentage.

Weights, before aging:

| Report | Weight |
| --- | --- |
| Works, pressed on the card | +1 |
| Broken, pressed on the card | -1 |
| The publication itself, counted for the author | +1 |
| A report the hub could not place on any network | a quarter of the above |
| A report from an author key the hub first saw less than a week ago | a quarter of the above |

Every report then loses half its weight every two weeks. A set whose works reports are all a month old and whose broken reports are from this week reads as broken, whatever the raw counts. The percentage is `(works + 1) / (all + 2)`, which is why a set with two works reports and nothing else shows 75% and a set with an equal number of each sits near 50%.

The hub also has weights for reports a router could send automatically, from the DPI detector, the watchdog and Discovery, capped so that they can never outweigh the reports people pressed. b4 does not send those yet; every report in the catalogue today is a button press or a publication.

## Complaints

**Report** on a card or in the details dialog opens a complaint to the moderators, with a reason of up to 500 characters. It is not a vote and does not change the score; a moderator reads it and decides whether to hide the set from the catalogue. Three complaints about the same version from different routers on different networks hide it without waiting for a moderator. A complaint does not require the set to be applied, and it is queued in the outbox like a report when no hub answers.

The reasons complaints are for:

- the strategy does not do what the title or description says;
- the targets include domains the set has no business covering, such as a catch-all pattern under a narrow title;
- a pin points at an address that is not the site's;
- the set is a duplicate or spam.

A set that does not work on this network is a **Broken** report, not a complaint.

## Limits

The hub accepts a bounded number of records from one router per day: shared sets, reports and complaints each have their own quota, set by the hub operator. A record over the quota is refused with a message naming the quota and when it resets; nothing is queued for retry, so the action has to be repeated later. A key the moderators have banned is refused with a message saying so.
