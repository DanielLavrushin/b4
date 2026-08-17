---
sidebar_position: 3
title: ASUS Merlin
---

## Requirements

- ASUS router running Asuswrt-Merlin firmware
- Entware installed (required)
- USB drive (for Entware and b4)

## Install Entware

Entware is required. If it is not installed yet:

1. A USB drive is attached to the router
2. SSH session to the router: `ssh admin@192.168.1.1`
3. `amtm` is started
4. Item `ep` in `amtm` installs Entware

More details: https://diversion.ch/amtm.html

## Install b4

After Entware is installed:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

## Service control

```bash
/opt/etc/init.d/S99b4 start
/opt/etc/init.d/S99b4 stop
/opt/etc/init.d/S99b4 restart
```

The service starts automatically on router boot through Entware.

## Paths

| What | Where |
| --- | --- |
| Binary | `/opt/sbin/b4` |
| Configuration | `/opt/etc/b4/b4.json` |
| Service | `/opt/etc/init.d/S99b4` |

## Without Entware

If Entware is not installed, b4 is placed in `/jffs/b4`. In that case autostart on boot is not configured - the binary has to be started manually.

The size of `/jffs` is usually limited to about 60 MB, so a USB drive with Entware is recommended.
