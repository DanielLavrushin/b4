---
sidebar_position: 5
title: Escalation
description: The Escalation tab of the set editor, which names a backup set for the sites this set keeps failing on.
---

When a set keeps failing to open a site, escalation switches that site to a backup set. Name lookups and connections for the site then go through the backup. After a while, b4 tries the original set again. [Keeping sets working](../guides/keep-working.md) shows how escalation fits with Discovery and the watchdog.

A typical pair is a light set for most sites and a heavier one for the sites that need it. The backup does not have to be another bypass strategy: it can [route through a proxy or an interface](./routing.md), or carry a [pinned address or another DNS server](../dns.md) for the site.

![The Escalation tab of a set](/img/escalation/escalation-en.png)

## Switch to

| Field | What it does | Default |
| --- | --- | --- |
| **Escalate to set** | The backup set. **None** turns escalation off. A disabled set is marked **disabled**: the link stays and sits idle until the set is enabled again. A [set that handles only calls](./udp.md#protocol-selection) is not offered. | **None** |

Sets can form a chain, A to B to C: b4 moves a site along it as each set fails, up to eight steps. A backup set limited to source devices is used only for those devices, and other devices stay on the original set.

The tab suggests turning on [RST Injection Protection](./tcp/rst-protection.md) in this set. It keeps the current page working while b4 detects the block.

## Triggers

Each trigger counts on its own, and whichever reaches its limit first switches the site over.

| Field | What it counts | Default |
| --- | --- | --- |
| **Unanswered TLS handshakes** | How many times the same connection resends its ClientHello without an answer. | `3` |
| **Handshake wait** | A resent ClientHello older than this also counts as unanswered. | `3000` ms |
| **Forged resets** | Resets for the same site that b4 judges injected rather than sent by the server. | `3` |
| **Reset counting window** | Resets count only within this time. | `30` s |
| **Unusable DNS answers** | NXDOMAIN, SERVFAIL or answers without an address for the same domain, after which the backup set resolves it. | `2` |

A fourth trigger has no field: connection attempts to an address go unanswered, and the address does not answer the router either when b4 tests it. No bypass strategy helps such an address, while a backup set that routes the site elsewhere or pins another address does.

For DNS, IPv4 and IPv6 lookups count separately, and an empty answer to an IPv6 (AAAA) lookup does not count, since a site without IPv6 answers that way. Lookups other than A and AAAA are not counted.

## Tuning

| Field | What it does | Default |
| --- | --- | --- |
| **Remember switch for** | How long a site stays on the backup set before b4 tries this set again, 5 to 1440 minutes. | `60` min |

## How a site moves and comes back

- **The switch is per site.** It applies to the host name that failed. Other sites on the same server stay on the original set.
- **The backup takes over the whole site.** Lookups use its pinned addresses, DoH server or forwarding, connections use its bypass strategy, and its routing applies. b4 writes the addresses it knows for the site into that routing.
- **A set with IP priority keeps its connections.** A connection that a set with [IP priority](./targets.md#ip-addresses) keeps follows an escalation only to a backup on that set's own chain or ranked above that set. Between sets without source devices, a backup below such a set and off its chain never learns that set's addresses for its routing or [DSCP value](./routing.md#dscp). Backups of the sets ranked above such a set are best placed above it or on its chain, when they route traffic or have a DSCP value. A backup placed there also takes the connections such a set keeps when their site escalates to it. [IP priority](../internals/matching.md#ip-priority) has the details.
- **The failed request is lost, the next one works.** For the DNS trigger, the lookup that trips the switch is already answered by the backup set.
- **The site comes back** after **Remember switch for**. **Clear** on the dashboard's [Active Escalations](../dashboard.md#active-escalations) panel sends every site back at once and forgets the counted resets and DNS failures. **Reset counters** on the dashboard leaves the switches as they are.

Escalation reacts to failures only, so the first failure of a site is still a failure. It does not replace [Discovery](../discovery.md), which finds a working strategy in advance.

The log records each switch with its reason, for example:

```text
escalation: ntc.party is not getting through with ntc.party (no usable DNS answer), switching it to ntc.party backup
```
