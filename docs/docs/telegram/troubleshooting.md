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

The route accepted the data and stopped answering. This is the failure mode a Cloudflare Worker produces, and b4 demotes a Worker that does it for ten minutes.

## Dialling fails

```text
[tg-bridge c=2] proxy dial DC 2 failed: dial tcp 149.154.167.51:443: i/o timeout
```

- With Direct TCP and no relay, the data-centre addresses are blocked by IP. Auto or WebSocket only, or a [DC Relay](./dc-relay.md), routes around it.
- With a DC Relay set, `socat` is not running on the VPS, the port is not open in its firewall, or the relay address is wrong. **Test direct TCP** probes without the relay and separates the two cases.

## The WebSocket bridge steers nothing

The mode needs the `tproxy` and `socket` kernel modules; without them b4 logs that transparent redirect is unavailable and installs no rule. **Settings -> Diagnostics** reports whether TPROXY is usable and names the modules that are missing and the packages carrying them, and the same list is under [Routing, requirements](../sets/routing.md#requirements).

Matching also has to happen: the set needs the `telegram` GeoIP category and the GeoIP database configured, and a set whose category is named while the database path is empty is rejected at save time rather than running without it.

A client that prefers QUIC bypasses the bridge silently, since this mode installs no QUIC rejection rule.

## The WEB proxy hostname shows a placeholder page

That page is the answer to everything the relay does not recognise, and none of those cases is logged: a hostname that does not match, a missing or wrong token, an expired ticket, an ordinary `GET` where a WebSocket upgrade was expected, or the carrier limit being reached.

Confirmation comes from the log instead:

```text
[tg-bridge] web carrier up from 203.0.113.9 (secret=desktop)
[tg-bridge] web proxy new stream 1 from 203.0.113.9
```

If neither line ever appears, work through the [preconditions](./web-proxy.md): the MTProto proxy has to be enabled and its listener has to have bound, the web server has to be running, the hostname has to reach b4 over trusted TLS on 443 with its `Host` header intact, and the link has to be the one from the share dialog rather than one assembled by hand.
