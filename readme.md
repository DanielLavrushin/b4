# B4

![GitHub Release](https://img.shields.io/github/v/release/daniellavrushin/b4)
![GitHub Downloads](https://img.shields.io/github/downloads/daniellavrushin/b4/total)

[[Documentation](https://daniellavrushin.github.io/b4/)] [[Документация (RU)](https://daniellavrushin.github.io/b4/ru/)] [[Telegram](https://t.me/byebyebigbro)]

Network packet processor that bypasses Deep Packet Inspection (DPI) using netfilter queue manipulation.

<img width="1187" height="787" alt="image" src="https://github.com/user-attachments/assets/3e4c105d-5b28-4e93-ab54-6d92338b1293" />

## Requirements

- Linux-system (desktop, server or router)
- Root-access (sudo)

That's it. The installer will take care of the rest

## Installation

> [!NOTE]
> In some systems you need to run `sudo b4install.sh`.

With wget:

```bash
wget -qO- https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

With curl:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

If something went wrong try to run it with the flag `--sysinfo` - this will diagnose the system

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --sysinfo
```

Or pass `--help` to get more information about the possible options.

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --help
```

To update B4:

```bash
#to latest version
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --update

#to specific version
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --update v1.43.0

#update silently
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --update --quiet

```

To remove B4:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --remove
```

### Installer options

```bash
# Install latest b4 version
./install.sh

# Show help message
./install.sh -h

# Show system diagnostics and b4 status
./install.sh --sysinfo

# Install specific version
./install.sh v1.10.0

# Quiet mode (suppress output except for errors)
./install.sh --quiet

# Specify geosite.dat source URL and destination
./install.sh --geosite-src=--geosite-src=https://example.com/geosite.dat --geosite-dst=/opt/etc/b4

# Update b4 to latest version
./install.sh --update

# Uninstall b4
./install.sh --remove
```

### Building from Source

```bash
git clone https://github.com/daniellavrushin/b4.git
cd b4

# Build UI
make build-ui

# Build binary
make build

# All architectures
make build-all

# Or build specific
make linux-amd64
make linux-arm64
make linux-armv7
```

## Docker

### Quick Start

```bash
docker run --network host \
  --cap-add NET_ADMIN --cap-add NET_RAW --cap-add SYS_MODULE \
  -v /etc/b4:/etc/b4 \
  lavrushin/b4:latest --config /opt/etc/b4/b4.json
```

Web UI: <http://localhost:7000>

### Docker Compose

```yaml
services:
  b4:
    image: lavrushin/b4:latest
    container_name: b4
    network_mode: host
    cap_add:
      - NET_ADMIN
      - NET_RAW
      - SYS_MODULE
    volumes:
      - ./config:/etc/b4
    command: ["--config", "/etc/b4/b4.json"]
    restart: unless-stopped
```

### Docker Requirements

- **Linux host only** — b4 uses netfilter queue (NFQUEUE) which is a Linux kernel feature
- `--network host` is mandatory — b4 must access the host network stack directly
- Capabilities: `NET_ADMIN` (firewall rules), `NET_RAW` (raw sockets), `SYS_MODULE` (kernel module loading)
- Host kernel must have `nfqueue` support (`xt_NFQUEUE`, `nf_conntrack` modules)

## Usage

### Starting B4

```bash

# Standard Linux (systemd)
sudo systemctl start b4
sudo systemctl enable b4 # Start on load

# OpenWRT
/etc/init.d/b4 restart # start | stop

# Entware/MerlinWRT
/opt/etc/init.d/S99b4 restart # start | stop
```

### Web UI

```text
http://your-device-ip:7000
```

### Command Line

```bash

# Print help
b4 --help

# Basic - manual domains
b4 --sni-domains youtube.com,netflix.com

# With geosite categories
b4 --geosite /etc/b4/geosite.dat --geosite-categories youtube,netflix

# Custom config
b4 --config /path/to/config.json
```

## Web interface

The web interface is available at `http://your-ip:7000` (default port, can be changed in `config` file).

**Features:**

- Realtime metrics (connections, packets, bandwidth)
- Logs streaming with filtering and keybinds (p to pause streaming, del to clear logs)
- Domain/ip configuration on the go (Add domain or ip to a set by clicking it in the Domains tab)
- Quick domain tests and domain-specific bypass strat discovery
- ipinfo.io api integration for ASN scanning
- Custom payload capturing for faking

## HTTPS / TLS Support

You can enable HTTPS for the web interface in the Web UI under **Settings > Network Configuration > Web Server** (TLS Certificate / TLS Key fields), or by setting `tls_cert` and `tls_key` in the config JSON:

```json
{
  "system": {
    "web_server": {
      "tls_cert": "/path/to/server.crt",
      "tls_key": "/path/to/server.key"
    }
  }
}
```

The installer automatically detects router certificates on **OpenWrt** (uhttpd) and **Asus Merlin** and enables HTTPS in the config if they are found.

## SOCKS5 Proxy

B4 includes a built-in SOCKS5 proxy server. Applications that support SOCKS5 (browsers, curl, torrent clients, etc.) can route traffic through B4 without any system-wide configuration.

Enable it in the Web UI under **Settings > Network Configuration > SOCKS5 Server**, or in the config JSON:

```json
{
  "system": {
    "socks5": {
      "enabled": true,
      "port": 1080,
      "bind_address": "0.0.0.0",
      "username": "",
      "password": ""
    }
  }
}
```

Leave `username` and `password` empty for no authentication.

**Examples:**

```bash
# curl
curl --socks5 127.0.0.1:1080 https://example.com

# Firefox: Preferences > Network Settings > Manual proxy
# SOCKS Host: 127.0.0.1, Port: 1080, SOCKS v5

# Git
git config --global http.proxy socks5://127.0.0.1:1080
```

> [!NOTE]
> Restart B4 after changing SOCKS5 settings.

## API Documentation (Swagger)

B4 provides interactive REST API documentation via Swagger UI. To enable, add to your `b4.json`:

```json
{
  "system": {
    "web_server": {
      "swagger": true
    }
  }
}
```

After restarting B4, open:

```text
http://your-device-ip:7000/swagger/
```

> [!TIP]
> If web authentication is enabled, call `POST /api/auth/login` to get a token first, then click **Authorize** in Swagger UI and enter `Bearer {your_token}`.

## Geosite Integration

B4 supports [v2ray/xray `geosite.dat`](https://github.com/v2fly/domain-list-community) files from various sources:

```bash
# Loyalsoldier
wget https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat

# RUNET Freedom
wget https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/geosite.dat
```

Place the file in `/etc/b4/geosite.dat` and configure categories:

```bash
sudo b4 --geosite /etc/b4/geosite.dat --geosite-categories youtube,netflix,facebook
```

> [!TIP]
> All these settings can be configured via the web interface.

## Contributing

Contributions are accepted through GitHub pull requests.

## Credits

Based on research from:

- [youtubeUnblock](https://github.com/Waujito/youtubeUnblock) - C-based DPI bypass
- [GoodbyeDPI](https://github.com/ValdikSS/GoodbyeDPI) - Windows DPI circumvention
- [zapret](https://github.com/bol-van/zapret) - Advanced DPI bypass techniques
- [dpi-detector](https://github.com/Runnin4ik/dpi-detector) - DPI/TSPU detection techniques and test target lists (the DPI Detector feature is based on this project)
- [tg-ws-proxy](https://github.com/Flowseal/tg-ws-proxy) - Telegram MTProto-over-WebSocket bridging

## License

This project is provided for educational purposes. Users are responsible for compliance with applicable laws and regulations.
The authors are not responsible for misuse of this software.
