---
sidebar_position: 2
title: Установка
---

# Установка

b4 устанавливается на Linux-устройства: серверы, компьютеры и роутеры. Доступные способы:

- [Linux](./linux) - универсальная установка на любой Linux-дистрибутив
- [OpenWRT](./openwrt) - роутеры с прошивкой OpenWRT
- [ASUS Merlin](./merlin) - роутеры ASUS с прошивкой Merlin
- [Keenetic](./keenetic) - роутеры Keenetic
- [MikroTik](./mikrotik) - RouterOS 7.x через контейнеры
- [Docker](./docker) - запуск в Docker-контейнере

После установки b4 доступен через веб-интерфейс в браузере (по умолчанию порт `7000`).

## Обновление и удаление {#update-remove}

### Обновление

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --update
```

Или обновление до конкретной версии:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- v1.46.5
```

При обновлении текущий бинарник сохраняется как резервная копия, сервис останавливается, заменяется на новую версию и запускается снова. Конфигурация не затрагивается.

### Удаление

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --remove
```

При удалении:
1. Сервис останавливается и убирается из автозапуска
2. Бинарник удаляется
3. Конфигурация сохраняется или удаляется в зависимости от ответа на вопрос установщика об удалении `/etc/b4` или `/opt/etc/b4`

### Диагностика

Для вывода информации о системе, установленной версии и состоянии модулей ядра:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --sysinfo
```
