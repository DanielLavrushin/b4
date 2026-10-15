---
sidebar_position: 2
title: Конфигурационный файл
description: Где b4 хранит конфигурацию, как устроен файл и в каком пути хранится каждая настройка веб-интерфейса.
---

b4 хранит конфигурацию в одном JSON-файле, по умолчанию `/etc/b4/b4.json`. [Флаг](./cli.md) `--config` указывает другой путь.

## Расположение по платформам

| Платформа | Путь |
| --- | --- |
| Linux | `/etc/b4/b4.json` |
| OpenWRT (с extroot или USB-накопителем) | `/opt/etc/b4/b4.json` |
| OpenWRT (без USB-накопителя) | `/etc/b4/b4.json` |
| ASUS Merlin | `/opt/etc/b4/b4.json` |
| Keenetic | `/opt/etc/b4/b4.json` |
| Docker | `/etc/b4/b4.json` внутри контейнера |

Без `--config` b4 ищет `b4.json` и `config.json` в каталогах `/etc/b4` и `/opt/etc/b4`, а если не находит, берёт `/etc/b4/b4.json`. Выбранный путь пишется в лог при запуске.

Карточка **Скачать конфигурацию** в разделе [Настройки > Система > Бэкап](../settings/system.md) сохраняет конфигурацию в том виде, в каком b4 записывает этот файл. Её [безопасная копия](../settings/system.md#safe-copy) маскирует учётные данные и частные имена хостов для передачи другим, а конфигурацию, в которой остались заглушки `[redacted]`, b4 не загружает и не сохраняет.

## Хранится только то, что отличается от умолчаний

Файл разрежённый. Настройка со встроенным значением по умолчанию в него не попадает, поэтому свежая установка даёт очень короткий файл, а раздел, в котором ничего не менялось, отсутствует. Отсутствующий ключ - не пропавшая настройка, и дописывание его с тем же значением ничего не меняет. Сохранения из веб-интерфейса, через API и через MCP записывают файл так же.

## Структура

```json
{
  "version": 52,
  "queue": {
    "start_num": 537,
    "threads": 4,
    "mark": 32768,
    "ipv4": true,
    "ipv6": false,
    "tcp_conn_bytes_limit": 19,
    "udp_conn_bytes_limit": 8,
    "interfaces": [],
    "mss_clamp": { "enabled": false, "size": 88 },
    "devices": { "enabled": false, "vendor_lookup": false }
  },
  "system": {
    "tables": {
      "skip_setup": false,
      "monitor_interval": 10,
      "engine": "",
      "masquerade": { "enabled": false, "interfaces": [] },
      "dscp": { "enabled": false, "value": 0, "interfaces": [] }
    },
    "logging": {
      "level": 1,
      "directory": "/var/log/b4",
      "instaflush": true,
      "syslog": false
    },
    "web_server": {
      "port": 7000,
      "bind_address": "0.0.0.0",
      "expose": false,
      "tls_cert": "",
      "tls_key": "",
      "username": "",
      "password": "",
      "language": "en",
      "mcp": { "enabled": false, "allow_writes": false }
    },
    "dns": {
      "tcp_disabled": false,
      "tcp_port": 5453,
      "query_timeout_sec": 5,
      "keep_ipv6_answers": false
    },
    "socks5": {
      "enabled": false,
      "port": 1080,
      "bind_address": "0.0.0.0",
      "expose": false,
      "allowed_sources": ["192.168.1.0/24", "127.0.0.1/32"]
    },
    "mtproto": { "enabled": false, "port": 3128, "bind_address": "0.0.0.0", "expose": false },
    "checker": {
      "discovery_timeout": 5,
      "config_propagate_ms": 1500,
      "dns_server": ""
    },
    "geo": { "sitedat_path": "", "ipdat_path": "", "sitedat_url": "", "ipdat_url": "" },
    "timezone": ""
  },
  "sets": []
}
```

Пример показывает часть разделов и часть их ключей. В реальном файле есть только ключи, которые отличаются от умолчаний.

- `system.mtproto.secrets` - список именованных секретов, у каждого есть `id`, `name`, `secret` и `enabled`. Файл, записанный до версии конфигурации 50, содержал одну строку `mtproto.secret`, и миграция переносит её в список записью с именем `default`.
- `system.socks5.allowed_sources` отсутствует, пока список пуст. Это значение по умолчанию, и прокси тогда принимает соединения с любого источника. Каждая запись - IP-адрес или CIDR-диапазон. `0.0.0.0/0`, `::/0` и некорректные записи отклоняются при загрузке и сохранении файла.

## Раздел `queue`

| Ключ | Смысл | По умолчанию |
| --- | --- | --- |
| `start_num` | Первый номер NFQUEUE | `537` |
| `threads` | Количество рабочих потоков | `4` |
| `mark` | Метка очереди, см. [Метки пакетов](../guides/marks.md) | `32768` |
| `ipv4` | Обрабатывать IPv4-трафик | `true` |
| `ipv6` | Обрабатывать IPv6-трафик | `false` |
| `tcp_conn_bytes_limit` | Сколько пакетов в начале TCP-соединения проверяет b4 | `19` |
| `udp_conn_bytes_limit` | То же для UDP | `8` |
| `interfaces` | **Интерфейсы захвата**, фильтр по интерфейсу, через который проходит пакет. Пустой список означает все | `[]` |

`queue.ipv6` определяет, обрабатывает ли IPv6 сам b4, а не пользуется ли им роутер. При выключенном значении, а оно по умолчанию, b4 обрабатывает только IPv4 и не пишет IPv6-правил файрвола ни для одного сета, поэтому клиент, открывший сайт по IPv6, проходит мимо сетов. b4 тогда убирает IPv6-адреса из DNS-ответов для доменов, с которыми совпадают сеты, см. [DNS](../dns.md). Исключение - режим NFQUEUE, пока у включённого сета включено `dns.strip_ech`. В этом режиме b4 пишет IPv6-правила для DNS-ответов и DNS поверх TCP, но только чтобы убрать из них [ключи ECH](../dns.md#удаление-ключей-ech). Изменение действует в полном объёме после перезапуска. `--ipv6` задаёт то же самое на один запуск.

## Раздел `sets`

Каждый сет - объект в массиве `sets`, несущий всю свою конфигурацию. Его ключи соответствуют вкладкам редактора сета:

- `targets` - домены, IP, категории GeoSite и GeoIP, ASN, устройства-источники
- `tcp` - общие настройки TCP, desync, window, incoming, защита от RST
- `fragmentation` - метод фрагментации и его параметры
- `faking` - SNI faking, SYN-фейки, мутация
- `udp` - фильтр QUIC, фильтр портов и режим действия для UDP
- `dns` - резолвер сета, DoH URL и закреплённые адреса
- `routing` - режим маршрутизации, выходной интерфейс, вышестоящий прокси или блокировка
- `dscp` - собственное значение DSCP сета
- `discovery` - адреса Дискавери сета и переключатель его мониторинга
- `escalate` - на какой сет переключаться, когда этот перестаёт работать

Вкладка **Импорт/Экспорт** редактора сета показывает сет в виде JSON и принимает вставленный обратно. В экспортируемом JSON нет значений, равных умолчаниям, и настроек выключенных функций. В нём нет и идентификатора сета, устройств-источников, переключателя мониторинга и настроек эскалации, а если у сета есть устройства-источники, то и его `dscp`.

### `dscp` сета {#set-dscp}

| Ключ | Смысл | По умолчанию |
| --- | --- | --- |
| `dscp.enabled` | **Включить DSCP для сета**: записывать собственное значение сета в пакеты к адресам сета, см. [DSCP](../sets/routing.md#dscp) | `false` |
| `dscp.value` | Значение, которое записывается в поле DSCP, 0-63. `0` обнуляет поле | `0` |

Каждый ключ хранится, только пока отличается от умолчания: включённый переключатель со значением `0` сохраняется как `"dscp": {"enabled": true}`, значение при выключенном переключателе - как `"dscp": {"value": 12}`. Пока `dscp.enabled` равно `true`, `dscp.value` вне 0-63 отклоняется, а не подрезается: веб-интерфейс не даёт сохранить такое значение, а файл с ним останавливает b4 при запуске. Ни один из ключей нельзя изменить через MCP, и ни один не передаётся в [общем сете](../sets/sharing.md).

## Настройки и их пути в конфигурации {#settings-and-their-configuration-paths}

Таблицы перечисляют поля веб-интерфейса и путь, в котором хранится каждое. Пути редактора сета указаны относительно одного объекта сета в массиве `sets`: `dns.strict` означает `sets[].dns.strict`.

### Настройки > Основные {#paths-core}

| Подвкладка | Поле | Путь |
| --- | --- | --- |
| Движок пакетов | **Режим захвата** | `queue.mode` |
| Движок пакетов | **Аплинк-интерфейс** | `queue.tun.out_interface` |
| Движок пакетов | **Шлюз аплинка** | `queue.tun.out_gateway` |
| Движок пакетов | **Адрес TUN** | `queue.tun.address` |
| Движок пакетов | **Имя устройства TUN** | `queue.tun.device_name` |
| Движок пакетов | **Интерфейсы захвата** | `queue.interfaces` |
| Движок пакетов | **Интервал перепроверки (секунды)** | `system.ip_health.retest_interval_sec` |
| Движок пакетов | **Поддержка IPv4** | `queue.ipv4` |
| Движок пакетов | **Поддержка IPv6** | `queue.ipv6` |
| Движок пакетов | **Форсировать IPv4 для совпавших доменов** | `system.dns.keep_ipv6_answers`, инвертировано: включённый переключатель - это `false` |
| Движок пакетов | **Начальный номер очереди** | `queue.start_num` |
| Движок пакетов | **Метка пакета** | `queue.mark` |
| Движок пакетов | **Рабочие потоки** | `queue.threads` |
| Движок пакетов | **Лимит TCP пакетов соединения** | `queue.tcp_conn_bytes_limit` |
| Движок пакетов | **Лимит UDP пакетов соединения** | `queue.udp_conn_bytes_limit` |
| Устройства | **Включить фильтрацию устройств** | `queue.devices.enabled` |
| Устройства | **Исключить выбранные устройства (чёрный список)** | `queue.devices.wisb` |
| Устройства | **Определение производителя** | `queue.devices.vendor_lookup` |
| Устройства | Устройства, выбранные в **Доступные устройства** и добавленные в **Ручные устройства** | `queue.devices.devices` |
| Файрвол | **Пропустить настройку IPTables/NFTables** | `system.tables.skip_setup` |
| Файрвол | **Движок файрвола** | `system.tables.engine` |
| Файрвол | **Интервал мониторинга файрвола (секунды)** | `system.tables.monitor_interval` |
| Файрвол | **Включить глобальный MSS Clamping** | `queue.mss_clamp.enabled` |
| Файрвол | **Размер MSS** | `queue.mss_clamp.size` |
| Файрвол | **Включить NAT Masquerade** | `system.tables.masquerade.enabled` |
| Файрвол | Теги интерфейсов NAT Masquerade | `system.tables.masquerade.interfaces` |
| Файрвол | **Задать DSCP** | `system.tables.dscp.enabled` |
| Файрвол | **Значение DSCP** | `system.tables.dscp.value` |
| Файрвол | Теги интерфейсов DSCP | `system.tables.dscp.interfaces` |
| DNS | **Перехватывать DNS поверх TCP** | `system.dns.tcp_disabled`, инвертировано: включённый переключатель - это `false` |
| DNS | **Порт слушателя** | `system.dns.tcp_port` |
| DNS | **Таймаут запроса** | `system.dns.query_timeout_sec` |
| DNS | **Таймаут простоя** | `system.dns.tcp_idle_sec` |
| DNS | **Таймаут чтения/записи** | `system.dns.tcp_io_sec` |
| DNS | **Таймаут пересылки** | `system.dns.tcp_dial_sec` |
| SOCKS5 | **Включить SOCKS5 прокси** | `system.socks5.enabled` |
| SOCKS5 | **Адрес привязки** | `system.socks5.bind_address` |
| SOCKS5 | **Порт** | `system.socks5.port` |
| SOCKS5 | **Доступ из интернета** | `system.socks5.expose` |
| SOCKS5 | **Имя пользователя** | `system.socks5.username` |
| SOCKS5 | **Пароль** | `system.socks5.password` |
| SOCKS5 | **Разрешённые источники** | `system.socks5.allowed_sources` |

### Настройки > Система {#paths-system}

| Подвкладка | Поле | Путь |
| --- | --- | --- |
| Сервис | **Язык** | `system.web_server.language` |
| Сервис | **Часовой пояс** | `system.timezone` |
| Сервис | **Лимит памяти** | `system.memory_limit` |
| Сервис | **Зеркала обновления** | `system.update.mirrors` |
| Сервис | **Уровень логирования** | `system.logging.level` |
| Сервис | **Папка логов** | `system.logging.directory` |
| Сервис | **Мгновенный сброс** | `system.logging.instaflush` |
| Сервис | **Syslog** | `system.logging.syslog` |
| Веб-сервер | **Адрес привязки** | `system.web_server.bind_address` |
| Веб-сервер | **Порт** | `system.web_server.port` |
| Веб-сервер | **Доступ из интернета** | `system.web_server.expose` |
| Веб-сервер | **TLS Сертификат** | `system.web_server.tls_cert` |
| Веб-сервер | **TLS Ключ** | `system.web_server.tls_key` |
| Веб-сервер | **Имя пользователя** | `system.web_server.username` |
| Веб-сервер | **Пароль** | `system.web_server.password`, хранится как хеш |

### Настройки > Geodat настройки {#paths-geodat}

| Карточка | Поле | Путь |
| --- | --- | --- |
| База данных Geosite | Файл, который записывают **Обновить** и **Загрузить** | `system.geo.sitedat_path` |
| База данных Geosite | **Источник** | `system.geo.sitedat_url` |
| База данных GeoIP | Файл, который записывают **Обновить** и **Загрузить** | `system.geo.ipdat_path` |
| База данных GeoIP | **Источник** | `system.geo.ipdat_url` |
| Автообновление | **Обновлять при запуске** | `system.geo.auto_update.on_startup` |
| Автообновление | **Расписание** | `system.geo.auto_update.interval`: `daily`, `weekly` или `monthly` |
| Автообновление | **Последний запуск** | `system.geo.auto_update.last_run`, записывает b4 |

### Настройки > Дискавери {#paths-discovery}

| Карточка | Поле | Путь |
| --- | --- | --- |
| Конфигурация тестирования | **Таймаут поиска** | `system.checker.discovery_timeout` |
| Конфигурация тестирования | **Задержка распространения конфигурации** | `system.checker.config_propagate_ms` |
| Конфигурация тестирования | **Доверенный DNS-сервер** | `system.checker.dns_server` |
| Мониторинг | **Включить мониторинг** | `system.checker.watchdog.enabled` |
| Мониторинг | **Интервал проверки** | `system.checker.watchdog.interval_sec` |
| Мониторинг | **Интервал после ошибки** | `system.checker.watchdog.failure_interval` |
| Мониторинг | **Перерыв между discovery** | `system.checker.watchdog.cooldown_sec` |
| Мониторинг | **Таймаут проверки** | `system.checker.watchdog.timeout_sec` |
| Мониторинг | **Макс. попыток** | `system.checker.watchdog.max_retries` |
| Мониторинг | **Старый список доменов** | `system.checker.watchdog.domains` |

### Настройки > Telegram {#paths-telegram}

| Карточка | Поле | Путь |
| --- | --- | --- |
| MTProto Прокси | **Включить MTProto прокси** | `system.mtproto.enabled` |
| MTProto Прокси | **Адрес привязки** | `system.mtproto.bind_address` |
| MTProto Прокси | **Порт** | `system.mtproto.port` |
| MTProto Прокси | **Доступ из интернета** | `system.mtproto.expose` |
| MTProto Прокси | **Домен для Fake SNI** | `system.mtproto.fake_sni` |
| MTProto Прокси | **Макс. подключений** | `system.mtproto.max_connections` |
| MTProto Прокси | **TCP User Timeout (сек)** | `system.mtproto.tcp_user_timeout_sec` |
| MTProto Прокси | **Тайм-аут простоя (сек)** | `system.mtproto.idle_timeout_sec` |
| Секреты | Список секретов | `system.mtproto.secrets` |
| Транспорт к Telegram (общий) | **Режим транспорта** | `system.mtproto.upstream_mode` |
| Транспорт к Telegram (общий) | **DC Relay** | `system.mtproto.dc_relay` |
| Транспорт к Telegram (общий) | **Домен Cloudflare Worker** | `system.mtproto.cfworker_domain` |
| Транспорт к Telegram (общий) | **Обрабатывать соединения с Worker сетами** | `system.mtproto.cfworker_dpi` |
| Транспорт к Telegram (общий) | **Свой WebSocket-домен** | `system.mtproto.ws_custom_domain` |
| Транспорт к Telegram (общий) | **IP edge-сервера Telegram (расширенное)** | `system.mtproto.ws_endpoint_host` |
| Транспорт к Telegram (общий) | **Имя для фронтинга WS-узла Telegram (расширенное)** | `system.mtproto.ws_front_sni` |
| Транспорт к Telegram (общий) | **CF-резерв** | `system.mtproto.cfproxy_enabled` |
| Транспорт к Telegram (общий) | **URL списка CF-доменов** | `system.mtproto.cfproxy_url` |
| Транспорт к Telegram (общий) | **Резервное зеркало списка DC** | `system.mtproto.dc_fallback_enabled` |
| Транспорт к Telegram (общий) | **URL зеркала списка DC** | `system.mtproto.dc_fallback_url` |
| Транспорт к Telegram (общий) | **Ожидание рукопожатия в мосте (сек)** | `system.mtproto.bridge_wait_sec` |
| Telegram через WebSocket | **Включить Telegram через WebSocket** | `system.mtproto.bridge.enabled` |
| WEB-прокси Telegram Desktop | **Включить WEB-канал** | `system.mtproto.web_proxy.enabled` |
| WEB-прокси Telegram Desktop | **Хост релея** | `system.mtproto.web_proxy.hostname` |
| WEB-прокси Telegram Desktop | **Порт relay** | `system.mtproto.web_proxy.port` |
| WEB-прокси Telegram Desktop | **Доступ из интернета** | `system.mtproto.web_proxy.expose` |
| WEB-прокси Telegram Desktop | **Файл сертификата** | `system.mtproto.web_proxy.tls_cert` |
| WEB-прокси Telegram Desktop | **Файл закрытого ключа** | `system.mtproto.web_proxy.tls_key` |

### Настройки > Интеграции {#paths-integrations}

| Карточка | Поле | Путь |
| --- | --- | --- |
| IPinfo | **Токен доступа** | `system.api.ipinfo_token` |
| AI-помощник | **Включить AI-помощника** | `system.ai.enabled` |
| AI-помощник | Вкладки провайдеров | `system.ai.provider` |
| AI-помощник | **Модель** | `system.ai.model` |
| AI-помощник | **Endpoint** | `system.ai.endpoint` |
| AI-помощник | **Макс. токенов** | `system.ai.max_tokens` |
| AI-помощник | **Температура** | `system.ai.temperature` |
| AI-помощник | **Таймаут (с)** | `system.ai.timeout_sec` |
| MCP-сервер | **Включить MCP-сервер** | `system.web_server.mcp.enabled` |
| MCP-сервер | **Токен доступа** | `system.web_server.mcp.token` |
| MCP-сервер | **Разрешить изменение настроек** | `system.web_server.mcp.allow_writes` |
| MCP-сервер | **Разрешить активные проверки** | `system.web_server.mcp.allow_active_probes` |
| Хаб сообщества | **Включить хаб сообщества** | `system.hub.enabled` |
| Хаб сообщества | **Зеркала хаба** | `system.hub.urls` |
| Хаб сообщества | **Публичный ключ хаба** | `system.hub.public_key` |

API-ключ AI-помощника в этот файл не входит: b4 хранит его в `ai_secrets.json`.

### Редактор сета {#paths-sets}

| Вкладка | Поле | Путь |
| --- | --- | --- |
| Цели | **Фильтр версии IP** | `targets.ip_version` |
| Цели | **Только по домену** | `targets.domain_only` |
| TCP > Основные | **Фильтр портов** | `tcp.dport_filter` |
| TCP > Фейкинг | **Сгенерированный payload** | `faking.payload_file`, путь вида `captures/tls_youtube_com.bin` |
| UDP | **Совпадение QUIC** | `udp.filter_quic` |
| UDP | **Фильтр портов** | `udp.dport_filter` |
| UDP | **Режим действия** | `udp.mode` |
| UDP | **Payload фейкового пакета** | `udp.fake_payload_file` |
| DNS & Маршрутизация | **Включить перенаправление DNS** | `dns.enabled` |
| DNS & Маршрутизация | **IP DNS-сервера** | `dns.target_dns` |
| DNS & Маршрутизация | **URL DNS-over-HTTPS** | `dns.doh_url` |
| DNS & Маршрутизация | **Фрагментировать DNS-запросы** | `dns.fragment_query` |
| DNS & Маршрутизация | **Отдавать SERVFAIL, если резолвер недоступен** | `dns.strict` |
| DNS & Маршрутизация | **Убирать ключи ECH из ответов DNS** | `dns.strip_ech` |
| DNS & Маршрутизация | **Закреплённые адреса** | `dns.pins`, имя и его адреса |
| DNS & Маршрутизация | **Включить DSCP для сета** | `dscp.enabled` |
| DNS & Маршрутизация | **Значение DSCP** | `dscp.value` |
| Дискавери | Список адресов | `discovery.urls` |
| Дискавери | **Поддерживать работу сета с помощью мониторинга** | `discovery.watchdog` |

### Дашборд {#paths-dashboard}

| Элемент | Путь |
| --- | --- |
| Раскладка, сохранённая через **Настроить** | `ui.dashboard` |

### Настройки без поля в интерфейсе {#settings-without-a-field}

Эти настройки есть только в файле.

| Путь | Что делает | По умолчанию |
| --- | --- | --- |
| `system.web_server.mcp.allowed_origins` | Origin страниц браузера, которые [точка доступа MCP](./api.md#mcp-server) принимает помимо собственного адреса b4. Одиночная `*` принимает любой origin | Пусто |
| `system.checker.watchdog.heal_validation_tries` | Сколько загрузок подряд должна пройти стратегия при поиске во время восстановления | `3` |
| `system.checker.watchdog.verify_tries` | Сколько попыток получает каждый адрес в проверке после того, как восстановление записало стратегию | `2` |
| `system.checker.discovery_flow_mark`, `system.checker.discovery_injected_mark` | Метки тестового трафика Дискавери, см. [Метки пакетов](../guides/marks.md) | Метка очереди плюс 1 и плюс 2 |
| `queue.tun.route_table` | Фиксированная таблица маршрутизации для движка TUN вместо свободной, которую выбирает b4, см. [Метки пакетов](../guides/marks.md) | Выбирает b4 |
| `sets[].routing.fwmark`, `sets[].routing.table` | Закреплённые метка и таблица для сета, который маршрутизирует трафик. Используются, только когда заданы обе, см. [Метки пакетов](../guides/marks.md) | Выбирает b4 |

## Правка вручную

`version` в начале файла - версия формата конфигурации. b4 читает её, чтобы понять, что нужно мигрировать, поэтому при правке остального её не меняют.

:::warning
Значения в файле проверяются только при запуске b4, и файл, не прошедший проверку, останавливает b4 с ошибкой `invalid configuration`. b4 читает файл только при запуске, поэтому ручная правка вступает в силу после перезапуска. Любое сохранение до перезапуска записывает в файл работающую конфигурацию, и ручная правка теряется. Сохраняют веб-интерфейс, MCP, восстановление мониторингом, закачка геоданных и обновление префиксов ASN.
:::

## Миграции

Когда формат меняется между выпусками, b4 мигрирует файл при запуске: новые поля приходят со значениями по умолчанию, переименованные переносятся. Прежде чем что-либо изменить, b4 кладёт рядом резервную копию с именем по версии, с которой мигрирует, например `b4.json.v51.bak`.

Файл, который не удаётся прочитать при запуске, например обрезанный при пропадании питания или с синтаксической ошибкой JSON, копируется рядом как `b4.json.corrupt`. Если там уже лежит копия с другим содержимым, имя будет `b4.json.corrupt.1` и далее. Лог называет эту копию. Затем b4 запускается со значениями по умолчанию, а следующее сохранение заменяет исходный файл, так что непрочитанные настройки остаются только в копии.

Файл, у которого `version` выше, чем понимает запущенная сборка, загружается как есть, с предупреждением. Незнакомые этой сборке настройки отбрасываются при следующем сохранении.
