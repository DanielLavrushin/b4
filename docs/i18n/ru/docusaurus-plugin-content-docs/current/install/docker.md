---
sidebar_position: 6
title: Docker
---

# Docker

Образ: [lavrushin/b4](https://hub.docker.com/r/lavrushin/b4) на Docker Hub.

## docker-compose

Файл `docker-compose.yml`:

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

Запуск:

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

## Параметры

| Параметр | Зачем |
| --- | --- |
| `network_mode: host` | b4 работает с сетевым стеком хоста напрямую |
| `NET_ADMIN` | управление netfilter и правилами firewall |
| `NET_RAW` | работа с raw-сокетами |
| `SYS_MODULE` | загрузка модулей ядра (modprobe) |
| `-v ./config:/etc/b4` | конфигурация сохраняется на хосте |

## Управление

```bash
docker compose logs -f b4     # логи
docker compose restart b4     # перезапуск
docker compose down            # остановка
docker compose pull && docker compose up -d   # обновление
```

## Веб-интерфейс

После запуска: `http://localhost:7000`

Порт настраивается в `config/b4.json` (параметр `web_server.port`).
