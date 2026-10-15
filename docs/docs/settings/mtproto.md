---
sidebar_position: 3
title: Telegram
description: Every field of the Telegram tab in Settings, for Telegram over WebSocket, the MTProto proxy, the WEB proxy and the Telegram upstream.
---

The **Telegram** tab of **Settings** sets up the three ways b4 carries Telegram traffic and how b4 reaches Telegram's data centres. The guides under [Telegram](../telegram/index.md) walk through each setup.

The **Secrets** and **Telegram Desktop WEB proxy** cards appear while the MTProto proxy is on.

## Telegram over WebSocket

The card turns on [Telegram over WebSocket](../telegram/websocket-bridge.md) for the whole network, with no set involved.

![The Telegram over WebSocket card](/img/telegram/20260925200001.png)

| Field | What it does | Default |
| --- | --- | --- |
| **Enable Telegram over WebSocket** | Relays Telegram from every device behind b4, and from the router itself, through the bridge. [Device filtering](./core.md#device-filtering) applies. The MTProto proxy is not needed. | Off |

The header shows **Save to apply** until the settings are saved. After that it reads **Working**, or **Not working** with the missing part named when the pointer rests on it.

The **Bridge status** box shows:

| Item | Shows |
| --- | --- |
| **Listener port** | The bridge's port, `13443`, and the address families it listens on, or **not running** |
| **Active connections** | Bridged connections open right now |
| **Telegram ranges** | How many address ranges are in use, their source (**telegram.org**, **b4 mirror**, **saved copy**, **GeoIP file** or **built-in list**) and when they were updated |
| **Relayed sessions** | How many sessions the bridge relayed, and when the last one was |
| Counters line | **Passed on undecoded**, **data center dial failures** and **closed without a handshake**, shown once one is above zero. See [The counters under the bridge status](../telegram/troubleshooting.md#the-counters-under-the-bridge-status) |

| Button | What it does |
| --- | --- |
| **Check again** | Repeats the kernel's TPROXY check, then installs the bridge's firewall rule or moves it back above another program's rule |
| **Refresh addresses** | Downloads Telegram's address list at once, and reports the number of ranges or the error |

Each warning above the box is explained under [The Telegram over WebSocket card shows a warning](../telegram/troubleshooting.md#the-telegram-over-websocket-card-shows-a-warning).

## MTProto Proxy {#proxy-server}

The [MTProto proxy](../telegram/mtproto-proxy.md) that Telegram connects to with a secret. The card's fields are hidden until its switch is on.

| Field | What it does | Default |
| --- | --- | --- |
| **Enable MTProto Proxy** | Starts the proxy server. | Off |
| **Bind Address** | The address the proxy listens on. `0.0.0.0` accepts connections on every address, `127.0.0.1` from this host only. | `0.0.0.0` |
| **Port** | The port Telegram connects to. | `3128` |
| **Expose to internet** | Adds a firewall rule that accepts connections to the port from any address, over IPv4 and IPv6. Off adds no rule and blocks nothing. See [Access from the internet](./security.md#expose-to-internet). | Off |
| **Fake SNI Domain** | The domain the proxy's TLS handshake presents, and the site that a connection failing the secret check is passed to. New secrets are generated for it. | `storage.googleapis.com` |

### Advanced

| Field | What it does | Default |
| --- | --- | --- |
| **Max Connections** | The most client connections open at once, probes included. Each Telegram client uses several. The WEB proxy is not limited by it. | `2048` |
| **TCP User Timeout (sec)** | Closes a client connection after this long with unacknowledged data, so a phone that left the network is noticed in about two minutes. `-1` turns it off. | `120` |
| **Idle Timeout (sec)** | Closes a relayed session after this long with no traffic in either direction. `-1` turns it off. | `300` |

In both timeouts, `0` uses the default.

## Secrets

One entry for each person lets access be given and withdrawn one person at a time. The log names the secret each connection used.

| Field | What it does | Default |
| --- | --- | --- |
| Switch | Turns the entry on or off. A client with a switched-off secret is rejected. | On |
| **Name / comment** | A name for the entry, shown in the log and on the Traffic page. | Empty |
| **Max networks** | Refuses new connections to the secret once it is in use from this many networks at once. Devices behind one internet connection count as one network. `0` means no limit. | `0` |
| **Secret** | The secret itself. **Generate Secret** replaces it with a new one for the current **Fake SNI Domain**. Only fake-TLS secrets, hexadecimal and starting with `ee`, are accepted. | Generated |

**Add secret** adds an entry with a generated secret. The share button (**Share connection link**) opens the link and QR code, see [Adding the proxy in Telegram](../telegram/mtproto-proxy.md#adding-the-proxy-in-telegram). The bin button (**Remove secret**) deletes the entry.

## Telegram Desktop WEB proxy

The [WEB proxy](../telegram/web-proxy.md) serves the MTProto proxy as an HTTPS site for Telegram Desktop 7.1.1 and later.

| Field | What it does | Default |
| --- | --- | --- |
| **Enable the WEB carrier** | Serves the proxy on the relay hostname. | Off |
| **Relay hostname** | A public DNS name used only for the relay, with a publicly trusted certificate on port 443. No scheme, port or path; punycode for international names. | Empty |
| **Relay port** | Empty: the relay is served on the web interface's port, on the relay hostname only. A port: the relay gets a listener of its own, where any other name or IP address gets the placeholder page. Filled with `443` when the carrier is first turned on. | Empty |
| **Expose to internet** | Shown while a relay port is set. Opens the relay port to connections from any address. Without a relay port, the relay is reachable from outside only when the web interface is exposed. | Off |
| **Certificate for the relay hostname** | Shown while a relay port is set. **Certificate file** and **Private key file** in PEM format, used on the relay port only. Empty: the web server's certificate. | Empty |
| **Placeholder page** | The page every visitor other than Telegram sees. An uploaded HTML file of up to 1 MiB replaces the built-in page, and **Remove** restores it. | Built-in |

## Telegram upstream (shared) {#telegram-upstream}

How b4 reaches Telegram's data centres, for the MTProto proxy and Telegram over WebSocket alike, even while the proxy is off. The routes are explained under [Telegram upstream](../telegram/upstream.md).

| Field | What it does | Default |
| --- | --- | --- |
| **Transport mode** | How the MTProto proxy reaches Telegram: **Direct TCP**, **Auto (WebSocket → TCP)** or **WebSocket only**. Telegram over WebSocket always uses Auto. | **Auto (WebSocket → TCP)** |
| **DC Relay** | `host:port` of a VPS that forwards to the data centres, shown for **Direct TCP** and **Auto (WebSocket → TCP)**. The **?** button shows the `socat` commands. Not used by Telegram over WebSocket. See [DC Relay](../telegram/dc-relay.md). | Empty |
| **Cloudflare Worker domain** | One or more `*.workers.dev` names, separated by commas. See [Cloudflare Worker relay](../telegram/cloudflare-worker.md). | Empty |
| **Let sets process Worker connections** | Shown once a Worker domain is set. Lets a set whose targets cover the Worker apply its DPI strategy to b4's connections to it. See [DPI processing](../telegram/websocket-bridge.md#dpi-processing). | Off |
| **Custom WebSocket domain** | A domain that relays WebSocket traffic to Telegram. b4 adds `kws1.`, `kws2.` and so on for each data centre. | Empty |
| **Telegram WS edge IP (advanced)** | Replaces the address of Telegram's own WebSocket edge. | `149.154.167.220` |
| **Fronting name for Telegram's WS edge (advanced)** | A TLS name, such as `sprinthost.ru`, tried on Telegram's edge when the handshake under its own names goes unanswered. See [Fronting name for the WS edge](../telegram/upstream.md#fronting-name-for-the-ws-edge). | Empty (off) |

**Test connection** tries each configured route to data centre 2 and lists the result of each. **Test direct TCP** tries data centre 2 over direct TCP, without the DC Relay.

### Fallback sources

| Field | What it does | Default |
| --- | --- | --- |
| **CF proxy fallback** | Uses a rotating pool of Cloudflare-proxied domains for the data centres that Telegram's own edge does not serve. | On |
| **CF proxy domain list URL** | Where the pool is refreshed from, hourly. | tg-ws-proxy's list |
| **DC list fallback mirror** | Fetches Telegram's data-centre list from the mirror below when Telegram's own endpoint is unreachable. | On |
| **DC list mirror URL** | The mirror. The default one is hosted by the b4 author and receives the requesting IP address, nothing else. | The b4 author's mirror |
| **Bridge Handshake Wait (sec)** | How long Telegram over WebSocket waits for a client's first byte before closing the connection. `-1` waits indefinitely. | `180` |

b4 downloads these lists only while the MTProto proxy, Telegram over WebSocket or an enabled set in its routing mode is on.

## Saving changes

The tab applies on **Save Changes**, without restarting the service.

- A change to **Enable MTProto Proxy**, **Bind Address**, **Port**, **Fake SNI Domain**, **Transport mode**, **Custom WebSocket domain**, the WS edge IP, the fronting name or a CF proxy setting restarts the MTProto proxy.
- Secrets apply without a restart. A secret switched off or removed loses its open connections.
- The WEB proxy's switch and hostname apply from the next request. A new relay port or certificate restarts only the relay's listener.
