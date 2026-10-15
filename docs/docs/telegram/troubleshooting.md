---
sidebar_position: 7
title: Telegram problems
description: Symptoms of the MTProto proxy, Telegram over WebSocket and the WEB proxy, with their causes and fixes.
---

Telegram's lines in the [log](../logs.md) carry the `[tg-bridge]` tag, and the lines of one connection share a counter, such as `[tg-bridge c=1f]`. The proxy's handshake lines, `proxy fake-TLS handshake OK` and `proxy fake-TLS failed`, appear only at the debug level.

## Telegram sits at "Connecting"

A working session of the MTProto proxy logs one line per client:

```text
[tg-bridge c=3] proxy relay [phone] 192.168.1.50:51234 <-> DC2 via ws:kws2.web.telegram.org
```

- **No such line.** The connection does not reach b4. From outside, nothing arrives until **Expose to internet** is on for the proxy port, and a router or modem in front of the host also needs a port forward. In **Internet** mode the share dialog names the causes it detects, and [What the switch cannot open](../settings/security.md#expose-limits) lists the ones no rule on the b4 host fixes.
- **The line appears, and Telegram still waits.** The problem is between b4 and Telegram. **Test connection** on the **Telegram upstream (shared)** card shows which routes work.

## Telegram says the proxy is misconfigured and turns it off

```text
[tg-bridge c=7] upstream answered -444 (invalid DC) for a DC 2 session: the route does not
end at the data center the client asked for, cutting it and ranking the route down
```

**Cause:** the session reached another data centre than the one the client asked for. b4 keeps the code from the client, ranks the route down and lets the client reconnect over another route.

**Fix:** a run of these lines for one data centre means every route to it ends in the wrong place. Another route helps: a [Cloudflare Worker domain](./cloudflare-worker.md) or a custom WebSocket domain. Through Telegram over WebSocket, b4 corrects the data centre by itself, see [Which data centre a session goes to](./websocket-bridge.md#which-data-centre-a-session-goes-to).

## Media, stickers or reactions do not load

**Cause:** Telegram's own WebSocket edge serves data centres 2 and 4 only. Media for foreign channels comes from data centre 1, and 203 carries media too.

**Fix:** a [Cloudflare Worker domain](./cloudflare-worker.md), with **CF proxy fallback** left on.

## The client is rejected

At the debug level, the log shows the reason:

```text
[tg-bridge c=4] proxy fake-TLS failed from 192.168.1.50:51234: HMAC verification failed for all secrets
```

The secret in Telegram matches no enabled secret in b4. The fix is the link from the share dialog, or turning the secret's switch on.

```text
[tg-bridge c=4] proxy fake-TLS failed from 192.168.1.50:51234: timestamp out of range: diff=214s
```

The clocks of the device and the b4 host differ by more than two minutes. NTP on both fixes it.

With every secret switched off, the proxy logs `secrets: 0` at start-up and closes every connection at once.

## The upstream goes silent mid-session

```text
[tg-bridge c=9] upstream silent for 8s with 512 B awaiting an answer, cutting the relay
```

**Cause:** the route accepted the client's requests and stopped answering, as a Cloudflare Worker does when Cloudflare ends its connection.

**Fix:** b4 cuts the session, and sets a Worker aside for ten minutes after two such sessions within five minutes. For a Worker that stalls often, see [Cloudflare Worker relay](./cloudflare-worker.md#when-it-does-not-work).

## Dialling fails

```text
[tg-bridge c=2] proxy dial DC 2 failed: dial tcp 149.154.167.51:443: i/o timeout
```

- **Direct TCP without a relay:** Telegram's addresses are blocked. **Auto (WebSocket → TCP)**, **WebSocket only** or a [DC Relay](./dc-relay.md) gets around it.
- **With a DC Relay:** `socat` is not running on the VPS, its port is closed, or the relay address is wrong. **Test direct TCP** tries without the relay and tells the cases apart.

## The Telegram over WebSocket card shows a warning

The card reads **Working** when the bridge's firewall rule is installed, its listener runs and no other program's rule sits above it. Otherwise it reads **Not working**, and resting the pointer on it names the missing part. A warning above the **Bridge status** box names the cause. The first four below stop the bridge; the others leave it running.

### TPROXY support is missing

**This kernel has no TPROXY support.** The bridge needs the `tproxy` and `socket` kernel modules. The warning names the packages that add them: on OpenWrt, `kmod-nft-tproxy` and `kmod-nft-socket`; for other systems, see [Routing requirements](../sets/routing.md#requirements). After they are installed, **Check again** repeats the check and installs the rule. The **Transparent proxy (TPROXY)** row in **System Info** on **Settings > System > Service** shows the same result.

### Firewall setup is turned off

**Skip IPTables/NFTables Setup** is on in [Settings > Core > Firewall](../settings/core.md#firewall), so b4 installs no firewall rules at start-up, the bridge's included. Saving the settings installs them only until the next restart. Turning the setting off fixes it.

### The listener could not start

**The bridge listener on port 13443 failed**, followed by the error. The fixed port could not be bound, for example because another process holds it. b4 keeps retrying, and Telegram takes the normal path meanwhile. `netstat -ltnp` on the router names the process, where the build supports `-p`.

When only IPv6 fails, a note says that IPv4 goes through the bridge and IPv6 takes the normal path. b4 tries IPv6 again only when the listener restarts, for example after b4 restarts.

### Another program's rule sits above the bridge

**Not working** names a rule such as `DIVERT`, which takes the bridge's connections. XrayUI adds one each time xray starts. b4 moves its rule back above it at the next firewall check, and **Check again** does it at once. With **Firewall Monitor Interval (seconds)** at `0`, only **Check again** or saving the settings restores the order. Background: [Using b4 with Xray or XrayUI](../guides/xray.md#xrayuis-tproxy-rule-and-connections-b4-diverts).

### Bridge netfilter is on for a network bridge

**Bridge netfilter is on for**, followed by network bridges such as `br-lan`. Telegram on the devices behind them stays at "Connecting", while the router's own Telegram works and the status reads **Working**. Sets in the **Upstream SOCKS5 proxy** mode are affected too. On OpenWrt, the `dockerd` package turns the setting on. **System Info** on **Settings > System > Service** shows how to turn it off, and [Bridge netfilter](../sets/routing.md#bridge-netfilter) describes the effect on Docker.

### The address list could not be downloaded

Neither `core.telegram.org` nor the b4 mirrors answered. The warning shows the error and the source of the list still in use, which stays, with the built-in list always part of it. b4 tries again by itself, and **Refresh addresses** tries at once.

### b4 runs in TUN mode

The bridge has not been verified with the TUN engine. **Relayed sessions** on the card and the **Telegram bridge** entries on the [Traffic](../connections.md) page show whether Telegram reaches it.

### Sets use the Telegram over WebSocket routing mode

The warning lists the sets in the **Telegram over WebSocket (built-in)** mode, disabled ones marked **off**. While the switch is on they add nothing, since the switch already sends Telegram from every device into the same bridge. Such a set, unless it is limited by an included source-device list, still keeps the bridge's connections to its targets out of DPI processing, see [DPI processing](./websocket-bridge.md#dpi-processing).

## The counters under the bridge status

The line under the **Bridge status** box appears once a counter is above zero.

| Counter | Counts | What to do |
| --- | --- | --- |
| **Passed on undecoded** | Connections that were not MTProto, such as HTTPS to Telegram's web hosts, and sessions b4 could not map to a data centre. They go on to the Cloudflare Worker or directly. | Nothing: a growing count is not a fault in itself. |
| **data center dial failures** | Sessions for which no route to Telegram connected. The log shows `bridge dial DC <n> failed`. | The causes under [Dialling fails](#dialling-fails), apart from the DC Relay, which the bridge does not use. |
| **closed without a handshake** | Connections that closed, or stayed silent for the whole **Bridge Handshake Wait (sec)**, before sending anything. | Nothing: Telegram opens connections before it needs them. A shorter wait produces more. |

## Some Telegram traffic does not go through the bridge

- **Voice calls** go over UDP, which the bridge does not take.
- **QUIC** is not rejected, so a client that uses QUIC to a Telegram address passes the bridge by.
- **IPv6** goes through the bridge only while **Enable IPv6 Support** is on in [Settings > Core > Packet Engine](../settings/core.md#protocols).
- **Devices excluded by [device filtering](../settings/core.md#device-filtering)** keep the normal path.
- **A set limited to devices or source interfaces** that covers Telegram addresses handles its devices first.
- **An address outside the list in use** does not go through the bridge.

## A block set stops the bridge

**Cause:** a block set covers Telegram addresses and is not limited to source interfaces or to an included list of source devices. It then also blocks the router's own connections to those addresses, the bridge's connections to Telegram among them.

**Fix:** limiting the block set to source interfaces or to an included list of source devices, or removing Telegram's addresses from its targets.

## A set in the Telegram over WebSocket mode steers nothing

- The set mode needs TPROXY like the switch, see [TPROXY support is missing](#tproxy-support-is-missing).
- The set needs the `telegram` GeoIP category and a configured GeoIP database. A set that names a category without a database is rejected on save.
- A set limited to source interfaces or devices leaves out the router's own connections, Telegram Desktop on the router included.

## The WEB proxy hostname shows a placeholder page

**Cause:** the relay answers everything it does not recognise with the placeholder page and logs nothing: a hostname that does not match, a missing or wrong token, an ordinary page request, or a reached carrier limit. A working relay logs:

```text
[tg-bridge] web carrier up from 203.0.113.9 (secret=desktop)
[tg-bridge] web proxy new stream 1 from 203.0.113.9
```

**Fix:** when neither line appears, one of the [requirements](./web-proxy.md#requirements) is not met:

- the MTProto proxy is on and its listener started;
- the web server runs, or a relay port is set;
- the hostname reaches b4 over trusted TLS on port 443, with its `Host` header intact;
- the link comes from the share dialog.
