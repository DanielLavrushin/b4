---
sidebar_position: 3
title: Зеркала обновления
---

# Зеркала обновления

b4 скачивает свои релизы, скрипт установки и базы GeoIP и GeoSite с GitHub. Архив релиза
отдаёт не сам `github.com`: адрес загрузки отвечает редиректом на
`release-assets.githubusercontent.com`, а этот хост в части сетей отбрасывается по адресу.
Установщик в таком случае сообщает о таймауте соединения, и обновление прекращается.

Зеркало - это хост, который обращается к GitHub вместо b4 и передаёт байты обратно, так что
клиент не открывает соединение к заблокированному адресу. В b4 встроен список зеркал, и
обращение к ним происходит только после неудачной прямой попытки к GitHub, поэтому в сети без
ограничений они не задействуются вовсе.

Порядок фиксирован: сначала GitHub, затем каждое настроенное зеркало, затем встроенные в b4,
затем SourceForge. На каждом шаге скачивается один и тот же архив и сверяется с одной и той
же опубликованной суммой SHA256.

## Настройка «Зеркала обновления»

В разделе **Настройки** -> **Управление** поле **Зеркала обновления** принимает список
базовых URL через запятую. Они опрашиваются раньше встроенных зеркал, и используются как
сервисом, так и установщиком: при обновлении b4 передаёт список установщику в переменной
окружения `B4_MIRRORS`.

Принимаются только адреса `https`. Запись со строкой запроса или фрагментом игнорируется, как
и запись, не являющаяся URL.

:::info Доступность зависит от провайдера, а не от страны
Зеркало, отвечающее в одной сети, может быть недоступно в другой с тем же кодом страны.
Блокировки применяются отдельными провайдерами, поэтому список работает как цепочка, а не как
один рекомендованный хост.
:::

:::warning Зеркало отдаёт бинарный файл, который станет b4
b4 сверяет каждую загрузку с суммой SHA256, опубликованной рядом с ней, но сумма приходит с
того же хоста, что и архив, поэтому хост, отдающий и то и другое, может отдать совпадающую
пару. В это поле следует вносить только зеркала под собственным контролем либо те, которым
есть доверие.
:::

По той же причине настройка недоступна для записи через MCP.

## Личный Cloudflare Worker

Cloudflare Worker - это бесплатное персональное зеркало, размещённое в собственном аккаунте
Cloudflare под именем вида `*.workers.dev`. Он обращается к GitHub изнутри сети Cloudflare и
передаёт ответ обратно потоком.

Отдельный Worker у каждого блокируется труднее, чем один общий хост. Каждый аккаунт получает
собственный поддомен `*.workers.dev`, поэтому единого имени для фильтрации нет, а квота
запросов бесплатного тарифа расходуется только владельцем.

:::warning В части сетей `workers.dev` режется по скорости
Измерено у московского провайдера: имя `*.workers.dev` завершало TLS-хендшейк за 130 мс и
отдавало первые байты за 390 мс, после чего передача падала примерно до 40 байт в секунду,
так что файл в 128 КБ дошёл до 16 КБ за 400 секунд. По той же линии и в ту же минуту
`speed.cloudflare.com` отдал 6 МБ за 0,38 секунды, а тот же файл пришёл напрямую с GitHub за
0,22 секунды. Ограничение там не в Cloudflare, а в имени `workers.dev`.

Небольшие ответы при этом проходят, поэтому проверка доступности и список релизов работают, а
архив - нет. b4 это учитывает: передача, держащаяся ниже 1 КБ/с в течение 30 секунд,
прерывается, и берётся следующее зеркало, что обходится примерно в 36 секунд.

Worker, привязанный к собственному домену, работает через обычные адреса Cloudflare и под это
не попадает. Перед тем как полагаться на имя `workers.dev` для загрузок, следует выполнить
команды ниже.
:::

:::info Это не релей для Telegram
[Cloudflare Worker relay](../telegram/cloudflare-worker) для MTProto - другой Worker с другим
скриптом. Тот держит долгоживущий WebSocket, который Cloudflare отбирает посреди сессии,
из-за чего он стоит последним среди WebSocket-маршрутов. Зеркало отвечает на один короткий
запрос и закрывается, поэтому это ограничение к нему не относится.
:::

### Настройка

1. Завести бесплатный аккаунт Cloudflare.
2. В разделе **Compute** -> **Workers & Pages** создать Worker из шаблона по умолчанию и
   задеплоить его.
3. Заменить код Worker на скрипт ниже и задеплоить ещё раз.
4. Скопировать домен `name-1234.username.workers.dev` в поле **Зеркала обновления** в виде
   `https://name-1234.username.workers.dev`.

`workers.dev` должен быть доступен из рассматриваемой сети и не резаться по скорости.

### Скрипт

```javascript
const REPO_OWNER = "DanielLavrushin";
const REPO_NAME = "b4";

const MIRROR_OWNERS = [
  "DanielLavrushin",
  "Loyalsoldier",
  "runetfreedom",
  "XTLS",
  "Flowseal",
];

const MIRROR_HOSTS = [
  "raw.githubusercontent.com",
  "github.com",
  "api.github.com",
  "objects.githubusercontent.com",
  "release-assets.githubusercontent.com",
  "codeload.github.com",
  "gist.githubusercontent.com",
];

const SF_BASE = "https://downloads.sourceforge.net/project/b4core";
const SEGMENT = /^[A-Za-z0-9][A-Za-z0-9._+-]*$/;
const RAW_PATH = /^[A-Za-z0-9][A-Za-z0-9._/+-]*$/;
const API_TTL = 300;

function text(body, status) {
  return new Response(body, {
    status,
    headers: { "content-type": "text/plain; charset=utf-8" },
  });
}

function allowed(raw) {
  let url;
  try {
    url = new URL(raw);
  } catch {
    return false;
  }
  if (url.protocol !== "https:") return false;
  if (!MIRROR_HOSTS.includes(url.hostname)) return false;
  const parts = url.pathname.split("/").filter(Boolean);
  const owner = parts[0] === "repos" ? parts[1] : parts[0];
  return MIRROR_OWNERS.includes(owner);
}

async function passthrough(request, target, ttl) {
  const headers = new Headers();
  const range = request.headers.get("range");
  if (range) headers.set("range", range);
  headers.set("accept-encoding", "identity");
  headers.set("user-agent", "b4-mirror");
  if (target.startsWith("https://api.github.com")) {
    headers.set("accept", "application/vnd.github+json");
  }

  const init = {
    method: request.method === "HEAD" ? "HEAD" : "GET",
    headers,
    redirect: "follow",
  };
  if (!range && ttl) init.cf = { cacheTtl: ttl, cacheEverything: true };

  let upstream;
  try {
    upstream = await fetch(target, init);
  } catch (err) {
    return text(`upstream fetch failed: ${err}\n`, 502);
  }

  const out = new Headers();
  for (const name of [
    "content-type",
    "content-length",
    "content-range",
    "accept-ranges",
    "etag",
    "last-modified",
  ]) {
    const value = upstream.headers.get(name);
    if (value) out.set(name, value);
  }
  out.set("access-control-allow-origin", "*");

  return new Response(upstream.body, { status: upstream.status, headers: out });
}

export default {
  async fetch(request) {
    const url = new URL(request.url);
    const path = url.pathname;

    if (request.method !== "GET" && request.method !== "HEAD") {
      return text("method not allowed\n", 405);
    }

    if (path === "/b4/health") return text("ok\n", 200);

    let m = path.match(/^\/b4\/dl\/latest\/(.+)$/);
    if (m) {
      if (!SEGMENT.test(m[1])) return text("bad file name\n", 404);
      return passthrough(
        request,
        `https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/latest/download/${m[1]}`,
        0,
      );
    }

    m = path.match(/^\/b4\/dl\/([^/]+)\/(.+)$/);
    if (m) {
      if (!SEGMENT.test(m[1]) || !SEGMENT.test(m[2])) {
        return text("bad tag or file name\n", 404);
      }
      return passthrough(
        request,
        `https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${m[1]}/${m[2]}`,
        0,
      );
    }

    m = path.match(/^\/b4\/sf\/([^/]+)\/(.+)$/);
    if (m) {
      if (!SEGMENT.test(m[1]) || !SEGMENT.test(m[2])) {
        return text("bad tag or file name\n", 404);
      }
      return passthrough(request, `${SF_BASE}/${m[1]}/${m[2]}`, 0);
    }

    m = path.match(/^\/b4\/raw\/(.+)$/);
    if (m) {
      if (!RAW_PATH.test(m[1])) return text("bad path\n", 404);
      return passthrough(
        request,
        `https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/${m[1]}`,
        API_TTL,
      );
    }

    if (path === "/b4/api/releases") {
      return passthrough(
        request,
        `https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases?per_page=25`,
        API_TTL,
      );
    }

    if (path === "/b4/api/releases/latest") {
      return passthrough(
        request,
        `https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest`,
        API_TTL,
      );
    }

    if (path.startsWith("/github/")) {
      const target = url.href.slice(url.origin.length + "/github/".length);
      if (!allowed(target)) return text("forbidden\n", 403);
      return passthrough(request, target, 0);
    }

    return text("not found\n", 404);
  },
};
```

Три детали в нём относятся к тому, куда Worker вправе обращаться.

`redirect: "follow"` - это то, что вообще делает Worker полезным. GitHub отвечает на загрузку
релиза редиректом на свой хост ассетов, и зеркало, вернувшее бы этот редирект клиенту,
отправило бы клиента прямо на недоступный ему адрес.

`allowed()` сверяет имя хоста с фиксированным списком, а первый сегмент пути - со списком
владельцев репозиториев, поэтому Worker нельзя использовать как универсальный прокси к
произвольным адресам. Маршруты `/b4/` по той же причине собирают адрес из проверенных тега и
имени файла.

Архивы релизов запрашиваются без `cacheEverything`, поэтому они проходят потоком, не попадая
в кеш Cloudflare. Условия Cloudflare отводят раздачу крупных файлов через CDN платным
продуктам хранения. Кешируются только небольшие ответы JSON и текста, на пять минут, и именно
это удерживает часовой лимит GitHub API на один адрес от исчерпания.

### Проверка

```sh
curl -sI https://name-1234.username.workers.dev/b4/health
curl -sL -o /dev/null -w '%{http_code} %{size_download}\n' \
  https://name-1234.username.workers.dev/b4/dl/latest/b4-linux-arm64.tar.gz
```

Вторая команда скачивает архив релиза через Worker и печатает его статус и размер.

## Установка из файла

Когда недоступен ни один источник, архив можно принести вручную. В окне обновления пункт
**Установить из файла** принимает `b4-linux-<arch>.tar.gz`, скачанный на другой машине.

Перед тем как что-либо заменить, сервис проверяет, что загружен gzip-архив с файлом по имени
`b4`, что этот файл является исполняемым файлом Linux и что он собран под эту машину. Архив
для другой архитектуры отклоняется с указанием обеих архитектур, а не устанавливается с
последующим откатом.

Поле **Ожидаемая SHA256** необязательно и сверяется с загруженным файлом до установки. Сумма
со страницы релиза читается в браузере, на машине, у которой был доступ к GitHub, и это даёт
независимую проверку: автоматический путь берёт архив и его `.sha256` с одного и того же
хоста, поэтому хост, отдающий и то и другое, может отдать совпадающую пару.

Сама установка идёт тем же кодом, что и обычное обновление. Предыдущий бинарный файл
откладывается, новый ставится на его место и однократно запускается для проверки, а файл, не
прошедший проверку, откатывается.

:::note ABI плавающей точки MIPS проверкой не покрывается
Сборки MIPS с аппаратной и программной плавающей точкой несут одну и ту же архитектуру в
заголовке исполняемого файла, поэтому расхождение между ними при загрузке не выявляется. Оно
всё равно выявляется после замены, той же проверкой, которая откатывает любой не
запускающийся файл.
:::

:::warning Установщик на GitHub должен это поддерживать
b4 забирает `install.sh` из репозитория во время обновления, поэтому опубликованный
установщик должен быть версией, понимающей переданный архив. b4 это проверяет и отклоняет
загрузку, если поддержки нет, вместо того чтобы передать управление установщику, который
проигнорирует файл и скачает другую версию. Роутер, у которого закешированный установщик это
поддерживает, использует свою копию.
:::

:::info Скрипт установщика сервису всё равно нужен
Загрузка заменяет скачивание архива релиза, но не скачивание `install.sh`. b4 сохраняет копию
установщика рядом со своей конфигурацией после каждого успешного обновления и обращается к
ней, когда скачать не удаётся, поэтому роутер, обновлявшийся хотя бы раз, может установиться
из файла вообще без сети. На роутере, который через сервис ещё не обновлялся, `install.sh`
по-прежнему должен быть доступен.
:::

Docker отклоняется, поскольку образ заменяется скачиванием нового, и так же отклоняется b4,
запущенный не под менеджером сервисов.

## Установщик

Установщик применяет тот же порядок самостоятельно и читает `B4_MIRRORS` из окружения,
поэтому зеркало можно задействовать при первой установке, когда конфигурации ещё нет:

```sh
B4_MIRRORS="https://name-1234.username.workers.dev" sh install.sh
```

Значение - список через пробел. `B4_SF_BASE` таким же образом переопределяет базовый адрес
SourceForge.
