
# Architecture Blueprint & Technical Specification
## Project Name: Apex (macOS High-Performance Proxy Client)

---

## 1. Обзор архитектуры

Архитектура системы построена на полной изоляции пользовательского интерфейса от сетевого ядра. Это гарантирует:
1. **Живучесть туннеля:** Падение или перезапуск GUI не обрывает системный VPN-туннель и сетевые соединения.
2. **Нулевую зависимость от Xcode:** Никаких проприетарных `.xcodeproj`, платных Apple Developer аккаунтов и жестких лимитов NetworkExtension (Jetsam 50MB RAM).
3. **Максимальную производительность:** Ядро работает как системный демон (`launchd` или root-процесс), имея прямой доступ к BSD-сокетам, а GUI работает в пространстве пользователя, общаясь с ядром через высокоскоростной Unix Domain Socket (UDS).

```
                      +------------------------------------------+
                      |         Apex.app (User Space GUI)        |
                      |   Swift 6 / AppKit + SwiftUI (No-Xcode)   |
                      |   • Status Item (Menu Bar)               |
                      |   • Telemetry Consumer (DisplayLink)     |
                      |   • Connection & Rules Inspector         |
                      +------------------------------------------+
                                           |
                                [Unix Domain Socket]
                             /var/run/apex/apex.sock
                         (JSON-RPC Control + Stream Telemetry)
                                           |
                                           v
                      +------------------------------------------+
                      |        apex-cored (Root System Daemon)    |
                      |        Golang 1.23+ with sing-box Core   |
                      |                                          |
                      |  • Fake-IP & Split-DNS Engine            |
                      |  • VLESS-Reality & Hysteria 2 Stacks     |
                      |  • Kernel PCB Inspector (net.inet.tcp)   |
                      |  • Memory Guard (GODEBUG, GOMEMLIMIT)    |
                      +------------------------------------------+
                               |                      |
                        [L3 IP Packets]        [Physical Sockets]
                               |                 (IP_BOUND_IF)
                               v                      v
                      +------------------+   +-------------------+
                      |   /dev/utun100   |   | Physical Adapter  |
                      | (Darwin TUN dev) |   |    (en0 / en4)    |
                      +------------------+   +-------------------+
```

---

## 2. Структура репозитория (No-Xcode Layout)

Проект полностью управляется через консольные утилиты (`swift build`, `go build`, `make`). В корне нет ни одного файла `.xcodeproj` или `.xcworkspace`.

```text
apex/
├── Makefile                     # Единая точка сборки всего проекта
├── Package.swift                # Swift Package Manager манифест для GUI
├── scripts/
│   ├── bundle_app.sh            # Скрипт упаковки SPM-бинарника в Apex.app
│   └── install_helper.sh        # Скрипт регистрации демона в launchd
├── core/                        # Golang: Демон и интеграция sing-box
│   ├── go.mod
│   ├── go.sum
│   ├── cmd/
│   │   └── apex-cored/
│   │       └── main.go          # Входная точка системного демона
│   └── pkg/
│       ├── config/              # Генерация и валидация sing-box JSON-конфигов
│       ├── engine/              # Управление жизненным циклом sing-box BoxService
│       ├── ipc/                 # Unix Domain Socket сервер (RPC + Telemetry)
│       └── inspector/           # Парсер Darwin sysctl PCB (поиск PID процесса)
├── ui/                          # Swift: Нативный интерфейс
│   └── Sources/
│       └── ApexUI/
│           ├── main.swift       # Инициализация NSApplication (без Storyboards)
│           ├── AppDelegate.swift
│           ├── Menu/
│           │   └── StatusBarController.swift
│           ├── Views/
│           │   ├── DashboardView.swift
│           │   ├── ConnectionsView.swift
│           │   └── TrafficGraphView.swift
│           ├── Services/
│           │   ├── IPCClient.swift          # Клиент UDS сокета
│           │   ├── TelemetryStreamer.swift  # Coalescing данных по CVDisplayLink
│           │   └── SleepWatcher.swift       # Обработка событий сна системы
│           └── Models/
│               └── IPCMessages.swift
└── assets/
    ├── Info.plist               # Метаданные бандла приложения
    └── AppIcon.icns             # Иконка приложения
```

---

## 3. Системный демон сетевого ядра (`apex-cored`)

Демон запускается с правами `root` через `launchd` или временный эскалатор прав (`sudo`), что дает ему возможность создавать интерфейсы `/dev/utun`, перенастраивать системные маршруты и инспектировать сокеты ядра.

### 3.1. Тюнинг Go-рантайма на Darwin
Для предотвращения накопления неиспользуемой физической памяти (RSS) и защиты от утечек памяти под нагрузкой сотен мегабит:
1. Запуск демона всегда сопровождается переменной окружения:
   ```bash
   GODEBUG=madvdontneed=1
   ```
   *Это заставляет macOS очищать страницы памяти немедленно через `MADV_DONTNEED`, а не помечать их как `MADV_FREE`.*
2. В рантайме жестко выставляется потолок памяти:
   ```go
   import "runtime/debug"

   func init() {
       // Мягкий лимит памяти в 96 МБ (с запасом для буферов Hysteria 2)
       debug.SetMemoryLimit(96 * 1024 * 1024)
       // Более частый запуск сборщика мусора при аллокациях
       debug.SetGCPercent(30)
   }
   ```

### 3.2. Нативная интеграция TUN (sing-box)
Демон не вызывает внешние CLI-утилиты через `exec`. Вместо этого `sing-box` импортируется напрямую как Go-библиотека (`github.com/sagernet/sing-box`).

Конфигурация Inbound-туннеля формируется динамически:
```json
{
  "inbounds": [
    {
      "type": "tun",
      "tag": "tun-in",
      "interface_name": "utun100",
      "address": ["172.19.0.1/30", "fdfe:dcba:9876::1/126"],
      "mtu": 9000,
      "auto_route": true,
      "strict_route": true,
      "stack": "system",
      "sniff": true,
      "sniff_override_destination": true
    }
  ]
}
```
* **`stack: "system"`**: Использует системный стек Darwin BSD, обеспечивая максимальную пропускную способность с наименьшим оверхедом на контекстные переключения.
* **`auto_route: true`**: `sing-box` самостоятельно настраивает системную таблицу маршрутизации через BSD routing sockets (`AF_ROUTE`), исключая необходимость ручного дергания команд `route add`.

### 3.3. Разрешение PID и приложений (Kernel PCB Inspection)
Для функции отображения приложений в Active Connections демон опрашивает структуры ядра через `sysctlbyname`:
* MIB имя: `net.inet.tcp.pcblist_n` и `net.inet.udp.pcblist_n`.
* Демон сопоставляет локальный порт входящего пакета (`inp_lport`) с владельцем сокета (`xso_e_pid`).
* Результат сопоставления (`port -> pid`) кэшируется в `sync.Map` с TTL 2 секунды. Это исключает нагрузку на CPU при потоке пакетов.

---

## 4. Клиентский интерфейс (`Apex.app`)

Интерфейс строится исключительно на Swift Package Manager без создания `.xcodeproj`.

### 4.1. SPM Конфигурация (`Package.swift`)
```swift
// swift-tools-version: 5.10
import PackageDescription

let package = Package(
    name: "Apex",
    platforms: [.macOS(.v14)],
    products: [
        .executable(name: "ApexUI", targets: ["ApexUI"])
    ],
    targets: [
        .executableTarget(
            name: "ApexUI",
            path: "ui/Sources/ApexUI",
            linkerSettings: [
                .linkedFramework("AppKit"),
                .linkedFramework("SwiftUI"),
                .linkedFramework("QuartzCore")
            ]
        )
    ]
)
```

### 4.2. Инициализация и Menu Bar
В `main.swift` создается `NSApplication` в режиме агента (LSUIElement — без иконки в Dock по умолчанию, только Menu Bar):
* `NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)` размещает иконку и индикатор скорости.
* Основное окно (Connections, Dashboard) поднимается в виде легковесного `NSWindow` с контентом на `SwiftUI`, обернутым в `NSHostingView`.

### 4.3. Каденция отрисовки телеметрии (CVDisplayLink)
Поток данных от демона о текущих скоростях и соединениях может приходить с частотой до 1000 сообщений в секунду. Чтобы не забивать главный поток UI (`MainActor`):
1. События сокета накапливаются в потокобезопасный кольцевой буфер в фоновом потоке.
2. Таймер `CVDisplayLink` синхронизируется с частотой обновления экрана (60 Гц / 120 Гц ProMotion).
3. На каждый такт экрана буфер пачкой («coalesced batch») переносится в стейт SwiftUI. Это полностью устраняет фризы интерфейса.

---

## 5. Межпроцессное взаимодействие (IPC)

Общение GUI и демона ядра осуществляется через Unix Domain Socket по пути `/var/run/apex/apex.sock`.

### 5.1. Модель безопасности сокета
* Каталог `/var/run/apex` создается демоном при старте.
* Права на сокет: `0660`.
* Владелец: `root:admin`.
* Любой пользователь из группы `admin` (стандартная группа пользователя macOS) может подключаться к сокету без необходимости запускать GUI через `sudo`.

### 5.2. Протокол передачи
Сокет работает в дуплексном режиме с передачей JSON-пакетов, разделенных символом переноса строки (`\n` framing):
1. **Командный канал (Request-Response):** Запросы конфигурации, переключение узлов, старт/стоп туннеля, ручные пинги.
2. **Потоковый канал (Push Stream):** Демон непрерывно транслирует в открытый сокет метрики скорости, новые соединения и системные логи.

---

## 6. Механизмы устойчивости (Zero-Drop Engine)

### 6.1. Машина состояний сна (Sleep / Wake)
В `SleepWatcher.swift` регистрируются системные хуки:
```swift
NSWorkspace.shared.notificationCenter.addObserver(
    self, selector: #selector(willSleep), 
    name: NSWorkspace.willSleepNotification, object: nil
)
NSWorkspace.shared.notificationCenter.addObserver(
    self, selector: #selector(didWake), 
    name: NSWorkspace.didWakeNotification, object: nil
)
```
* **Перед сном (`willSleep`):** GUI шлет команду в сокет: `{"command": "pause_tunnel"}`. Демон приостанавливает вычитку из `/dev/utun`, останавливает heartbeat-пакеты Hysteria 2 и замораживает соединения, предотвращая их таймаут системой.
* **При пробуждении (`didWake`):** GUI ждет подтверждения от `NWPathMonitor`, что физический интерфейс (например, `en0`) получил IP и шлюз. После этого шлется команда `{"command": "resume_tunnel"}`. Демон выполняет валидацию сокетов и обновляет QUIC-сессии.

### 6.2. Роуминг интерфейсов (Wi-Fi ↔ LTE Hotspot)
* Демон при открытии исходящих сокетов связывает их с реальным физическим адаптером через флаг `setsockopt(fd, IPPROTO_IP, IP_BOUND_IF, &ifIndex)`.
* При изменении маршрута по умолчанию индекс интерфейса обновляется без перезапуска ядра, что исключает циклическую маршрутизацию («loopback trap»).

---

## 7. Сборка и упаковка (No-Xcode Build Pipeline)

Весь процесс полностью автоматизирован через единый `Makefile`:

```makefile
SHELL := /bin/bash
BUILD_DIR := ./build
APP_BUNDLE := $(BUILD_DIR)/Apex.app

.PHONY: all core ui app install clean

all: core ui app

core:
	@echo "==> Building Go Core Daemon (apex-cored)..."
	@cd core && GODEBUG=madvdontneed=1 go build \
		-tags "with_gvisor with_quic with_dhcp with_wireguard with_ech with_utls" \
		-ldflags="-s -w" \
		-o ../$(BUILD_DIR)/bin/apex-cored ./cmd/apex-cored

ui:
	@echo "==> Building Swift UI via SPM..."
	@swift build -c release --arch arm64 --arch x86_64
	@mkdir -p $(BUILD_DIR)/bin
	@cp .build/apple/Products/Release/ApexUI $(BUILD_DIR)/bin/ApexUI

app:
	@echo "==> Packaging into $(APP_BUNDLE)..."
	@./scripts/bundle_app.sh $(BUILD_DIR)/bin/ApexUI $(APP_BUNDLE)

install:
	@echo "==> Installing Privileged Daemon..."
	@sudo ./scripts/install_helper.sh $(BUILD_DIR)/bin/apex-cored
	@echo "==> Copying Apex.app to /Applications..."
	@cp -R $(APP_BUNDLE) /Applications/

clean:
	@rm -rf .build $(BUILD_DIR)
```

Скрипт `scripts/bundle_app.sh` создает нативный бандл macOS за считанные миллисекунды:
1. Создает дерево: `Apex.app/Contents/{MacOS,Resources}`.
2. Копирует скомпилированный SPM-бинарник в `Contents/MacOS/ApexUI`.
3. Генерирует валидный `Info.plist` с указанием `LSUIElement = true` (Menu Bar Agent), минимальной версии macOS и бандл-идентификатора.
4. Копирует файл иконки `AppIcon.icns`.
```
