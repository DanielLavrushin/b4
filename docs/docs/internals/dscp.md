---
sidebar_position: 50
title: DSCP rules
description: How b4 writes a set's own DSCP value, which packets carry it, why a set is refused, the firewall rules behind it, and how Discovery tests with it.
---

The fields are described under [DSCP](../sets/routing.md#dscp) on the DNS & Routing tab, and the setup of both routers in [Passing sets to another router](../guides/dscp.md).

A set's own DSCP value is written by the rules of **Set DSCP** under [Settings > Core > Firewall](../settings/core.md#dscp), after the **Set DSCP** value, which it replaces in the packets to the set's addresses, whether **Set DSCP** is on or off. The rules cover forwarded traffic, b4's own connections and the fakes, fragments and segments b4 injects, in IPv4 and IPv6 whatever **Enable IPv4 Support** and **Enable IPv6 Support** say, and keep the two ECN bits. The packets **Set DSCP** leaves alone keep their value: loopback traffic, packets b4 sends toward LAN clients, such as DNS answers and resets, and replies within connections another host opened, such as the web interface's responses. The interface list of **Set DSCP** limits the sets' values too. Where the rules sit is described in [Rules of the Firewall sub-tab](./capture.md#firewall-sub-tab-rules).

## Which packets get the value {#dscp-addresses}

The value goes by destination address alone, whatever the protocol, the port and the set b4 picked for the connection's strategy. Port filters and **TLS Version Filter** do not narrow it.

| Addresses | Where they come from | Lifetime |
| --- | --- | --- |
| Static | IP and CIDR targets, GeoIP categories and ASNs | While the set lists them |
| Learned from DNS | Answers for the set's domains over UDP and TCP: forwarded, pinned or from the set's DNS redirect | **Resolved IP TTL (seconds)** while routing is on, otherwise 1 hour |
| Learned from escalation | The addresses of a host that [escalation](../sets/escalation.md) hands to the set | As for DNS |
| Learned from TLS and QUIC | The destination of a connection whose ClientHello or QUIC Initial matches the set by name, other than a [private address](./matching.md#the-order-of-checks) that no set's **Bypass IPs** list; over QUIC, no private address is learned at all | 10 minutes |
| b4's own lookups | The first 256 domain entries that are not `regexp:`, GeoSite excluded, when the set starts writing its value or its domains or resolver change, then every half lifetime, at least 5 minutes apart | As for DNS |

A learned address seen again gets a fresh lifetime once less than half of it is left, or at once when b4's lookup finds it. **IP Version Filter** at `4` or `6` leaves out the other family. **Domain-only matching** turns learning off. Sets below a set with [IP priority](./matching.md#ip-priority) do not learn its addresses, unless they are on its escalation chain or either set is limited to source devices. Static addresses are never affected.

Where sets overlap, an address learned for a set wins over every static range; among static ranges the longest prefix wins; with the same prefix or learned address, the set higher in the list wins. Only sets whose value is applied take part.

The router's own connections, the connections b4's SOCKS5 proxy opens directly, and the fakes and segments b4 injects get the value by destination as well, whatever **Router's own traffic** says. Queries to a set's resolver carry a value only when some set lists the resolver's address, and then all of them do, the devices' included.

## The first connection {#dscp-first-connection}

A packet to an address not yet written for the set leaves without the value. Static targets are written when the configuration is applied. After a forwarded or pinned answer over UDP, the NFQUEUE engine holds the answer until the address is written, at most 250 ms, and the TUN engine does not hold it. An answer from the set's DNS redirect, and any answer over TCP, goes out after the address is written. A connection whose address comes from its own ClientHello or QUIC Initial starts without the value, and b4's own lookups help only when the device got the same address. Where the devices' DNS does not pass through b4, as with b4 as a [container on MikroTik](../install/mikrotik.md) or with devices on DoH, DoT or DoQ, IP, CIDR, GeoIP and ASN targets give the value from the first packet.

## Routing mode and scope {#dscp-modes}

The `Per-set DSCP` group of **System Info** names a refused set's reason with these codes:

| Set | Reason |
| --- | --- |
| **Upstream SOCKS5 proxy** mode: the traffic leaves as b4's own connections to the proxy | `routing_proxy` |
| **Telegram over WebSocket (built-in)** mode: shared WebSocket connections | `routing_mtproto_ws` |
| **Block (blackhole)** mode: the packets never leave | `routing_block` |
| Source devices listed, included or excluded: the stage that writes the value cannot see the device | `source_devices` |
| Routing limited to **Source Interfaces**: that stage cannot see the arriving interface | `source_interfaces` |
| [Device Filtering](../settings/core.md#device-filtering) with a device selected, for every set | `device_filter` |
| No domain, IP, GeoIP or ASN target, or **Domain-only matching** without IP, GeoIP or ASN targets | `no_addresses` |

In **Network interface** mode the value is written into the packets that leave through an interface the list covers. Through a tunnel, the router in front sees only the tunnel's packets, and WireGuard writes 0 into their DSCP field. With **Skip IPTables/NFTables Setup** on, no set writes its value.

## Firewall rules and cost {#dscp-firewall}

The sets' rules sit after the **Set DSCP** rule, in its chain `B4_DSCP` with iptables or its table `inet b4_dscp` with nftables, installed while **Set DSCP** is on or an enabled set writes its own value.

| Backend | What the sets add | Needs |
| --- | --- | --- |
| iptables | A rule that ends the chain for a packet to none of the sets' addresses, then one rule per value and per set that learns addresses. The ipsets `b4d_u_v4`, `b4d_ul_v4`, `b4d_s<value>_v4`, `b4d_l_<set>_v4` and their `_v6` pairs hold the addresses | `ipset` and the `xt_set` kernel module. Without them b4 logs a warning, and that family gets only the **Set DSCP** value |
| nftables | The maps `s4_<n>` and `s6_<n>`, a chain `v<value>` per value, and the sets `l_<set>_4` and `l_<set>_6` of learned addresses | Nothing beyond **Set DSCP** |

`<set>` is derived from the set's id; [Checking marks on the router](../guides/marks.md#checking-marks-on-the-router) lists them. Every packet the host sends, apart from those left alone at the top of the chain, costs extra lookups: with nftables one in the static map and one per learning set; with iptables up to two in `b4d_u_*` and `b4d_ul_*`, then one per value and per learning set for a packet to a set's address.

**System Info** lists a `Per-set DSCP` group under **Firewall** while an enabled set has its own value switched on: a line per set, a line per iptables binary that cannot use ipsets, and the backend.

```text
"Video" (0c5d7e4a-2b1f-4c3d-9e8f-1a2b3c4d5e6f): DSCP 31, applied, 0 static entries, learned addresses written: 412 from DNS answers, 9 from TLS/QUIC names, 37 from lookups
"Ads" (5b8e2f10-7c4a-4e9d-8a1b-3c2d4e5f6a7b): DSCP 7, refused: routing_block
installed with nftables
```

`not applied` in place of `applied` means the set's rules are not in place, for example with iptables and no `ipset` command.

## Discovery and the watchdog {#discovery}

A [Discovery](../discovery.md) run for a set with its own value, and a [watchdog](../watchdog.md) heal of such a set, write the value into their probes, so the router in front of b4 routes them as the set's traffic. The value goes into the probes of each IP version in which the set's traffic carries it: one the set's **IP Version Filter** admits and, with iptables, one whose binary can use ipsets. A set with **Domain-only matching** on is tested without its value, which goes only to its IP, GeoIP and ASN targets.

Probes without a set's value carry what other traffic carries: the **Set DSCP** value while it is on, or none, also to the addresses of sets with values of their own. A run for sites, not for a set, takes this path throughout.

b4 records the path a run's probes took: the value in each IP version and the interface list of **Set DSCP**. Before a result goes into a set or creates one, from the Discovery page, over MCP or by the watchdog, b4 compares that record with the path the set's traffic takes at that moment, in the IP versions both the set and the result cover. Another value, a value on one side only, or another interface list refuses the write.

A run stops when b4 rewrites its DSCP rules during it, as after a change to the value or the targets of a set that writes one, or when a firewall rebuild cannot put the run's own rules back. Its results cannot be applied. While b4 is still applying its DSCP rules, a run does not start and nothing is written.

While a set writes its own value, a run gets a chain that writes the DSCP field of its probes after the sets' rules, so its value replaces the one they chose by destination: `B4_DISCOVERY_DSCP` in mangle, jumped to from `POSTROUTING` right after `B4_DSCP`, or `b4_discovery_dscp` in `inet b4_mangle` at postrouting priority 151. It matches only Discovery's two marks, see [Discovery](../guides/marks.md#discovery).
