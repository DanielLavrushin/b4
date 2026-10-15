---
sidebar_position: 6
title: Docker
description: Запуск b4 в контейнере Docker.
---

Образ - [lavrushin/b4](https://hub.docker.com/r/lavrushin/b4) на Docker Hub.

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
| `NET_ADMIN` | Управление netfilter и правилами файрвола |
| `NET_RAW` | Raw-сокеты |
| `SYS_MODULE` | Загрузка модулей ядра |
| `-v ./config:/etc/b4` | Конфигурация хранится на хосте |

## Управление {#management}

```bash
docker compose logs -f b4     # логи
docker compose restart b4     # перезапуск
docker compose down           # остановка
docker compose pull && docker compose up -d   # обновление
```

Окно обновления в веб-интерфейсе контейнер не обновляет. Контейнер, запущенный через `docker run`, обновляется так: скачивается новый образ, и контейнер создаётся заново.

## Веб-интерфейс

После запуска веб-интерфейс доступен по адресу `http://localhost:7000` на хосте или по адресу хоста с другой машины. Порт задаётся в разделе **Настройки > Система > Веб-сервер**, поле **Порт**, или в `config/b4.json`, см. [Конфигурационный файл](../advanced/config.md#paths-system).
