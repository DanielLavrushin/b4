---
sidebar_position: 1
title: Linux
description: Installing b4 on any Linux distribution.
---

The installer works on any distribution: Ubuntu, Debian, Fedora, Alpine, Arch and others.

## Install

As root:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

Or with `wget`:

```bash
wget -qO- https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

The installer puts the binary in `/usr/local/bin` and the configuration in `/etc/b4`.

Without prompts, with the default settings:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --quiet
```

Without a terminal, such as under cron or over `ssh` without `-t`, the installer runs as if `--quiet` were given.

## Service control

### systemd (Ubuntu, Debian, Fedora and most distributions)

```bash
systemctl start b4
systemctl stop b4
systemctl restart b4
systemctl status b4
systemctl enable b4      # start on boot
```

Logs:

```bash
journalctl -u b4 -f
```

### OpenRC (Alpine)

```bash
rc-service b4 start
rc-service b4 stop
rc-service b4 restart
rc-update add b4 default   # start on boot
```

### SysV init (systems without systemd or OpenRC)

```bash
/etc/init.d/b4 start
/etc/init.d/b4 stop
/etc/init.d/b4 restart
/etc/init.d/b4 status
```

## Paths

| What | Where |
| --- | --- |
| Binary | `/usr/local/bin/b4` |
| Configuration | `/etc/b4/b4.json` |
| Service (systemd) | `/etc/systemd/system/b4.service` |
| Service (OpenRC, SysV) | `/etc/init.d/b4` |

## Kernel modules

The NFQUEUE engine needs kernel modules that usually load when the service starts. When they do not, they can be loaded by hand:

```bash
modprobe nfnetlink_queue
modprobe xt_NFQUEUE
modprobe nf_conntrack
```

To check:

```bash
lsmod | grep nfqueue
```

On a kernel without these modules, the TUN engine is the alternative, see [Packet engine](../settings/core.md#packet-engine).

## LXC containers

In an LXC container, the kernel modules are loaded on the host. The container's configuration needs:

```yaml
lxc.cgroup2.devices.allow: c 10:200 rwm
features: nesting=1,keyctl=1
```
