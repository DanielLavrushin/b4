---
sidebar_position: 5
title: MikroTik
---

# MikroTik (RouterOS 7.x)

b4 запускается как контейнер на MikroTik RouterOS 7.x.

## Требования

- RouterOS версии 7.21.1 и выше
- Архитектура ARM64 или AMD64
- Подключённый внешний накопитель (Flash/SSD/HDD), отформатированный в Ext4

:::warning
Контейнеры на MikroTik требуют внешний накопитель - внутренней памяти роутера недостаточно.
:::

## Образ от сообщества

Кроме официального образа `lavrushin/b4` существует образ, собранный специально под RouterOS: [wiktorbgu/b4-mikrotik](https://hub.docker.com/r/wiktorbgu/b4-mikrotik), его поддерживает wiktorbgu. Образ выходит следом за релизами b4 с теми же номерами версий и упаковывает сервис под контейнеры RouterOS:

- b4 работает внутри контейнера как сервис OpenRC, поэтому его можно перезапустить, не останавливая контейнер
- entrypoint выбирает бэкенд файрвола при старте - nftables либо iptables-legacy с ipset - в зависимости от того, какие модули ядра RouterOS отдаёт контейнеру
- он приводит в порядок приоритеты правил маршрутизации `main` и `default`, которые RouterOS 7.22 передаёт в контейнер в виде, ломающем policy routing
- парный контейнер [wiktorbgu/dnsproxy-mikrotik](https://hub.docker.com/r/wiktorbgu/dnsproxy-mikrotik) пересылает DNS-запросы через DoH, чтобы их не перехватывали

Инструкция по настройке - на странице образа. Образ поддерживает сообщество, проект b4 его не собирает, исходники сборки не опубликованы, поэтому сверить его содержимое с этим репозиторием нельзя. Дальнейшее руководство использует официальный образ.

## Параметры примера

В руководстве используются следующие значения. Замените на свои:

| Параметр | Значение |
| --- | --- |
| Сеть моста | 192.168.210.0/24 |
| Шлюз моста | 192.168.210.1 |
| Имя моста | bridge-docker |
| IP контейнера | 192.168.210.10 |
| Имя интерфейса | B4 |
| Сеть LAN | 192.168.100.0/24 |
| DNS-сервер | 192.168.100.1 |
| Таблица маршрутизации | to_b4 |
| Диск | /usb1 |
| Список клиентов | b4users |

## Шаг 1: Мост

Создайте мост для Docker-сети:

```routeros
/interface/bridge add name=bridge-docker port-cost-mode=short
/ip/address add address=192.168.210.1/24 interface=bridge-docker network=192.168.210.0
```

## Шаг 2: Интерфейс

Создайте виртуальный Ethernet-интерфейс и подключите к мосту:

```routeros
/interface/veth add address=192.168.210.10/24 gateway=192.168.210.1 name=B4
/interface/bridge/port add bridge=bridge-docker interface=B4
```

## Шаг 3: Маршрутизация

Создайте таблицу маршрутизации и маршрут через контейнер:

```routeros
/routing table add disabled=no fib name=to_b4
/ip route add check-gateway=ping gateway=192.168.210.10 routing-table=to_b4
```

## Шаг 4: Маркировка трафика

Перенаправьте трафик клиентов из списка `b4users` через контейнер:

```routeros
/ip firewall mangle add chain=prerouting action=mark-connection \
    new-connection-mark=b4_connections passthrough=yes connection-state=new \
    dst-address-type=!local src-address-list=b4users in-interface-list=LAN \
    place-before=0

/ip firewall mangle add chain=prerouting action=mark-routing \
    new-routing-mark=to_b4 passthrough=no connection-mark=b4_connections \
    in-interface-list=LAN log=no place-before=1
```

:::caution FastTrack
FastTrack обходит правила mangle. Ограничьте его немаркированными соединениями:

```routeros
/ip firewall filter set [find action=fasttrack-connection] connection-mark=no-mark
```
:::

## Шаг 5: Точки монтирования

```routeros
/container/mounts add name=b4_etc src=/usb1/docker/b4-mounts/etc dst=/opt/etc/b4
```

Убедитесь, что директория `/usb1/docker/b4-mounts/etc` существует на диске.

## Шаг 6: Запуск контейнера

Настройте реестр:

```routeros
/container/config set registry-url=https://registry-1.docker.io tmpdir=/usb1/docker/pull
```

Создайте и запустите контейнер:

```routeros
/container add remote-image=lavrushin/b4:latest interface=B4 \
    root-dir=/usb1/docker/b4-mikrotik mounts=b4_etc \
    cmd="--config /opt/etc/b4/b4.json" start-on-boot=yes \
    logging=yes dns=192.168.100.1
```

После загрузки образа:

```routeros
/container start [find tag~"b4"]
```

:::info Перехват DNS
Если провайдер перехватывает DNS (редирект 53 порта), публичные резолверы внутри контейнера не помогут. Настройте DoH на MikroTik и укажите в контейнере шлюз моста вместо публичного DNS:

```routeros
/ip dns set use-doh-server=https://cloudflare-dns.com/dns-query verify-doh-cert=yes
```

Затем измените DNS контейнера на `dns=192.168.210.1` (шлюз моста).
:::

## Шаг 7: Добавление клиентов

Добавьте устройства в адресный список `b4users`:

```routeros
/ip firewall address-list add list=b4users address=192.168.100.50
/ip firewall address-list add list=b4users address=192.168.100.51
```

## Веб-интерфейс

После запуска контейнера: `http://192.168.210.10:7000`

:::tip Снижение износа диска
USB-флешки и SD-карты имеют ограниченное число циклов записи. Перенесите логи b4 в RAM через веб-интерфейс:

**Settings -> Logging Configuration -> Log file path:** `/tmp/log/b4/errors.log`

Логи теряются при перезагрузке, но накопитель прослужит дольше.
:::

## Обновление

```routeros
/container stop [find tag~"b4"]
/container remove [find tag~"b4"]
/container add remote-image=lavrushin/b4:latest interface=B4 \
    root-dir=/usb1/docker/b4-mikrotik mounts=b4_etc \
    cmd="--config /opt/etc/b4/b4.json" start-on-boot=yes \
    logging=yes dns=192.168.100.1
```

Конфигурация хранится на точке монтирования и сохраняется при пересоздании контейнера.

## Решение проблем

**Контейнер не запускается:**
1. Проверьте статус: `/container print`
2. Смотрите логи: `/log print where topics~"container"`
3. Убедитесь, что диск отформатирован в Ext4

**Нет доступа к веб-интерфейсу:**
1. Проверьте, что контейнер запущен: `/container print`
2. Проверьте связность: `/ping 192.168.210.10`

**Трафик не перенаправляется:**
1. Проверьте список: `/ip firewall address-list print where list=b4users`
2. Проверьте mangle: `/ip firewall mangle print`
3. Проверьте маршрут: `/ip route print where routing-table=to_b4`
