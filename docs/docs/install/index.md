---
sidebar_position: 2
title: Installation
---

b4 installs on Linux devices: servers, computers, and routers. The available methods:

- [Linux](./linux) - universal installation on any Linux distribution
- [OpenWRT](./openwrt) - routers running OpenWRT firmware
- [ASUS Merlin](./merlin) - ASUS routers running Merlin firmware
- [Keenetic](./keenetic) - Keenetic routers
- [MikroTik](./mikrotik) - RouterOS 7.x via containers
- [Docker](./docker) - run inside a Docker container

After installation, b4 is available through the web interface in the browser (default port `7000`).

## Update and remove {#update-remove}

### Update

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --update
```

Or update to a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- v1.46.5
```

During an update, the current binary is saved as a backup, the service is stopped, replaced with the new version, and started again. The configuration is not touched.

### Remove

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --remove
```

On removal:

1. The service is stopped and removed from autostart
2. The binary is deleted
3. The configuration is kept or removed depending on the answer to the installer's prompt about deleting `/etc/b4` or `/opt/etc/b4`

### Diagnostics

To print system information, the installed version, and the state of kernel modules:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --sysinfo
```
