# Telifon-VPN

Легковесный нативный macOS VPN и прокси-клиент с графическим интерфейсом на **SwiftUI** и высокопроизводительным встроенным ядром на базе **sing-box** (Go).

Разработан для работы без использования тяжелой IDE Xcode: проект полностью собирается через **Swift Package Manager (SPM)**, **Go Toolchain** и **Makefile**.

---

## Особенности архитектуры

- **UI**: 100% нативный интерфейс для macOS 14.0+ (Menu Bar Extra / Status Bar + Dashboard) на Swift (AppKit + SwiftUI).
- **Core**: Высокопроизводительный системный демон `telifon-cored` со встроенной библиотекой `sing-box` (VLESS-Reality, Hysteria 2, WireGuard, ShadowTLS).
- **Сетевой стек**: 
  - Режим **TUN** (`/dev/utun*` с автоматической маршрутизацией Darwin и защитой от DNS-утечек Fake-IP).
  - Пакетный I/O и низкие задержки.
- **IPC**: Быстрое межпроцессное взаимодействие через **Unix Domain Socket** (`/var/run/telifon/telifon.sock`) с потоковым кадрированием JSON (NDJSON).
- **Разделение прав**: GUI работает под обычным непривилегированным пользователем; только сетевой демон запускается с правами `root`.
- **Сборка**: Автономная сборка через CLI (`make core`, `make ui`, `make app`).

---

## Требования к окружению

- macOS 14.0+ (Sonoma, Sequoia) на Apple Silicon (`arm64`) или Intel (`x86_64`)
- Apple Command Line Tools (`clang`, `swiftc`, `swift-build`)
- Go 1.23+ (`go`)
- GNU Make (`make`)

---

## Сборка и запуск

```bash
# Собрать все компоненты и сгенерировать Telifon.app
make all

# Сборка только демона сетевого ядра (Go)
make core

# Сборка только интерфейса (Swift SPM)
make ui

# Упаковка бандла build/Telifon.app
make app

# Очистка артефактов сборки
make clean
```

---

## Документация проекта

- [PRD.md](PRD.md) — Продуктовые требования и целевые метрики.
- [ARCHITECTURE.md](ARCHITECTURE.md) — Техническая архитектура и системный дизайн.
- [IPC_SPEC.md](IPC_SPEC.md) — Спецификация межпроцессного взаимодействия и протокол UDS сокета.
- [AGENT_ROADMAP.md](AGENT_ROADMAP.md) — Пошаговый план реализации по фазам.
- [AGENTS.md](AGENTS.md) — Правила и соглашения для автономных агентов разработки.

---

## Лицензия
MIT
