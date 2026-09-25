---
sidebar_position: 7
title: Troubleshooting
---

# Troubleshooting

Every Telegram log line carries the `[tg-bridge]` tag, with a per-connection counter on the lines that belong to one client: `[tg-bridge c=1f]`.

:::info Handshake lines need debug logging
`proxy fake-TLS handshake OK` and `proxy fake-TLS failed` are logged at debug level. At the default level a working proxy shows only the relay line, and a rejected client shows nothing at all.
:::

## Telegram sits at "Connecting"

A working session logs one line per client at info level:

```text
[tg-bridge c=3] proxy relay [phone] 192.168.1.50:51234 <-> DC2 via ws:kws2.web.telegram.org
```

If nothing appears at all, the connection is not reaching b4. On a host with a default-deny input policy nothing does: b4 installs no firewall rule for its own listen port, and reaching it from outside the LAN also needs a port forward.

If the relay line appears and Telegram still waits, the problem is upstream. **Test connection** probes data centre 2 over the configured transports.

## Telegram says the proxy is misconfigured and turns it off

```text
[tg-bridge c=7] upstream answered -444 (invalid DC) for a DC 2 session: the route does not
end at the data center the client asked for, cutting it and ranking the route down
```

The session reached a data centre other than the one the client asked for, and both Telegram clients answer a single `-444` by disabling the proxy. The client repeats its data centre inside an encrypted field that b4 can neither read nor correct, so this is always the route's fault rather than a setting.

b4 swallows the code, ranks that route down and lets the client redial onto another one, so a single occurrence is expected to recover by itself. A run of them for the same data centre means the routes available for it all end in the wrong place, and the fix is to give it another one: a [Cloudflare Worker domain](./cloudflare-worker.md), or a custom WebSocket domain.

## Media, stickers or reactions do not load

Telegram's own WebSocket edge serves data centres 2 and 4 only. Media for foreign channels comes from 1, and 203 carries media as well, so both need a Cloudflare route. Setting a **Cloudflare Worker domain**, or leaving **CF proxy fallback** on, is what covers them.

## The client is rejected

```text
[tg-bridge c=4] proxy fake-TLS failed from 192.168.1.50:51234: HMAC verification failed for all secrets
```

The secret in Telegram does not match any enabled secret in b4. A secret whose entry is switched off produces the same line.

```text
[tg-bridge c=4] proxy fake-TLS failed from 192.168.1.50:51234: timestamp out of range: diff=214s
```

The clocks on the client and the b4 host disagree by more than two minutes. Both need NTP.

A configuration whose secrets are all disabled logs `secrets: 0` at start-up and closes every connection right after accepting it.

## The upstream goes silent mid-session

```text
[tg-bridge c=9] upstream silent for 8s with 512 B awaiting an answer, cutting the relay
```

The route accepted two or more writes and answered none of them, eight seconds after the second and three after the latest. A single write that expects no reply, such as an acknowledgement on an idle session, does not count. A session that Telegram closes within six seconds of a request, while the route has been silent for eight, counts as well. This is the failure mode a Cloudflare Worker produces, and b4 ranks a Worker down for ten minutes after two such sessions within five minutes.

## Dialling fails

```text
[tg-bridge c=2] proxy dial DC 2 failed: dial tcp 149.154.167.51:443: i/o timeout
```

- With Direct TCP and no relay, the data-centre addresses are blocked by IP. Auto or WebSocket only, or a [DC Relay](./dc-relay.md), routes around it.
- With a DC Relay set, `socat` is not running on the VPS, the port is not open in its firewall, or the relay address is wrong. **Test direct TCP** probes without the relay and separates the two cases.

## The Telegram over WebSocket card shows a warning

The card on Settings, Telegram reads **Working** when the diversion rule is installed and the bridge listener is running. Otherwise it reads **Not working**, and resting the pointer on it names the part that is missing: the listener, or the firewall rule that diverts Telegram traffic to the bridge. The warnings above the status box name the cause. The first three conditions below stop the bridge from diverting anything; the others leave it running. The [field reference](../settings/mtproto.md#telegram-over-websocket) lists the card's other contents.

### TPROXY support is missing

The warning reads "This kernel has no TPROXY support". The bridge rides TPROXY, which needs the `tproxy` and `socket` kernel modules. Without them b4 logs that the firewall does not support them, installs no diversion rule for the switch and leaves Telegram on the normal path, and the card reads **Not working**. The warning names the packages that provide the missing modules, or the modules themselves when no package is known; on OpenWrt they are `kmod-nft-tproxy` and `kmod-nft-socket`, and the full list is under [Routing, requirements](../sets/routing.md#requirements). **Check again** re-runs the kernel check once they are installed and then installs the rule. The same result is in the **System Info** dialog on Settings, Core, under **Kernel Capabilities**, where the **Transparent proxy (TPROXY)** row reads available or unavailable.

### Firewall setup is turned off

**Skip IPTables/NFTables setup** is on in [Settings, Core](../settings/core#firewall), stored as `system.tables.skip_setup` and also set by the `--skip-tables` flag. b4 then installs no firewall rules at start-up, the bridge's diversion among them, so after a restart no Telegram connection reaches the listener whatever the switch says. Saving the settings installs the routing rules until the next restart.

### The listener could not start

The warning reads "The bridge listener on port 13443 failed", followed by the error. The port could not be bound, for example because another process already listens on it. The port is fixed. b4 keeps retrying and installs the diversion rules only once the listener is up, so in the meantime Telegram connections take the normal path instead of being sent to a closed port. `netstat -ltnp` on the router, where the build supports `-p`, shows which process holds the port.

When only the IPv6 socket could not be opened, the card instead shows a note that IPv4 goes through the bridge and Telegram over IPv6 takes the normal path. The note appears only while IPv6 support is on. The IPv6 socket is tried again only when the listener restarts, for example after b4 restarts.

### The address list could not be downloaded

The warning carries the download error and names the source of the list still in use. Neither `core.telegram.org` nor the b4 mirror answered. The list already in use stays, whichever source it came from, and the built-in list is always part of it. The next attempt follows after 30 seconds, and the wait doubles after each further failure up to an hour; after a success the list is refreshed once a day. **Refresh addresses** tries again at once.

### b4 runs in TUN mode

The bridge has not been verified with the TUN engine. The switch can be turned on, and whether Telegram connections reach the listener shows in the card's session counter and on the [Traffic](../connections.md) page, under the set name **Telegram bridge**.

### Sets use the Telegram over WebSocket routing mode

The warning lists those sets, each linked to its editor, with disabled ones marked **off**. While the switch is on, a set in the *Telegram over WebSocket (built-in)* mode adds nothing: the switch already sends Telegram's addresses from every device into the same bridge. A set limited to devices or interfaces still takes its devices first, and they end up in the same bridge. Such a set, unless it is limited by an included source-device list, also keeps exempting the bridge's own connections to Telegram's addresses from DPI processing, which the switch alone does not do; see [DPI processing](./websocket-bridge.md#dpi-processing).

## The counters under the bridge status

A line under the status box appears once any of these is above zero.

- **Passed on undecoded** counts connections that were not MTProto, HTTPS to Telegram's web hosts among them, and MTProto sessions b4 could not map to a data centre. They are handed to the Cloudflare Worker or dialled directly, so a growing count is not a fault in itself.
- **Data center dial failures** counts MTProto sessions for which no upstream route connected. The log carries `bridge dial DC <n> failed` at error level, at most once per interval for the same data centre. The causes under [Dialling fails](#dialling-fails) apply, except the DC Relay, which the bridge does not use.
- **Closed without a handshake** counts connections that closed, or stayed silent for the whole **Bridge Handshake Wait**, before sending a first byte. Telegram opens connections to a data centre before it has anything to send, so some of these are expected, and a shorter wait produces more of them.

## Some Telegram traffic does not go through the bridge

- **Voice calls** travel over UDP, which the bridge does not take.
- **QUIC** is not rejected, so a client that prefers QUIC to a Telegram address bypasses the bridge silently.
- **IPv6** ranges are diverted only while IPv6 support is on in [Settings, Core](../settings/core#protocols).
- **Devices excluded by [device filtering](../settings/core.md#device-filtering)** keep the normal path.
- **A set limited to devices or source interfaces** that matches Telegram addresses handles its devices before the switch does, and so does every routing set while device filtering is in allow-list mode.
- **The router's own connections** keep the normal path while device filtering is in allow-list mode. See [Order among sets](./websocket-bridge.md#order-among-sets).
- **An address outside the list in use** is not diverted. The card shows how many ranges are in use and where they came from.

## A block set stops the bridge

A block set that is not limited to source interfaces or an included source-device list, and whose targets cover Telegram addresses, still blocks the router's own connections to them, and the bridge's own upstream connections are router connections. Bridged sessions then fail to dial for every address the block set covers.

## A set in the Telegram over WebSocket mode steers nothing

The set mode has the same TPROXY requirement as the switch, see [TPROXY support is missing](#tproxy-support-is-missing).

Matching also has to happen: the set needs the `telegram` GeoIP category and the GeoIP database configured, and a set whose category is named while the database path is empty is rejected at save time rather than running without it. A set scoped to source interfaces or devices leaves the router's own connections out, Telegram Desktop on the router included.

## The WEB proxy hostname shows a placeholder page

That page is the answer to everything the relay does not recognise, and none of those cases is logged: a hostname that does not match, a missing or wrong token, an expired ticket, an ordinary `GET` where a WebSocket upgrade was expected, or the carrier limit being reached.

Confirmation comes from the log instead:

```text
[tg-bridge] web carrier up from 203.0.113.9 (secret=desktop)
[tg-bridge] web proxy new stream 1 from 203.0.113.9
```

If neither line ever appears, work through the [preconditions](./web-proxy.md): the MTProto proxy has to be enabled and its listener has to have bound, the web server has to be running or a relay port has to be set, the hostname has to reach b4 over trusted TLS on 443 with its `Host` header intact, and the link has to be the one from the share dialog rather than one assembled by hand.
