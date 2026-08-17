---
sidebar_position: 6
title: Docker
---

Image: [lavrushin/b4](https://hub.docker.com/r/lavrushin/b4) on Docker Hub.

## docker-compose

A `docker-compose.yml` file:

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
    restart: unless-stopped
```

Run:

```bash
mkdir -p config
docker compose up -d
```

## docker run

```bash
mkdir -p config
docker run -d \
  --name b4 \
  --network host \
  --cap-add NET_ADMIN \
  --cap-add NET_RAW \
  --cap-add SYS_MODULE \
  -v ./config:/etc/b4 \
  --restart unless-stopped \
  lavrushin/b4:latest
```

## Parameters

| Parameter | Purpose |
| --- | --- |
| `network_mode: host` | b4 works with the host network stack directly |
| `NET_ADMIN` | manage netfilter and firewall rules |
| `NET_RAW` | use raw sockets |
| `SYS_MODULE` | load kernel modules (modprobe) |
| `-v ./config:/etc/b4` | configuration is stored on the host |

## Management

```bash
docker compose logs -f b4     # logs
docker compose restart b4     # restart
docker compose down            # stop
docker compose pull && docker compose up -d   # update
```

## Web interface

After startup: `http://localhost:7000`

The port is configured in `config/b4.json` (`web_server.port`).
