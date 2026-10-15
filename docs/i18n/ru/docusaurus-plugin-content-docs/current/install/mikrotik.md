---
sidebar_position: 5
title: MikroTik
description: Запуск b4 в контейнере на MikroTik RouterOS 7.
---

На MikroTik RouterOS 7 b4 работает как контейнер. RouterOS направляет через него трафик выбранных клиентов.

## Требования

- RouterOS 7.21.1 или новее, на ARM64 или AMD64.
- Внешний накопитель (флешка, SSD или HDD), отформатированный в Ext4. Внутренней памяти роутера для контейнеров недостаточно.

## Образ от сообщества

Кроме официального образа `lavrushin/b4` есть образ от сообщества [wiktorbgu/b4-mikrotik](https://hub.docker.com/r/wiktorbgu/b4-mikrotik), собранный под RouterOS. Он выходит следом за релизами b4 с теми же номерами версий. В отличие от официального образа:

- b4 работает как сервис OpenRC, поэтому перезапускается без остановки контейнера;
- бэкенд файрвола, nftables или iptables-legacy с ipset, выбирается по модулям ядра, которые RouterOS отдаёт контейнеру;
- исправлены приоритеты правил маршрутизации `main` и `default`, которые RouterOS 7.22 передаёт в контейнер в виде, ломающем policy routing;
- парный контейнер [wiktorbgu/dnsproxy-mikrotik](https://hub.docker.com/r/wiktorbgu/dnsproxy-mikrotik) пересылает DNS через DoH.

Настройка описана на странице образа. Его поддерживает wiktorbgu, а не проект b4, исходники сборки не опубликованы, и сверить содержимое образа с этим репозиторием нельзя. Шаги ниже используют официальный образ.

## Значения в примере

В шагах используются эти значения. Их заменяют на значения своей сети:

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

## Шаг 1: Мост {#step-1-bridge}

Мост для сети контейнеров:

```routeros
/interface/bridge add name=bridge-docker port-cost-mode=short
/ip/address add address=192.168.210.1/24 interface=bridge-docker network=192.168.210.0
```

## Шаг 2: Интерфейс {#step-2-interface}

Виртуальный Ethernet-интерфейс на мосту:

```routeros
/interface/veth add address=192.168.210.10/24 gateway=192.168.210.1 name=B4
/interface/bridge/port add bridge=bridge-docker interface=B4
```

## Шаг 3: Маршрутизация {#step-3-routing}

Таблица маршрутизации с маршрутом через контейнер:

```routeros
/routing table add disabled=no fib name=to_b4
/ip route add check-gateway=ping gateway=192.168.210.10 routing-table=to_b4
```

## Шаг 4: Маркировка трафика {#step-4-traffic-marking}

Трафик клиентов из списка `b4users` идёт через контейнер:

```routeros
/ip firewall mangle add chain=prerouting action=mark-connection \
    new-connection-mark=b4_connections passthrough=yes connection-state=new \
    dst-address-type=!local src-address-list=b4users in-interface-list=LAN \
    place-before=0

/ip firewall mangle add chain=prerouting action=mark-routing \
    new-routing-mark=to_b4 passthrough=no connection-mark=b4_connections \
    in-interface-list=LAN log=no place-before=1
```

FastTrack обходит правила mangle, поэтому его ограничивают немаркированными соединениями:

```routeros
/ip firewall filter set [find action=fasttrack-connection] connection-mark=no-mark
```

## Шаг 5: Точка монтирования {#step-5-mount-point}

```routeros
/container/mounts add name=b4_etc src=/usb1/docker/b4-mounts/etc dst=/opt/etc/b4
```

Папка `/usb1/docker/b4-mounts/etc` должна существовать на диске.

## Шаг 6: Контейнер {#step-6-container}

Реестр:

```routeros
/container/config set registry-url=https://registry-1.docker.io tmpdir=/usb1/docker/pull
```

Контейнер:

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

Если провайдер перехватывает DNS на порту 53, публичные резолверы внутри контейнера не помогут. Выход - DoH на RouterOS, а в контейнере DNS-сервером указывается шлюз моста, `dns=192.168.210.1`:

```routeros
/ip dns set use-doh-server=https://cloudflare-dns.com/dns-query verify-doh-cert=yes
```

## Шаг 7: Клиенты {#step-7-clients}

Устройства, трафик которых идёт через b4, добавляются в список `b4users`:

```routeros
/ip firewall address-list add list=b4users address=192.168.100.50
/ip firewall address-list add list=b4users address=192.168.100.51
```

## Шаг 8: NAT Masquerade {#step-8-nat-masquerade}

1. Откройте веб-интерфейс по адресу `http://192.168.210.10:7000`.
2. Включите **NAT Masquerade** в разделе [Настройки > Основные > Файрвол](../settings/core.md#nat-masquerade). Интерфейс маскарада остаётся по умолчанию - все интерфейсы.

Без маскарада RouterOS отправляет ответы сервера прямо клиенту, минуя контейнер. Тогда TLS ClientHello соединения до b4 не доходит, и его стратегии не применяются. С маскарадом соединения выходят из контейнера с его адресом, 192.168.210.10, поэтому правила RouterOS после контейнера не различают клиентов.

## Веб-интерфейс

После запуска контейнера веб-интерфейс доступен по адресу `http://192.168.210.10:7000`.

:::tip Меньше износа накопителя
У USB-флешек и SD-карт ограниченное число циклов записи. Если в поле **Папка логов** в карточке **Логирование** раздела **Настройки > Система > Сервис** указать `/tmp/log/b4`, логи хранятся в RAM. При перезагрузке они теряются.
:::

## Доступ из WAN {#access-from-the-wan}

RouterOS фильтрует и транслирует всё, что приходит из WAN, до того как оно попадёт на мост. Порт b4, например `3128` MTProto-прокси, доступен снаружи только через правило `dst-nat` на контейнер:

```routeros
/ip firewall nat add chain=dstnat action=dst-nat in-interface-list=WAN \
    protocol=tcp dst-port=3128 to-addresses=192.168.210.10 to-ports=3128
```

Переключатель **Доступ из интернета** в b4 меняет только файрвол внутри контейнера и это правило не заменяет, см. [Доступ из интернета](../settings/security.md#expose-to-internet).

## Обновление {#update}

```routeros
/container stop [find tag~"b4"]
/container remove [find tag~"b4"]
/container add remote-image=lavrushin/b4:latest interface=B4 \
    root-dir=/usb1/docker/b4-mikrotik mounts=b4_etc \
    cmd="--config /opt/etc/b4/b4.json" start-on-boot=yes \
    logging=yes dns=192.168.100.1
```

Конфигурация хранится на точке монтирования и сохраняется, когда контейнер создаётся заново.

## Отправка сета в другой контейнер

Сет может передавать свой трафик SOCKS5-прокси в другом контейнере на том же мосту, например Xray, sing-box или mihomo с входом SOCKS5. **Режим маршрутизации** сета - **Upstream SOCKS5 proxy**, с адресом другого контейнера, например 192.168.210.20, и его портом SOCKS5. Дополнительные правила на RouterOS не нужны: b4 обращается к прокси внутри `bridge-docker`, а через RouterOS выходят только собственные соединения прокси. См. [Отправка сайтов через прокси SOCKS5](../guides/proxy.md).

## Маршрутизация выбранных адресов через контейнер {#routing-by-destination}

Шаг 4 отбирает трафик по клиенту. Чтобы отбирать его по адресу назначения, например по подсетям сайта, адреса назначения заносятся в список, и правила маркировки сверяются с ним вместо `b4users`:

```routeros
/ip firewall address-list add list=via_b4 address=203.0.113.0/24

/ip firewall mangle add chain=prerouting action=mark-connection \
    new-connection-mark=b4_connections passthrough=yes connection-state=new \
    dst-address-type=!local dst-address-list=via_b4 in-interface-list=LAN \
    place-before=0

/ip firewall mangle add chain=prerouting action=mark-routing \
    new-routing-mark=to_b4 passthrough=no connection-mark=b4_connections \
    in-interface-list=LAN log=no place-before=1
```

Маршрут через контейнер остаётся в таблице `to_b4` из [шага 3](#step-3-routing). Маршрут на контейнер в основной таблице не подходит: контейнер отправляет трафик обратно RouterOS, а маршрут из основной таблицы возвращает его в контейнер вместо WAN.

Правила маркировки действуют только на пакеты из списка интерфейсов LAN, поэтому пакеты, которые контейнер отправляет обратно, уходят по основной таблице. После шага 1 `bridge-docker` в этот список не входит. Если его туда добавили, пакеты контейнера выводит из-под правил маркировки это правило. Оно добавляется после них, чтобы `place-before=0` поставило его первым:

```routeros
/ip firewall mangle add chain=prerouting action=accept \
    in-interface=bridge-docker place-before=0
```

b4 на отдельной машине с Linux требует той же схемы:

- собственная подсеть на своём порту RouterOS, VLAN или мосту, RouterOS - шлюз по умолчанию;
- маршрут в `to_b4` указывает на эту машину;
- в b4 включён NAT Masquerade, как в шаге 8;
- её интерфейс не входит в список интерфейсов LAN или выведен из-под правил тем же правилом accept с его именем вместо `bridge-docker`.

В подсети клиентов машина отдаёт ответы сервера прямо клиентам, и RouterOS задерживает или обрывает их TCP-соединения.

## Маршрутизация трафика сета по значению DSCP {#routing-by-dscp}

Всё, что b4 отправляет обратно в RouterOS, уходит основным маршрутом, к какому бы сету оно ни относилось. Собственное [значение DSCP](../sets/routing.md#dscp) сета помечает его пакеты, и по нему RouterOS отправляет трафик этого сета другим маршрутом, например через VPN. Для маршрутизации в контейнер, описанной выше, ничего подобного не нужно.

Значение включается переключателем **Включить DSCP для сета** на вкладке **DNS & Маршрутизация** сета, здесь это `31`. `wg-vpn` обозначает интерфейс VPN, а `via_vpn` - его таблицу маршрутизации:

```routeros
/routing table add disabled=no fib name=via_vpn
/ip route add gateway=wg-vpn routing-table=via_vpn

/ip firewall mangle add chain=prerouting action=mark-connection \
    new-connection-mark=b4_dscp31 passthrough=yes connection-state=new \
    connection-mark=no-mark in-interface=bridge-docker \
    src-address=192.168.210.10 dscp=31

/ip firewall mangle add chain=prerouting action=mark-routing \
    new-routing-mark=via_vpn passthrough=no connection-mark=b4_dscp31 \
    in-interface=bridge-docker

/ip firewall mangle add chain=prerouting action=mark-connection \
    new-connection-mark=b4_other passthrough=yes connection-state=new \
    connection-mark=no-mark in-interface=bridge-docker src-address=192.168.210.10

/ip firewall mangle add chain=postrouting action=change-dscp new-dscp=0 \
    out-interface-list=WAN

/ip firewall mangle add chain=postrouting action=change-dscp new-dscp=0 \
    out-interface=wg-vpn

/ip firewall nat add chain=srcnat action=masquerade out-interface=wg-vpn
```

| Правило | Что делает |
| --- | --- |
| Первое `mark-connection` | Маркирует соединение от b4, первый пакет которого несёт значение. `connection-mark=no-mark` не даёт соединению с меткой `b4_other` перейти в VPN посередине, после чего оно начиналось бы заново |
| `mark-routing` | Направляет маркированные соединения в `via_vpn`. `in-interface=bridge-docker` не пускает в VPN ответы сервера, у которых та же метка соединения |
| Второе `mark-connection` | Маркирует остальные соединения b4 меткой `b4_other`, и они не попадают в FastTrack. FastTrack пропускает правила `change-dscp`. Цена в том, что трафик b4 через FastTrack не идёт |
| `change-dscp` на `WAN` | Сбрасывает значение перед интернетом |
| `change-dscp` на `wg-vpn` | Сбрасывает значение перед туннелем. Без этого правила значение доходит до сервера VPN внутри туннеля |
| `masquerade` на `wg-vpn` | Даёт соединениям адрес интерфейса VPN |

Ограничение FastTrack из [шага 4](#step-4-traffic-marking) обязательно. Без него маркированные соединения уходят в FastTrack и выходят через основной аплинк с адресом источника VPN.

Если правило accept для `bridge-docker` из раздела [Маршрутизация выбранных адресов](#routing-by-destination) стоит в начале mangle prerouting, три правила prerouting пакетов b4 не видят. Их ставят выше него, добавив к каждому (кавычки обязательны):

```routeros
place-before=[find where action=accept in-interface="bridge-docker"]
```

Варианты:

- **Второй аплинк вместо VPN**: маршрут в `via_vpn` указывает на шлюз аплинка, а последние два правила называют его интерфейс вместо `wg-vpn`.
- **Несколько значений**: каждому значению нужно своё правило `mark-connection`. Значению, которое идёт другим путём, нужны ещё своя метка соединения, правило `mark-routing`, таблица и последние два правила для его интерфейса.
- **b4 на отдельной машине**: её интерфейс и адрес заменяют `bridge-docker` и `192.168.210.10`.
- **Фильтр обратного пути**: `/ip settings rp-filter=strict` отбрасывает ответы, которые возвращаются через VPN. `loose` и значение по умолчанию `no` их пропускают.

В этой схеме устройства разрешают имена через RouterOS, поэтому адреса доменов сета b4 узнаёт только из имён в TLS и QUIC и из собственных запросов. Соединение к адресу, которого b4 ещё не знает, остаётся на основном маршруте. Цели IP, GeoIP и ASN дают соединению значение с первого пакета.

Правила проверены на RouterOS 7.24.5 для IPv4.

## Решение проблем

Контейнер, который не запускается, недоступный веб-интерфейс, трафик, который не доходит до контейнера, и движок, который не запустился, разобраны на странице [Особенности платформ](../troubleshooting/platforms.md#mikrotik).
