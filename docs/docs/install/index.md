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

1. The service is stopped; if the process cannot be stopped, nothing is removed. The firewall and routing state b4 installed is cleared with b4's own cleanup before the binary is deleted
2. The binary is deleted
3. The configuration is kept or removed depending on the answer to the installer's prompt about deleting `/etc/b4` or `/opt/etc/b4`

### Unverified TLS

When neither `curl` nor `wget` can verify GitHub's certificate and installing CA certificates through the package manager does not help, the installer asks before downloading over unverified TLS. In `--quiet` mode it stops instead; `B4_ALLOW_INSECURE_TLS=1` in the environment accepts the risk for that run. A checksum fetched over the same unverified connection proves only that the download was not corrupted.

### Diagnostics

To print system information, the installed version, and the state of kernel modules:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --sysinfo
```
