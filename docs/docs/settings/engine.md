---
sidebar_position: 2
title: Capture engine
---

b4 has two ways of taking packets out of the kernel and into its own code. Which one runs is `queue.mode`, set under **Settings -> Core -> Packet Engine -> Ingestion mode**. It is read once at start-up, so a change takes effect only after the service restarts.

| Engine | `queue.mode` | How packets reach b4 |
| --- | --- | --- |
| NFQUEUE | `nfqueue` (default, stored as an empty value) | iptables or nftables rules put matching packets on a netfilter queue; b4 reads each one and returns a verdict |
| TUN | `tun` | Policy routing steers matching packets into a virtual interface b4 owns; b4 reads them there and re-injects what it accepts through a raw socket |

Everything above the capture layer is the same code in both modes: sets, targets, strategies, DNS handling and routing read the same packets and take the same decisions. What differs is which packets arrive, and that is what [Limitations](#limitations) describes.

## Why TUN exists

The NFQUEUE engine needs `nfnetlink_queue` and the `NFQUEUE` target in the kernel. Several stock router firmwares ship neither and offer no package that adds them, which leaves b4 with no way to see a packet at all. `/dev/net/tun` is present on far more of those firmwares, because the vendor's own VPN client needs it, so the TUN engine reaches hardware the NFQUEUE engine cannot run on.

The other case it covers is a router whose WAN is an L2TP or PPP tunnel that comes and goes: the TUN engine follows the system default route and re-points its uplink by itself when that route changes.

:::info
TUN is the fallback, not the better option. It carries the restrictions listed below, and NFQUEUE is the mode every other page on this site describes. The setting is worth changing only when NFQUEUE does not work on the hardware.
:::

## Requirements

| Requirement | Why |
| --- | --- |
| `/dev/net/tun` | The engine opens the character device to create its interface. Without it b4 cannot start in TUN mode |
| The `iptables` binary, or the `iptables-nft` compat shim | Every rule the engine writes goes through `iptables`: SNAT into the device, the `FORWARD` accepts, the `NOTRACK` rules for re-injected packets, and the capture chain itself. Native nftables rules for TUN are not implemented, so an nft-only system is refused at start-up with that reason |
| Policy routing, from full iproute2 | The engine adds `ip rule` entries keyed on firewall marks and puts default routes in tables 9999 and 9998. A busybox `ip` applet often rejects a custom table id, and `ip rule add` then fails with the message naming iproute2 |

Starting b4 with `--skip-tables` removes the `iptables` requirement: the engine then sets up its device, its capture routing and its bypass table and nothing else, and NAT and forwarding for the TUN device have to be provided by hand.

The queue mark is checked against the mark bits the engine reserves for itself, `0x40000000` for steering, `0x20000000` for the client direction and `0x10000000` for re-injection. A `queue.mark` overlapping any of them is refused when the configuration is saved.

## The two capture modes

At start-up the engine probes whether `iptables` can add a `connbytes` rule, and picks one of two ways to steer traffic into the device.

| Mode | Chosen when | What is captured |
| --- | --- | --- |
| `ports` | `xt_connbytes` is available | Only the first packets of a connection, on the TCP and UDP ports the enabled sets list, plus every UDP query to port 53. The counts are the global packet limits, `19` TCP and `8` UDP by default. The system default route is left alone |
| `default` | `xt_connbytes` is missing | The system default route is replaced by a route into the TUN device, so everything the router sends towards the internet passes through b4 |

In `ports` mode the steering is done by a mangle chain named `B4_TUN`, jumped to from `PREROUTING` and `OUTPUT`. It marks the packets described above and a policy rule sends that mark to a table whose default route is the TUN device. Everything else keeps the ordinary path, and traffic a routing set has already claimed returns from the chain untouched.

In `default` mode there is no chain: the main table's default route points at the device, and the packets b4 accepts are re-injected with a mark that a second policy rule sends to the bypass table holding the real uplink route.

`default` mode captures more and costs more. Every forwarded connection is read in full for as long as it lasts, rather than for its first few packets.

### Which mode is active

The active mode is reported in three places.

- The start-up log. `ports` mode logs `TUN: port-capture mode - first N tcp / M udp packets on ports ...`; `default` mode logs `TUN: xt_connbytes not available; capturing the whole default route ...` followed by `TUN: default-capture routing configured ...`. A heartbeat line repeats it periodically, naming the device, the uplink, the mode and the counters.
- **Settings -> System Info**, in the **Engine** section, as **Capture**.
- The chain itself:

```sh
iptables -t mangle -S B4_TUN
```

A populated chain means `ports` mode. `No chain/target/match by that name` means `default` mode, where no such chain is created.

## Settings that only apply in TUN mode

`queue.tun` in the configuration file. Four of the six have a control under **Settings -> Core -> TUN settings**, which appears once the ingestion mode is TUN; the other two are file-only.

| Field | Interface | Default | Meaning |
| --- | --- | --- | --- |
| `device_name` | TUN device name | `b4tun0` | Name of the interface b4 creates. An existing interface of that name is removed when it is a TUN device left by an earlier run, and refused when it is anything else |
| `address` | TUN address | `10.255.0.1/30` | Address and prefix put on the device. An overlap with a LAN or VPN subnet breaks routing for that subnet |
| `address_v6` | - | empty | IPv6 address for the device. IPv6 is not forwarded in TUN mode, so setting this only leaves IPv6 enabled on the interface and logs a warning |
| `out_interface` | Uplink interface | empty | Where processed packets are sent. Empty or `auto` follows the system default route and re-points when it changes; naming an interface pins it |
| `out_gateway` | Uplink gateway | empty | Gateway for the pinned uplink. Empty reads it from the current default route. The field is disabled while the uplink is on Auto |
| `route_table` | - | `9999` | Route table id for the bypass route. The capture route goes in `route_table - 1`, so the default pair is 9999 and 9998. A table already holding routes that b4 did not put there is refused, with a note to pick an unused id |

Device name, addresses and the route table are read at start-up only. Changing them while b4 runs logs a warning and needs a restart.

:::note What the interface hides in TUN mode
The firewall backend choice, the firewall monitor interval, the **Skip IPTables/NFTables setup** switch and the **Network interfaces** filter are hidden while the ingestion mode is TUN, because none of them applies: the engine talks to `iptables` directly, keeps its own rules alive on its own schedule, and has no per-interface capture filter. NAT masquerade stays, and is documented in [Core](./core#firewall).
:::

## Limitations

As of 1.80.0 the following hold in TUN mode. The diagnostics report lists the ones that apply to the current configuration, as `engine.limitations`.

### Strategy discovery is unavailable

Discovery runs its probes through a second netfilter queue, with its own queue numbers, its own marks and its own steering rules. The TUN engine has no equivalent, so a discovery run started in TUN mode is refused with that reason rather than returning a result that describes nothing. This covers the run started from the [Discovery](../discovery.md) page, the run the watchdog starts, and the MCP tool.

The [watchdog](../watchdog.md) depends on discovery for its self-healing: when a domain fails often enough it re-runs discovery and applies what comes back. In TUN mode it cannot, and marks such a domain unhealable instead, with one warning in the log. Health checks keep running, so the watchdog still reports which domains are failing; only the automatic repair stops. A strategy chosen by hand applies normally.

### IPv6 is not forwarded

The engine reads IPv4 and drops IPv6. An IPv6 packet that reaches the device is counted and discarded, with one warning the first time it happens, and the count is in **System Info** as **IPv6 Dropped**.

Turning **IPv6 support** on does not change this. b4 warns whenever the host has a working global IPv6 address while the TUN engine is running, because a dual-stack site that a set targets is then reached over IPv6 and bypasses the set entirely. That leaves disabling IPv6 on the WAN, or switching the engine to NFQUEUE. The [IPv4 fallback](../dns#the-ipv4-fallback) still applies to answers b4 does see and narrows the gap without closing it.

### DNS to the router's own resolver is not intercepted

The kernel consults the `local` table before any of b4's policy rules, so a query a client sends to the router's own address on port 53 is delivered to the resolver running there, dnsmasq on most firmwares, before the capture rule can steer it. A set's resolver, its pins and its blocking do not apply to those queries.

What the router then forwards upstream leaves through `OUTPUT`, where the capture rule does apply, so a set still sees the query and can answer it. The practical effect is that the client's own address is not attached to the lookup, and that a query the local resolver answers from its cache never reaches b4 at all. See [DNS](../dns.md) for what the interception does when it applies.

### Whole-default capture has no capture chain

In `default` mode the steering is a route, not a rule, and several features that live in the `B4_TUN` chain therefore do not exist:

- Device filtering. The allow and deny list is a set of `mac-source` rules in the `B4_TUN_GATE` chain, which is only built in `ports` mode.
- Reply-direction capture, described below.
- The shortcuts that return a routing set's destinations out of the capture path early. The mark guard still keeps routed traffic out of the device, at the cost of a few more rule evaluations.
- The first-N-packet cap. Every connection is read for its whole life, which is the main cost of this mode.

### Reply-direction TCP data is not captured

The capture rules mark the outgoing direction. The one exception is TCP RST packets on the ports the sets list, marked only while an enabled set uses RST protection or escalation, and only in `ports` mode.

Everything b4 does with inbound TCP data therefore does not run: the [incoming response bypass](../sets/tcp/faking#incoming-response-bypass) modes, which act on the server's answer after a threshold of data, and the health signals b4 takes from a server's SYN-ACK, which feed IP block detection and the known-good address list. The sets keep working on the outgoing direction, which is where the DPI-bypass strategies act.

Reply capture also has its sender created at start-up, so switching RST protection or escalation on in a set needs a restart before it takes effect.

### A set's egress IP is not preserved for captured traffic

Traffic on its way into the TUN device is SNATed to the uplink's own source address, so that replies come back, and b4 reads the packet after that rewrite. What it re-injects carries `NOTRACK`, so no later NAT rule sees it either.

A routing set is normally kept off that path: the capture chain and the SNAT chain both return early for a packet a routing set has already marked, so a set routing to an output interface keeps its own source address and its [egress IP](../sets/routing#egress-ip) applies. With whole-default capture there is no capture chain, and an egress IP has no effect on the traffic b4 re-injects there.

b4 reports `egress_ip` among the engine limitations whenever an enabled routing set carries one while the TUN engine is selected, without distinguishing the capture mode.

The same rewrite is why per-device attribution needs a second step. b4 recovers the original LAN address from conntrack, and where `/proc/net/nf_conntrack` cannot be read it logs that device logging and filtering will show the uplink address, DNS redirects for forwarded clients are switched off, and a blocking set's `reject` degrades to a silent drop.

## Since 1.80.0

TUN mode reached parity with the NFQUEUE path on several points:

- Routing rules are kept alive and restored the same way NFQUEUE keeps them: the engine runs the routing keeper, which notices a set's output interface changing address, and rebuilds the rules when they go missing.
- Capture settings apply on save. Adding a port to a set, changing a packet limit or editing the device list rebuilds the `B4_TUN` chain in place instead of waiting for a restart. Device name, addresses and route tables still need one.
- Routed traffic keeps its per-set route. The capture jump is placed below the routing chains in `PREROUTING` and `OUTPUT`, and moved down if it is found above them, so a routing set marks a packet before the capture rule can steer it into the device.
- Blackhole and QUIC-reject chains are reachable again. The `FORWARD` accepts the engine installs used to accept everything crossing the device; they now go through `B4_TUN_FWD`, which returns for the destinations a blocking set holds so the rules below can act on them.
- The DNS-over-TCP redirect is installed. TCP port 53 is redirected into b4's listener in TUN mode as well, instead of reaching the client's resolver unread.
- Masquerade no longer rewrites traffic bound for the TUN device. With NAT masquerade enabled, the rule now returns for packets leaving by the TUN device before the `MASQUERADE` rule is reached.
- MSS clamping, the masquerade rules, the conntrack sysctls and the DNS-over-TCP redirect are re-applied when the configuration is saved, not only at start-up.

## Troubleshooting

**Settings -> System Info** carries all of it. The **Engine** section shows the device and whether it is up, its address and MTU, the egress interface, gateway and resolved source address, the capture mode, the route table, the health of the steer rule and capture chain, the limitations that apply, and the counters for packets forwarded, forward errors and IPv6 dropped. The **Routing** section shows the `ip rule` output for both families, the default route, and the contents of the capture, bypass and per-set route tables. The **Firewall** section lists the `B4_TUN` and `B4_TUN_GATE` chains among its rule dumps. The same data is in `GET /api/system/diagnostics` under `engine` and `routing`.

From a shell on the router:

```sh
ip rule show
ip route show table 9999
ip route show table 9998
ip -4 route show default
iptables -t mangle -S B4_TUN
iptables -t filter -S FORWARD
iptables -t nat -S B4_TUN_SNAT
```

What each one should hold while the engine is running:

| Command | Expected |
| --- | --- |
| `ip rule show` | A rule at priority 90 sending the steer mark to the capture table, in `ports` mode, and a rule at priority 100 sending the re-inject mark to the bypass table |
| `ip route show table 9999` | A default route out of the real uplink. This is the path re-injected packets take |
| `ip route show table 9998` | A default route into the TUN device, in `ports` mode |
| `ip -4 route show default` | The real uplink in `ports` mode, and the TUN device in `default` mode |
| `iptables -t mangle -S B4_TUN` | The mark guards, then the routing-set returns, then one steer rule per port group. Absent in `default` mode |
| `iptables -t filter -S FORWARD` | Two rules for the device, `-i` and `-o`, jumping to `B4_TUN_FWD` |
| `iptables -t nat -S B4_TUN_SNAT` | A return for routing-claimed marks, then a SNAT to the uplink's address |

The engine reconciles all of this on a timer and whenever a netlink message says the default route changed, and logs what it restored. A missing capture route or an empty capture chain also downgrades the reported engine status to `degraded (tun: ...)`.

Errors that name their own cause:

- `route table N is already in use` means another VPN client holds that table, and `queue.tun.route_table` needs an unused id.
- `TUN mode needs the iptables binary` means the host has `nft` only, which leaves installing `iptables-nft` or switching the ingestion mode back to NFQUEUE.
