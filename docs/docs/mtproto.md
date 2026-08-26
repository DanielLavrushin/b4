---
sidebar_position: 12
title: MTProto / Telegram
---

# Telegram with B4

B4 can keep Telegram working on a censored network in two ways. They are independent and can run at the same time:

1. **MTProto proxy server** - clients add B4 as an MTProto proxy inside the Telegram app (with a secret). Use this when a device should connect *to* B4, for example a phone on a cellular network reaching your home B4.
2. **Telegram over WebSocket (transparent bridge)** - a per-set routing mode that intercepts Telegram traffic from LAN devices and relays it for them. No in-app proxy and no secret. Use this to fix Telegram for every device behind B4 at once.

Both rely on the same **Telegram upstream** settings (how B4 itself reaches Telegram's data centers), so that section is described once and applies to either mode.

| Aspect            | Proxy server                              | WebSocket bridge          |
| ----------------- | ----------------------------------------- | ------------------------- |
| Where configured  | Settings → MTProto                        | A set's routing mode      |
| Per-device setup  | Add proxy + secret in Telegram            | None (transparent)        |
| Good for          | One device reaching B4 (incl. remote/home)| All LAN devices at once   |
| Needs MTProto on  | Yes                                       | No (independent)          |

---

## Telegram upstream (shared)

Settings → **MTProto Proxy** → **Telegram upstream**. This controls how B4 reaches Telegram's data centers. It is used by the proxy server *and* by the WebSocket bridge mode, so it applies even when the proxy server is turned off.

### Transport mode

- **Direct TCP** - fastest. Use when the B4 host can reach `149.154.0.0/16` directly (e.g. a VPS abroad).
- **Auto (WebSocket → TCP)** - try WebSocket first via `kws*.web.telegram.org`, fall back to direct TCP. Recommended on censored networks.
- **WebSocket only** - strict WebSocket transport, no TCP fallback.

:::info
The transport-mode dropdown applies to the **proxy server** only. The **WebSocket bridge** routing mode always uses Auto (WebSocket-first with TCP fallback).
:::

### Cloudflare Worker domain (recommended fallback)

A **Cloudflare Worker domain** is a free per-user WebSocket relay you host on your own Cloudflare account (`*.workers.dev`). B4 can reach any data center through it, so it rescues DCs the shared pool cannot reach from your network. It is tried **last** of the WebSocket routes — after Telegram's own edge and after the shared CF pool — because Cloudflare reclaims a stateless worker mid-session: measured from a censored network against DC 1, a worker answered 8 handshake rounds and went mute at 8.7 seconds, while a pooled domain answered 100 rounds over two minutes on the same machine. That is long enough to pass a connection test and far too short for a video, so ahead of the pool it would win the dial and take the session down with it.

Setup, in short:

1. Create a free Cloudflare account.
2. In **Compute → Workers & Pages**, create a Worker from the default template and deploy it.
3. Replace the worker code with the proxy script, then redeploy.
4. Copy the worker's `name-1234.username.workers.dev` domain into the **Cloudflare Worker domain** field. Comma-separate multiple workers.

Make sure `cloudflare.com`, `cloudflare.dev`, and `workers.dev` are reachable (not blocked) on your network.

The full step-by-step, with screenshots, is maintained by tg-ws-proxy: [CfWorker.md](https://github.com/Flowseal/tg-ws-proxy/blob/main/docs/CfWorker.md). B4 asks the worker for a data center address only, never a port: Telegram serves the same endpoint on 80, 443 and 5222, `443` is the one every data center listens on, and DC 203 does not answer on 5222 at all. A worker deployed from that page still works, but the script below relays for longer before Cloudflare reclaims it — see below.

Set the worker's **compatibility date to `2026-04-07` or later**. From that date the runtime answers a close frame by itself, which is [the documented cause](https://developers.cloudflare.com/workers/observability/errors/) of `The Workers runtime canceled this request because it detected that your Worker's code had hung`.

Two differences from the tg-ws-proxy script, both about how long Cloudflare lets the relay live:

- The loop that carries data from Telegram back to the browser is handed to `ctx.waitUntil()`. A promise that is neither awaited nor returned nor passed to `waitUntil` is a *floating* promise, and the runtime may cancel it the moment the handler returns — which for this worker is immediately, since it returns as soon as the WebSocket is accepted. Measured against a real worker under load: 47 of 58 sessions were cancelled, half of them inside 400 ms, and Telegram was left with half a photo.
- `socket.closed` and the reader's and writer's `closed` promises get a no-op `catch`. Nothing awaits them, so when Cloudflare reclaims the socket each one surfaces as an unhandled rejection — three per session in the worker log, which buries anything real.

Neither makes a stateless worker a good place for a long session. `waitUntil` is documented to extend execution by about 30 seconds, and a Telegram session lasts minutes, so a large download can still be cut. Cloudflare's answer for a connection that must outlive a request is a Durable Object with WebSocket hibernation. Treat the worker as one route among several rather than the only one: B4 keeps Telegram's own edge and the shared Cloudflare pool behind it for exactly this reason.

```javascript
import { connect } from "cloudflare:sockets";

function toBytes(data) {
  if (data instanceof ArrayBuffer) {
    return new Uint8Array(data);
  }
  if (typeof data === "string") {
    return new TextEncoder().encode(data);
  }
  if (data && typeof data.arrayBuffer === "function") {
    return data.arrayBuffer().then((ab) => new Uint8Array(ab));
  }
  return new Uint8Array();
}

const ignore = () => {};

export default {
  async fetch(request, env, ctx) {
    if ((request.headers.get("Upgrade") || "").toLowerCase() !== "websocket") {
      return new Response("Expected websocket", { status: 426 });
    }

    const url = new URL(request.url);
    if (url.pathname !== "/apiws") {
      return new Response("Not found", { status: 404 });
    }

    const dst = url.searchParams.get("dst");
    const pair = new WebSocketPair();
    const client = pair[0];
    const server = pair[1];
    server.accept();

    const socket = connect({ hostname: dst, port: 443 });
    const tcpReader = socket.readable.getReader();
    const tcpWriter = socket.writable.getWriter();

    socket.closed.catch(ignore);
    tcpReader.closed.catch(ignore);
    tcpWriter.closed.catch(ignore);

    server.addEventListener("message", async (event) => {
      try {
        await tcpWriter.write(await toBytes(event.data));
      } catch {
        try {
          server.close(1011, "tcp write failed");
        } catch {}
      }
    });

    server.addEventListener("close", async () => {
      try {
        await tcpWriter.close();
      } catch {}
      try {
        socket.close();
      } catch {}
    });

    const pump = (async () => {
      try {
        while (true) {
          const { value, done } = await tcpReader.read();
          if (done) {
            break;
          }
          if (value) {
            server.send(value);
          }
        }
      } catch {
      } finally {
        try {
          server.close();
        } catch {}
        try {
          tcpReader.releaseLock();
        } catch {}
        try {
          socket.close();
        } catch {}
      }
    })();

    ctx.waitUntil(pump);

    return new Response(null, { status: 101, webSocket: client });
  },
};
```

:::warning
A Worker is a fallback, not the main road. Measured against a free-tier Worker from a censored network, a single WebSocket carried some 13 to 17 KB before it stopped forwarding and held the connection open in silence, while Telegram's own WebSocket edge carried a megabyte over the same link. B4 notices a Worker that goes quiet mid-session and ranks it below the other routes for ten minutes, but a data center with no native edge (1, 3 and 5) has nowhere else to go, which is why the direct route matters there.
:::

### CF proxy fallback

A rotating pool of Cloudflare-proxied domains used as a fallback when Telegram's native edge cannot reach a data center (notably DC 1, needed for media in foreign channels). The pool refreshes hourly.

### Custom WebSocket domain

An extra domain that proxies WebSocket traffic to Telegram, for a self-hosted relay rather than the shared pool. B4 prepends `kws1.`, `kws2.` and so on per data center, so one domain covers all of them and the name has to resolve for every data center in use. It is tried after Telegram's own edge and ahead of the shared CF pool, and it is independent of the CF proxy fallback: either works without the other.

### Telegram WS edge IP

Every dial that carries a native `kws*.web.telegram.org` server name goes to a single address, `149.154.167.220`, and the data center is chosen by the server name rather than by the address. This setting replaces that address, for a network on which the default one goes unanswered. It takes a host or an IP with no port, and an empty value keeps the default. The custom WebSocket domain resolves its own name and is unaffected.

:::warning
Changing either of these restarts the MTProto proxy and drops the sessions it is carrying. Telegram reconnects by itself.
:::

### Testing

- **Test connection** probes DC 2 over the configured transport(s) and reports latency.
- **Test direct TCP** probes DC 2 over direct TCP, bypassing any DC Relay, to isolate whether a problem is the relay or Telegram itself.

---

## Option 1: MTProto proxy server

A Telegram proxy that clients connect to with a secret. B4 disguises the traffic as a regular HTTPS connection to a popular website.

![20260531200322](/img/mtproto/20260531200322.png)

### Step 1: Configure B4

In the B4 web UI → **Settings** → **MTProto Proxy**:

1. **Enable MTProto Proxy** - turn it on
2. **Port** - listen port (recommended: `443`)
3. **Fake SNI Domain** - domain to impersonate (e.g. `storage.googleapis.com`)
4. Click **Generate Secret**
5. Copy the **Secret** value
6. Save settings and restart B4

Set the **Telegram upstream** transport (above) according to where B4 runs:

- **B4 on a VPS abroad** - Direct TCP. B4 reaches Telegram directly; leave DC Relay empty.
- **B4 on a router inside Russia** - Auto (WebSocket → TCP). B4 reaches Telegram over the WebSocket edge, so no VPS relay is required. If WebSocket is also blocked on your network, use a DC Relay (below).

### Step 2: Configure Telegram

1. Open **Telegram** → **Settings** → **Data and Storage** → **Proxy**
2. Tap **Add Proxy**
3. Choose **MTProto**
4. Fill in:
   - **Server**: B4 IP or hostname (LAN IP for local devices; public IP or DDNS for remote use, with port forwarding)
   - **Port**: the port from step 1
   - **Secret**: the copied secret
5. Tap **Done** and enable the proxy

![telegra](/img/mtproto/20260322135130.png)

You can also use the **Share connection link** button to generate a `tg://proxy` link or QR code for another device.

---

## Option 2: Telegram over WebSocket (transparent bridge)

A per-set routing mode that fixes Telegram for every device behind B4, with no in-app proxy and no VPS. When a device connects to a Telegram data center, B4 transparently intercepts the session and relays it over Telegram's WebSocket edge (with Cloudflare fallback).

This mode runs on its own. The MTProto proxy server under Settings → MTProto does **not** need to be enabled.

### Setup

1. Create or open a set and give it the **`telegram`** target in both the geosite and geoip categories (so the set matches Telegram's domains and IP ranges).
2. In the set's **Routing** tab, enable routing and set **Routing mode** to **Telegram over WebSocket (built-in)**.
3. Choose the **source interfaces** (the LAN interfaces whose devices should be bridged). Leave empty to bridge all devices.
4. Save.

A minimal set for this mode:

```json
{
  "name": "telegram-ws",
  "targets": {
    "geosite_categories": ["telegram"],
    "geoip_categories": ["telegram"]
  },
  "enabled": true,
  "routing": { "enabled": true, "mode": "mtproto-ws" }
}
```

The shared **Telegram upstream** settings (Settings → MTProto) apply here, so configure a Cloudflare Worker domain there if media fails to load.

:::info Best-effort
Only TCP MTProto sessions are bridged. Voice calls and transports B4 cannot map to a data center fall open to a direct connection.
:::

---

## DC Relay (VPS + socat)

Use a DC Relay only when B4 runs inside the censored zone *and* the WebSocket transport is also blocked, so direct IP-level connections to Telegram must go through a VPS.

```text
Phone ──────▶ B4 (router) ──────▶ VPS ──────▶ Telegram
       TSPU sees                 TSPU sees
   "HTTPS to google.com"      "traffic to VPS"
       (not blocked)            (not blocked)
```

The VPS only needs a simple TCP forwarder (`socat`); no keys, no MTProto-specific software.

### Step 1: Install socat on the VPS

```bash
apt install -y socat
```

### Step 2: Set the DC Relay address

In **Settings** → **MTProto Proxy**, set **DC Relay** to the VPS address with the base port (e.g. `my-vps.com:7007`). The field appears when the transport mode is Direct TCP or Auto.

With Auto + a DC Relay configured, relay TCP is tried first and WebSocket is used as the fallback.

### Step 3: Get the socat commands

Click the **?** button next to the **DC Relay** field. The "DC Relay socat setup" dialog lists Telegram's data centers and ready-to-run `socat` commands for each one. Click **Copy all**, switch to the VPS, and run them.

:::info Why the helper
Each `socat` forwards a relay port to the public data-center address B4 dials directly (`base_port + DC - 1` → the DC's `:443` endpoint). The media DC `203` reuses DC 2's relay port, so it needs no command of its own.
:::

:::warning VPS firewall
Open every port the helper shows (the "Open these ports on the VPS firewall" line at the bottom of the dialog). This is 5 ports, one for each main DC (1-5).
:::

:::tip
To auto-start `socat`, add the commands to `/etc/rc.local` or create a systemd service.
:::

### IPv4 or IPv6

The dialog has an **Address family** switch that controls which Telegram addresses the generated commands connect to. It is useful when the VPS reaches Telegram over IPv6 but not over IPv4.

The switch changes the upstream side of the command only:

```bash
# IPv4
socat TCP-LISTEN:7007,fork,reuseaddr TCP:149.154.175.50:443 &

# IPv6
socat TCP-LISTEN:7007,fork,reuseaddr TCP6:[2001:b28:f23d:f001::a]:443 &
```

The listen side follows the **DC Relay** address instead. If it is an IPv6 literal, write it in brackets (`[2001:db8::1]:7007`) and the helper emits `TCP6-LISTEN`, so the VPS also accepts the connection from B4 over IPv6.

The two sides are independent: an IPv4 relay address with IPv6 upstreams is a valid and common combination.

:::note IPv6 addresses and Refresh
The IPv6 data-center addresses are built into B4. **Refresh** does not change them, because Telegram's published proxy config lists IPv4 addresses only.

Media DC `203` has no IPv6 address and shares DC 2's relay port, so media traffic follows whatever the DC 2 command points at. If media stops loading after switching to IPv6, put that one command back on IPv4 and leave the rest on IPv6.
:::

---

## Choosing a fake SNI domain

The domain should be:

- popular in Russia
- not blocked
- critically important (so blocking it would break other services)

:::info
If someone connects to the B4 port without the correct secret, B4 transparently forwards them to the real site (the one configured in Fake SNI). A scanner sees an ordinary site, not a proxy.
:::

---

## Troubleshooting

### Telegram shows "Connecting…"

- If using the WebSocket transport, run **Test connection** to confirm B4 can reach a DC.
- If using a DC Relay, make sure `socat` is running on the VPS and the ports are reachable, and double-check the VPS address.
- B4 logs should show `MTProto fake-TLS handshake OK` and `MTProto relay` lines.

### Media, stickers, or reactions fail to load

- Set a **Cloudflare Worker domain** in the Telegram upstream settings. DC 1 (media for foreign channels) is the usual culprit, and the CF Worker / CF proxy fallback rescues it.

### Wrong secret

In the logs: `HMAC verification failed`. The secret in Telegram does not match the one configured in B4.

### Clock skew

In the logs: `timestamp out of range`. The clocks on the device and the B4 machine disagree. Sync them (NTP).

### VPS unreachable (DC Relay)

In the logs: `dial DC ... i/o timeout`.

- VPS is off, or `socat` is not running
- VPS firewall blocks inbound connections on the required ports

### No response from Telegram

In the logs: `DC->client: 0 bytes`.

- Direct TCP and no relay: Telegram servers are blocked by IP. Switch the transport to Auto/WebSocket, or set up a DC Relay.
- DC Relay set: `socat` is not running on the VPS, or the wrong port was specified.

---

## Credits

The WebSocket transport and the Cloudflare Worker relay are inspired by [tg-ws-proxy](https://github.com/Flowseal/tg-ws-proxy).
