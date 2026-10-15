---
sidebar_position: 1
title: Linux
description: Установка b4 на любой дистрибутив Linux.
---

Установщик работает на любом дистрибутиве: Ubuntu, Debian, Fedora, Alpine, Arch и других.

## Установка

От root:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

Или через `wget`:

```bash
wget -qO- https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

Установщик кладёт бинарник в `/usr/local/bin`, а конфигурацию в `/etc/b4`.

Без вопросов, с настройками по умолчанию:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --quiet
```

Без терминала, например из cron или по `ssh` без `-t`, установщик работает так, как если бы был указан `--quiet`.

## Управление сервисом

### systemd (Ubuntu, Debian, Fedora и большинство дистрибутивов)

```bash
systemctl start b4
systemctl stop b4
systemctl restart b4
systemctl status b4
systemctl enable b4      # запуск при загрузке
```

Логи:

```bash
journalctl -u b4 -f
```

### OpenRC (Alpine)

```bash
rc-service b4 start
rc-service b4 stop
rc-service b4 restart
rc-update add b4 default   # запуск при загрузке
```

### SysV init (системы без systemd и OpenRC)

```bash
/etc/init.d/b4 start
/etc/init.d/b4 stop
/etc/init.d/b4 restart
/etc/init.d/b4 status
```

## Пути

| Что | Где |
| --- | --- |
| Бинарник | `/usr/local/bin/b4` |
| Конфигурация | `/etc/b4/b4.json` |
| Сервис (systemd) | `/etc/systemd/system/b4.service` |
| Сервис (OpenRC, SysV) | `/etc/init.d/b4` |

## Модули ядра

Движку NFQUEUE нужны модули ядра, которые обычно загружаются при запуске сервиса. Если этого не произошло, их можно загрузить вручную:

```bash
modprobe nfnetlink_queue
modprobe xt_NFQUEUE
modprobe nf_conntrack
```

Проверка:

```bash
lsmod | grep nfqueue
```

На ядре без этих модулей можно использовать движок TUN, см. [Движок пакетов](../settings/core.md#packet-engine).

## LXC-контейнеры

В LXC-контейнере модули ядра загружаются на хосте. В конфигурацию контейнера добавляется:

```yaml
lxc.cgroup2.devices.allow: c 10:200 rwm
features: nesting=1,keyctl=1
```
