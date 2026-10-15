---
sidebar_position: 6
title: Docker
description: Running b4 in a Docker container.
---

The image is [lavrushin/b4](https://hub.docker.com/r/lavrushin/b4) on Docker Hub.

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

Start:

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
| `network_mode: host` | b4 works with the host's network stack directly |
| `NET_ADMIN` | Managing netfilter and the firewall rules |
| `NET_RAW` | Raw sockets |
| `SYS_MODULE` | Loading kernel modules |
| `-v ./config:/etc/b4` | Keeps the configuration on the host |

## Management

```bash
docker compose logs -f b4     # logs
docker compose restart b4     # restart
docker compose down           # stop
docker compose pull && docker compose up -d   # update
```

The update dialog of the web interface does not update a container. A container started with `docker run` is updated by pulling the new image and creating the container again.

## Web interface

After the start, the web interface is at `http://localhost:7000` on the host, or at the host's address from another machine. The port is set under **Settings > System > Web Server** in **Port**, or in `config/b4.json`, see [Configuration file](../advanced/config.md#paths-system).
