# B4

![GitHub Release](https://img.shields.io/github/v/release/daniellavrushin/b4)
![GitHub Downloads](https://img.shields.io/github/downloads/daniellavrushin/b4/total)
![Docker Pulls](https://img.shields.io/docker/pulls/lavrushin/b4)
[![Codacy Badge](https://app.codacy.com/project/badge/Grade/140c35c115f14640a4010e08091d2034)](https://app.codacy.com/gh/DanielLavrushin/b4/dashboard?utm_source=gh&utm_medium=referral&utm_content=&utm_campaign=Badge_grade)
[![Quality gate status](https://sonarcloud.io/api/project_badges/measure?project=DanielLavrushin_b4&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=DanielLavrushin_b4)

[[Documentation](https://docs.b4core.app/)] [[Документация (RU)](https://docs.b4core.app/ru/)] [[Telegram](https://t.me/byebyebigbro)]

B4 (Bye Bye Big Bro) is a Linux service that bypasses DPI-based blocking. It runs on routers, servers and desktops and rewrites packets so DPI equipment cannot classify the connection, while the destination server still gets valid data. Installed on a router, it covers every device on the network with nothing configured on the clients.

Configuration is built from **sets**. A set is a list of targets (domains, IPs/CIDRs, geosite/geoip categories, ports, LAN devices) plus the bypass techniques, DNS handling and routing applied to them. Sets are edited in the built-in web UI on port 7000 and applied to the running service without a restart.

If you don't know which technique your provider needs, Discovery tests strategies against your own connection and saves the working one as a set. The Watchdog re-runs it when a site breaks.

<img width="1187" height="787" alt="image" src="https://github.com/user-attachments/assets/3e4c105d-5b28-4e93-ab54-6d92338b1293" />

## Features

- Ten fragmentation strategies: `combo`, `hybrid`, `tcp`, `ip`, `tls`, `oob`, `disorder`, `extsplit`, `firstbyte`, `none`, plus a random strategy pool
- Fake packet injection: decoy ClientHellos, TCP desync bursts, window manipulation, fake SYN, SACK stripping, ClientHello mutation
- RST protection - detects forged resets by TTL/flag/option fingerprint and drops them
- [DNS](https://docs.b4core.app/docs/dns): per-set redirect to a plain resolver or DoH, hosts-style pins, fragmented queries, DNS-over-TCP interception, healing of answers pointing at dead addresses
- [Routing](https://docs.b4core.app/docs/sets/routing): send a set's traffic out a chosen interface (VPN, WireGuard, second WAN), through an upstream SOCKS5 proxy, or block it
- [Block mode](https://docs.b4core.app/docs/sets/blocking) for network-wide ad and tracker blocking
- Telegram: [built-in MTProto proxy](https://docs.b4core.app/docs/mtproto), plus a transparent Telegram-over-WebSocket bridge for stock clients
- [Discovery](https://docs.b4core.app/ru/docs/discovery) finds a working strategy, [Watchdog](https://docs.b4core.app/docs/watchdog) re-heals broken domains, [DPI Detector](https://docs.b4core.app/ru/docs/detector) reports what the provider is doing
- [Escalation chains](https://docs.b4core.app/docs/sets/escalation): when a set stops working for a host, that host moves to a heavier backup set
- Per-device rules by MAC
- Works on both iptables and nftables, picking the backend automatically, and handles IPv4 and IPv6
- Web UI in English and Russian

## Requirements

- Linux, kernel 3.13+, root access
- NFQUEUE support in the kernel, or [TUN mode](#tun-mode) if it has none
- `iptables` or `nft`, plus `ip`. `ipset` is needed for routing features on iptables-only systems
- ~64 MB free RAM, ~30 MB disk

> [!IMPORTANT]
> On OpenWrt and most router firmware, netfilter flow offloading puts established connections on a fast path that skips the hooks B4 uses. If bypass appears to do nothing, run `install.sh --sysinfo`.

## Installation

Run as root (`sudo -i`, or pipe into `sudo sh`).

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sudo sh
```

```bash
wget -qO- https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sudo sh
```

Then open `http://<device-ip>:7000`.

|             |                                                                                 |
| ----------- | ------------------------------------------------------------------------------- |
| Config      | `/etc/b4/b4.json`, or `/opt/etc/b4/b4.json` on Entware and OpenWrt-with-extroot |
| Logs        | `/var/log/b4/errors.log`                                                        |
| Diagnostics | `install.sh --sysinfo`                                                          |

### Installer options

```bash
./install.sh                         # interactive wizard
./install.sh v1.77.0                 # specific version
./install.sh --quiet                 # non-interactive, all defaults
./install.sh --arch=mipsle_softfloat # force architecture
./install.sh --platform=openwrt      # force platform
./install.sh --sysinfo               # system diagnostics
./install.sh --help                  # full usage
```

> [!WARNING]
> `--remove --quiet` deletes the config directory and geodata without asking.

## First run

1. Open the web UI, go to **Discovery**, type a blocked domain and press **Start search**. Takes 1-10 minutes.
2. On a successful result click **Use this configuration**, then **Create a new set**.
3. Open the site. On the **Traffic** page, connections to that domain now show the set name.

Docs: [Quickstart](https://docs.b4core.app/docs/quickstart)

## Supported platforms

| Platform                      | Service control                                              |
| ----------------------------- | ------------------------------------------------------------ |
| Generic Linux (systemd)       | `systemctl start\|stop\|restart b4`                          |
| Generic Linux (OpenRC / SysV) | `rc-service b4 start`, `/etc/init.d/b4 start`                |
| OpenWrt 19.07+                | `/etc/init.d/b4 start\|stop\|restart\|enable`                |
| Asus Merlin, Keenetic         | `/opt/etc/init.d/S99b4 start\|stop\|restart` (needs Entware) |
| MikroTik RouterOS 7.21.1+     | via the Docker container                                     |
| Docker                        | Linux hosts only, see [below](#docker)                       |

Architectures: `amd64`, `386`, `arm64`, `armv5`, `armv6`, `armv7`, `mips`, `mipsle`, `mips_softfloat`, `mipsle_softfloat`, `mips64`, `mips64le`, `loong64`, `ppc64`, `ppc64le`, `riscv64`, `s390x`.

Docs: [Linux](https://docs.b4core.app/docs/install/linux) · [OpenWrt](https://docs.b4core.app/docs/install/openwrt) · [Merlin](https://docs.b4core.app/docs/install/merlin) · [Keenetic](https://docs.b4core.app/docs/install/keenetic) · [MikroTik](https://docs.b4core.app/docs/install/mikrotik) · [Docker](https://docs.b4core.app/docs/install/docker)

## Docker

```bash
docker run -d --name b4 --network host \
  --cap-add NET_ADMIN --cap-add NET_RAW --cap-add SYS_MODULE \
  -v /etc/b4:/etc/b4 \
  lavrushin/b4:latest
```

```yaml
services:
  b4:
    image: lavrushin/b4:latest
    container_name: b4
    network_mode: host
    cap_add:
      - NET_ADMIN
      - NET_RAW
      - SYS_MODULE
    volumes:
      - ./config:/etc/b4
    restart: unless-stopped
```

Linux hosts only, and `--network host` is mandatory. The config is auto-discovered at `/etc/b4/b4.json` - do not override it with `--config` or it gets written inside the container and lost on recreation. Self-update and restart from the web UI do not work in Docker; pull a new image instead.

Images: `lavrushin/b4` and `ghcr.io/daniellavrushin/b4`.

## Sets

A set is a list of targets plus what to do with traffic matching them. A set can target any combination of domains, IPs and CIDRs, GeoSite and GeoIP categories, destination ports, LAN devices by MAC, and TLS or IP version.

Sets are evaluated top-down and the first match wins, so specific sets go above general ones. Two sets listing the same domain is a silent conflict - only the first applies.

Docs: [Sets](https://docs.b4core.app/docs/sets/) · [Targets](https://docs.b4core.app/docs/sets/targets)

## GeoSite / GeoIP

Rather than listing domains by hand, a set can target whole categories - `youtube`, `category-ads-all`, a country's address ranges - from the v2ray/xray geosite and geoip databases. This is how most people cover a service and everything it talks to.

Download the databases from **Settings → Geodat Settings** (Loyalsoldier, RUNET Freedom, b4geoip, or your own URL), or let the installer fetch them during setup, then add categories under **Sets → Targets**. They can be refreshed on a schedule.

Docs: [Geo data](https://docs.b4core.app/docs/settings/geodata)

## Bypass techniques

| Strategy          |                                                              |
| ----------------- | ------------------------------------------------------------ |
| `combo` (default) | multi-point split, shuffled segments, paced                  |
| `hybrid`          | picks combo, disorder, extsplit or firstbyte per ClientHello |
| `tcp`             | plain TCP segmentation                                       |
| `ip`              | real IP-layer fragments                                      |
| `tls`             | split relative to the TLS record header                      |
| `oob`             | two halves with a one-byte URG packet between them           |
| `disorder`        | out-of-order delivery                                        |
| `extsplit`        | cuts at the extension boundary before `server_name`          |
| `firstbyte`       | first byte, wait, then the rest                              |
| `none`            | no fragmentation, for faking- or desync-only sets            |

On top of the split: fake ClientHello injection, TCP desync, window manipulation, fake SYN, TCP-MD5, SACK stripping, ClientHello mutation, sequence overlap, packet duplication and MSS clamping.

Matched UDP and QUIC is either fragmented and faked like TCP, dropped, or rejected with an ICMP unreachable so the browser falls back to TCP straight away. QUIC Initial packets are decrypted to read the SNI. STUN and Discord voice packets are left alone by default.

Docs: [Splitting](https://docs.b4core.app/docs/sets/tcp/splitting) · [Faking](https://docs.b4core.app/docs/sets/tcp/faking) · [RST Protection](https://docs.b4core.app/docs/sets/tcp/rst-protection) · [UDP](https://docs.b4core.app/docs/sets/udp)

## DNS

B4 intercepts plain DNS on port 53 and answers matched domains itself. Per set: redirect to a plain resolver or DoH endpoint, hosts-style pins, fragmented queries, and HealDNS to strip unreachable addresses out of answers.

A device running its own DoH/DoT (Chrome Secure DNS, Android Private DNS) bypasses all of it - turn that off on the client or block it.

Docs: [DNS](https://docs.b4core.app/docs/dns)

## Routing and blocking

Besides mangling packets, a set can pick a routing mode:

| Mode                    |                                                           |
| ----------------------- | --------------------------------------------------------- |
| Network interface       | route out a chosen interface (VPN, WireGuard, second WAN) |
| Upstream SOCKS5 proxy   | hand matched traffic to an external proxy via TPROXY      |
| Block (blackhole)       | DNS NXDOMAIN, TCP RST, ICMP unreachable, or silent drop   |
| Telegram over WebSocket | carry Telegram traffic over B4's WebSocket bridge         |

Destination IPs are learned from DNS answers and live traffic, so CDNs that rotate DNS keep working. Block mode with a geosite ads category gives network-wide ad blocking.

The two proxy modes need TPROXY support in the kernel.

Docs: [Routing](https://docs.b4core.app/docs/sets/routing) · [Blocking](https://docs.b4core.app/docs/sets/blocking)

## Telegram

Two independent features, under **Settings → MTProto Proxy**.

**MTProto proxy** - a fake-TLS proxy Telegram clients connect to directly, with named revocable secrets and a `tg://proxy` share link. Disabled by default, port 3128.

**Telegram over WebSocket** - a routing mode you pick on a set, working as a transparent bridge. Clients keep using stock Telegram with no proxy configured, and B4 carries their traffic over WebSocket, Cloudflare-proxied domains, or your own Cloudflare Worker when direct TCP is blocked. It runs on its own; the proxy above does not need to be enabled.

Docs: [MTProto / Telegram](https://docs.b4core.app/docs/mtproto)

## Discovery, Watchdog, DPI Detector

**Discovery** tests bypass strategies against your real network on an isolated queue, tunes the parameters of whatever works, and checks for DNS poisoning and IP-level blocks along the way. Results apply as sets without a restart.

**Watchdog** fetches the domains you list periodically. After enough consecutive failures it re-runs Discovery for that domain, verifies the result, and rolls back if it does not hold. Disabled by default.

**DPI Detector** runs read-only probes and reports what the provider is doing: DNS integrity and availability, domain reachability, where connections get throttled, which SNI values pass, and Telegram speed.

## Web interface and REST API

The web UI and the REST API share one port, 7000 by default.

| Page         |                                                                          |
| ------------ | ------------------------------------------------------------------------ |
| Dashboard    | live metrics in reorderable panels                                       |
| Sets         | create, edit, compare, reorder and import/export sets                    |
| Discovery    | run strategy search and apply results                                    |
| Watchdog     | monitored domain health, force a check                                   |
| DPI Detector | run diagnostics, with history                                            |
| Traffic      | live connections with set/domain/TLS/device/ASN, click-to-add into a set |
| Logs         | live log stream, filters, downloadable trace bundle                      |
| Settings     | Core, Geodat, Discovery, MTProto Proxy, API, Payloads, Backup            |

Most changes apply immediately. Core settings need a restart, and the UI says so when they do.

> [!CAUTION]
> The web UI and REST API are unauthenticated until you set **both** a username and a password under **Settings → Core → Web Server**. Do not expose port 7000 to the internet.

HTTPS is enabled by pointing the same screen at a certificate and key. The installer detects router certificates on OpenWrt and Asus Merlin and offers to turn it on.

The API reference is on the docs site: <https://docs.b4core.app/swagger>

Docs: [Security](https://docs.b4core.app/docs/settings/security) · [Dashboard](https://docs.b4core.app/docs/dashboard) · [Connections (RU)](https://docs.b4core.app/ru/docs/connections) · [Logs (RU)](https://docs.b4core.app/ru/docs/logs)

## SOCKS5 proxy

B4 also ships a SOCKS5 server for applications that support it. Enable it under **Settings → Core → SOCKS5 Proxy**; it listens on port 1080 by default and routes through the bypass engine.

```bash
curl --socks5 127.0.0.1:1080 https://example.com
```

Leaving the username and password empty means no authentication, and it binds all interfaces by default. Restart B4 after changing SOCKS5 settings.

## Import from zapret or byedpi

Paste an existing `nfqws` or `ciadpi` command line into **Sets → Import** and B4 translates it into equivalent sets, with a per-option report of what was mapped exactly, approximated or lost. Only zapret and byedpi are supported.

Docs: [Import from another tool](https://docs.b4core.app/docs/sets/import)

## Configuration file

Everything lives in one JSON file, created on first run and migrated automatically on upgrade. Back it up from **Settings → Backup**.

> [!IMPORTANT]
> B4 does not watch the config file. Editing it by hand while the service runs has no effect and gets overwritten. Edit while stopped, or use the web UI.

Docs: [Configuration file (RU)](https://docs.b4core.app/ru/docs/advanced/config) · [Core settings](https://docs.b4core.app/docs/settings/core)

## TUN mode

For kernels without NFQUEUE support, switch the engine mode to **TUN interface** under **Settings → Core**. B4 creates its own TUN device and steers traffic into it with policy routing, running the same packet engine.

> [!IMPORTANT]
> TUN mode is a fallback, not an equal alternative. Roughly 60% of the feature set works there: B4 installs none of its usual firewall rules, so anything built on them is unavailable. Discovery and the Watchdog's auto-healing do not run, and IPv6 is not forwarded. Use NFQUEUE where the kernel supports it.

## Command line

Domains, sets and geosite categories are config-file and web UI only; there are no flags for them.

```bash
b4 --help                    # print help
b4 --version                 # version, commit, build date
b4 --config /path/b4.json    # use a specific config file
b4 --verbose debug           # debug | trace | info | silent
b4 --web-port 8080           # override the web UI port (0 disables it)
b4 --clear-tables            # remove b4's firewall rules and exit
```

Also accepted: `--queue-num`, `--threads`, `--mark`, `--ipv4`, `--ipv6`, `--tables-monitor-interval`, `--log-dir`, `--syslog`, `-i/--instaflush`.

Docs: [CLI parameters (RU)](https://docs.b4core.app/ru/docs/advanced/cli)

## Updating and uninstalling

```bash
# to the latest version
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sudo sh -s -- --update

# to a specific version
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sudo sh -s -- --update v1.43.0

# uninstall
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sudo sh -s -- --remove
```

Updating from the web UI also works, except in Docker.

## Troubleshooting

`install.sh --sysinfo` reports the kernel, platform, service status, firewall backend, NFQUEUE and conntrack probes, flow-offload state and missing tools. The same data is in the web UI under **Settings → System Info**.

Common causes when bypass appears to do nothing:

- **Flow offloading** (OpenWrt and most router firmware). Established flows skip B4's hooks. Disable it, or guard it with `ct original packets ge 40 flow offload @ft` in `/usr/share/firewall4/templates/ruleset.uc` followed by `fw4 restart`.
- **Client-side DoH/DoT.** A device resolving names itself bypasses every DNS feature.
- **Missing kernel modules.** B4 names the module and package it needs. If NFQUEUE cannot be made to work at all, fall back to [TUN mode](#tun-mode) and its limitations.
- **Set order.** First match wins. The **Traffic** page shows which set actually matched.

For bug reports: on the **Logs** page click **Start trace**, reproduce the problem, then stop and download. The bundle has the build version, system diagnostics and a redacted summary of the enabled sets.

## Building from source

Requires Go 1.25.3, Node 22 and pnpm 10.

```bash
git clone https://github.com/daniellavrushin/b4.git
cd b4

# Build the web UI first - it gets embedded into the binary
make build-ui

# Build for the current platform (VERSION is required)
make build VERSION=1.77.0

make build-all      # all release architectures into out/assets/
make linux-arm64    # a single architecture
make help           # all targets
```

`install.sh` is generated. Edit the sources under [installer/](installer/) and run `make build-installer`.

## Documentation

<https://docs.b4core.app/> · [Русская версия](https://docs.b4core.app/ru/)

A few English pages (Discovery, DPI Detector, Connections, Logs and the Advanced section) are still being translated. Use the language switcher for the Russian originals.

## Contributing

Contributions are accepted through GitHub pull requests.

## Community projects

Maintained outside this repository:

- [luci-app-b4](https://github.com/BugOldfag/luci-app-b4) by BugOldfag - adds b4 management to the LuCI interface on OpenWrt
- [b4-mikrotik](https://hub.docker.com/r/wiktorbgu/b4-mikrotik) by wiktorbgu - container image packaged for MikroTik RouterOS, with an OpenRC-supervised service, startup selection of the firewall backend and a fix for the routing rule priorities RouterOS 7.22 passes into containers

## Credits

Based on research from:

- [youtubeUnblock](https://github.com/Waujito/youtubeUnblock) - C-based DPI bypass
- [GoodbyeDPI](https://github.com/ValdikSS/GoodbyeDPI) - Windows DPI circumvention
- [zapret](https://github.com/bol-van/zapret) - Advanced DPI bypass techniques
- [byedpi](https://github.com/hufrea/byedpi) - proxy-based DPI bypass
- [dpi-detector](https://github.com/Runnin4ik/dpi-detector) - DPI/TSPU detection techniques and test target lists (the DPI Detector feature is based on this project)
- [Ladon](https://github.com/belotserkovtsev/ladon) - reactive Anti-DPI detection engine; its failure-classification approach (telling server-side rejection apart from DPI interference) informed the Watchdog feature
- [tg-ws-proxy](https://github.com/Flowseal/tg-ws-proxy) - Telegram MTProto-over-WebSocket bridging

## License

This project is provided for educational purposes. Users are responsible for compliance with applicable laws and regulations.
The authors are not responsible for misuse of this software.
