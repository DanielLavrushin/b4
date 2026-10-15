---
sidebar_position: 5
title: Релей на Cloudflare Worker
description: Cloudflare Worker как релей к дата-центрам Telegram, когда не загружаются медиа.
---

Cloudflare Worker - это небольшой релей на бесплатном аккаунте Cloudflare с именем вида `*.workers.dev`. Через него b4 достаёт любой дата-центр Telegram. Его добавляют, когда не загружаются медиа, стикеры или реакции: у дата-центров 1, 3 и 5 нет собственного WebSocket-узла, и там, где недоступен ещё и пул CF-доменов, Worker остаётся для них единственным маршрутом WebSocket.

Cloudflare может оборвать соединение Worker посреди сессии. Поэтому b4 пробует Worker последним из маршрутов WebSocket и откладывает тот, что замолчал. Worker - это резерв, а не основной путь.

Это не тот Worker, что описан в разделе [Зеркала обновления](../advanced/update-mirrors.md): тот подменяет GitHub при загрузке самого b4 и работает на другом скрипте.

## Требования

- Бесплатный аккаунт Cloudflare.
- Доступ к `cloudflare.com`, `cloudflare.dev` и `workers.dev` из этой сети.
- Для MTProto-прокси **Режим транспорта** **Авто (WebSocket → TCP)** или **Только WebSocket**. Telegram через WebSocket всегда использует Авто.

## Настройка

1. Заведите бесплатный аккаунт Cloudflare.
2. В разделе **Compute > Workers & Pages** создайте Worker из шаблона по умолчанию и разверните его.
3. Замените код Worker на [скрипт](#скрипт) ниже и разверните снова.
4. Убедитесь, что compatibility date у Worker - `2026-04-07` или новее.
5. В b4 откройте **Настройки > Telegram** и укажите домен Worker, например `name-1234.username.workers.dev`, в поле **Домен Cloudflare Worker** на карточке **Транспорт к Telegram (общий)**. Несколько Worker указываются через запятую.
6. Нажмите **Сохранить**.

Пошаговая инструкция со скриншотами есть у проекта tg-ws-proxy: [CfWorker.md](https://github.com/Flowseal/tg-ws-proxy/blob/main/docs/CfWorker.md).

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

## Проверка результата

**Проверить соединение** на карточке **Транспорт к Telegram (общий)** показывает результат по каждому маршруту. Строка Worker начинается с `wsworker://` и показывает задержку или этап, на котором маршрут не прошёл.

## Если не работает

- **Cloudflare сообщает `The Workers runtime canceled this request because it detected that your Worker's code had hung`.** Compatibility date раньше `2026-04-07`. Начиная с этой даты рантайм сам отвечает на закрывающий кадр.
- **Поток останавливается после первых нескольких килобайт.** В некоторых сетях замедляют `workers.dev`: рукопожатие проходит, а данные затем встают. После двух таких зависаний за пять минут b4 откладывает Worker на десять минут. Если у какого-то сета уже есть рабочая стратегия для Cloudflare, переключатель **Обрабатывать соединения с Worker сетами** применяет её и к соединениям с Worker, см. [Обработка DPI](./websocket-bridge.md#обработка-dpi).
- Строка лога при зависании описана в разделе [Маршрут замолчал посреди сессии](./troubleshooting.md#маршрут-замолчал-посреди-сессии).

## Как работает скрипт

Скрипт принимает WebSocket на пути `/apiws` и открывает TCP-соединение на порт 443 адреса, который запросил b4: этот порт слушают все дата-центры Telegram. Цикл, который несёт данные от Telegram обратно, передаётся в `ctx.waitUntil()`, поэтому рантайм не отменяет его сразу после возврата из обработчика. У промисов `closed` стоит пустой `catch`, поэтому сокет, который забрал Cloudflare, не оставляет в логе необработанных ошибок.

Для долгих сессий stateless-Worker всё равно не создан: `waitUntil` продлевает выполнение примерно на 30 секунд, и большая загрузка может оборваться. Для соединения, которое должно пережить запрос, у Cloudflare есть Durable Object с гибернацией WebSocket.

## Благодарности

WebSocket-транспорт и релей на Cloudflare Worker вдохновлены проектом [tg-ws-proxy](https://github.com/Flowseal/tg-ws-proxy).
