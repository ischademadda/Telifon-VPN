# Telifon-VPN

Легковесный нативный macOS VPN и прокси-клиент с графическим интерфейсом на **SwiftUI** и ядром **sing-box**.

Разработан для работы без необходимости установки полного пакета Xcode: проект собирается через **Swift Package Manager (SPM)** и системные **Command Line Tools**.

## Особенности архитектуры

- **UI**: Нативный интерфейс для macOS (Menu Bar Extra / Status Bar) на SwiftUI.
- **Core**: Высокопроизводительное ядро [sing-box](https://sing-box.sagernet.org/) (VLESS, Shadowsocks, WireGuard, Hysteria2, TUIC, Trojan).
- **Сетевой стек**: 
  - Режим **TUN** (`/dev/utun*` с автоматической маршрутизацией и управлением системным DNS).
  - Режим **System Proxy** (HTTP / SOCKS5 через `networksetup`).
- **IPC**: Взаимодействие со встроенным REST / Clash API ядра через `URLSession` и WebSockets.
- **Сборка**: Автономная сборка через CLI (`swift build`, `just` / `make`).

## Быстрый старт

### Требования
- macOS 14.0+ (Sonoma, Sequoia) на Apple Silicon / Intel
- Apple Command Line Tools (`xcode-select --install`)
- Homebrew (`sing-box`, `just`, `jq`)
- Go 1.22+

### Сборка и запуск
```bash
# Проверка зависимостей
just check

# Запуск приложения в режиме разработки
just run
```

## Лицензия
MIT
