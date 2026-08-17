---
sidebar_position: 3
title: ASUS Merlin
---

# ASUS Merlin

## Требования

- Роутер ASUS с прошивкой Asuswrt-Merlin
- Установленный Entware (обязательно)
- USB-накопитель (для Entware и b4)

## Установка Entware

Entware - обязательное условие. Если он ещё не установлен:

1. USB-накопитель подключается к роутеру
2. SSH-подключение к роутеру: `ssh admin@192.168.1.1`
3. Запускается `amtm`
4. Пункт `ep` в `amtm` устанавливает Entware

Подробнее: https://diversion.ch/amtm.html

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

Сервис запускается автоматически при загрузке роутера через Entware.

## Пути

| Что | Где |
| --- | --- |
| Бинарник | `/opt/sbin/b4` |
| Конфигурация | `/opt/etc/b4/b4.json` |
| Сервис | `/opt/etc/init.d/S99b4` |

## Без Entware

Если Entware не установлен, b4 устанавливается в `/jffs/b4`. В этом случае автозапуск при загрузке не настраивается - бинарник нужно запускать вручную.

Размер `/jffs` обычно ограничен ~60 МБ, поэтому рекомендуется USB-накопитель с Entware.
