---
sidebar_position: 3
title: Packet marks
---

A mark is a 32-bit number the kernel keeps with a packet. Firewall rules set it and match
it, `ip rule` picks a routing table by it, and a socket can put one on every packet it sends.
Conntrack keeps a second 32-bit mark for each connection. b4 uses marks to tell the packets
it sent itself from the ones it still has to handle, to send a set's traffic to its route or
to its own listener, and to keep its own connections out of its routing rules and, for some
of them, out of its packet processing.

Services that route by policy or proxy transparently use the same 32 bits: Xray and XKeen,
sing-box and mihomo, XrayUI, podkop, Tailscale, WireGuard, mwan3. When two of them use the
same bit, one can take the other's packets for its own, and a service that writes the whole
mark erases the bits the others put there. This page lists the bits b4 writes and reads, the
routing rules that go with them, and the values some other router services use.

## Reading a mark

| Form | Meaning |
| --- | --- |
| `0x5aef/0x27fff` | The bits covered by `0x27fff` equal `0x5aef`; the other bits are not compared |
| `0x8000/0x8000` | Bit `0x8000` is set, whatever the rest of the mark holds |
| `fwmark 0x5aef/0x27fff lookup 119` in `ip rule` | Packets whose mark matches `0x5aef/0x27fff` are routed by table 119 |
| `fwmark 0x111 lookup 111` in `ip rule` | No mask: the whole 32-bit mark has to equal `0x111` |

A rule that writes a mark with a mask changes only the bits under the mask and keeps the
rest. In iptables that is `MARK --set-xmark 0x5aef/0x27fff` or
`CONNMARK --save-mark --nfmask 0x27fff --ctmask 0x27fff`. nftables prints the same write as
`meta mark set meta mark & 0xfffddaef | 0x00005aef`: the AND keeps every bit outside
`0x27fff` (nft folds the value's own bits into that constant), and the OR writes `0x5aef`. A
rule without a mask, such as nftables `meta mark set 0x00021546` or iptables
`MARK --set-mark 0x111`, replaces all 32 bits and erases whatever another service wrote there.
`nft list` prints every value padded to eight digits, `0x00008000` for `0x8000`.

| Kind | Where it lives | Shown by |
| --- | --- | --- |
| Packet mark (fwmark) | One packet, from the rule or socket that set it until the packet leaves the router | `meta mark` in nftables, `-m mark` and `MARK` in iptables |
| Connection mark | The conntrack entry, shared by every packet of the connection in both directions | `ct mark`, `-m connmark` and `CONNMARK`, `mark=` in `/proc/net/nf_conntrack` |
| Socket mark (`SO_MARK`) | A socket; every packet it sends starts with this packet mark | `fwmark:` in the output of `ss -e`, run as root on kernel 4.9 or later (4.10 for raw sockets) |

## The bits b4 uses

### Packet mark

| Bits | Mask | Used for | Value |
| --- | --- | --- | --- |
| 0-14, 17 | `0x27fff` | The route of a routing set | One per set; sets routed through the same interface with the same egress IP and kill switch share one. See [sets routed through an interface](#sets-routed-through-an-interface) and [proxy sets](#proxy-sets-and-telegram-over-websocket) |
| 15 by default | `0x8000` by default | The queue mark: packets b4 sends from its raw sockets, except those toward a device in TUN mode, the DNS queries it sends for clients and for its sets, and its probes that bypass its own processing | **Packet Mark** under **Settings, Core, Packet Engine**, `0x8000` (32768) by default |
| 18 | `0x40000` | Connections b4 opens to the upstream of a proxy set, to Telegram, the Community Hub, ipinfo and RIPEstat, and its transparent listeners; see [socket marks](#socket-marks) | Fixed |
| 21 | `0x200000` | b4's own connections whose outgoing packets packet processing leaves alone | Fixed, carried together with bit 18 as `0x240000` |
| 24 | `0x1000000` | A proxy set's TCP packet that the router sent itself | Fixed, added to the set's mark |
| 28 | `0x10000000` | TUN mode: packets b4 writes back | Fixed, added to the queue mark |
| 29 | `0x20000000` | Packets b4 sends toward a device on the network | Fixed; added to the queue mark in NFQUEUE mode, and to Discovery's injected mark on the packets a Discovery run sends back toward its probes |
| 30 | `0x40000000` | TUN mode: packets steered into `b4tun0`; unused when the default route itself points at `b4tun0` | Fixed |
| all | `0xffffffff` | Discovery, only while a run is active | By default the queue mark plus 1 and plus 2, `0x8001` and `0x8002`; `system.checker.discovery_flow_mark` and `system.checker.discovery_injected_mark` replace them |

With the default queue mark and outside a Discovery run, b4 puts no other bit on a packet and
its firewall rules test no other bit: bits 16, 19, 20, 22, 23, 25, 26, 27 and 31 stay free.
The rules that write the whole mark, those of proxy sets on nftables and of Discovery, clear
them on the packets they take. Bit 16 is left free for XrayUI's `0x10000`.

### Connection mark

| Bits | Mask | Used for |
| --- | --- | --- |
| 30 | `0x40000000` | The connection belongs to a set routed through an interface |
| 0-14, 17 | `0x27fff` | The mark of that set, copied from the connection's first packet |
| 15 by default | The queue mark, `0x8000` by default | NFQUEUE mode, on iptables when the connmark module is available: a packet carrying the queue mark left the router on this connection, including a fake or split segment b4 sent into a device's connection. b4's queue rules for packets entering the router skip such connections |
| all | `0xffffffff` | Discovery's probe connections, while a run is active |

### Socket marks

| Sockets | Mark |
| --- | --- |
| Raw sockets for fakes, split segments and packets b4 sends back out | The queue mark, `0x8000` by default; in TUN mode the queue mark with `0x10000000` added, `0x10008000` by default |
| Raw sockets for packets toward a device: DNS answers b4 builds, resets, ICMP; in TUN mode also the DNS answers and reply packets it captured and writes back | The queue mark with `0x20000000` added, `0x20008000` by default; `0x20000000` alone in TUN mode |
| The DNS queries b4 sends for clients (a set's DNS redirect, DNS over TCP) and its lookups through a set's resolver, including the lookup of a DoH resolver's host name for either | The queue mark |
| The DPI Detector's checks, except its fetches through b4 and its lookups through the system resolver; health probes of pinned DNS answers and of unreachable addresses; the public address lookup; the fetches that bypass b4 in the MCP tool `b4_test_domain_now` and in the test of an applied Community Hub set | The queue mark |
| Transparent listeners of proxy sets and every connection they open, to the upstream or, with **Fall back to direct on upstream failure**, directly; TCP connections of b4's SOCKS5 server to a domain name a proxy set targets, which go through the set's upstream; connections of the MTProto proxy and Telegram over WebSocket to Telegram and the address lists they download; the MTProto proxy's forward to its fake SNI domain; Community Hub, ipinfo and RIPEstat requests | `0x40000`. When a Community Hub sync cannot connect, b4 retries it with the queue mark; if that retry connects, later Hub requests keep the queue mark until a sync cannot connect with it and a retry with `0x40000` can |
| Connections through Cloudflare-proxied domains or a custom WebSocket domain, and to a Cloudflare Worker | `0x240000`; `0x40000` for the Worker when **Let sets process Worker connections** is on |
| Discovery probes and their name lookups; the packets Discovery sends | The flow mark; the injected mark, with `0x20000000` added on packets sent back toward the probes. By default the queue mark plus 1 and plus 2, and `0x20008002` toward the probes |
| Watchdog checks; the fetches through b4 of the DPI Detector, `b4_test_domain_now` and the Hub set test; connections b4's SOCKS5 server opens to other destinations; every other lookup through the system resolver, including the names of the other connections above and a set's domains when the set has no resolver of its own, or when its resolver fails and **Fail closed when the resolver is unreachable** is off; GeoIP and GeoSite downloads, update checks and downloads, the device vendor database, AI assistant requests | None |

A connection with no mark is handled like any connection the router opens: it goes through
packet processing and through every set that carries the router's own traffic.

## What b4 leaves alone

| b4 rules | A packet is left alone when |
| --- | --- |
| Packet processing, nftables (`inet b4_mangle`) | `b4_chain`, the rules for outgoing ports, returns it when its mark has every bit of the queue mark, its bits under `0x27fff` equal `0x24bab`, or it has bit `0x200000`, and during a Discovery run when its mark is exactly the flow or injected mark; `output` accepts a packet with every bit of the queue mark before its jump there. `prerouting`, with the DNS rules and the rules for replies, tests no packet mark and skips connections whose connection mark has every bit of the queue mark |
| Packet processing, iptables (`B4`, `B4_PREROUTING`) | `B4` returns it when its bits under `0x27fff` equal `0x24bab` or the mark of a proxy set, or it has bit `0x200000`, and during a Discovery run when its mark is exactly the flow or injected mark. `B4` has no rule for the queue mark: mangle `OUTPUT` accepts a packet the router sends with every bit of the queue mark before its jump to `B4`, but mangle `POSTROUTING` jumps to `B4` as well, or, when [device filtering](/docs/settings/core#device-filtering) selects devices, mangle `FORWARD` does for their packets. So a forwarded packet with the queue mark, and without device filtering one the router sends, still meets the queue rules of `B4` for outgoing ports. `B4_PREROUTING` and the DNS rules test no packet mark; with the connmark module, `B4_PREROUTING` skips the same connections as `prerouting` on nftables |
| The queue itself, both backends | b4 releases a packet unchanged when its mark has bit `0x200000`, has `0x24bab` under `0x27fff`, or has every bit of the queue mark with the rest within `0x27fff` and is not exactly one of Discovery's two marks |
| Routing sets other than block sets, packets entering the router (`b4r_*_pre`), including the router's own packets a proxy set loops through `lo` | Its mark has every bit of the queue mark or bit `0x40000`, or its bits under `0x27fff` are not zero and, in a proxy set's chain, differ from that set's own mark |
| Routing sets, the router's own packets (`b4r_*_out`) | Its mark has bit `0x40000` or any bit under `0x27fff`. A packet with every bit of the queue mark is left alone as well, after a set routed through an interface has given it the set's mark when its destination is in the set; a set that leaves the router's own traffic alone and lists its devices only by IP address does this only for packets from those addresses |
| QUIC refusal of a proxy set with **Route UDP through upstream** off (`b4r_*_q`) | Its mark has every bit of the queue mark, or bit `0x40000` |
| Block sets | Never: they test no mark, only the destination and, when the set has them, its source interfaces and source devices |
| TUN capture (`B4_TUN`) | Its mark has every bit of the queue mark, bit `0x20000000` or bit `0x200000`, or its bits under `0x27fff` equal `0x24bab`; on iptables also when its destination is in a routing set other than a block set. A packet to a network in which the router has an IPv4 address of its own is left alone as well, unless it is UDP DNS or a TCP reset the chain captures; while b4 captures only the router's own connections, such a packet is always left alone, except a UDP DNS query to the uplink gateway |
| TUN on the whole default route | Only packets with bit `0x10000000`, and with bit `0x20000000` when `ip` accepts `suppress_prefixlength`, leave by b4's rules; every other packet that follows the default route enters `b4tun0` |
| **NAT Masquerade** (`b4_masq`, `B4_MASQ`) | Its mark has bit `0x20000000`. On iptables that `RETURN` sits at the top of nat `POSTROUTING`, so such a packet skips every later rule there, other services' included |
| **Set DSCP** (`B4_DSCP`, `inet b4_dscp`) | Its mark has bit `0x20000000`, it leaves through `lo`, or it travels in the reply direction of its connection. On iptables the jump to `B4_DSCP` sits at the top of mangle `POSTROUTING`; on nftables `inet b4_dscp` runs in postrouting at priority 150 |
| DNS over TCP redirect | Its mark has every bit of the queue mark |
| Discovery, during a run | At the top of the chain, a packet whose mark is exactly the injected mark is accepted, and a packet whose mark or connection mark is exactly the flow mark goes to Discovery's queue; either way it skips the rest of the chain. On iptables that is the built-in mangle `PREROUTING` and `OUTPUT`, so it skips every later rule there |

## What b4 writes on packets it did not create

| b4 rules | What they write |
| --- | --- |
| Sets routed through an interface | The packet mark's bits under `0x27fff`; the connection mark's bits under `0x27fff` and bit `0x40000000`. Masked, the other bits stay |
| Proxy sets, iptables | The packet mark's bits under `0x1027fff`. Masked |
| Proxy sets, nftables | The whole packet mark |
| Packet processing | The queue-mark bits into the connection mark of every connection on which the router itself sends a packet with every bit of the queue mark, a device's connection that b4 sends fakes or split segments into included. NFQUEUE mode only; on nftables not for packets sent to `lo`, on iptables only with the connmark module. Masked |
| TUN capture | Bit `0x40000000` of the packet mark. Masked |
| Discovery, during a run | The whole packet mark of a packet whose connection mark is exactly the flow mark, and the whole connection mark of a connection that carries a packet whose mark is exactly the flow mark; both get the flow mark |

## The queue mark

The queue mark is the **Packet Mark** setting under **Settings, Core, Packet Engine**, `queue.mark` in the
[configuration file](/docs/advanced/config#the-queue-section) and `--mark` on the command
line. The default is `0x8000` (32768); the web interface takes the value in decimal, and `0`
stands for the default. b4 sends every fake, split segment and packet it puts back on the
wire from raw sockets carrying this mark, with bit `0x10000000` added in TUN mode. The
packets toward a device in TUN mode carry `0x20000000` alone, and those of a Discovery run
carry Discovery's injected mark, with `0x20000000` added on the packets it sends back toward
its probes. The DNS queries b4 sends for clients and for its sets carry
the queue mark too; its lookups through the router's own resolver carry no mark. Every rule
below tests all bits of the queue mark at once, so other bits on the same packet do not hide
it.

| Rule | nftables | iptables |
| --- | --- | --- |
| Router's own packets | `output`: `meta mark & 0x8000 == 0x8000 ct mark set ct mark \| 0x8000`, then `... accept` | mangle `OUTPUT`: `-m mark --mark 0x8000/0x8000 -j CONNMARK --save-mark --nfmask 0x8000 --ctmask 0x8000` (with the connmark module), then `... -j ACCEPT` |
| Queue chain | `b4_chain`: `meta mark & 0x8000 == 0x8000 return` | none; b4 releases such a packet from the queue unchanged when the rest of its mark lies within `0x27fff` and the mark is not one of Discovery's two marks |
| Packets entering the router | `prerouting`: `ct mark & 0x8000 == 0x8000 return` | `B4_PREROUTING`: `-m connmark --mark 0x8000/0x8000 -j RETURN` (with the connmark module) |
| Routing sets other than block sets | `meta mark & 0x8000 == 0x8000 return` | `-m mark --mark 0x8000/0x8000 -j RETURN` |
| Sets routed through an interface, router's own packets | `meta mark & 0x8000 == 0x8000 ip daddr @<set> meta mark set ...` | `-m mark --mark 0x8000/0x8000 -m set --match-set <set> dst -j MARK --set-xmark <mark>/0x27fff` |
| DNS over TCP redirect, while it is on | `meta mark & 0x8000 == 0x8000 return` | `B4_DNSTCP`: `-m mark --mark 0x8000/0x8000 -j RETURN` |
| TUN capture | - | `B4_TUN`: `-m mark --mark 0x8000/0x8000 -j RETURN`, written with `iptables` on both backends |

The rule for sets routed through an interface sends the fakes and split segments of a
connection such a set routes out of the same interface as the connection itself. It
matches the destination, and the source address only in a set limited to source devices
that are all given by IP address, so it does the same for the injected packets of
connections to the set's destinations that the set does not route, such as those from a
device outside its source interfaces. The source address is matched per address family: in a
family for which such a set lists no device address, the rule matches the destination alone,
so with **Enable IPv6 Support** on, a set whose devices are all given by IPv4 address gives its mark
to every queue-marked IPv6 packet to its destinations.

Saving a changed **Packet Mark** moves the packet processing rules, the rules of routing sets
and the DNS over TCP redirect to the new value at once and removes those of the old value.
While a Discovery run is active, the packet processing rules and the redirect wait for it to
end; after a run that ends more than five minutes after the save they move at the next check
of the firewall monitor on iptables, and at the next start of b4 on nftables or with the
monitor off (`system.tables.monitor_interval` at `0`). Until they move, b4 queues its own DNS
queries, which already carry the new value, like those of a device, so the DNS redirect of a
set can stop applying. The raw sockets, b4's release of its own packets from the queue and,
in TUN mode, the `B4_TUN` chain take the new value at the next start of b4, which the web
interface asks for. Discovery's two marks follow the new value unless `system.checker` sets
them to something other than the **Packet Mark** plus 1 and plus 2. Before 1.84.0 the web
interface sent them back with every save, so a save that changed the **Packet Mark**, or one
made while `--mark` was in effect, could leave them there at values that no longer follow it,
such as the old value plus 1 and plus 2. Deleting `discovery_flow_mark` and
`discovery_injected_mark` from `system.checker` while b4 is stopped lets them follow again.

b4 refuses a value that:

- is made only of bits of `0x240000`, the marks of its own connections;
- is made only of bits of `0x27fff` and `0x1000000`, the bits of set marks;
- overlaps `0x70000000` in TUN mode;
- has bit `0x20000000` in NFQUEUE mode while **NAT Masquerade** or **Set DSCP** is on;
- has `0x24bab`, the Telegram over WebSocket mark, as its bits under `0x27fff`;
- equals the flow or injected mark of Discovery;
- is above `0xffffffff`, or, while either Discovery mark is left out of the configuration, is
  above `0xfffffffd` or gives that mark (the value plus 1 for the flow mark, plus 2 for the
  injected mark) bit `0x200000` or the value of the other Discovery mark.

A packet another service marks with every bit of the queue mark reads to b4 as one it sent
itself. Proxy sets, Telegram over WebSocket and the QUIC refusal leave it alone. A set
routed through an interface gives it the set's mark when the router sends it to an address
in the set and its mark has no bit under `0x27fff` and not `0x40000`, unless the set is
limited to source devices that are all given by IP address, and a block set rejects it by
destination as usual. The nftables queue chain and TUN capture skip it whatever the rest of
its mark holds. In NFQUEUE mode, when the router sends it, b4 also copies the queue-mark
bits into its connection mark (on nftables not for packets sent to `lo`, on iptables only
with the connmark module), so its replies are skipped as well. A packet that reaches the
queue anyway, on iptables through `B4` and on both backends through the rules for packets
entering the router, which test only the connection mark, is released unchanged only when
the rest of its mark lies within `0x27fff`, or when the mark has bit `0x200000` or `0x24bab`
under `0x27fff`. wg-quick's `51820` (`0xca6c`), which it puts on
the WireGuard socket when a peer routes `/0`, has bit 15 set, and the rest of it, `0x4a6c`,
lies within `0x27fff`.

## Sets routed through an interface

A set routed through an [output interface](/docs/sets/routing#output-interface) gets a mark
and a routing table from the interface name, the egress IP and the kill switch. Sets that
agree on all three share both.

| Item | Value |
| --- | --- |
| Mark | From a hash of the three, in `0x100`-`0x7eff` and never equal to the queue mark's bits under `0x27fff`; if every hashed candidate is taken, counted up from `0x66` instead |
| Table | `100`-`249`, skipping tables named in `rt_tables`, looked up by another service's rule, or holding routes b4 did not add |
| Rule | `ip rule add fwmark <mark>/0x27fff lookup <table> priority <10000 + table>`, for IPv4, and for IPv6 when **Enable IPv6 Support** is on |
| Table contents | A default route through the interface, plus `blackhole default metric 4096` with the [kill switch](/docs/sets/routing#kill-switch) |
| Pinned values | `routing.fwmark` and `routing.table` in the configuration file or through the API, used only when both are set, the mark lies within `0x27fff`, is not `0x24bab`, does not contain every bit of the queue mark and does not equal its bits under `0x27fff` |

The set marks the first packet of each connection to its destinations and saves the mark in
the connection mark, with bit `0x40000000` as b4's claim. Later packets of that connection
sent in the direction of its first packet get the mark back from the connection mark;
replies get no mark. The set's chain for packets entering the router, `b4r_<set>_pre`, in
this order:

1. Returns packets that carry the queue mark, bit `0x40000`, or any bit under `0x27fff`.
2. Returns packets that arrive on the output interface itself.
3. Restores the mark from the connection mark on later packets of claimed connections that
   travel in the direction of the first packet.
4. Marks new connections to the set's destinations and saves the mark with the claim.

A set that excludes source devices returns their packets before step 2, only the packets of
its devices enter the chain of a set limited to source devices, and a set limited to source
interfaces applies steps 3 and 4 only to packets arriving on them.
[Device filtering](/docs/settings/core#device-filtering) under **Settings, Core, Devices** narrows the
chain the same way. `b4r_<set>_out` does the same for the router's own connections when the
set carries them, and gives the set's mark to b4's own queue-marked packets addressed to the
set; a set limited to source devices that are all given by IP address does this only for
packets from those addresses. Masquerade matches the
set's mark and the output interface; with an egress IP present on the interface, SNAT to
that address takes its place and also requires the destination to be in the set.

On a router whose firmware overwrites the connection mark b4 writes, the restore never
matches. b4 on iptables notices this while the firewall monitor runs: when several
connections have been claimed, the restore has never matched, and no answered connection in
`/proc/net/nf_conntrack` still carries bit `0x40000000`, on two checks in a row, b4 writes
`0` to `.conntrack-mark` next to the configuration file. From then on, and at every later
start on either backend until the file is deleted, the set marks every packet to its
destinations that has no bit under `0x27fff`, not only the first.

While a set uses an output interface whose effective `rp_filter` is `1`, b4 writes `2` to
`net.ipv4.conf.<interface>.rp_filter`, and writes the old value back when the last set on
that interface goes.

On a test box the two sets on `wg0` got:

```text
10119:  from all fwmark 0x5aef/0x27fff lookup 119
10123:  from all fwmark 0x69cf/0x27fff lookup 123
```

## Proxy sets and Telegram over WebSocket

Sets in the *Upstream SOCKS5 proxy* and *Telegram over WebSocket* routing modes, and the
[Telegram over WebSocket](/docs/telegram/websocket-bridge) switch, divert connections to
transparent listeners of b4 with TPROXY. Each has a mark, and the mark gives the listener
port.

| Item | Value |
| --- | --- |
| Mark | `0x20000`-`0x27dff`, from a hash of the set's ID, or the value after it when the hash equals the queue mark's bits under `0x27fff`; `routing.fwmark` replaces it when it lies within `0x27fff`, is not `0x24bab` and does not equal the queue mark's bits under `0x27fff` |
| Telegram over WebSocket switch | `0x24bab`, port `13443` |
| Listener port | `13000 + mark % 50000` |
| Rule, priority 3 | `fwmark <mark>/0x27fff lookup 252`, for IPv4, and for IPv6 when **Enable IPv6 Support** is on; table 252 holds `local default dev lo` and is shared by every proxy set. When 252 belongs to another service, b4 takes 251, 250, then 300-399 |
| Rule, priority 2 | `fwmark <mark>/0x1027fff iif lo lookup main`, IPv4 |
| Router's own packets | `<mark> + 0x1000000`, TCP in the original direction; none for a set limited to source devices or source interfaces |

`b4r_<set>_pre` returns packets that carry the queue mark, bit `0x40000`, or bits under
`0x27fff` other than the set's own mark, and packets to local, broadcast, multicast and IPv6
link-local addresses. Of the packets to the set's destinations, it then marks TCP packets of
connections already held by a transparent socket and accepts them, and hands the other TCP
packets, and UDP ones when **Route UDP through upstream** is on, to TPROXY with the set's
mark; a set limited to source interfaces hands over only packets arriving on them. Source
devices limit this chain as they do for
[sets routed through an interface](#sets-routed-through-an-interface). iptables writes the
mark under the mask `0x1027fff`; nftables writes the whole mark.

The router's own connections reach a proxy set by a loop, unless the set is limited to
source devices or source interfaces. `b4r_<set>_out` marks their TCP packets in the original
direction with the set's mark plus bit `0x1000000`, the priority 3 rule sends them to `lo`,
and they come back through prerouting, where TPROXY takes them. Once new connections to the
set's destinations exceed 200 per second (burst 400), `b4r_<set>_out` leaves the first
packet of each further connection unmarked, but marks its later TCP packets in the original
direction as usual.

With `src_valid_mark` at `1`, in `net.ipv4.conf.all` or on the interface a connection
arrives on, the kernel checks the source of every diverted connection in the table its mark
selects, and table 252 answers every address with the router itself. The priority 2 rule
sends that check to `main`. Bit `0x1000000` keeps the router's own packets out of it, so
they still loop through `lo`. XrayUI sets `net.ipv4.conf.all.src_valid_mark` to `1` when Xray
has a WireGuard outbound, wg-quick and awg-quick when a peer takes the IPv4 default route,
and Tailscale in its default netfilter mode.

b4 also adds an accept for each proxy mark at the top of the input chain: on iptables
`-I INPUT 1 -m mark --mark <mark>/<mark> -j ACCEPT`, through ip6tables as well and through
the `-legacy` variants when they are installed; on nftables
`meta mark & <mark> == <mark> accept` at the top of `inet fw4 input`, or of
`inet filter input` when fw4 is absent, and nowhere when neither chain exists. Installing a
proxy set writes `0` to `net.ipv4.conf.lo.rp_filter` and `2` to
`net.ipv4.conf.all.rp_filter`; b4 does not write the old values back.

On a test box the Telegram over WebSocket switch and one proxy set got the following; the
other proxy set and a set in the Telegram over WebSocket mode each added the same pair of
rules with its own mark:

```text
2:      from all fwmark 0x24bab/0x1027fff iif lo lookup main
2:      from all fwmark 0x21546/0x1027fff iif lo lookup main
3:      from all fwmark 0x24bab/0x27fff lookup 252
3:      from all fwmark 0x21546/0x27fff lookup 252
```

## Connections b4 opens itself

b4 opens these connections with `0x40000`: to the upstream of a proxy set and the direct
connections a proxy set opens in its place, to Telegram, for the address and domain lists
the Telegram features download, the MTProto proxy's forward to its fake SNI domain, and
requests to the Community Hub, ipinfo and RIPEstat. The chains of interface, proxy and
Telegram over WebSocket sets return on it, so none of these sets routes such connections and
they never loop into b4's own listeners; they follow the rest of the router's routing rules,
normally the main table. Block sets still block them, and in TUN mode they can still pass
through `b4tun0`.

Packet processing does not skip `0x40000`: a set whose targets match the destination applies
its strategy to b4's own connections as it does to a device's. A set in a proxy or Telegram
over WebSocket mode, or one routed into a TUN, TAP or WireGuard tunnel that is up, lets them
through unmodified instead, and since its routing chains skip them, they leave by the main
route with no strategy at all.

`0x240000` adds bit `0x200000`, and packet processing skips packets that carry it. The
MTProto proxy and Telegram over WebSocket put it on their connections through
Cloudflare-proxied domains or a custom WebSocket domain, and to a Cloudflare Worker unless
**Let sets process Worker connections** is on; see
[DPI processing](/docs/telegram/websocket-bridge#dpi-processing).

Lookups and probes carrying the queue mark skip packet processing, and proxy sets leave
them alone. The DPI Detector's direct mode uses the queue mark to measure the network
without b4's processing. A set routed through an interface gives such packets its mark when
their destination is in the set, as it does to the packets b4 injects, so they leave by that
set's interface; a set limited to source devices that are all given by IP address does this
only for the packets b4 injects for those devices. Lookups through the router's own resolver
carry no mark, and a service that exempts b4 by its marks still sees them as ordinary
router traffic.

In TUN mode, when the default route itself points at `b4tun0`, the router's own packets with
the queue mark, `0x40000` or `0x240000` enter `b4tun0` like any other whenever they follow
the default route. b4 reads them there without their mark, so neither the queue mark nor bit
`0x200000` takes them out of packet processing; in TUN mode they do so only with port
capture, where `B4_TUN` returns such packets.

## TUN mode

In TUN mode (`queue.mode: "tun"`) packets reach b4 through a TUN device, `b4tun0` unless
`queue.tun.device_name` says otherwise, instead of a queue. These rules are written with
`iptables` also on the nftables backend, and cover IPv4 only.

| Item | Rule |
| --- | --- |
| Capture | Chain `B4_TUN`, jumped from mangle `OUTPUT` and `PREROUTING` (from `PREROUTING` through `B4_TUN_GATE` when [device filtering](/docs/settings/core#device-filtering) selects devices), or only from `OUTPUT` while b4 captures only the router's own connections (see [Packet engine](/docs/settings/core#packet-engine)): sets `0x40000000/0x40000000` on UDP DNS queries and answers, on the first packets of connections to the capture ports (on every packet of them where `xt_connbytes` is missing), on every TCP packet to the capture ports of a set's packet duplication addresses, and on TCP resets from the capture ports while a set has **RST Injection Protection** on or an escalation target |
| Rule | `fwmark 0x40000000/0x40000000 lookup <table>` at priority 10, or one less than the lowest rule another service has at 5-9, but not below 4 |
| Table | `default dev b4tun0`; chosen from 96 down to 61, skipping 77 and tables in use, or next to `queue.tun.route_table` |
| Packets b4 writes back | Socket mark queue mark + `0x10000000`, not tracked by conntrack (`raw OUTPUT ... -j CT --notrack`) unless `system.tables.skip_setup` is on or the kernel has no raw table or no `CT` target. When `system.tables.skip_setup` is off and b4 cannot install this rule, the one for packets toward a device or the SNAT into `b4tun0`, it captures only the router's own connections |
| Packets toward a device | Socket mark `0x20000000`, not tracked by conntrack under the same conditions |

When `xt_connbytes` is not available, b4 counts the packets of each connection itself and
processes only the first ones, as the match would. While b4 captures only the router's own
connections it keeps port capture, and `B4_TUN` marks every packet of the connections to the
capture ports; otherwise b4 points the default route itself at `b4tun0` instead. No setting
selects either mode. With the default route at `b4tun0`, b4's own packets leave through
rules 88 and 89
(`0x20000000`) and 99 and 100 (`0x10000000`): the first of each pair looks in `main`
without its default route, the second in the uplink table, which holds the uplink's default
route and is chosen from 97 down to 62 in the same way, or is `queue.tun.route_table`
itself. Rules 88, 89 and 99 need an `ip` that accepts `suppress_prefixlength`; without it
only rule 100 is added.

## Discovery

While a Discovery run is active, including runs the watchdog or the MCP server start, a
chain at the top of b4's `prerouting` and `output` chains in `inet b4_mangle`
(`b4_discovery`, nftables) or of mangle `PREROUTING` and `OUTPUT` (`B4_DISCOVERY`, iptables
and ip6tables) steers its probe connections to a queue of their own, numbered after the main
queues (541 with the defaults). Discovery uses this queue in TUN mode as well. The probes
carry the flow mark, the queue mark plus 1 by default; the packets Discovery sends carry the
injected mark, the queue mark plus 2. Both are compared as whole 32-bit values, and the flow
mark is copied between the packet and the connection whole. They are set by
`system.checker.discovery_flow_mark` and `system.checker.discovery_injected_mark` in the
configuration file. The default values have bits under `0x27fff`, so no routing set routes
Discovery's traffic. For the length of a run, b4's queue chain, `b4_chain` on nftables and
`B4` on iptables and ip6tables, returns a packet whose mark is exactly the flow or injected
mark as well, so packet processing leaves Discovery's packets alone on their way out of the
router.

During a run, a connection of another service whose mark is exactly the flow mark goes to
Discovery's queue too, and a packet whose mark is exactly the flow or injected mark skips the
rest of these chains; on iptables that includes every rule below them in mangle
`PREROUTING` and `OUTPUT`, other services' rules as well.

## Routing rules and tables

| Priority | Rule | Table | Present for |
| --- | --- | --- | --- |
| 2 | `fwmark <mark>/0x1027fff iif lo` | `main` | Proxy sets and Telegram over WebSocket, IPv4 |
| 3 | `fwmark <mark>/0x27fff` | 252; 251, 250, 300-399 | Proxy sets and Telegram over WebSocket |
| 4-10 | `fwmark 0x40000000/0x40000000` | 96 down to 61, or next to `queue.tun.route_table` | TUN mode with port capture, IPv4 |
| 88, 89 | `fwmark 0x20000000/0x20000000` | `main` without its default route, then the uplink table: 97 down to 62, or `queue.tun.route_table` | TUN mode on the whole default route, IPv4 |
| 99, 100 | `fwmark 0x10000000/0x10000000` | The same | TUN mode on the whole default route, IPv4 |
| 10000 + table | `fwmark <mark>/0x27fff` | 100-249, or the pinned `routing.table` | Sets routed through an interface |

The kernel walks the rules from the lowest priority number up and stops at the first one
that gives a route. A rule added without a priority gets the priority of the rule after
`local`, minus one; on kernels before 4.3 an IPv6 rule gets 16383 instead. Added after b4's
priority 2 rule, such a rule gets priority 1 and sits above every rule of b4; on a system
with only the standard rules it gets 32765 and sits below them. XKeen, XrayUI and OpenClash
outside their TUN modes, MagiTrickle, mihomo in iptables mode, the Xray guides, wg-quick and
awg-quick all add their rules that way, so their rules sit above b4's when they are added
while b4 is running, a restart of that service included. Added before b4 starts, they sit
below b4's only when no rule other than the standard ones was there: next to Tailscale's
5210, for example, such a rule gets 5209, above b4's interface sets at 10100-10249. On
kernels before 4.3 their IPv6 rules get 16383 and sit below b4's either way.

## Next to other services

- A mark with any bit under `0x27fff` on a packet before b4's routing chains keeps interface
  and proxy sets off that packet; only a proxy set still takes a packet whose bits under
  `0x27fff` equal its own mark. Block sets and the QUIC refusal of a proxy set do not look at
  those bits. An upstream proxy running on the router is kept out of a set that matches its
  server this way, for example with Xray's `sockopt.mark` at `255`. Packet processing still
  applies to its connections unless the mark is one of those in the next point, or its bits
  under `0x27fff` equal `0x24bab` or, on iptables, the mark of a proxy set.
- A mark with every bit of the queue mark reads as b4's own packet. Such a packet that still
  reaches the queue, on iptables through `B4` and on both backends through the rules for
  packets entering the router, is released only when the rest of its mark lies within
  `0x27fff`. A mark with bit `0x200000` reads as one packet processing leaves alone.
- On nftables, b4's routing chains run at priority `mangle - 1` (-151), ahead of chains at
  the mangle priority (-150): b4 marks first, and a service that later writes the whole mark
  replaces b4's, so the packet follows whichever rule matches the new value. A packet of the
  router itself is rerouted only when that rewrite happens in a chain of type `route`, as
  iptables mangle `OUTPUT` is.
- On iptables, b4 inserts its routing jumps at the top of mangle `OUTPUT`. In mangle
  `PREROUTING` it puts them right above its own `B4_PREROUTING` jump, which it inserts at the
  top when it installs its rules, or above an earlier rule of another service that matches
  local sockets; with neither, as in TUN mode, it appends them, and it moves them back when
  they end up below either. A rule another service inserts at the top afterwards runs first,
  unless it matches local sockets.
- On iptables in NFQUEUE mode, a packet b4 releases from its queue skips every later rule of
  the built-in mangle chain it was queued from, so another service's rules lower in that
  chain never see it. That covers the UDP packets to or from port 53 that `B4_PREROUTING`
  and b4's rules near the top of mangle `OUTPUT` queue unless the packet or its connection
  carries the queue mark, such as the DNS the Xray guides take with TPROXY, and the first
  packets of connections on b4's ports. b4's fakes and split segments carry only the queue
  mark, and b4 accepts them near the top of mangle `OUTPUT`, so a service whose rules come
  later in that chain, such as mwan3 or KVAS, never marks them either: for a connection such
  a service routes by its mark, they follow the router's main routing instead. In TUN mode
  the fakes and split segments carry bit `0x10000000` as well, mangle `OUTPUT` has no such
  accept, and those rules see them.
- When b4 picks a table itself, it skips one it has not claimed yet if the table is named in
  `rt_tables`, looked up by another rule, or holds routes b4 did not add. A table pinned with
  `routing.table` is used as given only together with `routing.fwmark`, and a
  `queue.tun.route_table` that is taken is refused. Some services use fixed table numbers in
  b4's ranges without such a check: XKeen 111, podkop 105, the Xray guides 100 and 106,
  XrayUI 250 in TUN mode. Started after b4 has taken one of them, XKeen flushes the table and
  adds its own routes, and XrayUI replaces b4's route in it; podkop and the Xray guides fail
  to add their route (`File exists`) while b4's default route is in the table. XKeen, podkop
  and XrayUI flush the table when they stop, and XrayUI also deletes every rule that looks up
  250, b4's priority 3 rule included when b4 uses that table.

Values used by other services, read from their source code:

| Service | Marks | Rules and tables |
| --- | --- | --- |
| [Xray-core](https://github.com/XTLS/Xray-core) 26.3.27 | `streamSettings.sockopt.mark`: the whole socket mark of an outbound, only when set; no firewall rules of its own. The [transparent proxy guides](https://github.com/XTLS/Xray-docs-next) write `1`, whole value, with TPROXY and on the router's own TCP and UDP in `OUTPUT`, and exempt Xray's own traffic only by an exact mark of `2` or `255` | Guides: `fwmark 1 lookup 100`, and `fwmark 1 lookup 106` for IPv6, no mask, no priority |
| [XKeen](https://github.com/jameszeroX/XKeen) 2.0.1 Beta | TPROXY and `MARK` `0x111`, whole value, in TProxy mode and for UDP in Hybrid mode; Redirect mode and TCP in Hybrid mode use nat `REDIRECT` without a mark. `CONNMARK` saves and restores the whole mark. With router traffic on (`proxy_router`), takes the router's own TCP and UDP the same way, except a mark of exactly `255`, a Keenetic policy mark, or bit `0x40000000` | `fwmark 0x111 lookup 111`, no priority |
| [XrayUI](https://github.com/DanielLavrushin/asuswrt-merlin-xrayui) 0.70.2 | `0x10000/0x10000` in `DIVERT`, TPROXY and the connection mark | `fwmark 0x10000/0x10000 lookup 77`, no priority; TUN mode: 49 `from <LAN> to <LAN> lookup main` and 51 `from <LAN> lookup 250`, no mark |
| [sing-box](https://github.com/SagerNet/sing-box) 1.14.2 | `route.default_mark`, `routing_mark`: socket marks. TUN `auto_redirect`: `0x2023` and `0x2024`, whole values, on the packet and connection mark of UDP and ICMP and, when NFQUEUE 100 is available, of TCP connections it judges on the first packet; `0x2025` on a TCP first packet it resets; every outbound socket `0x2024` | TUN `auto_route`: 9000-9010 to table 2022; with `auto_redirect` instead 9000 `fwmark 0x2024`, 9001 `fwmark 0x2023 lookup 2022`, 9002, and 32768 `not fwmark 0x2024 lookup 2022` |
| [mihomo](https://github.com/MetaCubeX/mihomo) 1.19.32 | `routing-mark`: socket mark, `2158` when iptables mode is on and it is unset. iptables mode: `0x2d0` (TPROXY under `0x2d0`, `MARK` whole). TUN `auto-redirect` writes `0x2023` and `0x2024`, and puts `0x2024` on every outbound socket, only with `route-address-set` or `route-exclude-address-set` | TUN: 9000-9010 to table 2022, and 32768 to table 2022 when `auto-redirect` writes marks; iptables mode: `fwmark 0x2d0 lookup 720`, IPv4 only, no priority |
| [OpenClash](https://github.com/vernesong/OpenClash) 0.47.156 | `0x162`, whole value: on UDP in the default TPROXY mode, where TCP goes through nat `REDIRECT` without a mark, and on TCP, UDP and ICMP echo in TUN mode | `fwmark 0x162 lookup 354`, no priority; TUN mode: 1888 |
| [nikki](https://github.com/nikkinikki-org/OpenWrt-nikki) 1.26.1 | `0x80/0xff` for TPROXY, `0x81/0xff` for TUN | 1024 to table 80, 1025 to table 81 |
| [podkop](https://github.com/itdoginfo/podkop) 0.7.22 | `0x100000`, whole value | 105: `fwmark 0x100000/0x100000 lookup podkop` (table 105, named in `rt_tables`), IPv4 only |
| [zapret](https://github.com/bol-van/zapret) 72.13 and [zapret2](https://github.com/bol-van/zapret2) 1.0.5.2 | Generated packets: the queued packet's mark plus `0x40000000`. With `FILTER_TTL_EXPIRED_ICMP=1`, the default, `0x40000000` is added to the connection mark: in nftables POSTNAT mode, also the default, of every connection queued to nfqws, otherwise of connections nfqws sent generated packets on. In POSTNAT mode `0x20000000` is added to the packets queued after NAT. NFQUEUE 200 and 300 | None |
| [MagiTrickle](https://github.com/MagiTrickle/MagiTrickle) 0.8.2 | `0x4d616769` and up, one per group, whole value, packet and connection mark | `fwmark <mark> lookup <mark>`, no priority |
| [KVAS](https://github.com/qzeleza/kvas) 1.1.9 | `0xd1000`, whole value, packet and connection mark | 1778: `fwmark 0xd1000/0xd1000 lookup 1001` |
| [Tailscale](https://github.com/tailscale/tailscale) 1.102.5 | Its own sockets `0x80000`; packets forwarded from `tailscale0` get `0x40000/0xff0000` and are masqueraded; bits `0xff0000` of the router's new connections saved in the connection mark and restored on later incoming packets, masked on iptables, whole value on nftables (the OpenWrt package's default) | 5210, 5230, 5250 on `0x80000/0xff0000`; 5270 to table 52; 1310-1370 instead on OpenWrt with mwan3 |
| [wg-quick](https://github.com/WireGuard/wireguard-tools) 1.0.20260223, [awg-quick](https://github.com/amnezia-vpn/amneziawg-tools) 3.1.20260812 | With a `/0` peer and `Table` unset or `auto`: `51820` (`0xca6c`) on the WireGuard socket, or its `FwMark`, or the next number up when table 51820 holds routes; the whole connection mark copied to the packet mark of every UDP packet of that address family in prerouting | `not fwmark 51820 lookup 51820`, `lookup main suppress_prefixlength 0`, no priority |
| [mwan3](https://github.com/openwrt/packages/tree/master/net/mwan3) 2.12.2 | Interface `N` as `N << 8` under `0x3f00`; connection mark saved and restored under the same mask | 1001-1060 by incoming interface, 2001-2060 by mark, 3001-3060 unreachable by mark, 2061 and 2062 blackhole and unreachable |
| [pbr](https://github.com/openwrt/packages/tree/master/net/pbr) 1.2.2 | `0x10000`, `0x20000` and on per interface under `0xff0000` | 30000, 29999 and down: `fwmark <mark>/0xff0000` |

Where these meet b4:

- **Xray, XKeen.** `255` lies under `0x27fff`, so routing sets leave Xray's outbound
  connections alone and packet processing still handles them. The QUIC refusal of a proxy
  set with **Route UDP through upstream** off, which tests only the queue mark and `0x40000`,
  and block sets, which test no mark, reject Xray's packets to their addresses as well.
  XKeen's whole-value writes replace b4's bits on the connections it takes. With router
  traffic on, it also takes b4's own connections: none of its exemptions covers `0x40000`.
  The nftables variant of the Xray guides starts with `flush ruleset`, which removes b4's
  nftables tables too, and the iptables `tproxyrules.service` of the IPv4 and IPv6 guide runs
  `iptables -t mangle -F` and `ip6tables -t mangle -F` when it stops, which removes b4's
  mangle rules.
- **XrayUI.** Its bit 16 is outside b4's. Its `DIVERT` rule and the order of the two
  services in mangle `PREROUTING` are described in
  [b4 with Xray or XrayUI](/docs/guides/xray#xrayuis-tproxy-rule-and-connections-b4-diverts).
- **sing-box, mihomo.** Their default marks lie under `0x27fff`; only sing-box's bridge
  outbound, when it falls back to iptables, uses bit `0x40000000`, b4's TUN capture bit.
  `auto_redirect`, in mihomo only with `route-address-set` or `route-exclude-address-set`,
  writes whole marks after b4's routing chains: on UDP and ICMP, and in sing-box also on TCP
  connections it judges through NFQUEUE. With `auto_route` a rule at 9000-9010 sends
  forwarded traffic to table 2022, before b4's interface sets at 10000 and above. The
  router's own connections, b4's included, are taken as well: `auto_redirect` exempts only a
  mark of exactly `0x2024`.
- **nikki.** Its mask, `0xff`, lies inside the marks of b4's interface sets. A set whose mark
  has `0x81` in the low byte, or `0x80` when nikki uses a TPROXY mode, matches nikki's rule
  first, since its priorities are lower. nikki's own writes change only bits 0-7, but these
  lie under `0x27fff`, so on destinations both cover nikki's route wins, whichever runs
  first. nikki's `proxy.bypass_fwmark` option takes mark and mask pairs; `0x40000/0x40000`
  and `0x8000/0x8000` there exempt b4's marked connections and packets from its proxying,
  but not from its router DNS hijack, which runs before that check, and not b4's
  connections that carry no mark.
- **mwan3.** Its mask, `0x3f00`, lies inside the marks of b4's interface sets. A set whose
  mark has an active mwan3 interface number in bits 8-13 matches mwan3's rule at 2001-2060
  first; with 61 or 62 there it matches mwan3's blackhole or unreachable rule, 2061 or 2062,
  whenever mwan3 runs. A set mark with no bit in `0x3f00` gets an interface number added by
  mwan3's policy, and then mwan3's rule matches it instead of b4's.
- **zapret, zapret2.** Bits 30 and 29 are b4's TUN capture bit and its bit for packets
  toward a device, and bit 30 of the connection mark is b4's claim. Neither service skips
  the packets the other generates by their mark. On nftables in zapret's default POSTNAT
  mode, though, zapret takes its generated packets out of conntrack, and b4 then queues them
  only toward a set's packet duplication addresses, since its other queue rules count a
  connection's packets. On nftables both queue the packets they select, b4 first (priority
  -150 against zapret's 99 or 101). With **Set DSCP** on, b4 writes no value into a packet
  with bit `0x20000000`, so in POSTNAT mode the packets zapret queues after NAT, and the
  packets it generates from them, leave without the DSCP value. On iptables the `NFQUEUE`
  rule that sits higher in mangle `POSTROUTING` takes the packets both select there, and the
  other never
  sees them; b4 also queues the router's own packets earlier, in mangle `OUTPUT`, and with
  device filtering forwarded packets in mangle `FORWARD`, so zapret can still process those
  after b4. zapret drops ICMP time-exceeded messages of connections whose connection mark has
  bit `0x40000000`, which includes every connection a b4 interface set claims. In TUN mode
  with port capture, b4's rule `fwmark 0x40000000/0x40000000` also matches zapret's generated
  packets and sends them into `b4tun0`.
- **MagiTrickle.** Its marks, `0x4d616769` and the ones after it, have bit `0x200000`, so in
  NFQUEUE mode packet processing skips its connections. They also have bit `0x40000000`: in
  TUN mode with port capture, b4's rule `fwmark 0x40000000/0x40000000`, IPv4 only, matches
  every packet MagiTrickle marks, and when it sits above MagiTrickle's `fwmark <mark>` rule
  in `ip rule`, those packets go into `b4tun0`, b4 processes them, and they leave by the
  router's own routing instead of MagiTrickle's table.
- **zapret, MagiTrickle and b4's connection-mark check.** Both put bit `0x40000000` into
  connection marks. b4 takes any answered connection that carries that bit as proof that the
  router keeps its connection mark, so they can hide firmware that overwrites b4's claim.
- **KVAS.** `0xd1000` contains `0x40000`: a packet that already carries it when b4's chains
  run reads to b4 as one of its own connections. With acceleration on, KVAS also marks the
  router's own connections to its `unblock` destinations, b4's included, and its rule 1778
  routes them into table 1001.
- **Tailscale.** `0x40000` is Tailscale's mark for packets from `tailscale0`. Its masquerade
  rule, present by default, matches every packet carrying that value under `0xff0000`, so
  b4's own connections marked `0x40000` are masqueraded to the address of the outgoing
  interface; those marked `0x240000` are not. Rule 5270 (1370 on OpenWrt with mwan3) comes
  before b4's interface sets, so a destination that table 52 routes leaves through Tailscale
  even when an interface set of b4 marked it.
- **wg-quick, awg-quick.** With the default queue mark, `0xca6c` has every bit of it, so b4
  treats WireGuard's own packets as its own. Their prerouting rule replaces the packet mark
  of every UDP packet with the connection mark. When both they and b4 use nftables, it runs
  after b4's routing chains: packets of an interface set still match its rule, since b4
  saved the set's mark in the connection mark, but UDP that a proxy set diverts loses the
  set's mark, so the priority 3 rule no longer sends it to b4's listener. Brought up after
  b4, their two rules sit above every rule of b4, for IPv6 only on kernel 4.3 or later.
- **pbr.** `0x40000` is its fourth interface's mark, so with four or more interfaces pbr's
  rule routes b4's own connections that carry `0x40000` by that interface's table. A pbr
  policy that matches a packet a b4 interface set has marked adds its interface byte; when
  that byte has bit 17, b4's rule no longer matches and pbr's rule routes the packet.
- **OpenClash, podkop.** Their writes replace b4's mark: whole values, and in OpenClash's
  default mode nat `REDIRECT` for TCP, so on destinations both cover, their route wins.
  OpenClash takes the router's own connections while router self-proxy is on, the default;
  podkop always takes those to addresses in its lists. Neither exempts b4's own connections.

:::note
The values in the table come from the releases named there and change between versions.
`ip rule` and the firewall listing on the router show what is actually installed.
:::

## Checking marks on the router

```sh
# b4's rules with their marks
nft list table inet b4_mangle
nft list table inet b4_route
nft list table inet b4_dscp
iptables -t mangle -S
iptables -t nat -S

# routing rules of every service, and b4's tables
ip rule
ip -6 rule
ip route show table 252

# socket marks of b4's own connections
ss -tanpe | grep b4

# connection marks in decimal, where the kernel exposes them
grep -o 'mark=[0-9]*' /proc/net/nf_conntrack | sort | uniq -c
```

System Info in the web interface lists b4's firewall rules and the output of `ip rule`.
