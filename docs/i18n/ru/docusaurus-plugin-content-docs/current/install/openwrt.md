---
sidebar_position: 2
title: OpenWrt
description: Установка b4 на роутер с OpenWrt.
---

## Требования

- OpenWrt 19.07 или новее.
- Свободное место для бинарника. Внутренняя память (overlay) часто мала, и установщик предупреждает, если свободно меньше 2 МБ. Рекомендуется USB-накопитель или [extroot](https://openwrt.org/docs/guide-user/additional-software/extroot_configuration).

## Установка

По SSH на роутере:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

Если `curl` не установлен:

```bash
opkg update && opkg install curl ca-certificates
```

Или через `wget`:

```bash
wget -qO- https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

Встроенный в OpenWrt `wget` из BusyBox не поддерживает HTTPS. Полная версия устанавливается так:

```bash
opkg update && opkg install wget-ssl ca-certificates
```

## Модули ядра {#kernel-modules}

Установщик пытается сам загрузить нужные b4 модули. Если он предупреждает `The kernel cannot queue packets to b4`, то печатает и пакеты, которые нужно установить. Обычно это пакеты из списков ниже.

### OpenWrt 24.x и новее (apk)

```bash
apk add kmod-nft-queue kmod-nft-nat kmod-nft-compat kmod-nft-conntrack
```

### OpenWrt 23.x и ниже (opkg)

```bash
opkg update
opkg install kmod-nft-queue kmod-nft-conntrack nftables-json coreutils-nohup
```

Для версий без nftables:

```bash
opkg install kmod-nfnetlink-queue kmod-ipt-nfqueue iptables-mod-nfqueue iptables-mod-conntrack-extra
```

### Загрузка модулей

Установленные модули иногда приходится загрузить вручную. Если команда ничего не вывела, модуль загружен:

```bash
modprobe nft_queue
modprobe nft_ct
modprobe xt_connbytes
```

## Управление сервисом

```bash
/etc/init.d/b4 enable     # запуск при загрузке
/etc/init.d/b4 start
/etc/init.d/b4 stop
/etc/init.d/b4 restart
```

Сервис продолжает работать после закрытия SSH-сессии, `screen` или `nohup` не нужны.

## Пути

| Что | С `/opt` (extroot или USB) | Без внешнего накопителя |
| --- | --- | --- |
| Бинарник | `/opt/bin/b4` | `/usr/bin/b4` |
| Конфигурация | `/opt/etc/b4/b4.json` | `/etc/b4/b4.json` |

## Веб-интерфейс

После запуска веб-интерфейс доступен по адресу `http://<адрес роутера>:7000`, например `http://192.168.1.1:7000`.

## LuCI-приложение

Сторонний пакет [luci-app-b4](https://github.com/BugOldfag/luci-app-b4) добавляет b4 в интерфейс LuCI. Он в стадии alpha и покрывает часть функций. Веб-интерфейс b4 на порту 7000 остаётся доступен.

## Решение проблем

Сервис, который не запускается, ошибка `Could not process rule`, низкая скорость и flow offloading разобраны на странице [Особенности платформ](../troubleshooting/platforms.md#openwrt).
