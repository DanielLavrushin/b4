---
sidebar_position: 50
title: Sending sites through a SOCKS5 proxy
description: Hand the connections of selected sites to an upstream SOCKS5 proxy, such as Xray or sing-box, without proxy settings on the devices.
---

A set in **Upstream SOCKS5 proxy** mode hands the connections of its sites to a SOCKS5 proxy, such as Xray or sing-box on the router or on another host in the network. The devices need no proxy settings. The mode also helps where blocking is whitelist-based and direct connections to other addresses are dropped.

## Requirements

- A SOCKS5 proxy the router can reach, and its address and port. Xray and sing-box carry UDP only when UDP is turned on in their inbound.
- TPROXY support in the kernel. On OpenWrt with nftables:

  ```sh
  opkg install kmod-nft-tproxy kmod-nft-socket
  ```

  On builds with `apk`, `apk add` takes the same packages. Other systems are listed under [Requirements](../sets/routing.md#requirements). **System Info** on **Settings > System > Service** shows under **Kernel Capabilities** whether **Transparent proxy (TPROXY)** is available.
- A set with the sites as domains or addresses. See [Adding sites to a set](./add-sites.md).

## Setup

1. Open **Sets** and open the set, or click **Create Set** and add the sites on the **Targets** tab.
2. Open the **DNS & Routing** tab, then **Traffic Routing**.
3. Turn on **Enable Routing**.
4. Set **Routing mode** to **Upstream SOCKS5 proxy**.
5. Enter **Upstream SOCKS5 host** and **Upstream SOCKS5 port**: for a proxy on the router itself, `127.0.0.1` and the port of its SOCKS5 inbound.
6. Fill in **Username (optional)** and **Password (optional)** if the proxy requires them.
7. Turn on **Route UDP through upstream** only if the proxy supports UDP.
8. Click **Save Changes**, or **Create Set** for a new set.

**Send domain name to upstream** is on by default: the proxy gets the site's name and resolves it itself. The fields are described under [Upstream SOCKS5 proxy](../sets/routing.md#upstream-socks5-proxy). The set's bypass strategy does not apply in this mode, since the proxy reaches the sites.

## Checking the result

1. Add `ipinfo.io` to the set's domains for the test.
2. On a device, run `curl -s https://ipinfo.io/ip` or open the address in a browser. It shows the proxy's exit address.
3. Open **Logs**. The connection ends with `[proxy]`:

   ```text
   TCP 192.168.1.37:20854 → 34.117.59.81:443 ipinfo.io sni-set=ipinfo.io [proxy]
   ```

Without a `[proxy]` line the connection was not diverted, usually because its address was not in the set. A single address is the clearer test, since a full web page loads parts from other hosts.

## Falling back to direct

**Fall back to direct on upstream failure** connects directly while the proxy cannot be reached. The sites keep opening, without the proxy, and the provider sees where the connections go. Off, the default, suits whitelist-based blocking, where a direct connection cannot work anyway, and setups where a failure has to be visible.

## When the upstream is down {#when-the-upstream-is-down}

While the proxy does not answer, every connection to the set's addresses hangs and then fails, on every TCP port. A CDN address the set shares with other sites breaks those sites as well. b4 shows it:

- under **Needs attention** on the dashboard: **Set X: proxy ... unreachable**, with "this set's traffic is not getting through", or "traffic goes direct" while the fallback is on;
- as **upstream down** next to the set in the dashboard's **Sets** panel;
- in the log, at most once a minute, and once more when the proxy answers again.

The cause is on the proxy's side: it is not running, it listens on another port, or the address on the set is wrong.

## When it does not work

- **No `[proxy]` line.** Usually the address the device connected to is not in the set. A device with encrypted DNS hides its lookups from b4; turning off secure DNS on it, or listing the site's domains in the set, fixes it.
- **Devices on the network hang, the router's own connections work.** Bridge netfilter is on, as OpenWrt's `dockerd` package sets it. The **Bridge netfilter** row in **System Info** shows it, and [Bridge netfilter](../internals/routing.md#bridge-netfilter) gives the fix.
- **UDP or QUIC does not work.** With **Route UDP through upstream** on and a proxy without UDP support, the set's UDP is dropped, and the log names the set. Tor's SOCKS port has no UDP support either. Off, b4 refuses the set's QUIC, and browsers use TCP through the proxy.
- **Video is slow through the proxy.** With **Route UDP through upstream** on, QUIC goes through the proxy too, and a proxy that carries UDP slowly, such as one behind a TCP-only VPN, slows video down. **Block QUIC (force TCP)** on the set's [UDP](../sets/udp.md#blocking-quic) tab makes browsers use TCP. The set's other UDP, such as WebRTC, still goes through the proxy. With the option off, QUIC is refused as well, but the set's other UDP leaves directly, and WebRTC can show the router's public address.
- **Some connections bypass the proxy over IPv6.** While **Enable IPv6 Support** is off, b4 handles only IPv4, and the dashboard shows **IPv6 traffic skips all sets**.
- **The proxy runs on another device and the set covers its server.** b4 recognises the proxy's own connections on the router by its process. On another device it recognises them only for a set that targets `0.0.0.0/0` or `::/0`, or while a connection to the same destination is relayed through the proxy. Excluding that device under **Targets > Source Devices** keeps its traffic out of the set.
