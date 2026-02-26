# B4

![GitHub Release](https://img.shields.io/github/v/release/daniellavrushin/b4)
![GitHub Downloads](https://img.shields.io/github/downloads/daniellavrushin/b4/total)

[[English](readme.md)] [[telegram](https://t.me/byebyebigbro)]

Процессор сетевых пакетов для обхода систем глубокой инспекции пакетов (DPI).

![alt text](image.png)

## Обзор

B4 использует Linux netfilter для перехвата и модификации сетевых пакетов в реальном времени, применяя различные техники для обхода DPI-систем, используемых провайдерами и сетевыми администраторами.

## Требования

- Linux-система (десктоп, сервер или роутер)
- Root-доступ (sudo)

Это всё. Установщик позаботится об остальном.

## Установка

### Автоматическая установка

> [!NOTE]
> В некоторых системах необходимо запускать `sudo b4install.sh`.

```bash
wget -O ~/b4install.sh https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh && chmod +x ~/b4install.sh && ~/b4install.sh
```

Если что-то пошло не так, попробуйте запустить — это выполнит диагностику системы:

```bash
wget -O ~/b4install.sh https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh && chmod +x ~/b4install.sh && ~/b4install.sh --sysinfo
```

### Опции установщика

```bash

./b4install.sh # установка приложения B4

./b4install.sh -h # показать справку

# Показать информацию о системе
./b4install.sh --sysinfo

# Установить определённую версию
./b4install.sh v1.10.0

# Тихий режим
./b4install.sh --quiet

# Указать источник geosite
./b4install.sh --geosite-src=https://example.com/geosite.dat --geosite-dst=/opt/etc/b4

# Обновить существующую установку
./b4install.sh --update

# Удалить
./b4install.sh --remove
```

### Сборка из исходников

```bash
# Клонировать репозиторий
git clone https://github.com/daniellavrushin/b4.git
cd b4

# Собрать UI
cd src/http/ui
pnpm install && pnpm build
cd ../../..

# Собрать бинарник
make build

# Собрать для всех архитектур
make build-all

# Собрать для конкретной архитектуры
make linux-amd64
make linux-arm64
make linux-armv7
```

## Базовое использование

### Запуск B4

```bash
# Стандартный Linux (systemd)
sudo systemctl start b4
sudo systemctl enable b4  # Запуск при загрузке

# OpenWRT
/etc/init.d/b4 restart # start | stop

# Entware/MerlinWRT
/opt/etc/init.d/S99b4 restart # start | stop
```

### Доступ к веб-интерфейсу

Откройте браузер и перейдите по адресу:

```cmd
http://ip-вашего-устройства:7000
```

## Использование из командной строки

```bash

# получить справку
b4  --help

# Базовое использование с указанием доменов вручную
sudo b4 --sni-domains youtube.com,netflix.com

# Использование категорий geosite
sudo b4 --geosite /etc/b4/geosite.dat --geosite-categories youtube,netflix

# Пользовательская конфигурация
sudo b4 --queue-num 100 --threads 4 --web-port 8080
```

### Конфигурационный файл

При установке автоматически создается по пути `/etc/b4/b4.json`
(файл можно переопределить, передав аргумент `--config=`):

```json
{
  "queue_start_num": 537,
  "mark": 32768,
  "threads": 4,
  "conn_bytes_limit": 19,
  "seg2delay": 0,
  "ipv4": true,
  "ipv6": false,

  "domains": {
    "geosite_path": "/etc/b4/geosite.dat",
    "geoip_path": "",
    "sni_domains": [],
    "geosite_categories": ["youtube", "netflix"],
    "geoip_categories": []
  },

  "fragmentation": {
    "strategy": "tcp",
    "sni_reverse": true,
    "middle_sni": true,
    "sni_position": 1
  },

  "faking": {
    "sni": true,
    "ttl": 8,
    "strategy": "pastseq",
    "seq_offset": 10000,
    "sni_seq_length": 1,
    "sni_type": 2,
    "custom_payload": ""
  },

  "udp": {
    "mode": "fake",
    "fake_seq_length": 6,
    "fake_len": 64,
    "faking_strategy": "none",
    "dport_min": 0,
    "dport_max": 0,
    "filter_quic": "parse",
    "filter_stun": true,
    "conn_bytes_limit": 8
  },

  "web_server": {
    "port": 7000
  },

  "logging": {
    "level": "info",
    "instaflush": true,
    "syslog": false
  },

  "tables": {
    "monitor_interval": 10,
    "skip_setup": false
  }
}
```

Загрузка с пользовательской конфигурацией:

```bash
sudo b4 --config /home/username/b4custom.json
```

### Параметры конфигурации

#### Сетевая конфигурация

| Флаг                | По умолчанию | Описание                       |
| ------------------- | ------------ | ------------------------------ |
| `--queue-num`       | 537          | Номер очереди netfilter        |
| `--threads`         | 4            | Количество рабочих потоков     |
| `--mark`            | 32768        | Значение метки пакета          |
| `--connbytes-limit` | 19           | Лимит байтов TCP-соединения    |
| `--seg2delay`       | 0            | Задержка между сегментами (мс) |
| `--ipv4`            | true         | Включить обработку IPv4        |
| `--ipv6`            | false        | Включить обработку IPv6        |

#### Фильтрация доменов

| Флаг                   | По умолчанию | Описание                     |
| ---------------------- | ------------ | ---------------------------- |
| `--sni-domains`        | []           | Список доменов через запятую |
| `--geosite`            | ""           | Путь к файлу geosite.dat     |
| `--geosite-categories` | []           | Категории для обработки      |

#### Фрагментация TCP

| Флаг                 | По умолчанию | Описание                            |
| -------------------- | ------------ | ----------------------------------- |
| `--frag`             | tcp          | Стратегия фрагментации: tcp/ip/none |
| `--frag-sni-reverse` | true         | Обратный порядок фрагментов         |
| `--frag-middle-sni`  | true         | Фрагментация в середине SNI         |
| `--frag-sni-pos`     | 1            | Позиция фрагмента SNI               |

#### Конфигурация поддельного SNI

| Флаг                | По умолчанию | Описание                                                     |
| ------------------- | ------------ | ------------------------------------------------------------ |
| `--fake-sni`        | true         | Включить поддельные SNI-пакеты                               |
| `--fake-ttl`        | 8            | TTL для поддельных пакетов                                   |
| `--fake-strategy`   | pastseq      | Стратегия: ttl/randseq/pastseq/tcp_check/md5sum              |
| `--fake-seq-offset` | 10000        | Смещение последовательности для поддельных пакетов           |
| `--fake-sni-len`    | 1            | Длина поддельной SNI-последовательности                      |
| `--fake-sni-type`   | 2            | Тип payload: 0=случайный, 1=пользовательский, 2=по умолчанию |

#### Конфигурация UDP/QUIC

| Флаг                     | По умолчанию | Описание                                        |
| ------------------------ | ------------ | ----------------------------------------------- |
| `--udp-mode`             | fake         | Обработка UDP: drop/fake                        |
| `--udp-fake-seq-len`     | 6            | Длина последовательности поддельных UDP-пакетов |
| `--udp-fake-len`         | 64           | Размер поддельного UDP-пакета (байты)           |
| `--udp-faking-strategy`  | none         | Стратегия: none/ttl/checksum                    |
| `--udp-dport-min`        | 0            | Минимальный порт назначения UDP                 |
| `--udp-dport-max`        | 0            | Максимальный порт назначения UDP                |
| `--udp-filter-quic`      | parse        | Фильтрация QUIC: disabled/all/parse             |
| `--udp-filter-stun`      | true         | Включить фильтрацию STUN                        |
| `--udp-conn-bytes-limit` | 8            | Лимит байтов UDP-соединения                     |

#### Системная конфигурация

| Флаг                        | По умолчанию | Описание                                           |
| --------------------------- | ------------ | -------------------------------------------------- |
| `--skip-tables`             | false        | Пропустить настройку iptables/nftables             |
| `--tables-monitor-interval` | 10           | Интервал мониторинга таблиц (секунды, 0=отключено) |
| `--web-port`                | 7000         | Порт веб-интерфейса (0=отключено)                  |
| `--verbose`                 | info         | Уровень логирования: debug/trace/info/error/silent |
| `--instaflush`              | true         | Немедленная запись логов                           |
| `--syslog`                  | false        | Включить вывод в syslog                            |

## Веб-интерфейс

Веб-интерфейс доступен по адресу `http://ip-устройства:7000` (порт по умолчанию, можно изменить в файле `config`).

**Возможности:**

- Метрики в реальном времени (соединения, пакеты, пропускная способность)
- Потоковая передача логов в реальном времени с фильтрацией и горячими клавишами (p - приостановить передачу логов, del - очистить логи)
- Управление доменами (добавление/удаление доменов на лету)
- Тест доменов и автоподбор стратегий
- Интеграция api ipinfo.io для сканирования ASN
- Захват TLS и QUIC Payload

## Поддержка HTTPS/TLS
Вы можете включить HTTPS для веб-интерфейса, предоставив файлы сертификатов и ключей с помощью флагов:
```bash
b4 --tls-cert /path/to/server.crt --tls-key /path/to/server.key
```
Пользователи OpenWRT: Установщик автоматически обнаруживает системные сертификаты (из uhttpd) и включает режим HTTPS по умолчанию, если они найдены.

## Интеграция Geosite

B4 поддерживает файлы [`geosite.dat` от v2ray/xray](https://github.com/v2fly/domain-list-community) из различных источников:

```bash
# Loyalsoldier
wget https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat

# RUNET Freedom
wget https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/geosite.dat

# Nidelon
wget https://github.com/Nidelon/ru-block-v2ray-rules/releases/latest/download/geosite.dat
```

Поместите файл в `/etc/b4/geosite.dat` и настройте категории:

```bash
sudo b4 --geosite /etc/b4/geosite.dat --geosite-categories youtube,netflix,facebook
```

> [!TIP]
> Все эти настройки можно настроить через веб-интерфейс.

## Сборка и разработка

### Требования для сборки

- Go 1.25 или новее
- Node.js 22+ и pnpm (для веб-интерфейса)
- Make

### Команды сборки

```bash
# Собрать для текущей платформы
make build

# Собрать для всех платформ
make build-all

# Собрать для конкретной платформы
make linux-amd64
make linux-arm64
make linux-armv7

# Очистить артефакты сборки
make clean

# Запустить с sudo (разработка)
make run

# Установить в /usr/local/bin
make install
```

## Участие в разработке

Вклад в проект принимается через pull request на GitHub.

### Настройка окружения для разработки

```bash
# Клонировать репозиторий
git clone https://github.com/daniellavrushin/b4.git
cd b4

# Установить зависимости
cd src/http/ui && pnpm install

# Собрать и запустить
pnpm build
cd ../../..
make build
sudo ./out/b4 --verbose debug
```

## Благодарности

Этот проект включает исследования и техники из:

- [youtubeUnblock](https://github.com/Waujito/youtubeUnblock) - обход DPI на C
- [GoodbyeDPI](https://github.com/ValdikSS/GoodbyeDPI) - обход DPI для Windows
- [zapret](https://github.com/bol-van/zapret) - продвинутые техники обхода DPI

## Лицензия

Этот проект предоставляется в образовательных целях. Пользователи несут ответственность за соблюдение применимых законов и правил.

**Варианты использования:**

- Обход интернет-цензуры в регионах с ограничениями
- Защита приватности от сетевого наблюдения
- Исследования и обучение сетевым протоколам

**Не предназначено для:**

- Незаконной деятельности
- Несанкционированного доступа к сетям
- Нарушения условий обслуживания

Авторы не несут ответственности за неправомерное использование данного программного обеспечения.
