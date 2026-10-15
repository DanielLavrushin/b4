---
sidebar_position: 8
title: Discovery
description: The timings and the trusted DNS server of Discovery runs, and the watchdog's switch and timings.
---

The **Discovery** tab of **Settings** has two cards. **Testing Configuration** holds the timings and the trusted DNS server of [Discovery](../discovery.md) runs. **Watchdog** turns the [watchdog](../watchdog.md) on and holds its timings.

![The Discovery tab](/img/discovery/20261004190000.png)

**Save Changes** in the page header saves the tab, see [Saving and restarting](./index.md#saving). No change here needs a restart. A change applies from the next Discovery run, not to one in progress.

## Testing Configuration

| Field | What it does | Default |
| --- | --- | --- |
| **Discovery Timeout** | Time limit for one fetch of a site. A higher value helps on a slow or unstable connection. | 5 sec |
| **Config Propagation Delay** | Pause after Discovery switches to the next strategy, before it fetches through it. A higher value helps on a slow router. The watchdog waits the same time after a heal, before it checks the set again. | 1500 ms |

How many fetches in a row a strategy has to pass is chosen for each run with **Confirm each strategy** in the Discovery [options](../discovery.md#options).

### Trusted DNS server

**Trusted DNS server**, under **DNS Configuration**, is the resolver whose answers **Check DNS for tampering** takes as correct. Discovery compares each site's address from the router's resolver with this server's answer. Empty uses the public resolvers built into b4.

| Form | Transport |
| --- | --- |
| `9.9.9.9`, `2620:fe::fe`, `127.0.0.1:53053`, `udp://9.9.9.9` | Plain DNS over UDP, repeated over TCP when the answer is truncated |
| `tcp://9.9.9.9` | Plain DNS over TCP |
| `tcp+udp://127.0.0.1:53053` | UDP first, TCP when UDP fails |
| `https://dns.google/dns-query` | DNS over HTTPS. `/dns-query` is added when the URL has no path |

The port defaults to 53. Plain DNS needs an IP address, and a host name works only in an `https://` URL. DNS over TLS is not supported directly: a local DNS proxy that speaks it can be named here instead.

When the router's resolver gives a site a wrong address, the proposed set gets a DNS fix. The trusted server itself goes into the set when a set can use it: a public IP address on port 53 that answers over UDP and is not one of this host's own, or an `https://` URL. Otherwise the set gets a built-in resolver that returns the correct address, or the correct addresses are pinned in it. The run log says which.

With **Check DNS for tampering** on, a run stops with an error when this server does not answer before it starts. The same field in the Discovery [options](../discovery.md#options) replaces this one for a single run. How the check decides is described in [How Discovery searches](../internals/discovery.md#the-dns-check).

## Watchdog

**Enable Watchdog** turns the watchdog on or off for every watched set and for the older per-domain list. It is off by default. A set is put under the watchdog on its own [Discovery tab](../sets/discovery.md#watchdog), and the switch here has to be on for any set to be checked.

The fields below show while the switch is on.

| Field | What it does | Default |
| --- | --- | --- |
| **Check Interval** | How often each watched set or domain is checked while it works. | 300 sec |
| **Failure Re-check Interval** | How often a set or domain is checked again after a failed check. | 60 sec |
| **Healing Cooldown** | Pause after a search for a new strategy, before the next check of that set or domain. | 900 sec |
| **Check Timeout** | Time limit for one check fetch. | 15 sec |
| **Max Retries** | Failed checks in a row that start a search for a new strategy. | 3 |

Turning the switch off cancels a search in progress, and that search writes nothing.

### Older per-domain list

**Older per-domain list** holds domains that the watchdog checks one by one, see [Older per-domain list](../watchdog.md#older-per-domain-list). **Add Domain** adds a domain or a full URL, and the cross on a chip removes it. The **Watchdog** page edits the same list.
