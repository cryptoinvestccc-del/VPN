# Обфускатор WireGuard-трафика

Прозрачный UDP-прокси, который прячет форму WireGuard-трафика от DPI, не
трогая криптографию самого WireGuard. Подробный дизайн и модель угрозы —
в [`docs/DESIGN.md`](docs/DESIGN.md).

Суть: WireGuard шифрует данные как обычно (Curve25519, ChaCha20-Poly1305,
forward secrecy — всё как в оригинале). Этот проект оборачивает уже
зашифрованные WG-пакеты в ещё один слой: AEAD-шифрование PSK-ключом со
случайным паддингом и мусорными пакетами, чтобы DPI не мог опознать
трафик по сигнатуре/длинам/поведению.

## Компоненты

- `internal/obfuscator` — ядро: `Wrap`/`Unwrap` пакета (nonce + ChaCha20-
  Poly1305 + случайный паддинг), генерация junk-пакетов.
- `internal/transport` — два прокси: `RunClient` (локально принимает
  WG-пакеты, шлёт наружу обфусцированными) и `RunServer` (принимает
  обфусцированные, форвардит на локальный WG-сервер).
- `cmd/obfsclient`, `cmd/obfsserver` — CLI-обёртки с конфигом из YAML.

## Быстрый старт (тестовый стенд)

```bash
go build ./...
go test ./...
```

## Разворачивание

### 1. На сервере (VPS)

Настрой обычный WireGuard-сервер, но привяжи его **только к localhost**
(не к публичному интерфейсу) — публично торчать должен только
`obfsserver`:

```ini
# /etc/wireguard/wg0.conf
[Interface]
ListenPort = 51821
Address = 10.0.0.1/24
PrivateKey = ...
# без публичного форвардинга порта — только 127.0.0.1
```

Скопируй `examples/obfsserver.yaml` → `obfsserver.yaml`, впиши свой PSK
(`openssl rand -base64 32`) и запусти:

```bash
go run ./cmd/obfsserver -config obfsserver.yaml
```

Открой публично только порт `listen_wire_addr` (по умолчанию 51820/udp) в
файрволе — порт реального WG (51821) должен остаться закрытым снаружи.

### 2. На клиенте

Настрой WireGuard-клиента с `Endpoint = 127.0.0.1:51821` (локальный порт,
на который слушает `obfsclient`). Скопируй `examples/obfsclient.yaml`,
впиши тот же PSK и IP сервера, запусти:

```bash
go run ./cmd/obfsclient -config obfsclient.yaml
```

Подними WG-интерфейс (`wg-quick up wg0`) — трафик пойдёт через
`obfsclient` → обфускация → `obfsserver` → реальный WG.

### Режим TLS (устойчивее к активному DPI-пробингу)

По умолчанию (`mode: udp`) канал — просто обфусцированный UDP: DPI видит
UDP-трафик на нестандартном порту, что само по себе подозрительно в
странах со строгой фильтрацией. Режим `mode: tls` заворачивает трафик в
настоящее TLS 1.3-соединение — порт сервера отвечает как обычный HTTPS,
включая полноценное рукопожатие, что проходит и активный пробинг DPI.

На сервере:

```bash
go run ./cmd/gencert -cn www.example.com -cert server.crt -key server.key
```

Скопируй строку `pinned_cert_sha256` из вывода. Используй
`examples/obfsserver-tls.yaml` и `examples/obfsclient-tls.yaml` как
шаблоны — впиши туда PSK, IP сервера и этот pin. Сертификат
самоподписанный: доверие устанавливается не через публичный CA, а через
pinning отпечатка (как в Trojan/ShadowTLS) — клиент откажется соединяться,
если отпечаток не совпадёт (защита от MITM).

## Roadmap / что дальше

См. раздел Roadmap в [`docs/DESIGN.md`](docs/DESIGN.md): TLS-мимикрия
поверх TCP для прохождения строгого DPI, ротация PSK, systemd-юниты,
статистический аудит трафика (сравнение с необфусцированным WG и с
чистым шумом через Wireshark).
