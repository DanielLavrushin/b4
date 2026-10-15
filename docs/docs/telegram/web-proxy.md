---
sidebar_position: 3
title: Telegram Desktop WEB proxy
description: Serving the MTProto proxy as an ordinary HTTPS site for Telegram Desktop 7.1.1 and later.
---

The WEB proxy serves the [MTProto proxy](./mtproto-proxy.md) as an ordinary HTTPS site on a hostname of its own. Telegram Desktop 7.1.1 and later loads that site in a hidden window and carries all of its connections through it, so the client opens no MTProto connection and the traffic looks like a browser visiting a website. The proxy's secrets and its [upstream](./upstream.md) apply as usual.

The hostname needs a publicly trusted certificate on port 443, so the WEB proxy suits b4 on a VPS more than a router behind carrier-grade NAT.

## Requirements

- The [MTProto proxy](./mtproto-proxy.md) is on, its port is free, and it has at least one secret.
- A public DNS name points at the b4 host and is used for nothing else. b4 answers every path on that name, so the web interface is not reachable there.
- A publicly trusted TLS certificate for that name is served on port 443, either by b4 itself or by a reverse proxy in front of it. The reverse proxy keeps the `Host` header, or sets `X-Forwarded-Host`, and allows the WebSocket upgrade on `/api/v1/ws`. A self-signed certificate is not enough.

## Setup

1. Open **Settings > Telegram**. The **Telegram Desktop WEB proxy** card is shown while the MTProto proxy is on.
2. Turn on **Enable the WEB carrier**.
3. Enter the **Relay hostname**: a bare name, with no scheme, port or path. An international name goes in its punycode (`xn--`) form.
4. Check **Relay port**. Turning the carrier on fills it with `443`, the port Telegram connects to. See [Relay port](#relay-port).
5. Under **Certificate for the relay hostname**, enter the **Certificate file** and **Private key file** for the hostname. Left empty, the relay uses the web server's certificate.
6. On a router, turn on **Expose to internet** under **Relay port**. When the relay port is not 443, forward WAN port 443 to it.
7. Click **Save Changes**.
8. On the **Secrets** card, click the share button of a secret, copy the link in the **WEB · Telegram Desktop 7.1.1+** row, and add it in Telegram Desktop.

![The WEB proxy card](/img/telegram/20260826233004.png)

![The share dialog, with the direct and WEB links](/img/telegram/20260826233005.png)

The link has the form `https://t.me/webproxy?server=<hostname>&secret=dd<32 hex characters>`. It carries another form of the secret than the list shows, so a link assembled by hand does not work, and the share dialog is the only source. The relay has no login of its own: anyone who holds the link can use the proxy.

:::warning Renaming the hostname breaks every link
The link is bound to the relay hostname. After **Relay hostname** changes, every link handed out stops working and has to be shared again.
:::

## Relay port

Telegram always opens `https://<hostname>/` on port 443, and the link carries no port. Public port 443 therefore has to lead to the relay: the relay port set to 443, a forward from 443, or a reverse proxy on 443.

| **Relay port** | Effect |
| --- | --- |
| A port, usually `443` | The relay gets a listener of its own. Any other name or an IP address on that port gets the placeholder page, so the web interface is not reachable through it. |
| Empty | The relay is served on the web interface's port, on the relay hostname only. Whatever brings public port 443 to that port also brings the web interface, and its login page answers on the public IP address. |

On a router, the usual layout is a relay port with **Expose to internet** on, and a forward from WAN port 443 when the relay port is not 443. The web interface port is neither exposed nor forwarded, so the interface stays reachable only from the local network.

The relay port uses the web server's certificate unless **Certificate for the relay hostname** holds one. A public certificate on the web server makes the web interface HTTPS-only, with a browser warning on every visit by IP address. A certificate under **Certificate for the relay hostname** goes to the relay port only, and the web interface keeps its own.

## Placeholder page

Every visitor that is not Telegram sees a plain "Service status" page, the same on every b4 installation. **Placeholder page** on the card replaces it with one HTML file of up to 1 MiB, and **Remove** restores the built-in page. The relay answers every path with that same file, so styles and images have to be inside it.

The file is kept as `webproxy_page.html` next to the configuration file. A file copied there by hand is served the same way, and deleting it restores the built-in page.

## Checking the result

Opening the hostname in a browser shows the placeholder page even when the relay does not work, so it proves nothing. The log confirms a working relay:

```text
[tg-bridge] web carrier up from 203.0.113.9 (secret=desktop)
[tg-bridge] web proxy new stream 1 from 203.0.113.9
```

Connections carried this way appear on the [Traffic](../connections.md) page as MTProto, with the secret's name.

## When it does not work

- **Only the placeholder page shows, and the log has no `web carrier up` line.** See [The WEB proxy hostname shows a placeholder page](./troubleshooting.md#the-web-proxy-hostname-shows-a-placeholder-page).
- **Nothing answers over TLS on port 443.** The certificate or key does not load, and the log says so. On the web interface's port, b4 then falls back to plain HTTP, and a relay port of its own does not start at all.
- **A phone has no WEB proxy entry.** Telegram's mobile apps do not support it and use the MTProto link from the same dialog.
