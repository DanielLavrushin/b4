---
sidebar_position: 1
title: Core
description: Packet engine, devices, firewall, DNS and SOCKS5 settings.
---

The **Core** tab decides how traffic reaches b4 and what b4 does with it. Some fields apply only after a restart, see [Saving and restarting](./index.md#saving). [Packet capture](../internals/capture.md) explains how the engine works.

## Packet Engine {#packet-engine}

The sub-tab decides how packets reach b4, which address families it handles and how much of each connection it reads.

![The Packet Engine sub-tab](/img/core/20261003230100.png)

### Ingestion mode {#ingestion-mode}

**Ingestion mode** selects how packets reach b4. A change applies after a restart.

| Mode | How packets reach b4 |
| --- | --- |
| **NFQUEUE (default)** | Firewall rules hand packets to b4. The usual choice. |
| **TUN interface** | Traffic is routed through a virtual interface that b4 reads. For kernels without the modules NFQUEUE needs. |

#### TUN settings {#tun-settings}

Shown while **TUN interface** is selected.

| Field | What it does | Default |
| --- | --- | --- |
| **Uplink interface** | Where b4 sends processed packets. **Auto (follow default route)** moves with the default route when the WAN or VPN changes. A selected interface pins the uplink. | **Auto (follow default route)** |
| **Uplink gateway** | The gateway of a pinned uplink. Empty takes it from the default route. | Empty |
| **TUN address** | Address and prefix of the TUN device. Must not overlap a LAN or VPN subnet. | `10.255.0.1/30` |
| **TUN device name** | Name of the interface b4 creates. Must not be the uplink or another existing interface. | `b4tun0` |

#### Capture Interfaces {#network-interfaces}

**Capture Interfaces**, shown in NFQUEUE mode, limits b4 to traffic that leaves the router through the selected interfaces. An empty selection, the default, covers every interface and is the usual choice.

A VPN client or a tunnel can move traffic to another interface, and traffic outside the selection gets no set. When nothing works after an interface is selected, clearing the selection is the first check, see [Capture Interfaces match nothing](../troubleshooting/traffic.md#capture-interfaces). See also [Which interface is which](../guides/interfaces.md).

#### When the engine does not start {#when-the-engine-does-not-start}

When the packet engine fails to start, the web interface keeps working, but traffic passes through the router untouched. The [dashboard](../dashboard.md#packet-engine-not-running) and the **Packet Engine** card show the reason. When the kernel lacks the modules NFQUEUE needs, installing them or switching to **TUN interface** fixes it. b4 retries the start three times within about two minutes, which covers a WAN or VPN that comes up late. [Engine start and retries](../internals/capture.md#engine-start-and-retries) has the details.

### IP Block Detection {#ip-block-detection}

IP block detection is turned on per set, on the **TCP > General** tab. This card holds its global setting.

| Field | What it does | Default |
| --- | --- | --- |
| **Retest interval (seconds)** | How long an address marked unreachable stays marked before it is tested again. Recovery after a block is lifted can take this long. The interval also paces the checks of [pinned DNS addresses](../dns.md#pinned-addresses). | `300` |

### IPv4 / IPv6 {#protocols}

| Field | What it does | Default |
| --- | --- | --- |
| **Enable IPv4 Support** | b4 handles IPv4 traffic. | On |
| **Enable IPv6 Support** | b4 handles IPv6 packets, DNS answers and firewall rules. While it is off, sets with [Remove ECH keys from DNS answers](../dns.md#removing-ech-keys) on still remove ECH keys from IPv6 DNS answers in NFQUEUE mode. | Off |
| **Force IPv4 for matched domains** | While IPv6 support is off, b4 removes IPv6 addresses from DNS answers for domains a set matched, so clients reach them over IPv4. | On |

:::warning IPv6 support off
With IPv6 support off, a targeted site that also answers over IPv6 can be reached over IPv6 past every set. **Force IPv4 for matched domains** narrows the gap for devices that resolve names through the router, see [The IPv4 fallback](../dns.md#the-ipv4-fallback).
:::

### Queue & Packet Processing {#queue-and-packet-processing}

| Field | What it does | Default |
| --- | --- | --- |
| **Queue Start Number** | The first NFQUEUE number b4 reads. Another value is needed only when another program uses the same numbers. | `537` |
| **Packet Mark** | The mark on the packets b4 sends itself, such as fakes, so that they pass its processing unchanged. See [Packet marks](../guides/marks.md#the-queue-mark). | `32768` |
| **Worker Threads** | How many workers process packets in parallel, from 1 to 16. | `4` |
| **TCP Connection Packets Limit** | How many packets at the start of each TCP connection reach b4, up to 100. A set can lower it, not raise it. | `19` |
| **UDP Connection Packets Limit** | The same for UDP, up to 30. | `8` |

## Devices {#device-filtering}

The **Device Filtering** card limits DPI bypass to selected devices, or excludes selected devices from it. [Applying b4 to selected devices](../guides/devices.md) explains what the filter covers and how to set it up.

![The Devices sub-tab](/img/core/20261003230200.png)

| Field | What it does | Default |
| --- | --- | --- |
| **Enable Device Filtering** | Turns the selection on. While it is off, every device gets DPI bypass. | Off |
| **Exclude Selected Devices (Blacklist)** | Off, only the selected devices get DPI bypass. On, the selected devices are excluded from it. | Off |
| **Vendor Lookup** | Downloads the IEEE vendor list, about 6 MB, to name each device's manufacturer. A randomized MAC address shows as **Private**. | Off |

### Available Devices {#device-table}

While filtering is on, the card lists the devices from the router's ARP table and those added by hand.

| Column | What it shows |
| --- | --- |
| Checkbox | Selects the device. |
| **MAC Address** | The MAC address, or **matched by IP** for a device added by hand. |
| **IP** | The current address. **manual** marks a device added by hand, **offline** a named device not on the network now. |
| **Name** | The name set with the edit icon, otherwise the vendor. |
| **MSS** | An MSS clamp for the device's TCP connections to port 443, from 10 to 1460. Applies whether or not filtering is on. |

### Manual Devices {#manual-devices}

Adds a device that the ARP table does not show, such as one behind another router, by **IP Address** and an optional **Name**. Such a device is matched by its IP address. It counts for routing sets, set source devices and its **MSS** value, but not for DPI bypass, which matches MAC addresses only.

## Firewall {#firewall}

The sub-tab sets how b4 manages its firewall rules, and the rules it adds besides capture.

![The Firewall sub-tab](/img/core/20261003230300.png)

### Firewall Rules {#firewall-rules}

| Field | What it does | Default |
| --- | --- | --- |
| **Skip IPTables/NFTables Setup** | For setups that add the rules by hand. In NFQUEUE mode b4 then installs no capture rules, and masquerade, MSS clamping, DSCP, DNS over TCP interception and **Expose to internet** are off. | Off |
| **Firewall Engine** | The backend for the rules of b4. **Auto-detect**, the usual choice, uses nftables when it works and iptables otherwise. | **Auto-detect** |
| **Firewall Monitor Interval (seconds)** | How often b4 puts back rules that the router's firewall removed. `0` turns this off in NFQUEUE mode. In TUN mode the minimum is 10. | `10` |

### Global MSS Clamping {#global-mss-clamping}

The clamp sets the TCP maximum segment size of every connection to port 443, the router's own included. The client's ClientHello then leaves in several small segments.

| Field | What it does | Default |
| --- | --- | --- |
| **Enable Global MSS Clamping** | Turns the clamp on. | Off |
| **MSS Size** | The segment size in bytes, from 10 to 1460. | `88` |

A device's value in [Available Devices](#device-table) wins over this one, and a set's value, on its [TCP > General](../sets/tcp/general.md#mss-clamping) tab, wins over both.

### NAT Masquerade {#nat-masquerade}

Masquerading rewrites the source address of forwarded packets to the address of the outgoing interface. Container and gateway setups need it.

| Field | What it does | Default |
| --- | --- | --- |
| **Enable NAT Masquerade** | Turns masquerading on. | Off |
| Interface tags | Limits it to the selected outgoing interfaces, typically the uplinks. None selected means every interface. | None |

### DSCP {#dscp}

b4 can write a DSCP value into the packets it sends, so that a router in front of it can tell its traffic apart. [Passing sets to another router](../guides/dscp.md) shows the setup.

| Field | What it does | Default |
| --- | --- | --- |
| **Set DSCP** | Writes **DSCP value** into the packets this host sends out. | Off |
| **DSCP value** | From 0 to 63. Turning the switch on fills in a free value, usually `7`, which keeps normal priority on Wi-Fi and on Linux routers. | `0` |
| Interface tags | The outgoing interfaces that get the values. None selected means every interface except loopback. | None |

A set can write its own value into the packets to its addresses, see [DSCP](../sets/routing.md#dscp), and the card lists such sets.

:::warning The value reaches the internet
The router that reads the value is expected to reset it on its internet uplink. Otherwise the provider sees it on every connection b4 carries.
:::

## DNS {#dns}

The sub-tab holds the DNS settings that apply to every set. The resolver itself is chosen per set, see [DNS](../dns.md).

![The DNS sub-tab](/img/dns/20261004000100.png)

| Field | What it does | Default |
| --- | --- | --- |
| **Intercept DNS over TCP** | Sends DNS over TCP to b4 too. Without it, a client that falls back to TCP skips the set's DNS server. | On |
| **Listener port** | The local port for DNS over TCP. Another value is needed only when another program holds it. | `5453` |
| **Query timeout** | Seconds to wait for a set's DNS server. | `5` |
| **Idle timeout** | Seconds an idle DNS-over-TCP connection stays open. | `30` |
| **Read/write timeout** | Seconds for one query or answer on an open connection. | `10` |
| **Forward timeout** | Seconds to wait for the client's own resolver when no set matched a TCP query. | `5` |

## SOCKS5 {#socks5-proxy}

The sub-tab configures the built-in SOCKS5 proxy. Connections through it leave from the router, and the sets apply to them, see [Connections through the built-in SOCKS5 proxy](../sets/routing.md#connections-through-the-built-in-socks5-proxy).

![The SOCKS5 sub-tab](/img/core/20261003230400.png)

| Field | What it does | Default |
| --- | --- | --- |
| **Enable SOCKS5 Proxy** | Starts the proxy. | Off |
| **Bind Address** | `0.0.0.0` listens on every address. `127.0.0.1` is the router only. | `0.0.0.0` |
| **Port** | The proxy port. | `1080` |
| **Expose to internet** | Adds a firewall rule that opens the port to any address, see [Access from the internet](./security.md#expose-to-internet). | Off |
| **Username**, **Password** | The login of the proxy, set together. Both empty means no login. | Empty |
| **Allowed Sources** | The client addresses allowed to connect. Empty means any. | Empty |

A change to **Enable SOCKS5 Proxy**, **Bind Address** or **Port** restarts the proxy and closes its connections.

### Allowed Sources {#allowed-sources}

**Add IP or CIDR** adds an address or a range, IPv4 or IPv6, and **From device** adds the address of a known device. Other addresses are refused. A client on the router itself needs `127.0.0.1/32`.

Chrome and Chromium cannot log in to a SOCKS5 proxy, so a source list is the way to limit them. With a login and a list, a client needs both. The list is not authentication: addresses can be spoofed. It adds no firewall rule either, and binding the proxy to the LAN address is what keeps it off the WAN.
