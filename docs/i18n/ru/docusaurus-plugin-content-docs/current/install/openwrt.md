---
sidebar_position: 2
title: OpenWRT
---

# OpenWRT

## Требования

- OpenWRT 19.07 и выше
- Внешнее хранилище (USB или extroot) - рекомендуется, так как на внутренней памяти роутера может не хватить места

:::warning Место на диске
На роутерах с OpenWRT внутренняя память ограничена (overlay). Если доступно менее 2 МБ, установщик предупредит об этом. Рекомендуется использовать extroot или USB-накопитель.

Инструкция по настройке extroot: https://openwrt.org/docs/guide-user/additional-software/extroot_configuration
:::

## Установка

Подключитесь к роутеру по SSH и выполните:

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

:::info wget на OpenWRT
Стандартный `wget` в OpenWRT (BusyBox) не поддерживает HTTPS. Установите полную версию:
```bash
opkg update && opkg install wget-ssl ca-certificates
```
:::

## Модули ядра

Установщик попытается загрузить необходимые модули автоматически. Если при запуске вы видите предупреждение `[WARN] No netfilter queue module available` или ошибки связанные с nftables - установите модули вручную.

### OpenWRT 24.x+ (apk)

```bash
apk add kmod-nft-queue kmod-nft-nat kmod-nft-compat kmod-nft-conntrack
```

### OpenWRT 23.x и ниже (opkg)

```bash
opkg update
opkg install kmod-nft-queue kmod-nft-conntrack nftables-json coreutils-nohup
```

Для совсем старых версий (без nftables):

```bash
opkg install kmod-nfnetlink-queue kmod-ipt-nfqueue iptables-mod-nfqueue iptables-mod-conntrack-extra
```

### Загрузка модулей

После установки модулей может потребоваться загрузить их вручную:

```bash
modprobe nft_queue
modprobe nft_ct
modprobe xt_connbytes
```

Если команда выполняется без вывода - модуль загружен успешно.

## Управление сервисом

```bash
/etc/init.d/b4 enable     # автозапуск при загрузке
/etc/init.d/b4 start
/etc/init.d/b4 stop
/etc/init.d/b4 restart
```

:::tip Работа через SSH
Сервис b4 работает как системный демон - он продолжит работать после закрытия SSH-сессии (PuTTY, терминал и т.д.). Не нужно использовать `screen` или `nohup` вручную.
:::

## Пути

При наличии `/opt` (extroot/USB):

| Что | Где |
| --- | --- |
| Бинарник | `/opt/bin/b4` |
| Конфигурация | `/opt/etc/b4/b4.json` |

Без внешнего хранилища (fallback):

| Что | Где |
| --- | --- |
| Бинарник | `/usr/bin/b4` |
| Конфигурация | `/etc/b4/b4.json` |

## Веб-интерфейс

После запуска b4 доступен по адресу `http://<IP роутера>:7000`. Например, если IP роутера `192.168.1.1`, откройте в браузере:

```text
http://192.168.1.1:7000
```

## LuCI-приложение

Существует сторонний пакет [luci-app-b4](https://github.com/BugOldfag/luci-app-b4), который добавляет управление b4 в интерфейс LuCI. Проект находится в стадии alpha и покрывает часть функций. Основной веб-интерфейс b4 (порт 7000) по-прежнему доступен.

## Устранение неполадок

### Service crashed / сервис не запускается

1. Убедитесь что модули ядра установлены и загружены (см. раздел «Модули ядра» выше)
2. Проверьте логи: `logread | grep b4`

### Error: Could not process rule

Если b4 вылетает с ошибкой при добавлении правил в цепочку, возможно остались «битые» таблицы от предыдущего неудачного запуска. Очистите их:

```bash
nft delete table inet b4_mangle 2>/dev/null
```

После этого запустите b4 заново:

```bash
/etc/init.d/b4 restart
```

### Низкая скорость / тормозит видео

Проверьте настройку **Software flow offloading** в разделе Network → Firewall. Попробуйте включить или выключить её - на некоторых устройствах это влияет на производительность b4.

### Как совместить flow offloading и b4

Flow offloading переводит установленное соединение на быстрый путь, минуя хуки netfilter, с которыми работает b4: при включённом offloading b4 запущен, но трафика не видит. На слабом железе выключение offloading заметно снижает пропускную способность.

b4 разбирает только первые пакеты соединения (по умолчанию 19 для TCP и 8 для UDP, параметры `queue.tcp_conn_bytes_limit` и `queue.udp_conn_bytes_limit`), поэтому offloading можно отложить до момента, когда b4 уже отработал. Откройте `/usr/share/firewall4/templates/ruleset.uc` и найдите строку:

```text
meta l4proto { tcp, udp } flow offload @ft;
```

Замените её на:

```text
meta l4proto { tcp, udp } ct original packets ge 40 flow offload @ft;
```

Перезапустите файрвол:

```bash
fw4 restart
```

На что обратить внимание:

- Порог должен оставаться выше лимитов пакетов, заданных в b4. Если поднять `tcp_conn_bytes_limit` выше порога, соединение уйдёт на быстрый путь раньше, чем b4 с ним закончит.
- `ct original packets` читает счётчики conntrack. Если `sysctl net.netfilter.nf_conntrack_acct` возвращает `0`, счётчик остаётся нулевым, правило не срабатывает и offloading не включается вообще.
- Сеты с включённым **Duplicate** для TCP разбираются на протяжении всего соединения, для них не подойдёт никакой порог.
- Файл принадлежит пакету `firewall4` и перезаписывается при обновлении пакета и при sysupgrade.

Системная диагностика (Настройки -> Информация о системе и экран диагностики установщика) показывает найденный порог и сравнивает его с лимитами b4.
