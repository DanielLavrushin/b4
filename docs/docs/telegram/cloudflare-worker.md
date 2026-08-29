---
sidebar_position: 5
title: Cloudflare Worker relay
---

# Cloudflare Worker relay

A Cloudflare Worker is a free per-user WebSocket relay, hosted on the reader's own Cloudflare account under a `*.workers.dev` name. b4 can reach any data centre through it, so it covers the ones the shared pool cannot reach from a given network.

It is tried last of the WebSocket routes, after Telegram's own edge and after the shared CF proxy pool.

:::info A different Worker mirrors b4's own downloads
[Update mirrors](../advanced/update-mirrors) covers a separate Worker that stands in for GitHub when a release download is blocked. It uses a different script, and the session-length limit below does not apply to it.
:::

:::warning A Worker is a fallback, not the main road
Cloudflare reclaims a stateless Worker mid-session. Measured from a censored network, a single Worker WebSocket carried some 13 to 17 KB before it stopped forwarding and held the connection open in silence, while Telegram's own edge carried a megabyte over the same link. That is long enough to pass a connection test and far too short for a video, which is why a Worker placed ahead of the pool would win the dial and take the session down with it. b4 demotes a Worker that goes quiet mid-session for ten minutes.
:::

Data centres 1, 3 and 5 have no native edge, so a network where the shared pool is also unreachable leaves the Worker as the only WebSocket route to them.

## Setting one up

1. Create a free Cloudflare account.
2. Under **Compute** -> **Workers & Pages**, create a Worker from the default template and deploy it.
3. Replace the Worker code with the script below and deploy again.
4. Copy the `name-1234.username.workers.dev` domain into the **Cloudflare Worker domain** field. Several Workers are comma-separated.

`cloudflare.com`, `cloudflare.dev` and `workers.dev` all have to be reachable from the network in question.

The Worker needs a **compatibility date of `2026-04-07` or later**. From that date the runtime answers a close frame by itself, which is the documented cause of `The Workers runtime canceled this request because it detected that your Worker's code had hung`.

The step-by-step with screenshots is maintained by tg-ws-proxy: [CfWorker.md](https://github.com/Flowseal/tg-ws-proxy/blob/main/docs/CfWorker.md). b4 asks the Worker for a data-centre address without a port: Telegram serves the same endpoint on 80, 443 and 5222, `443` is the one every data centre listens on, and DC 203 does not answer on 5222 at all.

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

Two details in it are about how long the relay lives:

The loop that carries data from Telegram back to the browser is passed to `ctx.waitUntil()`. A promise that is neither awaited nor returned nor handed to `waitUntil` is a floating promise, and the runtime may cancel it as soon as the handler returns, which for this Worker is immediately: it returns the moment the WebSocket is accepted. Measured against a Worker under load, 47 of 58 sessions were cancelled that way, half of them inside 400 ms, leaving Telegram with half a photo.

`socket.closed` and the reader's and writer's `closed` promises get a no-op `catch`. Nothing awaits them, so when Cloudflare reclaims the socket each one surfaces as an unhandled rejection, three per session in the Worker log.

Neither makes a stateless Worker a good place for a long session. `waitUntil` is documented to extend execution by about 30 seconds and a Telegram session lasts minutes, so a large download can still be cut. Cloudflare's answer for a connection that must outlive a request is a Durable Object with WebSocket hibernation.

## Credits

The WebSocket transport and the Cloudflare Worker relay are inspired by [tg-ws-proxy](https://github.com/Flowseal/tg-ws-proxy).
