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

В руководстве используются следующие значения, их нужно заменить на соответствующие локальной сети:

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

Мост для Docker-сети:

```routeros
/interface/bridge add name=bridge-docker port-cost-mode=short
/ip/address add address=192.168.210.1/24 interface=bridge-docker network=192.168.210.0
```

## Шаг 2: Интерфейс

Виртуальный Ethernet-интерфейс, подключённый к мосту:

```routeros
/interface/veth add address=192.168.210.10/24 gateway=192.168.210.1 name=B4
/interface/bridge/port add bridge=bridge-docker interface=B4
```

## Шаг 3: Маршрутизация

Таблица маршрутизации и маршрут через контейнер:

```routeros
/routing table add disabled=no fib name=to_b4
/ip route add check-gateway=ping gateway=192.168.210.10 routing-table=to_b4
```

## Шаг 4: Маркировка трафика

Трафик клиентов из списка `b4users` перенаправляется через контейнер:

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
FastTrack обходит правила mangle. Его нужно ограничить немаркированными соединениями:

```routeros
/ip firewall filter set [find action=fasttrack-connection] connection-mark=no-mark
```
:::

## Шаг 5: Точки монтирования

```routeros
/container/mounts add name=b4_etc src=/usb1/docker/b4-mounts/etc dst=/opt/etc/b4
```

Директория `/usb1/docker/b4-mounts/etc` должна существовать на диске.

## Шаг 6: Запуск контейнера

Настройка реестра:

```routeros
/container/config set registry-url=https://registry-1.docker.io tmpdir=/usb1/docker/pull
```

Создание контейнера:

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
Если провайдер перехватывает DNS (редирект 53 порта), публичные резолверы внутри контейнера не помогут. Выход - DoH на MikroTik, а в контейнере вместо публичного DNS указывается шлюз моста:

```routeros
/ip dns set use-doh-server=https://cloudflare-dns.com/dns-query verify-doh-cert=yes
```

DNS контейнера при этом становится `dns=192.168.210.1` (шлюз моста).
:::

## Шаг 7: Добавление клиентов

Устройства добавляются в адресный список `b4users`:

```routeros
/ip firewall address-list add list=b4users address=192.168.100.50
/ip firewall address-list add list=b4users address=192.168.100.51
```

## Шаг 8: NAT Masquerade

В веб-интерфейсе (`http://192.168.210.10:7000`) включается **NAT Masquerade** в разделе [Настройки, Основные, Файрвол](../settings/core#nat-masquerade). Интерфейс маскарада остаётся по умолчанию - все интерфейсы.

Метка маршрутизации из шага 4 ставится только на пакеты, пришедшие из LAN, поэтому ответы сервера RouterOS отправляет прямо клиенту, и через контейнер они не проходят. Conntrack внутри контейнера видит SYN клиента, не видит ответа на него и считает все следующие пакеты соединения недействительными (invalid). Первые пакеты соединения b4 отбирает по счётчику пакетов conntrack, поэтому без маскарада TLS ClientHello до него не доходит, и стратегии, которые с ним работают, не применяются.

С включённым маскарадом контейнер подменяет адрес источника пересылаемого трафика на свой, 192.168.210.10. Ответы возвращаются в контейнер, и он передаёт их клиенту через RouterOS. На участке от контейнера до интернета RouterOS видит источником каждого соединения 192.168.210.10, поэтому правила на этом участке не различают клиентов.

## Веб-интерфейс

После запуска контейнера: `http://192.168.210.10:7000`

:::tip Снижение износа диска
USB-флешки и SD-карты имеют ограниченное число циклов записи. Логи b4 можно перенести в RAM через веб-интерфейс:

**Настройки, Система, Сервис**, поле **Папка логов** в карточке **Логирование**: `/tmp/log/b4`

Логи теряются при перезагрузке, но накопитель прослужит дольше.
:::

## Доступ из WAN {#access-from-the-wan}

Контейнер стоит за RouterOS, которая фильтрует и транслирует всё, что приходит из WAN, до того как оно попадёт на мост. Порт b4, например `3128` MTProto-прокси, доступен снаружи только через правило `dst-nat` на RouterOS, ведущее на адрес контейнера:

```routeros
/ip firewall nat add chain=dstnat action=dst-nat in-interface-list=WAN \
    protocol=tcp dst-port=3128 to-addresses=192.168.210.10 to-ports=3128
```

Переключатель **Доступ из интернета** в b4 меняет только файрвол внутри контейнера и это правило не заменяет. Что переключатель делает в остальных случаях, описано в разделе [Доступ из интернета](../settings/security.md#expose-to-internet).

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

## Отправка сета в другой контейнер

Сет может передавать свой трафик SOCKS5-прокси в другом контейнере на том же мосту, например Xray, sing-box или mihomo с входом SOCKS5, вместо того чтобы выпускать его через основной маршрут RouterOS. **Режим маршрутизации** сета - *Upstream SOCKS5 proxy*, upstream - адрес другого контейнера (например, 192.168.210.20) и его порт SOCKS5. Дополнительные правила на RouterOS для этого не нужны: соединение b4 с прокси не выходит за пределы `bridge-docker`, через RouterOS идут только собственные соединения прокси.

В этом режиме b4 не применяет к трафику сета стратегии обхода DPI, до назначения его доводит прокси. Как устроено перенаправление, как обрабатывается UDP и какие модули ядра для этого нужны, описано в разделе [Вышестоящий прокси SOCKS5](/ru/docs/sets/routing#вышестоящий-прокси-socks5). Разные сеты могут указывать на разные прокси.

## Маршрутизация выбранных адресов через контейнер {#routing-by-destination}

Шаг 4 отбирает трафик по клиенту. Чтобы отбирать его по адресу назначения, например по подсетям сайта, адреса назначения заносятся в список адресов, и правила маркировки сверяются с этим списком вместо `b4users`:

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

Маршрут через контейнер остаётся в таблице `to_b4` из [шага 3](#шаг-3-маршрутизация). Маршрут на контейнер в основной таблице здесь не подходит: контейнер отправляет всё, что обработал, обратно RouterOS, своему шлюзу по умолчанию, а маршрут по адресу назначения в основной таблице возвращает те же пакеты в контейнер вместо того, чтобы выпустить их через WAN.

Правила маркировки, здесь и в шаге 4, действуют только на пакеты, пришедшие из списка интерфейсов LAN, поэтому пакеты, которые контейнер отправляет обратно, уходят по основной таблице. После шага 1 `bridge-docker` в этот список не входит. Если его туда добавили, пакеты контейнера выводит из-под правил маркировки правило выше них. Оно добавляется после них, чтобы `place-before=0` поставило его первым:

```routeros
/ip firewall mangle add chain=prerouting action=accept \
    in-interface=bridge-docker place-before=0
```

b4 на отдельной машине с Linux вместо контейнера требует той же схемы: собственная подсеть на своём порту RouterOS, VLAN или мосту, RouterOS в роли шлюза по умолчанию, маршрут в `to_b4`, указывающий на адрес этой машины, NAT Masquerade в b4, как в шаге 8, и этот интерфейс вне списка интерфейсов LAN или выведенный из-под правил тем же правилом accept с его именем вместо `bridge-docker`. В подсети клиентов машина отдаёт ответы сервера прямо клиенту, RouterOS видит только клиентскую половину каждого соединения и считает следующие TCP-пакеты клиента недействительными, из-за чего его TCP-соединения задерживаются или обрываются.

RouterOS различает пакеты b4 по интерфейсу, через который они пришли, поэтому этим правилам ничего не нужно от b4 внутри пакетов. Внутренние метки b4, включая метку пакета, не покидают контейнер или машину.

## Маршрутизация трафика сета по значению DSCP {#routing-by-dscp}

Всё, что b4 после обработки отправляет обратно в RouterOS, уходит основным маршрутом, к какому бы сету оно ни относилось. Собственное [значение DSCP](/ru/docs/sets/routing#dscp) сета - поле в этих пакетах, которое RouterOS может прочитать, и по нему RouterOS отправляет трафик этого сета другим маршрутом, например через VPN, а остальной выпускает через WAN. Для маршрутизации трафика в контейнер, описанной выше, ничего подобного не нужно.

Значение сета включает переключатель **Включить DSCP для сета** в разделе **DNS & Маршрутизация → Маршрутизация трафика** редактора сета, здесь это `31`. `wg-vpn` обозначает интерфейс VPN, а `via_vpn` - его таблицу маршрутизации:

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
| Первое `mark-connection` | Маркирует соединение от b4, первый пакет которого несёт значение. С NAT Masquerade из [шага 8](#шаг-8-nat-masquerade) пересылаемый трафик тоже выходит из контейнера с его адресом. Одно условие `connection-state=new` совпадает и со следующими пакетами до первого ответа, например с повторно отправленным SYN или пакетами QUIC до ответа сервера. С `connection-mark=no-mark` соединение, уже получившее метку `b4_other`, сохраняет её, когда такой пакет несёт значение. Без этого условия RouterOS переводит такое соединение в VPN посередине: он отбрасывает этот пакет, и соединение начинается заново через VPN с адресом `wg-vpn`. Метка действует всё соединение, и соединение, получившее значение позже, остаётся на основном маршруте |
| `mark-routing` | Направляет маркированные соединения в `via_vpn`. `in-interface=bridge-docker` не пускает под правило ответы сервера, у которых та же метка соединения: без него они уходят обратно в VPN, и соединения не работают |
| Второе `mark-connection`, `b4_other` | Маркирует остальные соединения b4, и ограничение FastTrack на `no-mark` из [шага 4](#шаг-4-маркировка-трафика) не пускает их в FastTrack. FastTrack пропускает правила `change-dscp`, и соединение, получившее значение после первого пакета, выносило бы его через WAN. Цена в том, что трафик b4 через FastTrack больше не идёт |
| `change-dscp` на списке `WAN` | Сбрасывает поле перед интернетом |
| `change-dscp` на `wg-vpn` | Сбрасывает поле перед туннелем. WireGuard в RouterOS копирует во внешний заголовок биты ECN, но не значение DSCP: без этого правила значение доходит до сервера VPN внутри туннеля |
| `masquerade` на `wg-vpn` | Даёт соединениям адрес интерфейса VPN |

Ограничение FastTrack из шага 4 обязательно: без него маркированные соединения уходят в FastTrack, и их пакеты выходят через основной аплинк с адресом источника VPN и значением.

Если правило accept для `bridge-docker` из раздела [Маршрутизация выбранных адресов через контейнер](#routing-by-destination) стоит в начале mangle prerouting, три правила prerouting пакетов b4 не видят. Их ставят выше него, добавив к каждому (кавычки обязательны):

```routeros
place-before=[find where action=accept in-interface="bridge-docker"]
```

Для второго аплинка вместо VPN маршрут в `via_vpn` указывает на шлюз этого аплинка, а последние два правила называют его интерфейс вместо `wg-vpn`. Каждому следующему значению нужно своё правило `mark-connection`. Значению, которое идёт другим путём, нужны ещё своя метка соединения, своё правило `mark-routing`, своя таблица и последние два правила для его интерфейса. Для b4 на отдельной машине её интерфейс и адрес заменяют `bridge-docker` и `192.168.210.10`. `/ip settings rp-filter=strict` отбрасывает ответы, которые возвращаются через VPN; `loose` и значение по умолчанию `no` их пропускают.

В этой схеме устройства разрешают имена через RouterOS, и адреса доменов сета b4 узнаёт только из имён в TLS и QUIC и из собственных запросов. Соединение к адресу, которого он ещё не знает, остаётся на основном маршруте. Значение с первого пакета соединения дают цели IP, GeoIP и ASN, см. [Первое соединение](/ru/docs/sets/routing#dscp-first-connection).

Правила проверены на RouterOS 7.24.5 для IPv4.

## Решение проблем

**Контейнер не запускается:**
1. Статус: `/container print`
2. Логи: `/log print where topics~"container"`
3. Диск должен быть отформатирован в Ext4

**Нет доступа к веб-интерфейсу:**
1. Контейнер должен быть запущен: `/container print`
2. Связность: `/ping 192.168.210.10`

**Трафик не перенаправляется:**
1. Список: `/ip firewall address-list print where list=b4users`
2. Mangle: `/ip firewall mangle print`
3. Маршрут: `/ip route print where routing-table=to_b4`

**Адреса, направленные через контейнер, не открываются:**
1. Маршрут на контейнер должен быть в таблице `to_b4`, а не в основной, см. [Маршрутизация выбранных адресов через контейнер](#routing-by-destination)
2. `bridge-docker` или интерфейс отдельной машины с b4 не должен входить в список интерфейсов LAN, или его пакеты нужно вывести из-под правил маркировки, как показано там

**Трафик доходит до контейнера, но обход не работает:**
1. В b4 должен быть включён NAT Masquerade, см. [шаг 8](#шаг-8-nat-masquerade)

**Дашборд сообщает, что движок NFQUEUE не запустился:**
1. Причину называет ошибка на карточке дашборда. `Extension NFQUEUE revision 0 not supported, missing kernel module?` означает, что в ядре, на котором RouterOS запускает контейнер, нет поддержки NFQUEUE
2. Кнопка **Переключить на TUN** на той же карточке выбирает движок TUN, которому модули очереди не нужны, и перезапускает b4, см. [Движок пакетов](../settings/core.md#packet-engine)
