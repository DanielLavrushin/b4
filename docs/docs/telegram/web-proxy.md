---
sidebar_position: 3
title: Telegram Desktop WEB proxy
---

# Telegram Desktop WEB proxy

b4 serves the MTProto stream over ordinary HTTPS on a hostname of its own. Telegram Desktop 7.1.1 and later loads that hostname in a hidden WebView, which opens a single WebSocket back to b4 and multiplexes every Telegram connection over it. The client opens no MTProto socket, and what a network sees is a browser loading a website.

Each multiplexed stream is handed to the same code path as a plain MTProto connection: the same secrets, the same handshake, the same [upstream](./upstream.md) routing to a data centre. Nothing is installed on the client.

Because the hostname needs a publicly trusted certificate on port 443, this is a mode for b4 on a VPS rather than on a router behind CGNAT.

## What has to be true first

Four conditions, none of which the interface checks:

- **The MTProto proxy is enabled and its listener binds.** The relay resolves to nothing while Settings, MTProto Proxy is off, and secrets are loaded only when that listener starts. If its port is already in use, start-up fails, no secrets load, and the relay answers every request with the placeholder page.
- **The web server is running.** The relay is a virtual host on b4's own web server, so a web server port of `0` means the relay does not exist. Telegram fixes the scheme and the port, so the hostname has to answer `https://<host>/` on 443, either from b4 directly or through a reverse proxy.
- **The hostname is not shared with the interface.** Once the `Host` header matches, b4 claims every path on that name. The interface, the API and the login endpoints all return the placeholder page there.
- **TLS is publicly trusted.** A self-signed certificate is enough for the b4 interface and is not enough here.

:::warning An unusable certificate does not stop b4
When the configured certificate and key do not load, b4 logs a warning and falls back to plain HTTP. The web server comes up, the relay comes up with it, and nothing answers on 443.
:::

## Setting it up

1. **Point a hostname at the b4 host.** A bare public DNS name. The field rejects a scheme, a port, a path, credentials, an IP address and a single-label name; an international name has to be entered in its punycode (`xn--`) form.
2. **Serve it over trusted TLS on 443.** Either b4's own web server with a valid certificate and key, or a reverse proxy that terminates TLS, preserves the `Host` header or sets `X-Forwarded-Host`, and allows the WebSocket upgrade on `/api/v1/ws`.
3. **Enable the MTProto proxy** and add at least one secret. Links are generated per secret.
4. **Enable the WEB carrier** and enter the relay hostname, under Settings, MTProto Proxy, **Telegram Desktop WEB proxy**. Both fields take effect on the next request; nothing restarts.
5. **Copy the link** from the secret's **Share connection link** dialog, the row labelled *WEB · Telegram Desktop 7.1.1+*, and add it in Telegram Desktop.

![The WEB proxy card](/img/telegram/20260826233004.png)

The link has the form `https://t.me/webproxy?server=<hostname>&secret=dd<32 hex characters>`.

![The share dialog, with the direct and WEB links side by side](/img/telegram/20260826233005.png)

:::info The link is not the secret shown in the list
Telegram Desktop refuses fake-TLS (`ee`) secrets for WEB entries, so the link carries the padded (`dd`) form of the same key, without the domain the `ee` secret ends in. A link assembled by hand from the visible secret does not work; the dialog is the only correct source.
:::

:::warning Renaming the hostname invalidates every link
The token in the link is derived from the secret and the hostname together, and it is recomputed against the current hostname on every request. Changing the relay hostname silently breaks every link already handed out, and each one has to be reissued.
:::

## Confirming it works

Opening the hostname in a browser proves nothing. A wrong hostname, a missing or wrong token, an expired ticket, a plain `GET` where a WebSocket upgrade was expected and an exhausted carrier limit all produce the same placeholder page, and none of them is logged.

What confirms the relay is carrying traffic is the log:

```text
web carrier up from <ip> (secret=<label>)
web proxy new stream <n> from ...
```

followed by the ordinary relay line for the data centre the stream reached. Connections carried this way also appear in the connections list tagged as MTProto with the secret's name.

## Boundaries

- Telegram Desktop only, 7.1.1 and later. The mobile clients have no WEB proxy entry type.
- The relay is served ahead of b4's web authentication. The token is the only gate: there is no source allowlist and no rate limit, so the hostname plus a secret is enough for anyone holding both.
- Every Telegram connection from one client rides a single carrier. When the carrier drops, on a 90 second idle timeout, a protocol violation, or the client closing, all of its streams end at once.
- `Max connections` and the TCP timeouts do not apply here. The WEB path is bounded separately at 256 concurrent carriers and 512 streams per carrier, and nothing is reported when either limit is reached.
- The share dialog renders the WEB link whenever the carrier is enabled and a hostname is set, including while the MTProto proxy itself is off. A link can therefore exist for a relay that is not running.
