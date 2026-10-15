---
sidebar_position: 2
title: OpenWrt
description: Installing b4 on a router with OpenWrt.
---

## Requirements

- OpenWrt 19.07 or newer.
- Free space for the binary. Internal storage (the overlay) is often small, and the installer warns when less than 2 MB is free. A USB drive or [extroot](https://openwrt.org/docs/guide-user/additional-software/extroot_configuration) is recommended.

## Install

Over SSH on the router:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

When `curl` is missing:

```bash
opkg update && opkg install curl ca-certificates
```

Or with `wget`:

```bash
wget -qO- https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

The BusyBox `wget` that OpenWrt ships does not support HTTPS. The full version installs with:

```bash
opkg update && opkg install wget-ssl ca-certificates
```

## Kernel modules

The installer tries to load the modules b4 needs. When it warns `The kernel cannot queue packets to b4`, it also prints the packages to install. The usual ones are below.

### OpenWrt 24.x and newer (apk)

```bash
apk add kmod-nft-queue kmod-nft-nat kmod-nft-compat kmod-nft-conntrack
```

### OpenWrt 23.x and older (opkg)

```bash
opkg update
opkg install kmod-nft-queue kmod-nft-conntrack nftables-json coreutils-nohup
```

For versions without nftables:

```bash
opkg install kmod-nfnetlink-queue kmod-ipt-nfqueue iptables-mod-nfqueue iptables-mod-conntrack-extra
```

### Loading the modules

Installed modules may still need to be loaded by hand. A command that prints nothing has loaded its module:

```bash
modprobe nft_queue
modprobe nft_ct
modprobe xt_connbytes
```

## Service control

```bash
/etc/init.d/b4 enable     # start on boot
/etc/init.d/b4 start
/etc/init.d/b4 stop
/etc/init.d/b4 restart
```

The service keeps running after the SSH session closes, so `screen` or `nohup` is not needed.

## Paths

| What | With `/opt` (extroot or USB) | Without external storage |
| --- | --- | --- |
| Binary | `/opt/bin/b4` | `/usr/bin/b4` |
| Configuration | `/opt/etc/b4/b4.json` | `/etc/b4/b4.json` |

## Web interface

After the start, the web interface is at `http://<router address>:7000`, for example `http://192.168.1.1:7000`.

## LuCI application

A third-party package, [luci-app-b4](https://github.com/BugOldfag/luci-app-b4), adds b4 to the LuCI interface. It is in alpha and covers part of the features. The b4 web interface on port 7000 stays available.

## Troubleshooting

A service that does not start, `Could not process rule`, slow speed and flow offloading are covered under [Router platforms](../troubleshooting/platforms.md#openwrt).
