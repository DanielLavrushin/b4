---
sidebar_position: 50
title: Packet capture
description: How packets reach b4 with NFQUEUE and TUN, the firewall rules b4 installs, kernel requirements, and how the engine starts.
---

The packet engine is the part of b4 that sees traffic. It reads the first packets of each connection, matches them against the sets and applies the strategies. Packets reach it in one of two ingestion modes, NFQUEUE or TUN, selected under [Settings > Core > Packet Engine](../settings/core.md#packet-engine). Both depend on firewall rules that b4 installs and keeps in place.

## NFQUEUE {#nfqueue}

In NFQUEUE mode, iptables or nftables rules hand packets to netfilter queues, and b4 reads them. b4 reads one queue per worker thread, numbered upward from **Queue Start Number**: 537 to 540 with the defaults.

Only the start of each connection is queued. **TCP Connection Packets Limit** and **UDP Connection Packets Limit** set how many packets, 19 and 8 by default. With iptables the count uses the `connbytes` match. The fakes and split segments b4 sends into a connection count toward the limit as well. A limit below these defaults is raised to them when the configuration is saved, and a set cannot go above the global value.

### Where the rules sit {#hooks}

| Traffic | Hook | Interface compared by Capture Interfaces |
| --- | --- | --- |
| Forwarded by the router | `postrouting`, after the routing decision | The one the packet leaves by |
| Forwarded, with **Device Filtering** on and devices from the ARP table selected | `forward`, matching the source MAC address | The one the packet leaves by |
| Opened by the router itself | `postrouting`; `output` with **Device Filtering** on and devices selected | The one the packet leaves by |
| The reply direction, and DNS | `prerouting` | The one the packet arrives on |

The TCP rules, apart from those for packet duplication addresses, select packets by conntrack direction. In `postrouting`, `forward` and `output` they take the packets in the direction of a connection's first packet, in `prerouting` the replies. b4 takes the direction of a TCP packet from the hook it was queued at. Where the kernel cannot select by direction, see [When b4 tells the direction by port](#direction-by-port).

Packets that carry the queue mark of b4 pass without being queued. The queue mark is described under [Packet marks](../guides/marks.md#the-queue-mark).

With iptables, an address family whose iptables lacks the `NFQUEUE` target or the `connbytes` match gets no capture rules. When neither IPv4 nor IPv6 can take them, the engine does not start.

### Capture Interfaces in depth {#capture-interfaces}

**Capture Interfaces** is a filter on the rules above, not a list of interfaces b4 attaches to, and it does not choose a direction. For forwarded traffic the interface compared is the one the packet leaves by, which the routing table decides. A VPN client or a transparent proxy that moves the default route therefore moves every packet to another interface, and a selection made before that stops matching.

While the selection matches nothing, packets are still queued to b4, at the same cost, and every one is accepted unchanged. No set applies and no strategy runs. The card on **Packet Engine** then names the interface the traffic leaves by and, while some traffic still matches, the share b4 does not inspect. The per-interface counts behind that notice are in the diagnostics report as `packets_leaving` and `packets_arriving`. [Which interface is which](../guides/interfaces.md) compares this field with the interface fields of a set.

## TUN {#tun}

In TUN mode, target traffic is routed through a virtual interface, `b4tun0` by default, that b4 reads. b4 sends each processed packet on through a raw socket and out the uplink, which follows the default route unless **Uplink interface** pins it. TUN mode handles IPv4 only.

### What TUN captures {#tun-capture}

| Kernel | Captured | **Capture** row in System Diagnostics |
| --- | --- | --- |
| With the `connbytes` match | The first packets of each connection to the capture ports | `ports` |
| Without `connbytes` | The whole default route goes through the TUN device | `default` |

The capture ports are TCP and UDP 443, the ports in the port filters of the sets, and TCP 80 while a set uses **Empty line before HTTP method**.

In `ports` mode with the `xt_conntrack` match, the TCP capture rules, apart from those for packet duplication addresses, take only the packets in the direction of a connection's first packet, and the reset capture only resets in the reply direction. With **Skip IPTables/NFTables Setup** off, b4 takes the direction of a TCP packet it reads from the TUN device from the first of these that applies:

- In `ports` mode with `xt_conntrack`, every packet other than a reset is the device's.
- Where b4 counts the packets itself, without `connbytes`, the first packet b4 saw of the connection decides: after a SYN its packets are the device's, after a SYN-ACK they are replies.
- Otherwise the ports decide: for a reset in `ports` mode with `connbytes`, for every packet in `ports` mode with `connbytes` but without `xt_conntrack`, and for a connection whose first packet b4 did not see. See [When b4 tells the direction by port](#direction-by-port).

### Forwarded traffic and the raw table {#tun-forwarded}

The router forwards packets for other hosts, such as LAN clients or Docker containers on a bridge network. TUN sends such a packet on as a copy, and the copy has to skip connection tracking. b4 arranges this with a `NOTRACK` rule in the iptables `raw` table. Without it, the kernel's NAT gives the copy a new source port, and the reply never reaches the client.

A kernel without the `raw` table (`iptable_raw`) or without the `CT` target (`xt_CT`), such as the Synology DSM kernel, cannot hold that rule. b4 treats a failed SNAT into the TUN device the same way. In both cases it captures only the connections the router makes itself, those of its SOCKS5 and MTProto proxies included:

- forwarded traffic gets no bypass strategies, and b4 does not see its DNS queries;
- routing and blocking sets still apply to forwarded traffic;
- where `connbytes` is missing as well, every packet of the captured connections goes through the TUN device, and b4 counts them itself to process only the first ones.

The log says so at startup, and **System Diagnostics** shows **This device only** in the **Captured Traffic** row under **Engine**. With **Skip IPTables/NFTables Setup** on, b4 installs neither rule and keeps capturing forwarded traffic.

### Address translation {#tun-nat}

The TUN engine itself rewrites the source address of captured packets to the uplink address. Its rule stays ahead of any masquerade rule that names no outgoing interface. NAT Masquerade leaves the TUN device out. When TUN captures only the router's own traffic, the rule is a NAT exemption instead, `-o <device> -j ACCEPT`, which keeps the source address.

## When b4 tells the direction by port {#direction-by-port}

b4 tells a TCP packet's direction by its ports with **Skip IPTables/NFTables Setup** on. In NFQUEUE mode it does so as well for an address family whose kernel lacks the conntrack direction match: `xt_conntrack` with iptables, `ct direction` of `nft_ct` with nftables. The cases in TUN mode are listed under [What TUN captures](#tun-capture). A packet from one of the TCP ports b4 queues, 443, the ports in the sets' TCP port filters and 80 while a set uses **Empty line before HTTP method**, counts as a server's reply. Rules added by hand while **Skip IPTables/NFTables Setup** is on are expected to queue a device's packets by destination port and the replies by source port.

Devices pick the source ports of their own connections from 32768-60999 (Linux, Android) or 49152-65535 (Windows, macOS, iOS). In this mode, a set's TCP port filter that covers these ports makes b4 pass the connections from them unprocessed. **Recent changes** on the [dashboard](../dashboard.md#recent-changes) then shows **TCP port filters cover the source ports of client connections**, with the ports. A filter below 32768 avoids it.

## Dropped QUIC connections {#dropped-quic-connections}

b4 sees only the first packets of a connection. When a set drops or rejects a QUIC connection, by its UDP action mode or as a block set, b4 keeps the later packets of the connection from passing as well:

| Engine | How | Needs |
| --- | --- | --- |
| NFQUEUE, nftables | The connection's addresses and ports go into the set `b4_udp_drop4` or `b4_udp_drop6` in `inet b4_mangle` for 2 minutes, and a rule in `b4_chain` drops its packets. b4 also deletes the connection's conntrack entry, and once the set lets go of the connection, its packets reach b4 again | `nf_conntrack_netlink` for the deletion |
| NFQUEUE, iptables | b4 deletes the connection's conntrack entry. The later packets start a new entry, reach b4 and are dropped again | `nf_conntrack_netlink` |
| TUN, with `xt_connbytes` | b4 deletes the connection's conntrack entry, and the later packets are steered into the TUN device again | `nf_conntrack_netlink` |
| TUN, without `xt_connbytes` | b4 processes every later packet of the connection | Nothing |

Without `nf_conntrack_netlink`, iptables and TUN with `xt_connbytes` drop only the packets b4 sees, the first 8 by default (**UDP Connection Packets Limit**). With nftables the drop set still holds the connection, for 2 minutes or until b4 rebuilds its rules. On OpenWrt the module is in the package `kmod-nf-conntrack-netlink`, which the installer recommends. The row **QUIC drop holds for the whole connection** under **Kernel Capabilities** in [System Info](../settings/system.md#system-info) shows whether the router holds such a drop. UDP other than QUIC that a set drops, such as a game's, is dropped only within the packets b4 sees.

A settings change applies to the held connections too. b4 decides each of them again under the new settings. With nftables, b4 empties the drop sets for this only where `nf_conntrack_netlink` is available. Without it, a connection in a drop set stays dropped until its 2 minutes pass or b4 rebuilds its rules.

## Kernel requirements {#kernel-requirements}

| Feature | Needs |
| --- | --- |
| NFQUEUE with iptables | `nfnetlink_queue`, the `xt_NFQUEUE` target and the `xt_connbytes` match. `xt_conntrack` for selecting TCP packets by direction |
| NFQUEUE with nftables | `nfnetlink_queue` and `nft_queue`. `nft_ct` for the packet limits and for selecting TCP packets by direction |
| TUN | `/dev/net/tun` and the `iptables` command or the iptables-nft shim. `xt_connbytes` for capture by port, `xt_conntrack` for selecting TCP packets by direction, `iptable_raw` and `xt_CT` for forwarded traffic |
| Holding a dropped QUIC connection | `nf_conntrack_netlink`, with iptables and in TUN mode with `xt_connbytes`; with nftables for deleting the conntrack entry and for releasing held connections after a settings change, see [Dropped QUIC connections](#dropped-quic-connections) |
| DSCP with iptables | The `xt_DSCP` module, on OpenWrt in `kmod-ipt-ipopt` and `iptables-mod-ipopt`. The values of sets also need the `ipset` command and the `xt_set` module |
| DSCP with nftables | Nothing extra |
| Interception of DNS over TCP | The `REDIRECT` target in the `nat` table |

The **Kernel Modules** and **Kernel Capabilities** sections of [System Info](../settings/system.md#system-info) show which of these are present, and the packages that provide a missing one.

## Engine start and retries {#engine-start-and-retries}

b4 starts the packet engine before the web interface. When the engine fails, b4 removes the rules it had installed and keeps running without it:

- traffic passes through the router untouched;
- the web interface, the SOCKS5 proxy and the MTProto proxy stay up, and so do the rules of their **Expose to internet** switches;
- the other firewall rules, the routing sets and the watchdog stay off.

The reason is logged at the ERROR level, shown on the [dashboard](../dashboard.md#packet-engine-not-running) and on the **Packet Engine** card. For example, on a kernel without the queue modules iptables rejects the `NFQUEUE` target. A TUN engine with an automatic uplink finds no default route while the WAN or VPN is still down.

b4 retries by restarting itself in place: it shuts down as on a stop signal and starts again with the same command line, which reads the configuration file anew. The process ID stays the same, and a container runtime or a supervisor sees no exit.

| Retry | After |
| --- | --- |
| First | 15 seconds |
| Second | 30 seconds |
| Third | 60 seconds |

A cause that clears up by itself, such as a WAN link that comes up after b4, is picked up by one of these retries. After the third failed retry, b4 stays without the engine until it is restarted, from the web interface or by the service manager, which starts the count again. With the web server off, port `0`, b4 exits with the error instead.

## Keeping the rules in place {#firewall-monitor}

Router firmware rebuilds its firewall on events such as an interface coming up or a change of port forwarding, and removes the rules of b4 with it. At **Firewall Monitor Interval (seconds)** b4 checks its rules and puts back the missing ones. `SIGUSR1` starts the check at once.

- In NFQUEUE mode, `0` turns the check off, and removed rules stay removed.
- In TUN mode the interval is at least 10 seconds, and `0` turns nothing off. The TUN engine checks its capture chain `B4_TUN` and the jumps into it, and the monitor checks the masquerade, MSS clamp, DSCP and routing-set rules. With **Skip IPTables/NFTables Setup** on, only the capture rules are checked.

When TUN captures by port, the **Capture Rules** row of **System Diagnostics** compares the rules in the capture chain with the number b4 installed. After the first restore, the **Capture Rules Restored** row and, in either engine mode, the **Rules Restored by the Monitor** row count the restores and show the last one. A count that keeps growing points to another service on the router that rewrites the firewall.

## Rules of the Firewall sub-tab {#firewall-sub-tab-rules}

| Rule | iptables | nftables |
| --- | --- | --- |
| NAT Masquerade | Chain `B4_MASQ` in the `nat` table, jumped from the end of `POSTROUTING` | Table `ip b4_nat` |
| DSCP | Chain `B4_DSCP` in the `mangle` table, jumped from the top of `POSTROUTING`, above the capture jump of b4 | Table `inet b4_dscp` |
| MSS clamping | `TCPMSS` rules at the top of mangle `OUTPUT`, `FORWARD` and `B4_PREROUTING`; in TUN mode at the top of mangle `PREROUTING` instead of `B4_PREROUTING` | Rules at the top of `prerouting`, `output` and `forward` in `inet b4_mangle` |

Masquerading covers IPv4, and IPv6 only with iptables in NFQUEUE mode while **Enable IPv6 Support** is on. Packets b4 sends toward LAN clients keep their source address.

The DSCP jump sits above the capture jump because a packet that b4 inspects leaves `mangle POSTROUTING` at that jump, and a rule below it never sees the packet. When the kernel refuses the DSCP rule with iptables, b4 logs it once and leaves that address family without the value. With nftables, a refused table leaves both families without it. How sets write their own values is described under [DSCP](../sets/routing.md#dscp).

The MSS rules sit above the queue rules of b4 for the same reason: a SYN-ACK, or a SYN of the router itself, that b4 inspects never reaches a rule below them.

With iptables, a device in the **MSS** column that is matched by its MAC address can be matched only on the packets it sends. While the global clamp is off, b4 therefore also sets the smallest such value on every SYN-ACK it forwards from port 443, whichever device it goes to. A device added by hand is matched by its IP address in both directions.

## Expose to internet rules {#expose-to-internet-rules}

The **Expose to internet** switches, described in [Securing the web interface](../settings/security.md#expose-to-internet), add one accept rule per exposed port. The rule goes wherever the firewall of the host can drop the connection, whichever **Firewall Engine** b4 uses for its own rules.

- **iptables.** A chain `B4_EXPOSE` in the `filter` table holds the accept rules, and one jump in `INPUT` leads to it. The jump goes right below the last ban-list rule in `INPUT`, or at the top when there is none, so an address banned there stays banned on the ports of b4. Ban-list rules are jumps to fail2ban `f2b-*` chains, to CrowdSec, sshguard or `ufw-before-input`, and `DROP` or `REJECT` rules that match an ipset of source addresses. IPv4 rules go through `iptables` and IPv6 rules through `ip6tables`, wherever that variant already has a `filter` table.
- **nftables.** An `accept` goes at the top of every filter chain on the input hook in tables that b4 does not own, such as `inet fw4 input` on OpenWrt. Its comment reads like `b4-expose:mtproto`. A chain whose policy is `accept` and whose priority is below `filter` gets no rule; fail2ban, CrowdSec and banIP place their chains there, so the addresses they ban stay banned. In nftables an `accept` ends the path of a packet through one base chain only. A `drop` in any other chain on the hook is still final, so a rule in a table of b4 itself would leave the port closed.

A host with no such chain has nothing that drops the connection, and b4 adds no rule there. A listener bound to one address gets a rule for that address only, a listener on loopback gets none, and a listener that did not start gets none either.

The rules are checked with the other rules of b4, at **Firewall Monitor Interval (seconds)** and on `SIGUSR1`, whatever the packet engine does. A rule is added to an input chain that appeared since the last check. A save removes the rules of the ports it closes before the listeners take the new settings. Turning the switch off, stopping b4 or `b4 --clear-tables` removes the rule. At start, b4 removes rules left behind by a run that ended without cleaning up, unless **Skip IPTables/NFTables Setup** is on.

firewalld 2.2 and later marks its nftables table as owned where Linux 6.9 and nftables 1.1 support it, as on Fedora 42, RHEL 10 and Debian 13. The kernel then refuses changes from other programs. b4 recognises the refusal, tries that table again every 10 minutes instead of at every check, and logs the `firewall-cmd` commands that open the port in firewalld itself. `NftablesTableOwner=no` in `/etc/firewalld/firewalld.conf` turns the ownership off. The rule then sits at the top of the `filter_INPUT` chain of firewalld, above its zones, so an address blocked in a zone can still reach the exposed ports.

The DNS-over-TCP listener and the transparent-proxy listeners of proxy sets and of Telegram over WebSocket have no switch. They exist only as targets of the redirect and diversion rules of b4.
