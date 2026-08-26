---
sidebar_position: 1
title: MTProto proxy
---

# MTProto proxy

A Telegram proxy that clients connect to with a secret. b4 terminates a fake-TLS handshake, so what crosses the network looks like an HTTPS connection to the domain configured as the Fake SNI, and relays the MTProto stream to a Telegram data centre over whatever [upstream](./upstream.md) transport is configured.

This is the only mode that works from outside the LAN, and the only one every Telegram client supports.

## Enabling the proxy

Settings, **MTProto Proxy**. The card's fields stay hidden until its switch is on.

| Field | What it does |
| --- | --- |
| Bind address | The address the listener binds to. `0.0.0.0` accepts from every interface, `127.0.0.1` from the host only. |
| Port | The listen port. The default is `3128`. |
| Fake SNI domain | The domain the fake-TLS handshake presents, and the site an unrecognised connection is forwarded to. |

Port `443` is a common choice because the traffic is already shaped like TLS and that port normally carries it.

:::warning The port is not opened automatically
b4 installs no firewall rule for its own listen port. On a host with a default-deny input policy, nothing reaches the proxy until a rule is added by hand, and the symptom is a client that sits at "Connecting" forever. Reaching the proxy from outside the LAN also needs a port forward on the router in front of it.
:::

Settings take effect on save. b4 restarts the proxy itself when the port, bind address, Fake SNI or an upstream setting changes, which drops the sessions it is carrying; Telegram reconnects on its own. The service does not need restarting.

## Secrets

Secrets are a list, not a single value. Each entry has a name, its own switch and its own share link, and the connection log records which secret a client used, so access can be given and withdrawn one person at a time.

**Generate secret** creates one from the current Fake SNI domain. The result is a fake-TLS secret: hexadecimal, starting with `ee`, carrying a 16-byte key and the domain name.

:::info Only fake-TLS secrets
b4 accepts nothing else. A padded (`dd`) secret, a bare 32-character key or a base64 secret from another proxy is rejected when the configuration is saved, and the save fails until the field is corrected. The domain inside the secret is a label: b4 never checks it against the server name a client actually sends.
:::

A configuration with secrets present but all of them disabled starts normally, logs `secrets: 0`, and closes every client connection immediately after accepting it.

## Adding the proxy in Telegram

**Share connection link** opens a dialog with the server address, a `tg://proxy` link, a QR code, and, when the [WEB proxy](./web-proxy.md) is configured, a second link for Telegram Desktop.

Entered by hand, in **Settings** -> **Data and Storage** -> **Proxy** -> **Add proxy** -> **MTProto**:

- **Server** - the b4 address. A LAN address for local devices; a public address or DDNS name for anything else, with the port forwarded to it.
- **Port** - the listen port from above.
- **Secret** - the value copied from the secrets list.

![Telegram proxy details](/img/mtproto/20260322135130.png)

## What an unrecognised connection sees

A connection whose fake-TLS handshake fails verification is spliced through to the Fake SNI domain on port 443, so it is answered by the real site rather than by b4. This only covers connections that got as far as a well-formed TLS handshake record: bytes that are not a TLS handshake at all are dropped, and the splice is skipped entirely when the Fake SNI field is empty.

The domain should be one that is reachable from the network in question and carries enough real traffic that its address is unremarkable.

## Where the proxy runs

- **On a VPS outside the blocked network** - the upstream transport can be Direct TCP, and no relay is needed.
- **On a router inside it** - the WebSocket transport reaches Telegram without a VPS. If WebSocket is blocked too, the direct route has to go through a [DC Relay](./dc-relay.md).

:::info
The MTProto proxy is not required for [Telegram over WebSocket](./websocket-bridge.md), which is built at start-up regardless. It is required for the [WEB proxy](./web-proxy.md), which reuses its listener and its secrets.
:::
