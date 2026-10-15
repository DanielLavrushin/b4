---
sidebar_position: 4
title: DNS & Routing
description: The set editor tab that picks a resolver for the set's domains and sends the set's traffic through an interface, a proxy or a DSCP value.
---

The **DNS & Routing** tab of the set editor decides which resolver answers the set's domains and where the set's traffic goes. Guides that use it:

- [Routing sites through a VPN or another interface](../guides/vpn.md)
- [Sending sites through a SOCKS5 proxy](../guides/proxy.md)
- [Passing sets to another router](../guides/dscp.md)
- [DNS](../dns.md), for resolvers, pinned addresses and ECH
- [Blocking ads and sites](./blocking.md)

## DNS Redirect {#dns-redirect}

The sub-tab sends the DNS queries for the set's domains to a resolver of the set's own. Other domains keep the resolver the device uses.

![The DNS Redirect sub-tab in DNS-over-HTTPS mode](/img/dns/20260810210220.png)

| Field | What it does | Default |
| --- | --- | --- |
| **Enable DNS Redirect** | Sends the queries for the set's domains to the resolver below. | Off |
| **Resolver type** | **Plain DNS (UDP)** for a resolver's IP address, **DNS-over-HTTPS** for a DoH URL. | **Plain DNS (UDP)** |
| **Fail closed when the resolver is unreachable** | On, the set's names stop resolving while the resolver is down. Off, b4 answers from the last good reply or from the device's own resolver. | Off |
| **DNS Server IP**, **DNS-over-HTTPS URL** | The resolver, typed or picked from the recommended list. | Empty |
| **Fragment DNS Queries** | Splits each query into IP fragments, for DPI that reads names in DNS queries. | Off |
| **Remove ECH keys from DNS answers** | Browsers then send an ordinary ClientHello. See [Removing ECH keys](../dns.md#removing-ech-keys). | Off |
| **Domain and addresses** | Pinned addresses in hosts file order: `157.240.0.174 www.instagram.com`. A pin applies only to names the set targets, also with the redirect off. | Empty |

## Traffic Routing {#traffic-routing}

The sub-tab sends the set's traffic through a network interface or a SOCKS5 proxy, relays it to Telegram, or blocks it.

![The Traffic Routing sub-tab with routing on](/img/routing/20260418235517.png)

| Field | What it does | Default |
| --- | --- | --- |
| **Enable Routing** | Turns routing on for the set. | Off |
| **Routing mode** | **Network interface**, **Upstream SOCKS5 proxy**, **Telegram over WebSocket (built-in)** or **Block (blackhole)**, described below. | **Network interface** |
| **Source Interfaces** | Limits the set to traffic that arrives on the selected interfaces, such as the LAN bridge. See [Which interface is which](../guides/interfaces.md). | None, every interface |
| **Resolved IP TTL (seconds)** | How long an address learned for the set's domains stays in the set. IP targets stay as long as the set lists them. | `3600` |

Routing matches destination addresses: the set's IP, CIDR, GeoIP and ASN targets, and the addresses b4 learns for its domains. A set with no domain, IP or ASN target routes nothing, and the tab warns about it. [Source Devices](./targets.md#source-devices) only limit whose traffic the set takes: routing everything a device sends also needs **Match any IP address** under **Targets > Bypass IPs**.

### Network interface {#output-interface}

The set's traffic leaves through the selected interface, such as a WireGuard or OpenVPN tunnel or a second uplink.

| Field | What it does | Default |
| --- | --- | --- |
| **Output Interface** | The interface the set's traffic leaves by. One that is down or missing is marked **unavailable**, and the set's routing stops until it is back. | None |
| **Egress IP (optional)** | Rewrites the source address of the set's traffic. b4 adds the address to the output interface itself. The device upstream then routes by that source. | Empty |
| **Gateway (optional)** | The device on the output interface's network that receives the set's traffic, such as a VPN gateway or another router. Empty uses the gateway b4 finds on the interface. | Empty |
| **Router's own traffic** | Whether the router's own connections to the set's addresses use the output interface too. See [below](#routers-own-traffic). | **Automatic** |
| **Kill switch** | Drops the set's traffic while the output interface is gone. See [below](#kill-switch). | Off |

Traffic that arrives on the output interface itself does not follow the set. With a **Gateway** on an Ethernet port, bridge, VLAN, Wi-Fi or TAP interface, IPv4 devices on that network follow the set too, apart from the gateway itself. b4 ignores a gateway that is outside the interface's network or is one of the router's own addresses, and logs the reason. See [Gateway](../internals/routing.md#gateway) and [Sites through a VPN on another device](../guides/vpn.md#another-device).

When the output interface is a tunnel that is up, such as WireGuard or a TUN or TAP device, b4 leaves the set's packets alone: the strategy tabs have no effect, and the set's connections end with `[routed->wg0]` on the **Logs** page. A set routed to a plain interface, such as a second uplink, keeps its bypass strategy.

#### Router's own traffic

The setting covers connections the router opens itself, such as package updates or a proxy running on it. Traffic from the devices on the network follows the set either way.

| Value | What happens |
| --- | --- |
| **Automatic** | The router's own traffic follows the set, except into a TUN or TAP device read by a program on the router, such as Xray or sing-box |
| **Route it too** | Always follows the set |
| **Leave it alone** | Always takes the normal route |

The exception prevents a loop: a proxy behind a TUN device opens its own connection to each destination, which would go back into the same TUN until the router runs out of memory. See [Using b4 with Xray or XrayUI](../guides/xray.md). The field is greyed out on a set limited to a source interface or to included source devices, which never takes the router's own traffic.

#### Kill switch

Without the kill switch, the set's traffic leaves through the normal uplink with the router's real address while the output interface is gone, for example while a VPN client reconnects. With it on, that traffic is dropped. Other traffic is not affected, and routing resumes by itself when the interface returns.

### Upstream SOCKS5 proxy {#upstream-socks5-proxy}

b4 takes over the set's TCP connections and opens each one again through a SOCKS5 proxy, such as Xray or sing-box on the router or on another host. The set's bypass strategy does not apply in this mode.

| Field | What it does | Default |
| --- | --- | --- |
| **Upstream SOCKS5 host** | The proxy's host name or address, `127.0.0.1` for a proxy on the router itself. | Empty |
| **Upstream SOCKS5 port** | The proxy's port, commonly `1080`. | `0` |
| **Username (optional)**, **Password (optional)** | For a proxy that requires authentication. | Empty |
| **Send domain name to upstream** | Sends the proxy the host name instead of the address when b4 can tie a name to the connection. Names pinned in the set always go as addresses. TCP only. | On |
| **Route UDP through upstream** | Sends the set's UDP through the proxy too. Off, b4 refuses the set's QUIC, and browsers fall back to TCP. | Off |
| **Fall back to direct on upstream failure** | Connects directly while the proxy cannot be reached. | Off |

#### Requirements

This mode, the **Telegram over WebSocket (built-in)** mode and the Telegram over WebSocket switch need TPROXY support in the kernel. On OpenWrt with nftables it comes from `kmod-nft-tproxy` and `kmod-nft-socket`, with iptables from `kmod-ipt-tproxy`, `kmod-ipt-socket` and `iptables-mod-tproxy`. Keenetic firmware has the modules. `kmod-nft-fib`, or `kmod-ipt-extra` with `iptables-mod-extra`, lets the rule skip the router's own addresses by type; without them b4 lists the addresses. **System Info** on **Settings > System > Service** shows under **Kernel Capabilities** whether **Transparent proxy (TPROXY)** is available, and names the missing packages.

#### Verifying that it works

A connection the set sends through the proxy ends with `[proxy]` on the **Logs** page. Without that line the connection was not diverted, usually because its address is not in the set. See [Checking the result](../guides/proxy.md#checking-the-result).

#### When the upstream itself is down

While the proxy does not answer, every connection to the set's addresses hangs and fails, on every TCP port, including other sites on a shared CDN address. The dashboard shows **Set X: proxy ... unreachable** and **upstream down**. See [When the upstream is down](../guides/proxy.md#when-the-upstream-is-down).

#### Connections through the built-in SOCKS5 proxy

A client of b4's own [SOCKS5 proxy](../settings/core.md#socks5-proxy) that asks for a host name a proxy set matches goes straight to that set's upstream. See [The built-in SOCKS5 proxy](../internals/routing.md#the-built-in-socks5-proxy).

#### Bridge netfilter

With bridge netfilter on, as OpenWrt's `dockerd` package sets it, connections from devices behind a network bridge such as `br-lan` hang in this mode and in the Telegram modes. See [Bridge netfilter](../internals/routing.md#bridge-netfilter) for the fix.

### Telegram over WebSocket (built-in)

The set's Telegram traffic is relayed over Telegram's WebSocket edge, with no proxy in the Telegram app. The switch on **Settings > Telegram** does the same for every device without a set. See [Telegram over WebSocket](../telegram/websocket-bridge.md).

### Block (blackhole)

The set's traffic never leaves the router. **Block action** decides how: **Reject (fail fast)**, the default, ends the connection at once, and **Drop (silent blackhole)** lets it time out. See [Blocking ads and sites](./blocking.md).

## DSCP {#dscp}

The **DSCP** block at the bottom of **Traffic Routing** writes the set's own DSCP value into the packets sent to the set's addresses, with routing on or off. The router in front of b4 reads the value and routes the set's traffic, for example through a VPN. See [Passing sets to another router](../guides/dscp.md).

### Fields {#dscp-settings}

| Field | What it does | Default |
| --- | --- | --- |
| **Enable per-set DSCP** | Writes the set's value. Turned on with the value at `0`, it fills in the first of `7`, `31`, `6` and `25` that no other set and no **Set DSCP** uses. | Off |
| **DSCP value** | 0-63. `0` clears the field. | `0` |

The value replaces the value of **Set DSCP** under [Settings > Core > Firewall](../settings/core.md#dscp) in those packets, whether **Set DSCP** is on or off, and the interface list there limits it too. The value stays on this router: a [shared set](./sharing.md) never carries it.

### Choosing the value {#dscp-values}

`7`, `31`, `6` and `25` keep normal priority in Linux Wi-Fi, cake and pfifo_fast; the hint under the field lists where other values land. pfSense cannot match these four, and there AF11-AF13 (`10`, `12`, `14`) disturb traffic least. Sets meant for different routes need different values.

### Which packets carry the value {#dscp-addresses}

The value goes by destination address alone, whatever the protocol and port: the set's IP, CIDR, GeoIP and ASN targets, and the addresses b4 learned for its domains. A CDN address shared by several sites carries the value for all of them. See [Which packets get the value](../internals/dscp.md#dscp-addresses) for overlapping sets.

### The first connection {#dscp-first-connection}

A packet to an address b4 has not written for the set yet leaves without the value. A domain's address is written when b4 sees its DNS answer. Where the devices' DNS does not pass through b4, as with b4 in a [container on MikroTik](../install/mikrotik.md), IP, CIDR, GeoIP and ASN targets give the value from the first packet.

### When the value is not written {#dscp-modes}

The block names the reason, and the switch cannot be turned on while it holds. No value is written:

- in the **Upstream SOCKS5 proxy**, **Telegram over WebSocket (built-in)** and **Block (blackhole)** modes;
- for a set with [source devices](./targets.md#source-devices), included or excluded, or with routing limited to **Source Interfaces**;
- for any set while [Device Filtering](../settings/core.md#device-filtering) is on with a device selected;
- for a set with no domain, IP, GeoIP or ASN target, or with **Domain-only matching** and no IP, GeoIP or ASN target;
- while **Skip IPTables/NFTables Setup** is on.

Through a tunnel such as WireGuard, the router in front of b4 sees only the tunnel's packets, which usually do not carry the value.

### Reading the value on the next router {#dscp-reader}

See [Reading the value on the next router](../guides/dscp.md#reading-the-value-on-the-next-router).

#### Linux with nftables {#dscp-reader-nftables}

See [Linux with nftables](../guides/dscp.md#linux-with-nftables).

#### RouterOS {#dscp-reader-routeros}

See [Routing a set's traffic by its DSCP value](../install/mikrotik.md#routing-by-dscp).

### Firewall rules {#dscp-firewall}

With iptables, the sets' values need the `ipset` command and the `xt_set` kernel module. The rules and their cost per packet are in [Firewall rules and cost](../internals/dscp.md#dscp-firewall).

## How it works in detail

See [How routing sets work](../internals/routing.md), [DSCP rules](../internals/dscp.md) and [Packet marks](../guides/marks.md).

### When the rules cannot be installed {#install-failures}

The **Routing Sets** row under **Firewall** in **System Info** shows how many routing sets are installed, and the error while installing fails. See [When the rules cannot be installed](../internals/routing.md#install-failures).
