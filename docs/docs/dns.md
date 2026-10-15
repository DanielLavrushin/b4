---
sidebar_position: 15
title: DNS for sets
description: How a set answers DNS for its domains - another resolver, DNS over HTTPS, pinned addresses, the IPv4 fallback and ECH keys.
---

A set can answer DNS for the domains it targets: send them to another resolver, answer them with fixed addresses, or remove parts of the answers. Names that no set targets go to the resolver the device chose, unchanged.

The options are on the set's **DNS & Routing** tab, under **DNS Redirect**, see [DNS & Routing](sets/routing.md#dns-redirect).

## What b4 sees {#what-b4-intercepts}

b4 sees DNS that goes to port 53, from the devices on the network and from the router itself. Encrypted DNS that a device sends by itself (DoH, DoT or DoQ) passes b4 unseen, and the sets do not apply to it. The usual sources are secure DNS in a browser such as Chrome or Firefox, Android Private DNS, and a DoH resolver set up on the device. The sets apply to such a device once encrypted DNS is turned off on it.

An encrypted forwarder on the router is not a problem, since devices still ask the router on port 53. The interception itself is described in [How b4 handles DNS](internals/dns.md#what-b4-intercepts).

## How a set handles a query

A query name matches a set the way a server name does: a domain covers its subdomains, and a domain listed in one set wins over a `regexp:` entry in another. A [block set](sets/blocking.md) answers first, then the set's [pins](#pinned-addresses), then the [set's resolver](#resolver-types) while **Enable DNS Redirect** is on. Otherwise the query goes to the device's resolver unchanged. b4 can then remove [ECH keys](#removing-ech-keys) and [IPv6 addresses](#the-ipv4-fallback) from the answer, see [the full sequence](internals/dns.md#how-a-set-handles-a-query).

## Sending a set's domains to another resolver {#resolver-types}

A set's resolver helps when the provider's DNS answers the set's domains with wrong addresses.

1. Open **Sets** and open the set.
2. On the **DNS & Routing** tab, under **DNS Redirect**, turn on **Enable DNS Redirect**.
3. Under **Resolver type**, choose **Plain DNS (UDP)** and a **DNS Server IP**, or **DNS-over-HTTPS** and a **DNS-over-HTTPS URL**, typed in or picked from the recommended list.
4. Click **Save Changes**.

| Resolver type | When it fits |
| --- | --- |
| **Plain DNS (UDP)** | Plain DNS to other servers arrives intact. **Fragment DNS Queries** splits each query into IP fragments, for DPI that reads the names in DNS queries. |
| **DNS-over-HTTPS** | The provider tampers with plain DNS to other servers too, or the resolver serves only DoH. The queries leave encrypted. |

Queries from the set's own resolver go on unchanged, so a resolver on the local network can serve a set. A resolver on the router itself has one more condition, see [When it does not work](#when-it-does-not-work).

A service that checks the address a connection comes from needs another exit rather than another resolver, see [Routing sites through a VPN](guides/vpn.md).

### Lookups b4 makes itself {#lookups-b4-makes-itself}

When b4 looks up a set's names itself, for routing, a [DSCP value](sets/routing.md#dscp) or a proxy upstream, it uses the set's resolver while **Enable DNS Redirect** is on, and the router's resolver otherwise, see [How b4 handles DNS](internals/dns.md#lookups-b4-makes-itself).

## When the resolver fails {#when-the-resolver-fails}

When the set's resolver does not answer, b4 answers from the last good reply it holds for the name, or else asks the resolver the device was addressing. **Query timeout** under [Settings > Core > DNS](settings/core.md#dns) sets how long b4 waits.

**Fail closed when the resolver is unreachable** turns the fallback off. The device then gets an error (SERVFAIL), and the set's names resolve through its resolver or not at all.

:::warning
With fail closed on, an unreachable resolver stops every name of the set from resolving, even while the bypass strategy works. On a censored connection, the DoH server itself can get blocked.
:::

## Pinned addresses

A pin answers a name with fixed addresses, with no hosts file to edit on every device. It helps when DNS keeps handing out an address that does not answer.

**Domain and addresses** under **Pinned addresses** takes hosts file lines, one per address: the address first, then the names it answers for.

```text
157.240.0.174 www.instagram.com
157.240.205.63 scontent.cdninstagram.com scontent-a.cdninstagram.com
```

- A pin covers each name and its subdomains. The longest matching entry wins.
- A pin applies only to a name the set targets. For other names the editor offers **Add to this set's domains**.
- Pins work with **Enable DNS Redirect** off too, and win over the set's resolver.
- The pins are the whole address answer: a name pinned to IPv4 addresses only gets no IPv6 addresses.
- In a set without routing, b4 leaves out pinned addresses that do not answer on TCP port 443, checked at start, on save and at the **Retest interval (seconds)** of [IP Block Detection](settings/core.md#ip-block-detection). When none answers, the pins are used anyway.

## The IPv4 fallback

While **Enable IPv6 Support** is off, b4 processes IPv4 only, and a site with IPv6 addresses would be reached over IPv6, past every set. b4 therefore removes the IPv6 addresses (AAAA records) from the answers for names a set matches, and the device connects over IPv4. Other names keep their full answers.

- The switch is **Force IPv4 for matched domains** on the **IPv4 / IPv6** card under [Settings > Core > Packet Engine](settings/core.md#protocols). It is on by default and unused while **Enable IPv6 Support** is on.
- A set limited to **IPv6** by its **IP Version Filter** keeps its IPv6 answers.
- An address the device already has from its cache, a hosts file or its own encrypted DNS still leads over IPv6. **Enable IPv6 Support** closes that path.

## Removing ECH keys

Sites behind some CDNs publish an Encrypted ClientHello (ECH) key in DNS. A browser that gets the key, such as Chrome, sends a ClientHello that names the CDN's shared name instead of the site: `cloudflare-ech.com` for every Cloudflare site. Where DPI drops such ClientHellos, all these sites stop loading at once.

**Remove ECH keys from DNS answers**, under **Encrypted ClientHello (ECH)**, takes the key out, and the browser sends an ordinary ClientHello naming the site. It works with **Enable DNS Redirect** off too. The key is removed when:

- the name asked matches the set, such as `steamdb.info` listed in the set;
- the key's public name is a domain the set matches. A set listing `cloudflare-ech.com` removes the key from every Cloudflare site's answers.

While **Enable IPv6 Support** is off, b4 in NFQUEUE mode also removes the key from answers that arrive over IPv6. A set that uses **Source Devices** leaves such answers alone, since b4 cannot tell which device they go to. A device [added by hand](settings/core.md#manual-devices) by its IPv6 address is the exception.

[Discovery](discovery.md#sites-with-an-ech-key) proposes such a set, with no packet changes, for a site that loads without ECH but not with it. A key already in a browser's cache is used until it expires.

## Sending every DNS query to DoH

A set that targets every name sends the DNS of the whole network and of the router to one DoH resolver. Domains listed in other sets keep their own sets' resolvers.

1. Open **Sets** and click **Create Set**.
2. On the **Import/Export** tab, paste the configuration below into **Set Configuration JSON** and click **Import**.
3. To use another resolver, change **DNS-over-HTTPS URL** on the **DNS & Routing** tab.
4. Click **Create Set**.

```json
{
  "b4_version": "dev",
  "name": "all DOH",
  "tcp": { "dport_filter": "53" },
  "udp": { "dport_filter": "53" },
  "fragmentation": { "strategy": "none" },
  "faking": { "sni": false },
  "targets": { "sni_domains": ["regexp:.*"] },
  "enabled": true,
  "dns": {
    "enabled": true,
    "doh_url": "https://wikimedia-dns.org/dns-query"
  }
}
```

The set changes no packets, and its port filters of 53 keep it off TLS and QUIC connections. Router host names and `.lan` names stop resolving, since the public resolver does not know them; pins keep the ones that matter working. When the resolver is unreachable, every name [falls back](#when-the-resolver-fails), so a resolver reachable from the connection matters more than its features.

## Global DNS settings

[Settings > Core > DNS](settings/core.md#dns) holds the interception of DNS over TCP and the timeouts for all sets. Without **Intercept DNS over TCP**, a device that retries a query over TCP reaches its own resolver, past the set.

## Checking the result {#reading-the-result}

1. Open **Traffic**.
2. Enter `flags:dns` in the filter to keep only DNS queries.
3. Open a site of the set on a device.

Each query shows the set that matched it and a label:

| Label | Meaning |
| --- | --- |
| `doh <server>`, `forward <address>` | Resolved through the set's DoH or plain DNS server |
| `pin`, `pin empty` | Answered from the pins, empty when they hold no address of the type asked |
| `ipv4 only`, `no ech` | IPv6 addresses or the ECH key removed from the answer |
| `heal`, `heal + ipv4 only` | Unreachable addresses removed or replaced by **Heal DNS answers** |
| `sinkhole`, `block` | A block set answered "does not exist" or dropped the query |
| `fallback-cache`, `fallback-upstream` | The set's resolver did not answer: the last good reply was used, or the device's resolver answered |
| `servfail` | The set's resolver did not answer and nothing else could, or the set fails closed |
| `passthrough`, `bad target`, `from target`, `ipv6 off` | Passed on unchanged: the set has no resolver, **DNS Server IP** is not a valid address, the query came from the set's own resolver, or it came over IPv6 while IPv6 support is off |
| `escalate <set>` | Lookups for the name kept failing, and the backup set of [Escalation](sets/escalation.md) answered |

The [log](logs.md) records the same decisions, as `dns-doh->`, `dns-pin` and so on. Both show DNS decisions while **Log Level** is **Info**, the default, or more detailed.

## When it does not work {#when-it-does-not-work}

| What happens | Cause and fix |
| --- | --- |
| A device's lookups never show on the Traffic page | The device uses encrypted DNS. Turning off secure DNS in its browser, or Android Private DNS, sends its lookups to the router. |
| The site still goes to the old address | The device keeps an answer it already has until the answer expires. |
| `servfail`, `fallback-cache` or `fallback-upstream` | The set's resolver is unreachable from this connection. Another resolver, or DoH in place of plain DNS, is the next try. |
| The set's names resolve slowly or not at all, with the set's resolver on the router | A resolver on the router, and the router's resolver when a resolver on the network uses it upstream, has to reach its own upstream over DoH or DoT, which b4 does not intercept. |
| A browser still uses ECH, with a resolver on the network that checks DNSSEC, such as BIND or Unbound | The resolver gets the original answer on a path b4 does not see: DoT or DoH to its upstream, an interface not selected in **Capture Interfaces**, or TCP while **Intercept DNS over TCP** is off. Plain DNS through a captured interface closes these paths, see [ECH keys](internals/dns.md#ech-keys). |

### On KeeneticOS and older kernels {#copied-requests}

On KeeneticOS and some other routers with an older kernel, b4 works on copies of DNS requests and replaces the resolver's reply with the set's answer. The row **DNS queries copied, not held (NFLOG)** in [System Info](settings/system.md#system-info) shows when. The resolver the device asked then still receives every request, and a resolver outside the router has to reply before the device gets the set's answer. Requests that a `nat` rule moves to another port, or that NAT sends from another source port, get no set handling, see [Copied requests](internals/dns.md#copied-requests).
