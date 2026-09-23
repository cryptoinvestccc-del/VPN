# Мониторинг сервера для сайта BESY

Карточка «Сервер сейчас» на первом экране сайта показывает живые данные твоего сервера Amnezia: сколько человек подключено, скорость, загрузку процессора, аптайм. Данные обновляются раз в секунду.

Работает это так. На сервер Amnezia ставится маленькая программа `besy-agent`. Раз в секунду она читает `awg show all dump` из контейнера AmneziaWG и нагрузку системы. Сайт забирает у неё только общие цифры.

**Что наружу не уходит никогда:** ключи, IP-адреса клиентов и любые данные по отдельным пользователям. Агент ничего не меняет на сервере и ничего не пишет на диск.

## Установка на сервер Amnezia

Нужен root. Выполни по порядку.

**1. Скопируй программу на сервер** (с компьютера, где лежит файл):

```sh
scp besy-agent-linux-amd64 root@АДРЕС_СЕРВЕРА:/usr/local/bin/besy-agent
```

**2. На сервере сделай её исполняемой и проверь, что она видит Amnezia:**

```sh
chmod +x /usr/local/bin/besy-agent
docker ps --format '{{.Names}}' | grep amnezia-awg
```

Команда должна вывести `amnezia-awg2` или `amnezia-awg`. Если не выводит ничего, значит, протокол AmneziaWG на этом сервере не запущен.

**3. Поставь службу, чтобы агент запускался сам:**

```sh
cat > /etc/systemd/system/besy-agent.service <<'UNIT'
[Unit]
Description=BESY server status agent (reads AmneziaWG load for the website)
After=docker.service network-online.target
Wants=network-online.target

[Service]
Type=simple
# No account of its own on disk; the docker group is what lets it run
# `docker exec ... awg show all dump`. It reads, and changes nothing.
DynamicUser=yes
SupplementaryGroups=docker
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true

# BESY_AGENT_TOKEN lives here, readable by root only.
EnvironmentFile=-/etc/besy-agent.env
ExecStart=/usr/local/bin/besy-agent -listen ${BESY_AGENT_LISTEN}
Environment=BESY_AGENT_LISTEN=127.0.0.1:9180
Restart=always
RestartSec=2s

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now besy-agent
```

**4. Проверь:**

```sh
sleep 3
curl -s http://127.0.0.1:9180/v1/snapshot
```

Должен прийти JSON с `clients_online`, `throughput_mbps` и другими полями. Если пришло `no fresh sample`, смотри журнал: `journalctl -u besy-agent -n 20`.

## Подключение сайта

Здесь два случая, в зависимости от того, где будет работать сайт.

### Сайт на том же сервере, что и Amnezia

Ничего дополнительно настраивать не нужно. Сайт запускается так:

```sh
obfsweb -addr 0.0.0.0:80 -agent http://127.0.0.1:9180/v1/snapshot
```

### Сайт на другом сервере

Агенту нужно разрешить принимать запросы снаружи, но только с паролем.

На **сервере Amnezia**:

```sh
TOKEN=$(openssl rand -hex 24)
echo "BESY_AGENT_TOKEN=$TOKEN" > /etc/besy-agent.env
echo "BESY_AGENT_LISTEN=0.0.0.0:9180" >> /etc/besy-agent.env
chmod 600 /etc/besy-agent.env
systemctl restart besy-agent
echo "$TOKEN"   # этот пароль понадобится для сайта
```

Закрой порт 9180 для всех, кроме сервера сайта. Например, в `ufw` (порядок строк важен: сначала разрешение, потом запрет):

```sh
ufw allow from IP_СЕРВЕРА_САЙТА to any port 9180 proto tcp
ufw deny 9180/tcp
```

На **сервере сайта**:

```sh
BESY_AGENT_TOKEN=ТОТ_САМЫЙ_ПАРОЛЬ obfsweb -addr 0.0.0.0:80 \
  -agent http://АДРЕС_СЕРВЕРА_AMNEZIA:9180/v1/snapshot
```

Агент сам откажется слушать внешний адрес, если пароль не задан. Открыть его миру случайно не получится.

## Что видно на сайте

| Ситуация | Бейдж на карточке |
|---|---|
| Агент отвечает | зелёный «онлайн», цифры обновляются каждую секунду |
| Сервер или агент недоступен | жёлтый «сервер не отвечает» |
| Сайт запущен без `-agent` | серый «демонстрационные данные» |

Сайт спрашивает агента не чаще раза в секунду, сколько бы посетителей ни было на странице. Вкладка, которую никто не видит, запросы не шлёт.
