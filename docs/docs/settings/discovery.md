---
sidebar_position: 8
title: Discovery
---

**Settings, Discovery** has two cards. **Testing Configuration** holds the timings, the reference domain and the DNS servers of [Discovery](../discovery.md) runs. **Watchdog** holds the global switch, the timings of the [watchdog](../watchdog.md) and the older per-domain list.

![The Discovery tab](/img/discovery/20261004000201.png)

Changes are saved with **Save Changes** in the page header, see [Saving and restarting](./index.md#saving). None of them needs a restart. A change applies to the next Discovery run, not to one in progress. The watchdog applies the switch within 10 seconds; a check or a cooldown already scheduled keeps its time, and the new timings apply to what is scheduled after it.

## Testing Configuration

| Field | Effect | Range | Default |
| --- | --- | --- | --- |
| **Discovery Timeout** | Time limit for one fetch of a site through one of its addresses, from the connection to the end of the read. Each address a fetch tries gets the whole limit. The limits of the DNS phase's queries and connection checks are based on the same value. | 3-30 sec | `5` sec |
| **Config Propagation Delay** | Pause between switching the run's capture queue to a new configuration and the first fetch through it. A run pauses this way after loading each preset, before the confirmation fetches and before testing a fragmented DNS query. The watchdog waits the same time after a heal writes a strategy into a watched set, before checking the set's addresses. | 500-5000 ms | `1500` ms |
| **Reference Domain** | Domain fetched once at the start of every run, see below. An empty value means `yandex.ru`. | - | `yandex.ru` |

How many fetches in a row a preset has to pass is not set on this tab. It is chosen for each run with **Confirm each strategy** under [Options](../discovery.md#options).

At the start of every run, Discovery fetches `https://<reference domain>/`, reads at most 100 KB of it and writes the speed to the run's log in the line `Network baseline`. Discovery does not use the value: verdicts and the ranking of working strategies come from the fetches of the sites themselves. A failed baseline fetch is logged, and the run goes on.

:::info Speeds in Discovery
Each speed in Discovery's results, like the baseline in its log, comes from one fetch of at most 100 KB, timed from the start of the request, connection and TLS handshake included. The figures compare strategies on the same site and are not a measure of the connection's throughput.
:::

### DNS Configuration

The list of fallback DNS servers holds plain DNS servers that the DNS phase of a run turns to when b4's built-in DoH servers do not answer. Runs with **Check DNS for tampering** off skip the DNS phase and do not use the list; every heal the watchdog starts is such a run. A server is added with **Add fallback DNS server** and removed with the cross on its chip. An entry is an IP address, queried on port 53.

The DNS phase uses the list as follows:

- **Reference addresses.** The addresses a site should have are first looked up through b4's built-in DoH servers. When none of them returns an address, the servers on the list join b4's built-in UDP servers in a race of plain queries, and the first answer with addresses is used.
- **Last resort for the reference.** When the race gives nothing either, the servers on the list are asked one by one, and the first one whose answer completes a TLS handshake with the site's name gives the reference address.
- **DNS fix.** When the system resolver's answer for a site turns out false, b4's built-in DoH servers are tried first as the set's redirect. When none of them answers, each server on the list is tried in turn, with a plain query and then with a fragmented one. The first that returns an address serving the site becomes the [DNS redirect](../dns.md#resolver-types) of the set Discovery proposes, with **Fragment DNS Queries** on when only the fragmented query worked.

The default list is `9.9.9.9`, `1.1.1.1`, `8.8.8.8`, `9.9.1.1` and `8.8.4.4`.

:::tip When to change the list
The list matters only where b4's built-in DoH servers do not get through. Where the provider also intercepts plain DNS to public resolvers, the servers on the list can return forged answers, and servers that answer honestly from that network are the ones to list. Turning **Check DNS for tampering** off for a run skips the DNS phase altogether.
:::

## Watchdog

The **Watchdog** card holds the global switch **Enable Watchdog**, off by default. It turns all watching on or off, for watched sets and for the older per-domain list alike, and the timings below apply to both. Turning the switch off cancels a heal whose search is in progress, and that heal writes nothing. A set waiting in the heal queue returns to **Queued** and is checked again once the switch is back on.

The other fields show only while the switch is on:

| Field | Effect | Range | Default |
| --- | --- | --- | --- |
| **Check Interval** | How often each watched set or domain is checked while healthy | 60-1800 sec | `300` sec |
| **Failure Re-check Interval** | How often a set or domain is checked again after a failed check | 10-300 sec | `60` sec |
| **Healing Cooldown** | Pause after a heal attempt before the next check of that set or domain | 60-3600 sec | `900` sec |
| **Check Timeout** | Time limit for one check fetch, the checks after a heal included | 3-30 sec | `15` sec |
| **Max Retries** | Failed checks in a row that queue a heal | 1-10 | `3` |

The checks, the heal and the statuses are described on the [Watchdog](../watchdog.md) page. Two more watchdog values exist only in the configuration file, see [Parameters](../watchdog.md#parameters).

### Older per-domain list

**Older per-domain list** holds domains checked one by one, each healed through whichever set lists it at the time; see [Older per-domain list](../watchdog.md#older-per-domain-list). A domain is added with **Add Domain** and removed with the cross on its chip. An entry is a domain, checked as `https://<domain>/`, or a full URL, checked as given. The same list is edited in the **Older domain list** section of the Watchdog page.

:::info Watching a set
A set is put under the watchdog in its own [Discovery tab](../sets/discovery.md#watchdog), with **Keep this set working with the watchdog**. Its Discovery addresses are then checked together and a heal writes into that set only. **Enable Watchdog** still has to be on for any set to be checked.
:::
