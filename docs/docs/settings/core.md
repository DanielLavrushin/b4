---
sidebar_position: 1
title: Core
---

Most changes on this tab require a service restart. The exceptions are the interface language and the [SOCKS5 proxy](#socks5-proxy) settings, which apply on save.

## Controls

Buttons at the top of the settings:

- **Restart service** - restart b4 (expected downtime: 5-10 seconds)

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
| Packet mark | netfilter mark for iptables/nftables rules. b4 uses it to mark processed packets | - | `32768` |
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

### Firewall

![20260418230000](../../static/img/core/20260418230000.png)

| Parameter | Description | Default |
| --- | --- | --- |
| Skip IPTables/NFTables setup | b4 will not create firewall rules. Use this if you manage rules manually | Off |
| Firewall monitor interval | How often to check and restore rules (seconds). If external programs delete rules, b4 will restore them | `10` |
| Firewall engine | Which backend to use for rules | Auto-detect |
| NAT Masquerade | Enable NAT masquerading. Needed for containers and gateways where b4 forwards traffic | Off |
| Masquerade interface | Interface to apply masquerading on. Appears when NAT Masquerade is enabled | All |

:::warning Monitor interval
Setting this to 0 turns off rule monitoring completely. If an external program or script removes b4's rules, they will not be restored.
:::

Firewall engine options:

| Value | Description |
| --- | --- |
| Auto-detect | b4 picks the available backend (recommended) |
| nftables | Use nftables |
| iptables | Use iptables |
| iptables-legacy | Use iptables-legacy (for older systems) |

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
At the **Error** level, the **Logs** and **Connections** sections in the web interface will not show data - they read from the log stream, which is almost empty at this level.
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
| Bind address | IP to listen on. `0.0.0.0` = all interfaces, `127.0.0.1` = localhost only, `::` = all IPv6 | `0.0.0.0` |
| Port | Web interface port | `7000` |
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

A built-in SOCKS5 proxy. Applications can route traffic through it - it is processed by b4 with the configured sets applied.

![20260418230122](../../static/img/core/20260418230122.png)

| Parameter | Description | Default |
| --- | --- | --- |
| Enable | Start the SOCKS5 server | Off |
| Bind address | IP to listen on. `0.0.0.0` = all, `127.0.0.1` = localhost only | `0.0.0.0` |
| Port | Proxy port | `1080` |
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
Refusing a connection is not the same as not listening. The listener accepts and then closes, so the port still answers a port scan, and b4 adds no firewall rule for it. With the default bind address `0.0.0.0` the proxy is exposed on the WAN whatever the source list contains. Binding to the LAN address is still the way to keep the proxy off the WAN.
:::

`0.0.0.0/0` and `::/0` are refused when the configuration is saved, because an entry that matches every address switches the restriction off while leaving it looking enabled. An entry that is neither an IP address nor a CIDR range is refused the same way.

## MTProto proxy

A built-in Telegram MTProto proxy with fake-TLS obfuscation. Telegram traffic is wrapped in a TLS connection, masquerading as regular HTTPS. Detailed setup in the [MTProto Proxy](../mtproto) section.

![20260418230138](../../static/img/core/20260418230138.png)

| Parameter | Description | Default |
| --- | --- | --- |
| Enable | Start the MTProto server | Off |
| Bind address | IP to listen on | `0.0.0.0` |
| Port | Proxy port | `3128` |
| Fake SNI domain | The domain visible in the TLS handshake. The DPI sees this domain instead of Telegram | `storage.googleapis.com` |
| DC Relay | External relay address (host:port) for reaching Telegram DCs when they are IP-blocked | - |
| Secret | Secret for the Telegram client to connect. Paste it into the Telegram proxy settings | - |

The **Generate Secret** button creates a secret based on the current Fake SNI domain.

:::info DC Relay
DC Relay is needed when b4 is installed on a router inside a country with blocking, and Telegram server IPs are blocked. A VPS outside the blocking area is used as the relay.
:::

:::info
Changes to MTProto settings require a service restart.
:::

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
