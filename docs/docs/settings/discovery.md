---
sidebar_position: 7
title: Discovery
---

Parameters of the automatic configuration search. Configured in **Settings -> Discovery**.

![Discovery settings](/img/discovery/20261003230000.png)

## Parameters

| Parameter | Description | Range | Default |
| --- | --- | --- | --- |
| Search timeout | Maximum time to wait for a response when testing each strategy | 3-30 sec | `5` sec |
| Propagation delay | Time to wait after applying a configuration before testing. Needed so the rules have time to take effect | 500-5000 ms | `1500` ms |

How many fetches in a row a preset has to pass is chosen per run with **Confirm each strategy** on the [Discovery](../discovery#options) page.

## Trusted DNS server

With **Check DNS for tampering** on, Discovery compares each site's address from the router's resolver with the address from a resolver it trusts, to tell whether DNS hands out a wrong one. **Trusted DNS server** names that resolver. Empty, Discovery asks the public resolvers built into b4: DNS over HTTPS first, then plain DNS.

| Form | Transport |
| --- | --- |
| `9.9.9.9`, `2620:fe::fe`, `127.0.0.1:53053`, `udp://9.9.9.9` | Plain DNS over UDP, asked again over TCP when the answer is truncated |
| `tcp://9.9.9.9` | Plain DNS over TCP only |
| `tcp+udp://127.0.0.1:53053` | UDP first, TCP when UDP fails |
| `https://dns.google/dns-query` | DNS over HTTPS; `/dns-query` is added when the URL has no path |

The port defaults to 53. Plain DNS needs an IP address: a host name works only in an `https://` URL. DNS over TLS and other transports are not supported directly; a local DNS proxy that speaks them covers them when its address is given here.

A server named here is the only reference. Its answers are taken as each site's correct addresses, its NXDOMAIN as a name that does not exist, and no public resolver is asked beside it. A run asks the server once before it starts and stops with an error when it does not answer. A server that stops answering later in the run leaves the site it was asked about without a DNS comparison.

When the router's resolver gives a site a wrong address, the proposed set gets a DNS fix, the first that works of:

1. the trusted server itself, when a set can use it: an IP address on port 53 that answered over UDP and is not this host's own address, or an `https://` URL;
2. a built-in DNS-over-HTTPS server that returns the site's correct address;
3. a built-in plain DNS server that does so, asked plainly and then with the query fragmented;
4. the site's correct addresses, [pinned](../dns#pinned-addresses) in the set, when no resolver gives them.

A trusted server a set cannot use, such as a local proxy on another port or a `tcp://` server, still decides which addresses are correct, and the run log says why it did not go into the set.

The same field is in the [options of a Discovery run](../discovery#options), where it applies to that run only.

:::warning Speed in discovery results
The speed shown in discovery (for example, "40 KB/s") is **not the real speed** of your connection. The test downloads a very small amount of data, not enough for a precise measurement. These numbers are only useful for **comparing strategies against each other** - which is faster, which is slower. Do not treat the absolute values as meaningful.
:::

## Watchdog

The Watchdog section holds the global switch **Enable Watchdog**. While it is on, the section also shows four timings, **Max Retries** and the **Older per-domain list**. The switch turns all watching on or off, for watched sets and for the older list alike, and the timings apply to both. Each parameter, with its range and default, is described under [Watchdog parameters](../watchdog#parameters).

**Older per-domain list** holds domains checked one by one, each healed through whichever set lists it at the time; see [Older per-domain list](../watchdog#older-per-domain-list). The same list can be edited on the Watchdog page.

:::info Watching a set
A set is put under the watchdog in its own [Discovery tab](../sets/discovery#watchdog), with **Keep this set working with the watchdog**. Its Discovery addresses are then checked together and a heal writes into that set only. The switch here still has to be on for any set to be checked.
:::
