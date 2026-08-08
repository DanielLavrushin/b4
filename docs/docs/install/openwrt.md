---
sidebar_position: 2
title: OpenWRT
---

## Requirements

- OpenWRT 19.07 or newer
- External storage (USB or extroot) is recommended, since the internal router memory may not have enough space

:::warning Disk space
On OpenWRT routers, internal memory is limited (overlay). If less than 2 MB is available, the installer will warn you. Using extroot or a USB drive is recommended.

Extroot setup guide: https://openwrt.org/docs/guide-user/additional-software/extroot_configuration
:::

## Install

Connect to the router over SSH and run:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

If `curl` is not installed:

```bash
opkg update && opkg install curl ca-certificates
```

Or with `wget`:

```bash
wget -qO- https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

:::info wget on OpenWRT
The default `wget` in OpenWRT (BusyBox) does not support HTTPS. Install the full version:

```bash
opkg update && opkg install wget-ssl ca-certificates
```

:::

## Kernel modules

The installer will try to load the required modules automatically. If on startup you see the warning `[WARN] No netfilter queue module available` or errors related to nftables, install the modules manually.

### OpenWRT 24.x+ (apk)

```bash
apk add kmod-nft-queue kmod-nft-nat kmod-nft-compat kmod-nft-conntrack
```

### OpenWRT 23.x and older (opkg)

```bash
opkg update
opkg install kmod-nft-queue kmod-nft-conntrack nftables-json coreutils-nohup
```

For very old versions (without nftables):

```bash
opkg install kmod-nfnetlink-queue kmod-ipt-nfqueue iptables-mod-nfqueue iptables-mod-conntrack-extra
```

### Loading modules

After installing the modules you may need to load them manually:

```bash
modprobe nft_queue
modprobe nft_ct
modprobe xt_connbytes
```

If the command runs with no output, the module loaded successfully.

## Service control

```bash
/etc/init.d/b4 enable     # autostart on boot
/etc/init.d/b4 start
/etc/init.d/b4 stop
/etc/init.d/b4 restart
```

:::tip Working over SSH
The b4 service runs as a system daemon - it keeps running after the SSH session is closed (PuTTY, terminal, etc.). You do not need to use `screen` or `nohup` manually.
:::

## Paths

When `/opt` is available (extroot/USB):

| What | Where |
| --- | --- |
| Binary | `/opt/bin/b4` |
| Configuration | `/opt/etc/b4/b4.json` |

Without external storage (fallback):

| What | Where |
| --- | --- |
| Binary | `/usr/bin/b4` |
| Configuration | `/etc/b4/b4.json` |

## Web interface

After startup, b4 is reachable at `http://<router IP>:7000`. For example, if the router IP is `192.168.1.1`, open in the browser:

```text
http://192.168.1.1:7000
```

## LuCI application

There is a third-party package [luci-app-b4](https://github.com/BugOldfag/luci-app-b4) that adds b4 management to the LuCI interface. The project is in alpha and covers only part of the features. The main b4 web interface (port 7000) remains available.

## Troubleshooting

### Service crashed / service will not start

1. Make sure the kernel modules are installed and loaded (see "Kernel modules" above)
2. Check the logs: `logread | grep b4`

### Error: Could not process rule

If b4 fails with an error while adding rules to a chain, there may be leftover "broken" tables from a previous failed run. Clear them:

```bash
nft delete table inet b4_mangle 2>/dev/null
```

Then start b4 again:

```bash
/etc/init.d/b4 restart
```

### Slow speed / video stuttering

Check the **Software flow offloading** setting under Network -> Firewall. Try turning it on or off - on some devices this affects b4 performance.

### Keeping flow offloading and b4 together

Flow offloading moves an established connection to a fast path that skips the netfilter hooks b4 works from, so with it enabled b4 runs but never sees the traffic. On weaker hardware turning it off costs a lot of throughput.

b4 only inspects the first packets of a connection (19 for TCP, 8 for UDP by default, see `queue.tcp_conn_bytes_limit` and `queue.udp_conn_bytes_limit`), so offloading can be delayed until b4 is done. Edit `/usr/share/firewall4/templates/ruleset.uc` and find:

```text
meta l4proto { tcp, udp } flow offload @ft;
```

Replace it with:

```text
meta l4proto { tcp, udp } ct original packets ge 40 flow offload @ft;
```

Then reload the firewall:

```bash
fw4 restart
```

Points to check:

- The threshold has to stay above the packet limits configured in b4. Raising `tcp_conn_bytes_limit` above the threshold puts the connection on the fast path before b4 is finished with it.
- `ct original packets` reads conntrack accounting. If `sysctl net.netfilter.nf_conntrack_acct` returns `0`, the counter stays at zero, the rule never matches and nothing is offloaded at all.
- Sets with **Duplicate** enabled for TCP are inspected for the whole life of the connection, so no threshold is high enough for them.
- The file belongs to the `firewall4` package and is overwritten by package upgrades and by sysupgrade.

The system diagnostics (Settings -> System info, and the installer's diagnostics screen) report the threshold they find and compare it against b4's own limits.
