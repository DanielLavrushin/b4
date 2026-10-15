---
sidebar_position: 3
title: ASUS Merlin
description: Установка b4 на роутер ASUS с Asuswrt-Merlin.
---

## Требования

- Роутер ASUS с прошивкой Asuswrt-Merlin.
- Entware на USB-накопителе.

## Установка Entware

1. Подключите USB-накопитель к роутеру.
2. Подключитесь к роутеру по SSH: `ssh admin@192.168.1.1`.
3. Запустите `amtm`.
4. Выберите пункт `ep`, чтобы установить Entware.

Подробности - на [странице amtm](https://diversion.ch/amtm.html).

## Установка b4

После установки Entware:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

## Управление сервисом

```bash
/opt/etc/init.d/S99b4 start
/opt/etc/init.d/S99b4 stop
/opt/etc/init.d/S99b4 restart
```

Entware запускает сервис при загрузке роутера.

## Пути

| Что | Где |
| --- | --- |
| Бинарник | `/opt/sbin/b4` |
| Конфигурация | `/opt/etc/b4/b4.json` |
| Сервис | `/opt/etc/init.d/S99b4` |

## Без Entware

Без Entware b4 устанавливается в `/jffs/b4` и не запускается при загрузке: бинарник приходится запускать вручную. Размер `/jffs` обычно ограничен примерно 60 МБ, поэтому рекомендуется USB-накопитель с Entware.
