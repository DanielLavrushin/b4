---
sidebar_position: 1
title: Core
---

Most changes on this tab require a service restart. The exceptions are the interface language, the web interface's username and password, the [SOCKS5 proxy](#socks5-proxy) settings, the [Set DSCP](#dscp) settings and the web server's **Expose to internet** switch, which apply on save. The header shows the restart notice only while a change that needs one is unsaved.

## Controls

Buttons at the top of the settings:

- **Restart service** - restart b4 (expected downtime: 5-10 seconds)

When b4 runs under systemd or from an init script (`/etc/init.d/b4`, or `/opt/etc/init.d/S99b4` on Entware), the button restarts it through that service manager. Without one, in a container or when b4 was started by hand, b4 restarts itself in place: it shuts down the same way it does on a stop signal, then starts again with the same command line and reads the configuration file anew. The process ID stays the same, so a container runtime or a supervisor sees no exit.

:::warning Reset configuration
When the configuration is reset, these are preserved: domains, GeoSite/GeoIP categories, and test settings. Everything else (network, DPI bypass, protocols, logging) is reset to defaults.
:::

![20260418225826](../../static/img/core/20260418225826.png)

## Queue and packet processing

Settings for the packet processing core over netfilter.

![20260418225903](../../static/img/core/20260418225903.png)

| Parameter | Description | Range | Default |
| --- | --- | --- | --- |
| Starting queue number | NFQUEUE number. Change if other programs use the same numbers | 0-65535 | `537` |
| Packet mark | The queue mark: b4 puts it on the fakes, split segments and packets it sends back out, and on the DNS queries it sends for clients and for its sets, and lets its own packets that carry it pass its packet processing unchanged. The mark is kernel metadata of this host and is never written into the packet, so another router cannot match it; [Set DSCP](#dscp) writes a value that leaves the host. See [Packet marks](/docs/guides/marks#the-queue-mark) | - | `32768` |
| Worker threads | Number of parallel workers. More threads = higher throughput on multi-core systems | 1-16 | `4` |
| TCP per-connection packet limit | How many TCP packets per connection to analyze. Sets cannot exceed this value | 1-100 | `19` |
| UDP per-connection packet limit | How many UDP packets per connection to analyze. Sets cannot exceed this value | 1-30 | `8` |

:::tip Packet limits
These limits are a global ceiling. Each set can define its own limit, but not above the global one. A higher value gives b4 more time to analyze but increases load.
:::

## Features

### Protocols

| Parameter | Description | Default |
| --- | --- | --- |
| IPv4 support | Process IPv4 traffic | On |
| IPv6 support | Process IPv6 traffic | Off |

These switches say which address families **b4** looks at. They do not turn IPv6 on or off on the router, and they do not stop the network from using it.

:::warning What IPv6 support being off means
With it off, b4 creates no IPv6 firewall rules and reads no IPv6 packets. A site that a set targets and that also answers over IPv6 is reached over IPv6 by the client, which means it bypasses the set entirely: no bypass strategies, no routing, no blocking. That is why b4 writes a warning to the log when the host has a working global IPv6 address while this setting is off.

Two things soften it. b4 strips IPv6 addresses out of DNS answers for domains a set matched, so clients that resolve through the router stay on the IPv4 path b4 protects, and a set can be pointed at IPv6 explicitly. See [DNS -> The IPv4 fallback](../dns#the-ipv4-fallback).
:::

:::note Restart to apply
Turning IPv6 support on or off changes which rules exist in the firewall for every set. Sets are rebuilt on the next configuration sync, but the packet queue itself binds its address families at startup, so the change takes full effect only after the service restarts.
:::

### Packet engine

**Ingestion mode** selects how packets reach b4. The engine starts once, when the service starts, so a change applies after a restart.

| Mode | How packets reach b4 | Kernel and tools it needs |
| --- | --- | --- |
| NFQUEUE (default) | iptables or nftables rules hand packets to a netfilter queue that b4 reads | The queue modules: `nfnetlink_queue`, plus `xt_NFQUEUE` with iptables or `nft_queue` with nftables |
| TUN interface | Target traffic is routed through a virtual interface that b4 reads | `/dev/net/tun` and the `iptables` binary (or the iptables-nft shim); no queue modules |

Selecting TUN adds the **TUN settings** group: uplink interface, uplink gateway, TUN address and TUN device name.

:::info TUN without the raw table or the CT target
The TUN engine sends each packet it reads on through a raw socket. For a packet the device forwards for another host, such as a LAN client or a Docker container on a bridge network, that copy has to skip connection tracking, which b4 arranges with a `NOTRACK` rule in the iptables `raw` table; without it the kernel's NAT gives the copy a new source port and the reply never reaches the client. A kernel without the `raw` table (`iptable_raw`) or without the `CT` target (`xt_CT`), such as the Synology DSM kernel, cannot hold that rule, and b4 treats a failed SNAT into the TUN device the same way. There b4 captures only the connections the device makes itself, those of its SOCKS5 and MTProto proxies included. Forwarded traffic stays out of the TUN device, so it gets no bypass strategies and b4 does not see its DNS queries, while routing and blocking sets still apply to it. The capture ports are TCP and UDP 443, the ports in the sets' port filters and TCP 80 when a set uses **Empty line before HTTP method**; where `xt_connbytes` is missing as well, every packet of those connections goes through the TUN device, and b4 counts them itself so that it processes only the first `tcp_conn_bytes_limit` / `udp_conn_bytes_limit` packets (19 and 8 by default). The log says so at startup, and System Info shows it in the **Captured Traffic** row under Engine. With **Skip IPTables/NFTables setup** on, b4 installs neither rule and keeps capturing forwarded traffic.
:::

#### When the engine does not start

b4 starts the packet engine before the web interface. When the engine fails, b4 removes the rules it had installed and keeps running without it:

- traffic passes through the router without b4 touching it;
- the web interface, the SOCKS5 proxy and the MTProto proxy stay up, and so do the rules of their **Expose to internet** switches;
- the other firewall rules, routing sets and the watchdog stay off.

The reason is written to the log at the ERROR level, and the [dashboard](../dashboard.md#packet-engine-not-running) shows it together with buttons that switch to the other engine or restart b4. For example, on a kernel without the queue modules iptables rejects the NFQUEUE target, and a TUN engine with an automatic uplink finds no default route while the WAN or VPN is still down.

b4 retries on its own by restarting itself in place, three times: 15, 30 and 60 seconds after each failure. A cause that clears up by itself, such as a WAN link that comes up after b4, is picked up by one of these retries. After the third failed retry b4 stays in this state until it is restarted. A restart from the web interface or by the service manager starts the count again.

With the web server off (port `0`) there is no interface to fall back to, and b4 exits with the error instead.

### Firewall

![20260418230000](../../static/img/core/20260418230000.png)

| Parameter | Description | Default |
| --- | --- | --- |
| Skip IPTables/NFTables setup | b4 will not create firewall rules. Use this if you manage rules manually | Off |
| Firewall monitor interval | How often to check and restore rules (seconds). If external programs delete rules, b4 will restore them | `10` |
| Firewall engine | Which backend to use for rules | Auto-detect |
| NAT Masquerade | Enable NAT masquerading. Needed for containers and gateways where b4 forwards traffic | Off |
| Masquerade interface | Interface to apply masquerading on. Appears when NAT Masquerade is enabled | All |
| Set DSCP | Write a DSCP value into the packets this host sends out. See [Set DSCP](#dscp) | Off |
| DSCP value | The value written into the DSCP field, 0-63. Appears when Set DSCP is enabled; turning the switch on with the value at `0` fills in `7` | `0` |
| DSCP interfaces | Output interfaces that get the value. Appears when Set DSCP is enabled | All |

:::warning Monitor interval
With the NFQUEUE engine, setting this to 0 turns off rule monitoring completely. If an external program or script removes b4's rules, they will not be restored.
:::

:::info Rules for Expose to internet
The **Expose to internet** switches of the web server, the SOCKS5 proxy and the listeners on the Telegram tab add accept rules to the host's own input chains, separately from the rules of the packet engine. The monitor interval also sets how often b4 checks them and puts back the ones a firewall reload removed, whatever the packet engine; at `0` only `SIGUSR1` starts that check. **Skip IPTables/NFTables setup** stops b4 from adding them. See [Access from the internet](./security.md#expose-to-internet).
:::

In TUN mode the Firewall group holds only NAT Masquerade, Set DSCP and the monitor interval. At that interval the TUN engine checks its capture chain `B4_TUN` and the jumps into it, and the firewall monitor checks the masquerade, MSS clamp, DSCP and routing-set rules. Each puts back what the router's own firewall removed, for example when the router restarts its firewall after a port-forwarding change. In this mode the interval is at least 10 seconds, and 0 turns neither check off. `SIGUSR1` starts both checks without waiting for the interval. NAT Masquerade leaves the TUN device out: the TUN engine rewrites the source address of captured packets to the uplink address itself, and keeps that rule ahead of any masquerade rule that names no outgoing interface. When TUN captures only the device's own traffic (see [TUN without the raw table or the CT target](#packet-engine)), that rule is a NAT exemption, `-o <device> -j ACCEPT`, which leaves the source address as it is.

:::info Rule restores in System Info
When TUN captures by port (the Capture row reads `ports`), System Info compares the number of rules in the capture chain with the number b4 installed. After the first restore it also shows how many times the capture rules were restored and, in either engine mode, how many times the firewall monitor restored its rules, each with the time of the last restore. A count that keeps growing points to another service on the router rewriting the firewall.
:::

Firewall engine options:

| Value | Description |
| --- | --- |
| Auto-detect | b4 picks the available backend (recommended) |
| nftables | Use nftables |
| iptables | Use iptables |
| iptables-legacy | Use iptables-legacy (for older systems) |

#### Set DSCP {#dscp}

DSCP is the upper six bits of the IPv4 ToS byte and of the IPv6 Traffic Class byte. With **Set DSCP** on, b4 writes the configured value into that field of every packet the host sends out, so a router in front of b4 can tell b4's traffic apart by a field in the packet itself. The marks b4 uses internally, **Packet mark** among them, cannot do this: they are kernel metadata of the packet on this host and never appear on the wire.

The value is written into:

- traffic forwarded through the host;
- b4's own connections;
- the fakes, fragments and segments b4 injects for the DPI bypass, so they carry the same value as the real packets of the same connection.

It is not written into loopback traffic, into the packets b4 itself sends toward LAN clients, such as DNS answers and resets, or into replies within connections another host opened, such as the web interface's responses or the server replies b4 passes back to a LAN client after masquerade. The two ECN bits of the field are kept. Both IPv4 and IPv6 are covered, whatever the **IPv4** and **IPv6** switches say. With interfaces selected, only packets leaving through them get the value.

With iptables the rule sits in its own chain `B4_DSCP` in the `mangle` table, jumped from the top of `POSTROUTING` and kept above b4's capture jump there: a packet b4 inspects leaves `mangle POSTROUTING` at that jump, and a rule below it never sees the packet. The DSCP target needs the `xt_DSCP` kernel module, packaged on OpenWrt as `kmod-ipt-ipopt` and `iptables-mod-ipopt`. With nftables the rule sits in its own table `inet b4_dscp` and needs no extra module. With iptables, when the kernel refuses the rule for one address family, b4 logs it once and leaves that family without the value; with nftables, a refused table leaves both families without it. The firewall monitor puts the rule back when another program removes it, in both engine modes.

The value is written only while b4 runs with its firewall rules. While b4 is stopped, restarting or running without its packet engine, the host still forwards packets, without the value. A router that uses the value to keep b4's traffic out of a route leading to b4 sends those packets back to b4 meanwhile; a match on the interface or the MAC address does not depend on b4 running.

Every device on the path sees the value until something rewrites it, and some of them act on it. Linux Wi-Fi drivers and many access points choose the WMM queue from it: `7`, from the range RFC 2474 leaves for local use, stays in the best-effort queue with traffic that carries no DSCP value, `8` and several other values below `24` go to the background queue, and values from `32` up go to the video and voice queues. `0` clears whatever value the packets carried.

:::warning The value reaches the internet
The router that reads the value is expected to reset the field on its internet uplink. Without that reset, the ISP sees the value on every connection b4 carries.
:::

:::info Flow offloading
Flows that the kernel or the hardware offloads (an nftables flowtable, `FLOWOFFLOAD`, vendor hardware NAT) skip the rule once offloaded, so only their first packets carry the value. A router that decides on the first packet of a connection and keeps that decision for the whole connection is not affected; one that reads the value on every packet sees the rest of the connection without it.
:::

:::info RouterOS
RouterOS tells the packets of a b4 container, or of a b4 machine with a subnet of its own, apart by the interface they arrive on, so routing through b4 there needs no DSCP value, see [Routing chosen destinations through the container](../install/mikrotik.md#routing-by-destination).
:::

### Network interfaces

Which packets the engine inspects at all. Interfaces are shown as clickable tags - click to
enable/disable. Empty means every interface, and that is what almost every setup wants.

This is a filter, not a list of interfaces b4 attaches to, and it does not select a
direction by name. b4's capture rules sit in the `postrouting` and `output` hooks, where
the kernel has already decided where the packet is going, so for forwarded traffic the
interface compared here is **the one the packet leaves by**. Only the reply direction and
DNS, captured in `prerouting`, are matched on the arriving interface.

Because the interface a packet leaves by comes from the routing table, another service can
change it without touching this list. A VPN client or a transparent proxy that moves the
default route puts every packet on a different interface, and a selection made before that
stops matching.

:::warning
While the selection matches nothing, b4 inspects nothing: packets are still queued to it,
so the cost is still paid, and every one is accepted unchanged. No set applies and no
strategy runs. The web interface warns whenever an interface it is not watching is carrying
outgoing traffic, and names it. The per-interface counts behind that warning are in the
diagnostics report as `packets_leaving` and `packets_arriving`.
:::

:::info
b4 has three settings that take an interface name and they mean three different things.
[Which interface is which](/docs/guides/interfaces) puts them side by side.
:::

## Logging

![20260418230040](../../static/img/core/20260418230040.png)

| Parameter | Description | Default |
| --- | --- | --- |
| Log level | Log verbosity | INFO |
| Error file path | File to write errors to | `/var/log/b4/errors.log` |
| Timezone | Timezone for timestamps | System (auto) |
| Immediate flush | Flush the buffer after every write. May affect performance | On |
| Syslog | Also send logs to the system syslog | Off |

Log levels:

| Level | What is shown |
| --- | --- |
| Error | Only errors |
| Info | Errors + main events |
| Trace | Info + packet processing details |
| Debug | Everything, including debug info |

:::warning Error level
At the **Error** level the **Logs** section shows almost nothing: it reads the log stream, which carries next to nothing at this level. The **Traffic** section has its own stream and is not affected.
:::

:::info Error file
b4 does not keep a persistent log file - everything goes to stdout/stderr (and is captured by the web interface through a WebSocket). Only critical errors and crashes are written to `errors.log`.
:::

:::tip
For diagnosing issues use **Trace** or **Debug**. For normal operation **Info** is enough.
:::

## Web server

Settings for the b4 web interface.

![20260418230100](../../static/img/core/20260418230100.png)

| Parameter | Description | Default |
| --- | --- | --- |
| Bind address | IP to listen on. `0.0.0.0` or `::` = every address, IPv4 and IPv6; `127.0.0.1` = localhost only | `0.0.0.0` |
| Port | Web interface port | `7000` |
| Expose to internet | `system.web_server.expose`. Adds a firewall rule that accepts connections to the web interface port from any address, IPv4 and IPv6, and puts it back after firewall reloads. Can be turned on only with a username and a password set. The rule follows the port the running server listens on, so a port change moves it after a restart. See [Access from the internet](./security.md#expose-to-internet) | Off |
| TLS Certificate | Path to a `.crt` or `.pem` certificate file (empty = HTTP) | - |
| TLS Key | Path to a `.key` or `.pem` key file (empty = HTTP) | - |
| Language | Interface language: English / Русский | English |

### Authentication

| Parameter | Description | Default |
| --- | --- | --- |
| Username | Login for the web interface | - |
| Password | Password | - |

:::warning Partial authentication
Authentication only applies when **both** fields are filled. If only the username or only the password is set, authentication stays off.
:::

:::warning HTTP + authentication
If authentication is enabled but TLS is not configured, the username and password travel over unencrypted HTTP. Configure TLS certificates for secure transport. See the [Security](./security) section.
:::

## SOCKS5 proxy

A built-in SOCKS5 proxy. Applications can route traffic through it - it is processed by b4 with the configured sets applied. A host name matched by a set in proxy mode is handed to that set's upstream; see [Connections through the built-in SOCKS5 proxy](../sets/routing.md#connections-through-the-built-in-socks5-proxy).

![20260418230122](../../static/img/core/20260418230122.png)

| Parameter | Description | Default |
| --- | --- | --- |
| Enable | Start the SOCKS5 server | Off |
| Bind address | IP to listen on. `0.0.0.0` = every address, IPv4 and IPv6; `127.0.0.1` = localhost only | `0.0.0.0` |
| Port | Proxy port | `1080` |
| Expose to internet | `system.socks5.expose`. Adds a firewall rule that accepts TCP connections to the proxy port from any address, IPv4 and IPv6, and puts it back after firewall reloads. Can be turned on only with credentials or an allowed sources list and, while the web server is on, with a username and a password on the web interface, which a SOCKS5 client could otherwise reach through the proxy. UDP is not covered. See [Access from the internet](./security.md#expose-to-internet) | Off |
| Username | Login for SOCKS5 authentication (empty = no authentication) | - |
| Password | Password for SOCKS5 authentication (empty = no authentication) | - |
| Allowed sources | IP addresses and CIDR ranges permitted to open a connection (empty = no restriction) | - |

Every field except "Enable" becomes available only after the proxy is enabled.

:::warning Partial credentials
Username and password work as a pair. If exactly one of the two is filled, the proxy refuses every client rather than running unauthenticated. Both empty means no authentication; both filled means authentication is required. A configuration with only one of them is rejected when it is saved.
:::

:::info
No SOCKS5 field needs a service restart. Credentials and the source list are applied to the running proxy, and changing **Enable**, **Bind address** or **Port** rebinds the listener on save.
:::

### Allowed sources

`system.socks5.allowed_sources` lists the client addresses that may open a connection to the proxy. An empty list means no restriction, which is the default and the behaviour of earlier versions. With a non-empty list, a peer whose address falls outside every entry has its TCP connection closed at accept time, before any SOCKS5 byte is exchanged and before it occupies a connection slot.

An entry is either a bare IP address, v4 or v6, read as `/32` and `/128`, or a CIDR range. Host bits are masked off, so `192.168.1.7/24` is stored as `192.168.1.0/24`.

A list holding `192.168.1.0/24` and `127.0.0.1/32` accepts clients from that LAN subnet and from the router itself, and closes the connection for every other source.

:::warning A network gate, not an authentication factor
A source list is the same kind of control as a firewall rule: it decides which addresses reach the proxy and nothing more. It establishes no identity. A source address can be spoofed, a DHCP lease moves from one device to another, and an entry for a host that is itself a router stands for every host behind that router. Credentials are the control that identifies a client.
:::

:::info Chrome and Chromium
Chrome and Chromium do not implement SOCKS5 username/password authentication ([crbug 40323993](https://issues.chromium.org/issues/40323993)), so they cannot use an authenticated proxy at all. A source list is how the proxy serves those browsers without credentials while still refusing arbitrary clients.
:::

Credentials and the source list are independent controls that stack. The list never changes which authentication method the proxy offers; it only decides whether a connection reaches the authentication step. With both configured, a client has to come from a listed address **and** present valid credentials.

Loopback is not implicitly allowed. A client running on the router itself needs `127.0.0.1/32`, or `::1/128` over IPv6, listed explicitly, otherwise it is refused like any other unlisted source.

Editing the list takes effect on save, with no service restart. Live sessions whose source no longer matches are disconnected, and a source added to the list can connect immediately.

:::warning The port stays reachable
Refusing a connection is not the same as not listening. The listener accepts and then closes, so the port still answers a port scan. The list adds no firewall rule either: with the default bind address `0.0.0.0` the proxy listens on the WAN address as well, and whether a connection from outside gets through is decided by the host's firewall and by **Expose to internet**, whose rule accepts every source address. Binding to the LAN address remains the way to keep the proxy off the WAN.
:::

`0.0.0.0/0` and `::/0` are refused when the configuration is saved, because an entry that matches every address switches the restriction off while leaving it looking enabled. An entry that is neither an IP address nor a CIDR range is refused the same way.

## MTProto proxy

A built-in Telegram MTProto proxy with fake-TLS obfuscation. Telegram traffic is wrapped in a TLS connection, so what crosses the network looks like ordinary HTTPS.

The proxy is configured on the **Telegram** tab, together with the WebSocket bridge and the WEB proxy. Its fields are listed under [Settings, Telegram](./mtproto.md), and the guides for the three Telegram modes are under [Telegram](../telegram/index.md).

## Global MSS Clamping

Limits TCP Maximum Segment Size on SYN/SYN-ACK packets for port 443 traffic. A smaller MSS leads to natural fragmentation - the DPI cannot reassemble a fragmented ClientHello.

![20260418230236](../../static/img/core/20260418230236.png)

| Parameter | Description | Range | Default |
| --- | --- | --- | --- |
| Enable | Turn on global MSS Clamping | - | Off |
| MSS size | MSS size in bytes. Lower = more fragmentation | 10-1460 | `88` |

:::info Where MSS can be set
There are three places, from broadest to narrowest:

- **Global**, here - applies to **all** port 443 traffic.
- **Per-device**, in the **MSS** column of the [device table](#device-filtering) below - for example a TV running YouTube. Works independently of the global setting.
- **Per-set**, on a set's [TCP -> General](../sets/tcp/general#mss-clamping) tab - for the addresses or devices that set targets. It takes precedence over both settings above for the connections it covers.
:::

## DNS

Holds what applies to DNS across every set: whether DNS over TCP is intercepted, the port it is redirected to, the timeouts, and the IPv4 fallback. The resolver itself is picked per set. See [DNS](../dns.md).

**Force IPv4 for matched domains** is in this group. While IPv6 support is off, it strips IPv6 addresses out of DNS answers for domains a set matched, so those clients stay on the IPv4 path b4 protects, and it leaves every other name alone. It is on by default and greyed out while IPv6 support is on. See [The IPv4 fallback](../dns#the-ipv4-fallback).

## Device filtering

Limits b4 to traffic from specific devices on the network. Useful when bypass is not needed for every device.

Devices discovered from the ARP table are matched by their MAC address. Devices you add by hand have no MAC address on the
network, so they are matched by the IP address you enter for them, both when a set is limited to source devices and for a
per-device MSS clamp. Give such a device a fixed or reserved address; it cannot be matched at all when an intermediate
router replaces the source address of its traffic before it reaches b4.

The allow and deny list below selects traffic for DPI bypass by MAC address only, so a list containing nothing but
manually added devices leaves DPI bypass applying to every device.

![20260418230312](../../static/img/core/20260418230312.png)

| Parameter | Description | Default |
| --- | --- | --- |
| Enable | Turn on device filtering | Off |
| Vendor detection | Download the vendor database to identify manufacturer by MAC (~6 MB) | Off |
| Invert selection | Toggle between allow list and deny list | Off |

:::info Filter modes

- **Allow list** (default) - DPI bypass works **only** for the selected devices
- **Deny list** (invert selection) - selected devices are **excluded** from DPI bypass

:::

The filter selects devices on the network, and the router is not one of them. Connections the router opens itself, including
the ones the [SOCKS5 proxy](#socks5-proxy) opens for its clients, get DPI bypass in both modes. Block sets, and for TCP proxy and
Telegram over WebSocket sets, act on them unless the set is limited to source interfaces or an included source-device list. Sets
in interface mode follow their [Router's own traffic](../sets/routing.md#routers-own-traffic) setting.

### Device table

When filtering is enabled, a table of discovered devices appears:

| Column | Description |
| --- | --- |
| Select | Checkbox to include/exclude the device |
| MAC | MAC address, or `matched by IP` for a device added by hand |
| IP | Current IP address |
| Name | Device alias (editable through the edit icon) or vendor |
| MSS | Per-device MSS Clamping (10-1460, empty = off) |

The **Refresh** button reloads the device list from the ARP table.

:::tip Per-device MSS
MSS Clamping can be set per device - for example, lowering the MSS only for a TV running YouTube without affecting other devices.
:::
