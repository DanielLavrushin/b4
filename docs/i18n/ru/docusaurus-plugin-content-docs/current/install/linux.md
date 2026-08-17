---
sidebar_position: 1
title: Linux
---

# Linux (универсальная установка)

Подходит для любого дистрибутива: Ubuntu, Debian, Fedora, Alpine, Arch и других.

## Установка

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

Или через `wget`:

```bash
wget -qO- https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

Установщик автоматически определяет архитектуру, устанавливает бинарник в `/usr/local/bin` и создаёт конфигурацию в `/etc/b4`.

Для установки без интерактивных вопросов (с настройками по умолчанию):

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --quiet
```

## Управление сервисом

### systemd (Ubuntu, Debian, Fedora и большинство дистрибутивов)

```bash
systemctl start b4
systemctl stop b4
systemctl restart b4
systemctl status b4
systemctl enable b4      # автозапуск при загрузке
```

Просмотр логов:

```bash
journalctl -u b4 -f
```

### OpenRC (Alpine)

```bash
rc-service b4 start
rc-service b4 stop
rc-service b4 restart
rc-update add b4 default   # автозапуск при загрузке
```

## Пути

| Что | Где |
| --- | --- |
| Бинарник | `/usr/local/bin/b4` |
| Конфигурация | `/etc/b4/b4.json` |
| Сервис (systemd) | `/etc/systemd/system/b4.service` |
| Сервис (OpenRC/SysV) | `/etc/init.d/b4` |

## Модули ядра

b4 использует NFQUEUE для перехвата пакетов. Нужные модули ядра обычно загружаются автоматически при запуске сервиса. При проблемах их можно загрузить вручную:

```bash
modprobe nfnetlink_queue
modprobe xt_NFQUEUE
modprobe nf_conntrack
```

Для проверки:

```bash
lsmod | grep nfqueue
```

:::info LXC-контейнеры
В LXC-контейнерах модули ядра должны быть загружены на хосте. В конфигурацию контейнера добавляется:
```
lxc.cgroup2.devices.allow: c 10:200 rwm
features: nesting=1,keyctl=1
```
:::
