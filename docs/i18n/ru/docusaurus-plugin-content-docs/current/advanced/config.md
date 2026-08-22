---
sidebar_position: 2
title: Конфигурационный файл
---

# Конфигурационный файл

b4 хранит конфигурацию в JSON-файле. По умолчанию: `/etc/b4/b4.json`. Путь можно изменить через флаг `--config`.

## Расположение по платформам

| Платформа | Путь |
| --- | --- |
| Linux | `/etc/b4/b4.json` |
| OpenWRT (с extroot/USB) | `/opt/etc/b4/b4.json` |
| OpenWRT (без USB) | `/etc/b4/b4.json` |
| ASUS Merlin | `/opt/etc/b4/b4.json` |
| Keenetic | `/opt/etc/b4/b4.json` |
| Docker | `/etc/b4/b4.json` (внутри контейнера) |

## Структура

Файл создаётся автоматически при первом запуске с значениями по умолчанию. Основные секции:

```json
{
  "queue": {
    "start_num": 537,
    "threads": 4,
    "mark": 32768,
    "ipv4": true,
    "ipv6": false,
    "tcp_conn_bytes_limit": 19,
    "udp_conn_bytes_limit": 8,
    "interfaces": [],
    "mss_clamp": {
      "enabled": false,
      "size": 88
    },
    "devices": {
      "enabled": false,
      "vendor_lookup": false
    }
  },
  "system": {
    "tables": {
      "skip_setup": false,
      "monitor_interval": 10,
      "engine": "",
      "masquerade": false,
      "masquerade_interface": ""
    },
    "logging": {
      "level": 1,
      "error_file": "/var/log/b4/errors.log",
      "instaflush": true,
      "syslog": false
    },
    "web_server": {
      "port": 7000,
      "bind_address": "0.0.0.0",
      "tls_cert": "",
      "tls_key": "",
      "username": "",
      "password": "",
      "language": "en",
      "swagger": false
    },
    "socks5": {
      "enabled": false,
      "port": 1080,
      "bind_address": "0.0.0.0"
    },
    "mtproto": {
      "enabled": false,
      "port": 3128,
      "bind_address": "0.0.0.0",
      "fake_sni": "storage.googleapis.com"
    },
    "checker": {
      "discovery_timeout": 5,
      "config_propagate_ms": 1500,
      "reference_domain": "yandex.ru",
      "reference_dns": ["9.9.9.9", "1.1.1.1", "8.8.8.8"],
      "validation_tries": 1
    },
    "geo": {
      "sitedat_path": "",
      "ipdat_path": "",
      "sitedat_url": "",
      "ipdat_url": ""
    },
    "timezone": ""
  },
  "sets": []
}
```

:::warning Редактирование вручную
Конфигурацию можно редактировать вручную, но рекомендуется использовать веб-интерфейс - он валидирует значения и применяет миграции при обновлении версии. При ручном редактировании перезапустите b4 для применения изменений.
:::

## Секция sets

Каждый сет - объект в массиве `sets` с полной конфигурацией TCP/UDP/DNS/маршрутизации. Структура сета соответствует вкладкам в веб-интерфейсе:

- `targets` - домены, IP, GeoSite/GeoIP категории, устройства
- `tcp` - общие настройки TCP
- `fragmentation` - метод фрагментации и параметры
- `faking` - SNI faking, SYN fake, desync, window, incoming, mutation
- `udp` - QUIC, STUN, fake-режим
- `dns` - DNS-редирект
- `routing` - маршрутизация через интерфейсы

:::tip Импорт/Экспорт
Для переноса конфигурации сетов между устройствами используйте вкладку **Импорт/Экспорт** в редакторе сета - она показывает JSON-представление и позволяет копировать/вставлять.
:::

## Миграции

При обновлении b4 структура конфигурации может измениться. b4 автоматически мигрирует старые конфигурации при запуске - добавляет новые поля с значениями по умолчанию, переименовывает устаревшие. Ручное вмешательство не требуется.
