---
sidebar_position: 3
title: Reports and complaints
---

# Reports and complaints

A router sends two kinds of feedback to the hub. A **works** or **broken** report describes whether an applied set does its job on this network and goes into the score. A **complaint** goes to the moderators and describes a problem with the set as published: a strategy that never worked, a misleading title, targets it should not cover.

## Works and broken

**Works** and **Broken** are on the card of an applied set. A report is accepted while the local set exists and its strategy is unmodified. On an edited set the buttons are disabled; **Reapply**, described under [Applying a set](./applying.md#updating-and-reapplying), restores the published strategy and enables them again.

A report carries:

- the hub set id and version;
- the fingerprint of the strategy;
- the kind of report;
- the domain the list was filtered by at the time, if any;
- the ASN and country the router knows for itself;
- the capture engine and the b4 version.

It is signed with the router's author key. The hub places the report on the network it sees the request coming from, not on the network named inside the report.

One router has one current report per set, network and week. Pressing the other button replaces it. The tooltip of the pressed button shows the date of the report. A report made while no hub answers is kept in the outbox and delivered with the next sync; the status line on the Community page and the settings block show how many are waiting. The outbox keeps a report for 30 days.

:::tip
**Test** on the same card fetches a domain through b4 and around it. A set that fails because the site is down or blocked outright is not a broken set.
:::

## How reports become the score

The hub aggregates the reports of a set in three ways: per ASN, per country and worldwide. A report goes into the ASN and country cells of the network the hub saw it from, and into the worldwide cell. A report the hub could not place on a network goes into the worldwide cell only. Each cell holds the total weight, the number of routers and the date of the newest report; the card prints those under the percentage.

Weights before decay:

| Report | Weight |
| --- | --- |
| Works | +1 |
| Broken | -1 |
| The publication itself, counted as a works report from the author | +1 |
| A report the hub could not place on a network | a quarter of the above |
| A report from an author key the hub first saw less than a week ago | a quarter of the above |

Every report loses half its weight every two weeks. The percentage is `(works + 1) / (all + 2)`: two works reports and nothing else give 75%, an equal number of each gives about 50%.

The hub also defines weights for reports a router could send automatically, from the DPI detector, the watchdog and Discovery, capped so that they never outweigh the reports made by hand. b4 does not send those yet.

## Complaints

**Report** on a card or in the details dialog opens a complaint with a reason of up to 500 characters. A complaint does not change the score. A moderator reads it and decides whether to hide the set. Three complaints about one version from different routers on different networks hide it without a moderator. A complaint does not require the set to be applied, and it is queued in the outbox like a report when no hub answers.

Complaints are for:

- a strategy that does not do what the title says;
- targets the set has no reason to cover, such as a `regexp:` pattern under a narrow title;
- a pin that points at an address not belonging to the site;
- a duplicate, or spam.

A set that does not work on this network is a **Broken** report, not a complaint.

## Limits

The hub accepts a limited number of records from one router per day, with separate limits for shared sets, reports and complaints. The limits are set by the hub operator. A record over the limit is refused with a message that names the limit and the time it resets; it is not queued. A key banned by the moderators is refused with a message saying so.
