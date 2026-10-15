---
sidebar_position: 3
title: ASUS Merlin
description: Installing b4 on an ASUS router with Asuswrt-Merlin.
---

## Requirements

- An ASUS router with Asuswrt-Merlin firmware.
- Entware on a USB drive.

## Install Entware

1. Attach a USB drive to the router.
2. Connect to the router over SSH: `ssh admin@192.168.1.1`.
3. Run `amtm`.
4. Choose `ep` to install Entware.

The [amtm page](https://diversion.ch/amtm.html) has the details.

## Install b4

With Entware in place:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

## Service control

```bash
/opt/etc/init.d/S99b4 start
/opt/etc/init.d/S99b4 stop
/opt/etc/init.d/S99b4 restart
```

Entware starts the service when the router boots.

## Paths

| What | Where |
| --- | --- |
| Binary | `/opt/sbin/b4` |
| Configuration | `/opt/etc/b4/b4.json` |
| Service | `/opt/etc/init.d/S99b4` |

## Without Entware

Without Entware, b4 goes to `/jffs/b4` and does not start on boot: the binary has to be started by hand. `/jffs` is usually limited to about 60 MB, so a USB drive with Entware is the usual choice.
