---
sidebar_position: 3
title: Downloading b4 when GitHub is blocked
description: Mirrors, a personal Cloudflare Worker and installing from a file, for networks where b4 cannot download from GitHub.
---

b4 downloads its releases, the installer and the GeoSite and GeoIP databases from GitHub. On some networks the host that serves the release files, `release-assets.githubusercontent.com`, is blocked, and an update stops with a connection timeout. There are three ways around it: mirrors, a personal Cloudflare Worker, and installing from a file.

## How b4 picks a source {#sources}

b4 tries GitHub first, then each mirror in **Update mirrors**, then the mirrors built into b4, then SourceForge. Every source serves the same archive, checked against the same published SHA256. On a network without blocks, the mirrors are never used.

## The Update mirrors setting {#the-update-mirrors-setting}

**Update mirrors** under **Settings > System > Service** takes a comma-separated list of mirror addresses. They are tried before the built-in mirrors, and b4 passes them to the installer, so an update from the web interface uses them too. Only `https` addresses are accepted. An entry with a query string or a fragment is ignored.

A mirror that works on one network can be blocked on another, because each provider blocks on its own. A list of several mirrors is tried in order.

:::warning A mirror serves the binary that becomes b4
b4 checks every download against the SHA256 published next to it, but the checksum comes from the same host as the archive. A host that serves both can serve a matching pair. Only mirrors under one's own control, or trusted ones, belong in this field.
:::

## A personal Cloudflare Worker {#a-personal-cloudflare-worker}

A Cloudflare Worker is a free mirror on a personal Cloudflare account, under a `*.workers.dev` name. It fetches from GitHub inside Cloudflare's network and passes the result on. Every account gets its own subdomain, so there is no single name to block.

On some networks `*.workers.dev` names are throttled: small responses get through, while an archive crawls. b4 abandons a stalled download and moves on to the next source. The [checks](#checking-it-works) below show whether a Worker is usable from a given network. A Worker bound to a custom domain is not affected.

This Worker is not the [Cloudflare Worker relay](../telegram/cloudflare-worker.md) for Telegram, which runs a different script.

### Setup {#setting-one-up}

1. Create a free Cloudflare account.
2. Under **Compute** > **Workers & Pages**, create a Worker from the default template and deploy it.
3. Replace the Worker's code with the script below and deploy again.
4. In b4, add the Worker's address, such as `https://name-1234.username.workers.dev`, to **Update mirrors** under **Settings > System > Service**.

### The script {#the-script}

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

The script fetches only from GitHub and SourceForge, and only for the repository owners it lists, so it cannot serve as an open proxy. Release archives pass through it without being cached.

### Checking the result {#checking-it-works}

```sh
curl -sI https://name-1234.username.workers.dev/b4/health
curl -sL -o /dev/null -w '%{http_code} %{size_download}\n' \
  https://name-1234.username.workers.dev/b4/dl/latest/b4-linux-arm64.tar.gz
```

The first command checks that the Worker answers. The second downloads a release archive through it and prints the status and the size. A download that stalls here means `workers.dev` is throttled on this network.

## Installing from a file {#installing-from-a-file}

When no source can be reached, the archive can be brought in by hand, from a machine that has access.

Requirements:

- A username and a password for the web interface, under **Settings > System > Web Server**. Without them the upload is refused, since it replaces the binary that runs as root.
- b4 running as a service, not in Docker.
- The installer script. b4 fetches `install.sh`, or uses the copy it saved at its last successful update. A router that has never been updated from the web interface needs `install.sh` reachable.

Steps:

1. Open the **Version Management** dialog from the version at the bottom of the side menu.
2. Click **Install from a file instead**.
3. On a machine with access, download `b4-linux-<arch>.tar.gz` for the router's architecture from the release page. The dialog links to it on GitHub, and **SourceForge mirror** opens the same release on SourceForge, with the same archives and `.sha256` files.
4. Click **Choose archive** and pick the file.
5. Optionally, paste the SHA256 from the release page into **Expected SHA256 (optional)**.
6. Click **Install**.

Before anything is replaced, b4 checks that the archive holds a `b4` binary for this router's architecture, and refuses one built for another. The SHA256 read on the release page, on another machine, is an independent check of the upload. Hard-float and soft-float MIPS builds pass the architecture check alike, so on MIPS the archive name has to match the router, such as `mipsle_softfloat`. A wrong one is rolled back after the swap, as is any new binary that does not run.

## The installer {#the-installer}

The installer tries the sources in the same order and reads `B4_MIRRORS` from the environment. A mirror then works for a first install too, before b4 has any settings:

```sh
B4_MIRRORS="https://name-1234.username.workers.dev" sh install.sh
```

The value is a space-separated list. `B4_SF_BASE` replaces the SourceForge address the same way. When `raw.githubusercontent.com` is blocked as well, the Worker serves the installer itself:

```sh
curl -fsSL https://name-1234.username.workers.dev/b4/raw/main/install.sh -o install.sh
```
