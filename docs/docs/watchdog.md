---
sidebar_position: 10
title: Watchdog
description: The sets and domains the watchdog checks, their status, and the strategies it wrote.
---

The **Watchdog** page shows the sets and domains the watchdog checks, their status and its last heals. The watchdog fetches a set's pages on a schedule. When they keep failing, it heals the set: it runs [Discovery](./discovery.md) for it, writes the strategy found into it, and undoes the change if the pages still fail. [Keeping sets working](./guides/keep-working.md) shows how to set it up.

The watchdog is off by default. **Enable Watchdog** under [Settings > Discovery](./settings/discovery.md#watchdog) turns it on for all sets and domains.

## Watching a set

A set is watched while **Keep this set working with the watchdog** is on in its [Discovery tab](./sets/discovery.md#watchdog). The set also has to be enabled, have Discovery addresses, route no traffic, not be limited to specific devices by an include list, and target domains or GeoSite categories. The tab names the condition that fails.

## Checks

A check fetches the set's Discovery addresses through b4, the way a device behind the router reaches them. A set is checked as soon as it is watched, then every **Check Interval**, and every **Failure Re-check Interval** after a failed check.

An address that another set handles, or that an [escalation](./sets/escalation.md) currently sends to another set, is not counted: a strategy written into this set would not affect it. An address fails when it does not load, answers with an HTTP error such as 451 or 5xx, returns a block page or is cut off. A certificate error, a required client certificate or a private destination makes it **Unusable**: no strategy changes that, so it is never healed.

## Heals

After **Max Retries** failed checks in a row, the watchdog runs [Discovery for the set](./discovery.md#a-run-for-a-set). Heals run one at a time, and a Discovery run started by hand makes them wait. A heal shows on the Discovery page but cannot be stopped there.

A heal writes only a strategy that works on every address, and only the bypass strategy, as [Writing into an existing set](./discovery.md#writing-into-an-existing-set) describes. Then the addresses are checked again, and when one still fails, the set's previous strategy comes back. A heal never touches another set.

After every heal, the set waits for the **Healing Cooldown**. After three failed heals in a row, the watchdog gives up on the set. Checks go on, but no heal starts until one of these happens:

- a check finds every address loading;
- **Check now** is clicked;
- the set's watchdog is turned off and on again.

A page opened before a heal, such as the set editor, cannot undo it. Its save is refused with an offer to reload, which discards the unsaved changes.

## Statuses

A watched set shows its status here, on its Discovery tab, on its card on the **Sets** page and on the dashboard.

| Status | Meaning |
| --- | --- |
| **Queued** | Not checked yet, or waiting for a fresh check after a change. |
| **Healthy** | Every address that could be checked loaded. |
| **Degraded** | At least one address failed. The row shows the failed checks in a row. |
| **Search queued** | **Max Retries** was reached, and the heal waits for its turn. |
| **Searching** | The heal's Discovery run is in progress. |
| **Cooldown** | A heal failed or wrote nothing, and the next check waits for the cooldown. |
| **Cannot verify** | No address can be checked: each is handled by another set, escalated or unusable. |
| **Gave up** | Three heals failed in a row. Checks continue, heals do not. |

Each address shows **OK**, **Failed**, **Other set** (with the set that handles it), **Escalated**, **Unusable** or **Queued**.

The reason under a set's status says why it is in that state or how the last heal ended:

| Reason | Meaning |
| --- | --- |
| Handled by another set | Another set, or none, handles the addresses, so a strategy written into this set does not affect them. |
| Current strategy works | Tested alone, the set's strategy loads the addresses, yet the live check fails. Something outside the strategy is in the way: another set, DNS or IPv6. Nothing was changed. |
| No bypass needed | Tested alone, the addresses load without a bypass, yet the live check fails. Same causes as above. |
| Partial | A strategy was found for only some of the addresses. Nothing was written. |
| Nothing found | The search found no working strategy. |
| Over 20 minutes | The search took too long and was stopped. It is tried again after the cooldown. |
| Verification failed | The strategy did not pass the live check and was undone. |
| Edited | The set changed while the search ran, so nothing was written. |
| Busy | Another Discovery run was in progress. The watchdog tries again later. |

## Watched sets

Each row of **Watched sets** opens the set's Discovery tab. It shows the status and the reason, the last check, **Last heal** with the strategy it wrote, **Failed heals** and the end of the **Cooldown**. **Show addresses** lists every address with its status, the HTTP response and the last error.

**Check now** checks every address at once and clears the cooldown, the failed heals and a give-up. It is unavailable while a heal is queued or running. **Stop watching this set** turns the set's watchdog off.

## Parameters

The **Watchdog** card of [Settings > Discovery](./settings/discovery.md#watchdog) sets the timings for sets and domains alike: **Check Interval**, **Failure Re-check Interval**, **Healing Cooldown**, **Check Timeout** and **Max Retries**.

## Older per-domain list

**Older domain list** holds domains checked one by one. It predates per-set watching and keeps working beside it. A domain is added in the field above the list or under **Settings > Discovery**. An entry is a domain, checked as `https://<domain>/`, or a full URL.

After **Max Retries** failed checks, Discovery runs for the domain. A confirmed strategy goes into the first enabled set without routing that lists the domain, or a parent of it, in its domain list. Without such a set, a new set named `watchdog-<domain>` is created. A domain is never written into a set that routes or blocks traffic.

| Status | Meaning |
| --- | --- |
| **Healthy** | The domain loaded on the last check. |
| **Degraded** | Failures were recorded, but **Max Retries** is not reached yet. |
| **Healing** | Discovery is running for the domain. |
| **Queued** | The domain waits for its next check. |

An entry whose host a watched set already checks reads **Checked through set &lt;name&gt;**. **Watch through &lt;set&gt;** moves an entry to the set that handles its host. It adds the address to that set's Discovery addresses, turns the set's watchdog on and removes the entry. Watching the set is the usual choice, since its heal always writes into that set and checks all of its pages together.
