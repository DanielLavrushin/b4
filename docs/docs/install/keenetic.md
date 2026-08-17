---
sidebar_position: 4
title: Keenetic
---

## Requirements

- Keenetic router with OPKG support
- Entware installed (required)

## Install Entware

### Newer models (with built-in storage)

1. The router web interface is opened
2. Section **System settings**
3. The **OPKG package manager** component is enabled

### Older models (USB drive required)

1. A USB drive is attached to the router
2. Entware is installed through the package manager

More details: [help.keenetic.com](https://help.keenetic.com/hc/en-us/articles/360021214160)

## Netfilter components {#netfilter-components}

Keenetic NDMS does not ship all the netfilter kernel modules b4 needs out of the box. The required components have to be enabled before b4 is installed:

1. In the router web interface: **System settings** → **Component options**
2. **Netfilter** - the base netfilter component - is enabled
3. Once Netfilter is enabled, a new option appears: **Xtables-addons for Netfilter** (provides `xt_connbytes` and other extensions b4 relies on)
4. The changes are applied, followed by a router reboot or a component reload

Then, over SSH, the iptables userspace is installed:

```bash
opkg install iptables
```

## Install b4

Over an SSH connection to the router:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

## Service control

```bash
/opt/etc/init.d/S99b4 start
/opt/etc/init.d/S99b4 stop
/opt/etc/init.d/S99b4 restart
```

## Paths

| What | Where |
| --- | --- |
| Binary | `/opt/sbin/b4` |
| Configuration | `/opt/etc/b4/b4.json` |
| Service | `/opt/etc/init.d/S99b4` |

## Architecture

- Older models (MT7621) - `mipsle_softfloat`
- Newer models (aarch64) - `arm64`

The installer detects the architecture automatically.

:::warning Without Entware
Without Entware, b4 is placed in `/tmp`, which is cleared on every reboot. For persistent operation, Entware is required.
:::

## Troubleshooting

After the service starts, the log is worth checking:

```bash
cat /var/log/b4/errors.log
```

The line `xt_connbytes kernel module is not available` means the Netfilter components were not enabled correctly - see [Netfilter components](#netfilter-components) above, where both **Netfilter** and **Xtables-addons for Netfilter** have to be active.

If the log is empty (or has no errors), the b4 web interface should be reachable on the router's LAN IP.
