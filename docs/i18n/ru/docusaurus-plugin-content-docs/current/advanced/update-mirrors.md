---
sidebar_position: 3
title: Загрузка b4 при блокировке GitHub
description: Зеркала, личный Cloudflare Worker и установка из файла для сетей, где b4 не может скачивать с GitHub.
---

b4 скачивает свои релизы, скрипт установки и базы GeoSite и GeoIP с GitHub. В части сетей хост, который отдаёт файлы релизов, `release-assets.githubusercontent.com`, заблокирован, и обновление прерывается с таймаутом соединения. Обойти это можно тремя способами: зеркалами, личным Cloudflare Worker и установкой из файла.

## Как b4 выбирает источник {#sources}

b4 сначала обращается к GitHub, затем к каждому зеркалу из **Зеркала обновления**, затем к зеркалам, встроенным в b4, и наконец к SourceForge. Каждый источник отдаёт один и тот же архив, который сверяется с одной и той же опубликованной суммой SHA256. В сети без блокировок зеркала не используются вовсе.

## Настройка «Зеркала обновления» {#the-update-mirrors-setting}

Поле **Зеркала обновления** в разделе **Настройки > Система > Сервис** принимает список адресов зеркал через запятую. Они опрашиваются раньше встроенных зеркал, и b4 передаёт их установщику, поэтому обновление из веб-интерфейса тоже их использует. Принимаются только адреса `https`. Запись со строкой запроса или фрагментом игнорируется.

Зеркало, доступное в одной сети, может быть заблокировано в другой, потому что каждый провайдер блокирует по-своему. Список из нескольких зеркал опрашивается по порядку.

:::warning Зеркало отдаёт бинарник, который станет b4
b4 сверяет каждую загрузку с суммой SHA256, опубликованной рядом с ней, но сумма приходит с того же хоста, что и архив. Хост, который отдаёт и то и другое, может отдать совпадающую пару. В это поле следует вносить только зеркала под собственным контролем или те, которым есть доверие.
:::

## Личный Cloudflare Worker {#a-personal-cloudflare-worker}

Cloudflare Worker - бесплатное зеркало в личном аккаунте Cloudflare под именем вида `*.workers.dev`. Он обращается к GitHub изнутри сети Cloudflare и передаёт результат дальше. Каждый аккаунт получает собственный поддомен, поэтому единого имени для блокировки нет.

В части сетей имена `*.workers.dev` режутся по скорости: небольшие ответы проходят, а архив еле ползёт. b4 прерывает зависшую загрузку и переходит к следующему источнику. [Проверка](#checking-it-works) ниже показывает, пригоден ли Worker в конкретной сети. Worker, привязанный к собственному домену, под это ограничение не попадает.

Этот Worker - не [Cloudflare Worker relay](../telegram/cloudflare-worker.md) для Telegram, у того другой скрипт.

### Настройка {#setting-one-up}

1. Заведите бесплатный аккаунт Cloudflare.
2. В разделе **Compute** > **Workers & Pages** создайте Worker из шаблона по умолчанию и задеплойте его.
3. Замените код Worker на скрипт ниже и задеплойте ещё раз.
4. В b4 добавьте адрес Worker, например `https://name-1234.username.workers.dev`, в поле **Зеркала обновления** в разделе **Настройки > Система > Сервис**.

### Скрипт {#the-script}

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

Скрипт обращается только к GitHub и SourceForge и только для перечисленных в нём владельцев репозиториев, поэтому как открытый прокси его использовать нельзя. Архивы релизов проходят через него без кеширования.

### Проверка результата {#checking-it-works}

```sh
curl -sI https://name-1234.username.workers.dev/b4/health
curl -sL -o /dev/null -w '%{http_code} %{size_download}\n' \
  https://name-1234.username.workers.dev/b4/dl/latest/b4-linux-arm64.tar.gz
```

Первая команда проверяет, что Worker отвечает. Вторая скачивает через него архив релиза и печатает статус и размер. Если загрузка здесь зависает, `workers.dev` в этой сети режется по скорости.

## Установка из файла {#installing-from-a-file}

Если ни один источник недоступен, архив можно принести вручную с машины, у которой есть доступ.

Требования:

- Имя пользователя и пароль веб-интерфейса в разделе **Настройки > Система > Веб-сервер**. Без них загрузка отклоняется, поскольку она заменяет бинарник, работающий от root.
- b4 работает как сервис, а не в Docker.
- Скрипт установки. b4 скачивает `install.sh` или использует копию, сохранённую при последнем успешном обновлении. Роутеру, который ещё ни разу не обновлялся из веб-интерфейса, нужен доступ к `install.sh`.

Шаги:

1. Откройте окно **Управление версиями** нажатием на версию внизу бокового меню.
2. Нажмите **Установить из файла**.
3. На машине с доступом скачайте со страницы релиза `b4-linux-<arch>.tar.gz` для архитектуры роутера. Окно ссылается на релиз на GitHub, а **Зеркало SourceForge** открывает тот же релиз на SourceForge, с теми же архивами и файлами `.sha256`.
4. Нажмите **Выбрать архив** и выберите файл.
5. При желании вставьте сумму SHA256 со страницы релиза в поле **Ожидаемая SHA256 (необязательно)**.
6. Нажмите **Установить**.

Перед тем как что-либо заменить, b4 проверяет, что в архиве есть бинарник `b4` для архитектуры этого роутера, и отклоняет архив для другой архитектуры. Сумма SHA256, прочитанная на странице релиза на другой машине, - независимая проверка загрузки. Сборки MIPS с аппаратной и программной плавающей точкой проходят проверку архитектуры одинаково, поэтому на MIPS имя архива должно соответствовать роутеру, например `mipsle_softfloat`. Неподходящий бинарник откатывается после замены, как и любой новый бинарник, который не запускается.

## Установщик {#the-installer}

Установщик опрашивает источники в том же порядке и читает `B4_MIRRORS` из окружения. Тогда зеркало работает и при первой установке, когда у b4 ещё нет настроек:

```sh
B4_MIRRORS="https://name-1234.username.workers.dev" sh install.sh
```

Значение - список через пробел. `B4_SF_BASE` так же заменяет адрес SourceForge. Если заблокирован и `raw.githubusercontent.com`, Worker отдаёт и сам установщик:

```sh
curl -fsSL https://name-1234.username.workers.dev/b4/raw/main/install.sh -o install.sh
```
