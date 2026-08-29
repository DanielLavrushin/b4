---
sidebar_position: 3
title: Update mirrors
---

# Update mirrors

b4 downloads its own releases, the installer script and the GeoIP and GeoSite databases from
GitHub. A release archive is not served by `github.com` itself: the download URL answers with
a redirect to `release-assets.githubusercontent.com`, and that host is dropped by address on
some networks. The installer then reports a connection timeout and the update stops.

A mirror is a host that fetches from GitHub on b4's behalf and passes the bytes back, so the
client never opens a connection to a blocked address. b4 ships with a list of mirrors and
tries them only after a direct attempt to GitHub has failed, which means an unrestricted
network never touches them.

The order is fixed: GitHub first, then each configured mirror, then the mirrors built into
b4, then SourceForge. Every step downloads the same archive and verifies it against the same
published SHA256.

## The Update mirrors setting

Under **Settings** -> **Control**, the **Update mirrors** field takes a comma-separated list
of base URLs. They are tried ahead of the built-in mirrors, and both the service and the
installer use them: on update, b4 passes the list to the installer in the `B4_MIRRORS`
environment variable.

Only `https` URLs are accepted. An entry carrying a query string or a fragment is ignored,
as is one that is not a URL at all.

:::info Reachability is per-provider, not per-country
A mirror that answers on one network can be unreachable on another with the same country
code. Blocking is applied by individual providers, so the list is a chain rather than a
single recommended host.
:::

:::warning A mirror serves the binary that becomes b4
b4 verifies every download against the SHA256 published beside it, but the checksum comes
from the same host as the archive, so a host serving both can serve a matching pair. Only
mirrors under the reader's own control, or ones the reader trusts, belong in this field.
:::

The setting is not writable over MCP, for the same reason.

## A personal Cloudflare Worker

A Cloudflare Worker is a free per-user mirror hosted on the reader's own Cloudflare account
under a `*.workers.dev` name. It fetches from GitHub inside Cloudflare's network and streams
the response back.

A Worker per reader is harder to block than one shared host. Each account gets its own
`*.workers.dev` subdomain, so there is no single name to filter, and the free-tier request
quota is spent only by its owner.

:::warning `workers.dev` is throttled on some networks
Measured from a Moscow provider: a `*.workers.dev` name completed its TLS handshake in 130 ms
and delivered the first bytes in 390 ms, then the transfer clamped to about 40 bytes per
second, so a 128 KB file reached 16 KB after 400 seconds. Over the same link and in the same
minute, `speed.cloudflare.com` delivered 6 MB in 0.38 seconds and the same file came from
GitHub directly in 0.22 seconds. Cloudflare is not the limit there; the `workers.dev` name is.

Small responses still get through, so the health check and the release list work while an
archive does not. b4 handles that: a transfer holding below 1 KB/s for 30 seconds is
abandoned and the next mirror is tried, which costs about 36 seconds before moving on.

A Worker bound to a custom domain rides ordinary Cloudflare addresses and is not affected.
Check with the commands below before relying on a `workers.dev` name for downloads.
:::

:::info This is not the Telegram relay
The [Cloudflare Worker relay](../telegram/cloudflare-worker) for MTProto is a different
Worker with a different script. That one carries a long-lived WebSocket, which Cloudflare
reclaims mid-session, which is why it sits last among the WebSocket routes. A mirror answers
one short request and closes, so the same limit does not apply to it.
:::

### Setting one up

1. Create a free Cloudflare account.
2. Under **Compute** -> **Workers & Pages**, create a Worker from the default template and
   deploy it.
3. Replace the Worker code with the script below and deploy again.
4. Copy the `name-1234.username.workers.dev` domain into **Update mirrors**, as
   `https://name-1234.username.workers.dev`.

`workers.dev` has to be reachable, and fast enough, from the network in question.

### The script

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

Three details in it are about what the Worker is allowed to reach.

`redirect: "follow"` is what makes the Worker useful at all. GitHub answers a release
download with a redirect to its asset host, and a mirror that passed that redirect back to
the client would send the client straight to the address it cannot reach.

`allowed()` checks the hostname against a fixed list and the first path segment against a
list of repository owners, so the Worker cannot be used as a general-purpose proxy for
arbitrary destinations. The `/b4/` routes build their URLs from a validated tag and file
name for the same reason.

Release archives are fetched without `cacheEverything`, so they stream through rather than
populating Cloudflare's cache. Cloudflare's terms reserve large-file delivery through the
CDN for its paid storage products. Only the small JSON and text responses are cached, for
five minutes, which is what keeps the GitHub API's hourly per-address limit from being
reached.

### Checking it works

```sh
curl -sI https://name-1234.username.workers.dev/b4/health
curl -sL -o /dev/null -w '%{http_code} %{size_download}\n' \
  https://name-1234.username.workers.dev/b4/dl/latest/b4-linux-arm64.tar.gz
```

The second command downloads a release archive through the Worker and prints its status and
size.

## Installing from a file

When no source can be reached at all, the archive can be carried in by hand. In the update
window, **Install from a file instead** takes a `b4-linux-<arch>.tar.gz` downloaded on
another machine.

The window links to both release pages, because the machine running the browser may not
reach GitHub either. **SourceForge mirror** opens that version's folder at
`sourceforge.net/projects/b4core/files/<tag>/`, which carries the same archives and the same
`.sha256` files as the GitHub release.

Before anything is replaced, the service checks the entry the installer would actually put
in place: exactly one file named `b4` at the root of the archive, large enough to be the
service, holding a Linux executable built for this machine. An archive carrying a second
`b4` deeper in the tree is not what gets installed and does not satisfy the check. An archive for the wrong architecture is refused with both architectures named,
rather than being installed and rolled back afterwards.

The **Expected SHA256** field is optional and compared against the upload before it is
installed. The checksum on the release page is read in a browser, on a machine that reached
GitHub, which makes it an independent check: the automatic path takes the archive and its
`.sha256` from the same host, so a host serving both can serve a matching pair.

Installation itself is the same code path as an ordinary update. The previous binary is set
aside, the new one is put in place and run once to confirm it works, and a binary that fails
that check is rolled back.

:::note MIPS float ABI is not covered by the check
Hard-float and soft-float MIPS builds carry the same architecture in the executable header,
so a mismatch between them is not caught by the upload check. It is still caught after the
swap, by the same check that rolls back any binary that will not run.
:::

:::warning The installer on GitHub has to support it
b4 fetches `install.sh` from the repository at update time, so the published installer has
to be a version that understands a supplied archive. b4 checks for that and refuses the
upload if it is not, rather than handing over to an installer that would ignore the file and
download a different version instead. A router whose cached installer supports it can use
that copy instead.
:::

:::info The service still needs the installer script
The upload replaces the download of the release archive, not the download of `install.sh`.
b4 keeps a copy of the installer next to its configuration after each successful update and
falls back to it when it cannot be fetched, so a router that has updated once before can
install from a file with no network at all. On a router that has never updated through the
service, `install.sh` still has to be reachable.
:::

Docker is refused, because an image is replaced by pulling a new one, and so is a b4 that is
not running under a service manager.

:::warning A username and a password have to be set
The web server only enforces authentication once both are configured, and it listens on
every interface by default. Uploading a file replaces the binary that runs as root, so the
route refuses to work at all while the web server would accept any request that reaches the
port. Credentials are set under **Settings** -> **Web Server**.
:::

## The installer

The installer applies the same order on its own, and reads `B4_MIRRORS` from the
environment, so a mirror can be used for a first install before there is any configuration
to hold it:

```sh
B4_MIRRORS="https://name-1234.username.workers.dev" sh install.sh
```

The value is a space-separated list. `B4_SF_BASE` overrides the SourceForge base in the same
way.
