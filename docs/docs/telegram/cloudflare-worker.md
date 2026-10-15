---
sidebar_position: 5
title: Cloudflare Worker relay
description: Setting up a Cloudflare Worker as a relay to Telegram's data centres, for media that does not load.
---

A Cloudflare Worker is a small relay that runs on a free Cloudflare account under a `*.workers.dev` name. b4 reaches every Telegram data centre through it. It is the route to add when media, stickers or reactions do not load: data centres 1, 3 and 5 have no WebSocket edge of their own, and where the CF proxy pool is unreachable too, a Worker is their only WebSocket route.

Cloudflare can end a Worker's connection in the middle of a session. b4 therefore tries a Worker last among the WebSocket routes and sets aside one that goes quiet. A Worker is a fallback, not the main route.

This Worker is not the one under [Update mirrors](../advanced/update-mirrors.md), which stands in for GitHub for b4's own downloads and runs a different script.

## Requirements

- A free Cloudflare account.
- `cloudflare.com`, `cloudflare.dev` and `workers.dev` reachable from the network.
- For the MTProto proxy, **Transport mode** set to **Auto (WebSocket → TCP)** or **WebSocket only**. Telegram over WebSocket always uses Auto.

## Setup

1. Create a free Cloudflare account.
2. Under **Compute > Workers & Pages**, create a Worker from the default template and deploy it.
3. Replace the Worker's code with the [script](#the-script) below and deploy it again.
4. Make sure the Worker's compatibility date is `2026-04-07` or later.
5. In b4, open **Settings > Telegram** and enter the Worker's domain, such as `name-1234.username.workers.dev`, in **Cloudflare Worker domain** on the **Telegram upstream (shared)** card. Several Workers are separated by commas.
6. Click **Save Changes**.

tg-ws-proxy keeps a step-by-step guide with screenshots: [CfWorker.md](https://github.com/Flowseal/tg-ws-proxy/blob/main/docs/CfWorker.md).

## The script

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

## Checking the result

**Test connection** on the **Telegram upstream (shared)** card lists a result for each route. The Worker's line starts with `wsworker://` and shows its latency, or the stage where it failed.

## When it does not work

- **Cloudflare reports `The Workers runtime canceled this request because it detected that your Worker's code had hung`.** The compatibility date is earlier than `2026-04-07`. From that date on, the runtime answers a closing frame by itself.
- **The stream stops after the first few kilobytes.** Some networks slow down `workers.dev`: the handshake completes, then the data stalls. b4 sets a Worker aside for ten minutes after two stalls within five minutes. When a set already has a strategy that works for Cloudflare, **Let sets process Worker connections** applies it to the Worker connections too, see [DPI processing](./websocket-bridge.md#dpi-processing).
- The log line of a stall is described under [The upstream goes silent mid-session](./troubleshooting.md#the-upstream-goes-silent-mid-session).

## How the script works

The script accepts a WebSocket on the `/apiws` path and opens a TCP connection to port 443 of the address b4 asks for, the port every Telegram data centre listens on. The loop that carries data back from Telegram is handed to `ctx.waitUntil()`, so the runtime does not cancel it as soon as the handler returns. The `closed` promises get an empty `catch`, so a socket that Cloudflare reclaims does not log unhandled rejections.

A stateless Worker still does not suit long sessions: `waitUntil` extends execution by about 30 seconds, and a large download can be cut. For a connection that has to outlive a request, Cloudflare offers a Durable Object with WebSocket hibernation.

## Credits

The WebSocket transport and the Cloudflare Worker relay are inspired by [tg-ws-proxy](https://github.com/Flowseal/tg-ws-proxy).
