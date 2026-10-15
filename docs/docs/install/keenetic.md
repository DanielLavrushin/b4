---
sidebar_position: 4
title: Keenetic
description: Installing b4 on a Keenetic router with Entware.
---

## Requirements

- A Keenetic router with OPKG support.
- Entware.
- The Netfilter components, see [below](#netfilter-components).

## Install Entware

### Newer models, with built-in storage

1. Open the router's web interface.
2. Open **System settings**.
3. Turn on the **OPKG package manager** component.

### Older models, with a USB drive

1. Attach a USB drive to the router.
2. Install Entware through the package manager.

The [Keenetic help](https://help.keenetic.com/hc/en-us/articles/360021214160) has the details.

## Netfilter components {#netfilter-components}

KeeneticOS does not ship every netfilter module b4 needs, so two components are turned on before b4 is installed. The components are available only under the `admin` account.

1. In the router's web interface, open **System settings** > **Component options**.
2. Search the components for `netfilter`, and turn on **Kernel modules for Netfilter** if it is not on yet.
3. Turn on **Xtables-addons for Netfilter**, which appears once the Netfilter modules are on.
4. Apply the changes, then reboot the router or reload the components.
5. Over SSH, install the iptables userspace:

   ```bash
   opkg install iptables
   ```

## Install b4

Over SSH on the router:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

The installer detects the architecture: `mipsle_softfloat` on older models (MT7621), `arm64` on newer ones (aarch64).

:::warning Without Entware
Without Entware, b4 goes to `/tmp`, which is cleared on every reboot. Entware keeps b4 installed across reboots.
:::

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

## Firewall rewrites {#firewall-rewrites}

KeeneticOS rebuilds its firewall from time to time and removes b4's rules with it. The installer adds a hook that makes b4 restore them as soon as the rebuild ends. The log lines this leaves are explained under [Router platforms](../troubleshooting/platforms.md#keenetic-firewall-rewrites).

## DNS queries {#dns-queries}

On KeeneticOS with a kernel before 4.18 (4.14 before 4.14.173), b4 copies DNS queries instead of holding them. What that changes is described under [Router platforms](../troubleshooting/platforms.md#keenetic-dns-queries).

## Troubleshooting

After the service starts, the error log shows whether anything failed:

```bash
cat /var/log/b4/errors.log
```

The errors and their fixes are listed under [Router platforms](../troubleshooting/platforms.md#keenetic). With no errors, the web interface is at the router's LAN address, on port 7000.
