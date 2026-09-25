---
sidebar_position: 7
title: Discovery
---

Parameters that affect the automatic configuration search and the DPI detector. Configured in **Settings -> Discovery**.

![20260418233744](../../static/img/discovery/20260418233744.png)

## Parameters

| Parameter | Description | Range | Default |
| --- | --- | --- | --- |
| Search timeout | Maximum time to wait for a response when testing each strategy | 3-30 sec | `5` sec |
| Propagation delay | Time to wait after applying a configuration before testing. Needed so the rules have time to take effect | 500-5000 ms | `1500` ms |
| Reference domain | A domain known to be reachable - used as the control during checks | - | `yandex.ru` |

How many fetches in a row a preset has to pass is chosen per run with **Confirm each strategy** on the [Discovery](../discovery#options) page.

## DNS servers

List of DNS servers used when checking for DNS-based blocking. Discovery compares responses from these servers with the provider's DNS to detect tampering.

Defaults:

| Server | Provider |
| --- | --- |
| `9.9.9.9` | Quad9 |
| `1.1.1.1` | Cloudflare |
| `8.8.8.8` | Google |
| `9.9.1.1` | Quad9 (backup) |
| `8.8.4.4` | Google (backup) |

Servers can be added and removed through the interface.

:::tip When to change DNS servers
If the provider blocks queries to public DNS (for example, intercepts port 53 traffic), discovery may return false DNS results. In that case DNS servers reachable from the network can be added here, or **Check DNS for tampering** turned off in the options of a Discovery run.
:::

:::info Reference domain
The reference domain must be **unblocked** in your network. It is used for two things:

- Basic connectivity check - if it is unreachable, discovery results will be incorrect
- Baseline speed measurement - the reference domain's download speed is used as the reference point for comparing strategies against each other

:::

:::warning Speed in discovery results
The speed shown in discovery (for example, "40 KB/s") is **not the real speed** of your connection. The test downloads a very small amount of data, not enough for a precise measurement. These numbers are only useful for **comparing strategies against each other** - which is faster, which is slower. Do not treat the absolute values as meaningful.
:::

## Watchdog

The Watchdog section holds the global switch **Enable Watchdog**. While it is on, the section also shows four timings, **Max Retries** and the **Older per-domain list**. The switch turns all watching on or off, for watched sets and for the older list alike, and the timings apply to both. Each parameter, with its range and default, is described under [Watchdog parameters](../watchdog#parameters).

**Older per-domain list** holds domains checked one by one, each healed through whichever set lists it at the time; see [Older per-domain list](../watchdog#older-per-domain-list). The same list can be edited on the Watchdog page.

:::info Watching a set
A set is put under the watchdog in its own [Discovery tab](../sets/discovery#watchdog), with **Keep this set working with the watchdog**. Its Discovery addresses are then checked together and a heal writes into that set only. The switch here still has to be on for any set to be checked.
:::
