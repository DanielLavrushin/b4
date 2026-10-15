---
sidebar_position: 50
title: Keeping sets working
description: Let the watchdog search for a new strategy when a set stops working, and fall back to a backup set.
---

A strategy that works today can stop working later. The [watchdog](../watchdog.md) checks a set's pages on a schedule. When they keep failing, it heals the set: it searches for a new strategy with [Discovery](../discovery.md) and writes it into the set. [Escalation](../sets/escalation.md) adds a second safety net: it moves a site that keeps failing to a backup set for a while, then tries the original set again.

## Requirements

- A set that applies a bypass strategy. A set that routes its traffic to a proxy or an interface cannot be watched.
- The set targets domains or GeoSite categories, and is not limited to specific devices by an include list.
- Up to five pages that represent the set and do not open without b4, such as the main page of each service it covers.

## Setup

1. Open **Settings > Discovery**, turn on **Enable Watchdog** and click **Save Changes**.
2. Open **Sets** and open the set.
3. On the **Discovery** tab, enter a page under **Address** and press Enter. Repeat for the other pages. **Suggest** offers candidates, and **Add** puts one in the list.
4. Turn on **Keep this set working with the watchdog**.
5. Click **Save Changes**.

A backup set for escalation is optional:

1. On the set's **Escalation** tab, pick the backup set under **Switch to**. A set that sends sites through a [VPN](./vpn.md) or a [proxy](./proxy.md) can be the backup.
2. Click **Save Changes**.

The fields of these tabs are described in [Discovery tab](../sets/discovery.md) and [Escalation](../sets/escalation.md).

## Checking the result

The **Watchdog** page lists the set under **Watched sets**. When the first check passes, its status reads **Healthy**. The set's card on the **Sets** page carries a **Watchdog** badge, and the dashboard's **Sets** panel shows the same status.

When the watchdog writes a new strategy, the row shows it under **Last heal**, and the dashboard lists it under **Recent changes**. While escalation sends a site to the backup set, the dashboard's **Active Escalations** panel lists it.

## When it does not work

| Status or message | Cause and fix |
| --- | --- |
| **The watchdog is off, but N sets ask for it**, on the dashboard | **Enable Watchdog** is off under **Settings > Discovery**. Turning it on starts the checks. |
| The switch cannot be turned on | A condition fails, and the caption under the switch names it, see [Watchdog](../sets/discovery.md#watchdog). |
| **Cannot verify** | No page can be checked: another set handles them, an escalation sends them elsewhere, or they are unusable. The set needs pages that it handles itself. |
| **Cooldown** or **Gave up**, with the reason that the current strategy loads the addresses | Tested alone, the set's strategy loads the pages, yet the live check fails. Something outside the strategy is in the way: another set, DNS or IPv6. The **Traffic** page shows which set the connections get. |
| **Gave up** | Three heals in a row failed, and the reason shows the last cause. A manual run with **Find a strategy** on the set's **Discovery** tab shows what works, and **Check now** on the **Watchdog** page resumes the heals. |

When no strategy helps any more, the sites need a [VPN](./vpn.md) or a [proxy](./proxy.md). A set routed that way, as the escalation backup, catches the sites that keep failing.
