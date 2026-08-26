---
sidebar_position: 3
title: MTProto
---

# MTProto

Settings, **MTProto Proxy**. Four cards: the proxy server, its secrets, the shared Telegram upstream, and the Telegram Desktop WEB proxy. The proxy card and the WEB proxy card each hide their own fields until their switch is on.

Task-shaped guides for the three Telegram modes are under [Telegram](../telegram/index.md). This page is the field reference.

## Proxy server

| Parameter | Description | Default |
| --- | --- | --- |
| Enable MTProto Proxy | Starts the listener. Also required by the WEB proxy, which reuses it. | Off |
| Bind Address | Address to listen on. `0.0.0.0` accepts from every interface, `127.0.0.1` from the host only. | `0.0.0.0` |
| Port | Listen port. | `3128` |
| Fake SNI Domain | The domain the fake-TLS handshake presents, and the site an unverified connection is spliced through to on port 443. Also seeds a generated secret. | `storage.googleapis.com` |

:::warning No firewall rule is added
b4 does not open its own listen port. On a host with a default-deny input policy the proxy is unreachable until a rule is added by hand.
:::

## Secrets

A list rather than a single value. Each entry has a name, its own switch, its own share link and its own label in the connection log.

Only fake-TLS secrets are accepted: hexadecimal, starting with `ee`, at least 17 bytes. A padded (`dd`) secret, a bare key or a base64 secret is rejected when the configuration is saved, and the save fails. The domain carried inside the secret is a label; it is never checked against the server name a client sends.

A configuration whose secrets all have their switch off starts, logs `secrets: 0`, and closes every client connection immediately.

## Telegram upstream

Shared by the proxy server and by the `Telegram over WebSocket` routing mode, so these apply even while the proxy server is off. The routes and the order they are tried in are described under [Telegram upstream](../telegram/upstream.md).

| Parameter | Description | Default |
| --- | --- | --- |
| Transport mode | `Direct TCP`, `Auto (WebSocket -> TCP)` or `WebSocket only`. Applies to the proxy server; the bridge forces Auto. | `Auto` |
| DC Relay | `host:port` of a VPS forwarding to the data centres. Ignored by the bridge and in WebSocket only mode. See [DC Relay](../telegram/dc-relay.md). | empty |
| Cloudflare Worker domain | One or more `*.workers.dev` names, comma-separated. Tried last of the WebSocket routes. | empty |
| CF proxy fallback | Uses a rotating pool of Cloudflare-proxied domains for the data centres Telegram's own edge does not serve. | On |
| Custom WebSocket domain | One domain that proxies WebSocket traffic to Telegram. b4 prepends `kws1.`, `kws2.` and so on per data centre. | empty |
| Telegram WS edge IP | Replaces the address a native `kws*.web.telegram.org` dial goes to. Does not affect the custom domain. | `149.154.167.220` |

## Telegram Desktop WEB proxy

| Parameter | Description | Default |
| --- | --- | --- |
| Enable the WEB carrier | Serves the MTProto stream over HTTPS on the relay hostname. Does nothing while the proxy server is off. | Off |
| Relay hostname | A bare public DNS name, no scheme, port or path, punycode for international names. Needs its own hostname with publicly trusted TLS on 443. | empty |

Both fields take effect on the next request. The full set of preconditions is on [Telegram Desktop WEB proxy](../telegram/web-proxy.md).

## Advanced

Three timeouts where `0` selects the built-in value rather than turning anything off.

| Parameter | Description | Default |
| --- | --- | --- |
| Max Connections | Ceiling on accepted TCP connections, counted before any handshake, so probes and port scans consume it too. It is not a limit on authenticated clients, and it does not apply to the WEB carrier. `0` uses the built-in value. | `2048` |
| TCP User Timeout (sec) | Force-closes a client connection after this long with unacknowledged data, so a phone that left the network is detected in about two minutes instead of about fifteen. `0` uses the built-in value, `-1` disables it. Applies to the accepted client socket only; connections towards Telegram use a fixed 120 seconds. | `120` |
| Idle Timeout (sec) | Closes a relayed session after this long with no traffic in either direction. `0` uses the built-in value, `-1` disables it. | `300` |

## Fallback sources

| Parameter | Description | Default |
| --- | --- | --- |
| CF proxy domain list URL | Where the CF proxy pool is refreshed from, hourly. | tg-ws-proxy's list |
| DC list fallback mirror | Uses the mirror below when Telegram's own endpoint for the data-centre list is unreachable. | On |
| DC list mirror URL | The mirror to use. The default is hosted by the b4 author and receives the requesting IP address, nothing else. | b4 author's mirror |
| Bridge Handshake Wait (sec) | How long the `Telegram over WebSocket` bridge waits for a client's first byte before dropping the connection. `0` uses the built-in value; `-1` waits indefinitely rather than disabling the wait. | `180` |

:::info Saving does not restart the service
b4 restarts the MTProto proxy itself when the enable switch, port, bind address, Fake SNI, transport mode, custom WebSocket domain, WS edge IP or CF proxy fallback changes, which drops the sessions it is carrying. Secrets and the WEB proxy fields are applied without restarting anything.
:::
