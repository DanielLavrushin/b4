---
sidebar_position: 2
title: Installation
description: Choosing the installation page for a router, a Linux computer or Docker.
---

b4 installs on Linux routers, computers and servers. Linux, OpenWrt, ASUS Merlin and Keenetic use the same installer script, run over SSH. It detects the platform and the processor architecture. On MikroTik and in Docker, b4 runs as a container.

| Platform | Page | Notes |
| --- | --- | --- |
| Any Linux distribution | [Linux](linux.md) | Ubuntu, Debian, Fedora, Alpine, Arch and others, with systemd, OpenRC or SysV init |
| OpenWrt 19.07 or newer | [OpenWrt](openwrt.md) | A USB drive or extroot is recommended, since internal storage is small |
| ASUS routers with Asuswrt-Merlin | [ASUS Merlin](merlin.md) | Needs Entware on a USB drive |
| Keenetic routers with OPKG | [Keenetic](keenetic.md) | Needs Entware and the Netfilter components |
| MikroTik with RouterOS 7.21.1 or newer | [MikroTik](mikrotik.md) | A container on ARM64 or AMD64, with external storage formatted as Ext4 |
| Any system with Docker | [Docker](docker.md) | Host network mode |

The memory, kernel and architecture requirements are listed under [Requirements](../intro.md#requirements).

After installation, the web interface listens on port `7000`: `http://<device address>:7000`, for example `http://192.168.1.1:7000` on a router. [First steps](../quickstart.md) continues from there.

## Update and remove {#update-remove}

Updating, backups, removal and the installer's diagnostics are described under [Updating, backing up and removing](update.md).
