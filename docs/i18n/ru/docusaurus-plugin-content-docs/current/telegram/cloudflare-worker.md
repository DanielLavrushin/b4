---
sidebar_position: 5
title: Релей на Cloudflare Worker
---

# Релей на Cloudflare Worker

Cloudflare Worker - это бесплатный персональный WebSocket-релей на собственном аккаунте Cloudflare, под именем вида `*.workers.dev`. Через него b4 достаёт любой дата-центр, поэтому он закрывает те, до которых из конкретной сети не дотягивается общий пул.

Пробуется он последним среди маршрутов WebSocket, после собственного узла Telegram и после общего пула CF.

:::info Загрузки самого b4 зеркалит другой Worker
[Зеркала обновления](../advanced/update-mirrors) описывают отдельный Worker, который подменяет GitHub, когда загрузка релиза заблокирована. У него другой скрипт, и ограничение на длительность сессии ниже к нему не относится.
:::

:::warning Worker - это резерв, а не основной путь
Cloudflare забирает stateless-воркер посреди сессии. Замерено из цензурируемой сети: один WebSocket через Worker пронёс от 13 до 17 КБ, после чего перестал передавать и молча держал соединение открытым, тогда как собственный узел Telegram по той же линии пронёс мегабайт. Этого хватает, чтобы пройти проверку соединения, и совершенно не хватает на видео, поэтому Worker впереди пула выигрывал бы дозвон и уносил сессию с собой. Замолчавший посреди сессии Worker b4 понижает в приоритете на десять минут.
:::

У дата-центров 1, 3 и 5 собственного узла нет, поэтому в сети, где недоступен и общий пул, Worker остаётся для них единственным маршрутом WebSocket.

## Настройка

1. Завести бесплатный аккаунт Cloudflare.
2. В разделе **Compute** -> **Workers & Pages** создать Worker из шаблона по умолчанию и задеплоить его.
3. Заменить код воркера на скрипт ниже и задеплоить снова.
4. Скопировать домен вида `name-1234.username.workers.dev` в поле **Домен Cloudflare Worker**. Несколько воркеров указываются через запятую.

`cloudflare.com`, `cloudflare.dev` и `workers.dev` должны быть доступны из этой сети.

Воркеру нужна **compatibility date `2026-04-07` или новее**. С этой даты рантайм сам отвечает на закрывающий кадр, а это задокументированная причина ошибки `The Workers runtime canceled this request because it detected that your Worker's code had hung`.

Пошаговая инструкция со скриншотами поддерживается проектом tg-ws-proxy: [CfWorker.md](https://github.com/Flowseal/tg-ws-proxy/blob/main/docs/CfWorker.md). b4 просит у воркера только адрес дата-центра, без порта: Telegram отдаёт один и тот же узел на 80, 443 и 5222, слушают везде именно `443`, а DC 203 на 5222 не отвечает вовсе.

## Скрипт

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

Две детали в нём касаются того, сколько релей проживёт.

Цикл, который несёт данные от Telegram обратно в браузер, передаётся в `ctx.waitUntil()`. Промис, который не ждут, не возвращают и не отдают в `waitUntil`, - висячий, и рантайм вправе отменить его в тот же момент, когда обработчик вернул ответ, а этот воркер возвращает его сразу после принятия WebSocket. Замерено на живом воркере под нагрузкой: 47 сессий из 58 отменены, половина быстрее 400 мс, и Telegram оставался с половиной фотографии.

У `socket.closed` и у `closed` читателя и писателя стоит пустой `catch`. Их никто не ждёт, поэтому когда Cloudflare забирает сокет, каждый всплывает необработанным отказом - три на сессию в логе воркера.

Хорошим местом для долгой сессии stateless-воркер от этого не становится. `waitUntil` по документации продлевает выполнение примерно на 30 секунд, а сессия Telegram живёт минутами, поэтому большая загрузка всё ещё может оборваться. Ответ Cloudflare для соединения, которое должно пережить запрос, - Durable Object с гибернацией WebSocket.

## Благодарности

WebSocket-транспорт и релей на Cloudflare Worker вдохновлены проектом [tg-ws-proxy](https://github.com/Flowseal/tg-ws-proxy).
