---
sidebar_position: 1
title: Linux
---

Universal installation. Works on any distribution: Ubuntu, Debian, Fedora, Alpine, Arch, and others.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

Or with `wget`:

```bash
wget -qO- https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

The installer detects the architecture automatically, places the binary in `/usr/local/bin`, and creates the configuration in `/etc/b4`.

For a non-interactive install (default settings, no prompts):

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --quiet
```

## Service control

### systemd (Ubuntu, Debian, Fedora, and most distributions)

```bash
systemctl start b4
systemctl stop b4
systemctl restart b4
systemctl status b4
systemctl enable b4      # autostart on boot
```

View logs:

```bash
journalctl -u b4 -f
```

### OpenRC (Alpine)

```bash
rc-service b4 start
rc-service b4 stop
rc-service b4 restart
rc-update add b4 default   # autostart on boot
```

## Paths

| What | Where |
| --- | --- |
| Binary | `/usr/local/bin/b4` |
| Configuration | `/etc/b4/b4.json` |
| Service (systemd) | `/etc/systemd/system/b4.service` |
| Service (OpenRC/SysV) | `/etc/init.d/b4` |

## Kernel modules

b4 uses NFQUEUE to intercept packets. The required kernel modules are usually loaded automatically when the service starts. If problems occur, they can be loaded manually:

```bash
modprobe nfnetlink_queue
modprobe xt_NFQUEUE
modprobe nf_conntrack
```

To verify:

```bash
lsmod | grep nfqueue
```

:::info LXC containers
In LXC containers, kernel modules must be loaded on the host. The container config needs:

```yaml
lxc.cgroup2.devices.allow: c 10:200 rwm
features: nesting=1,keyctl=1
```

:::
