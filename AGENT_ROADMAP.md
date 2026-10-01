# Autonomous Agent Implementation Roadmap
## Project Name: TELIFON (macOS High-Performance Proxy Client)

---

## 1. Инструкции для ИИ-агента (Rules of Engagement)

Перед началом работы агент (Cursor, Claude Code, Devin и др.) обязан следовать правилам:
1. **Строго без Xcode IDE:** Запрещено создавать `.xcodeproj`, `.xcworkspace` или вызывать `xcodebuild` без крайней необходимости. Все операции производятся через `swift build`, `go build`, `make` и shell-скрипты.
2. **Идемпотентность и атомарность:** Каждая фаза должна завершаться компилируемым и проверяемым кодом. Не переходить к следующей фазе без прохождения критериев приемки (Acceptance Criteria).
3. **Разделение прав (Zero Root in GUI):** Пользовательский интерфейс на Swift **никогда** не должен требовать запуска через `sudo`. Только системный демон `telifon-cored` работает с привилегиями root.
4. **Контракты IPC:** Строго соблюдать структуры данных, описанные в `IPC_SPEC.md`.

---

## 2. Фазы разработки

```
 +-----------------------------------------------------------------------+
 | Phase 0: Scaffolding, Makefile, Toolchain & Directory Structure       |
 +-----------------------------------------------------------------------+
                                     |
                                     v
 +-----------------------------------------------------------------------+
 | Phase 1: Go Core Engine (sing-box wrapper, TUN setup, Memory Tuning)  |
 +-----------------------------------------------------------------------+
                                     |
                                     v
 +-----------------------------------------------------------------------+
 | Phase 2: Darwin PCB Inspector & IPC Unix Socket Server                |
 +-----------------------------------------------------------------------+
                                     |
                                     v
 +-----------------------------------------------------------------------+
 | Phase 3: Swift SPM Project (AppKit Menu Bar + SwiftUI Dashboard)      |
 +-----------------------------------------------------------------------+
                                     |
                                     v
 +-----------------------------------------------------------------------+
 | Phase 4: Sleep/Wake Roaming Engine & App Bundling (`scripts/bundle`)   |
 +-----------------------------------------------------------------------+
                                     |
                                     v
 +-----------------------------------------------------------------------+
 | Phase 5: End-to-End Integration, Reality & Hy2 Verification           |
 +-----------------------------------------------------------------------+
```

---

### Phase 0: Инициализация окружения и Makefile

#### Задачи
1. Создать файловую структуру согласно `ARCHITECTURE.md`:
   * `mkdir -p core/cmd/telifon-cored core/pkg/{config,engine,ipc,inspector}`
   * `mkdir -p ui/Sources/TelifonUI/{Menu,Views,Services,Models}`
   * `mkdir -p scripts assets build/bin`
2. Создать корневой `Package.swift` с таргетом `TelifonUI` (платформа `macOS(.v14)`).
3. Инициализировать `go.mod` в каталоге `core` (`module telifon-core`).
4. Написать базовый корневой `Makefile` с целями `core`, `ui`, `app`, `clean`.

#### Критерии приемки Phase 0
* Команда `make clean && make ui` успешно собирает тестовый Swift Hello World через `swift build`.
* Команда `make core` собирает заглушку Go-бинарника в `build/bin/telifon-cored`.

---

### Phase 1: Сетевое ядро (Golang + sing-box)

#### Задачи
1. **Зависимости:** В `core/go.mod` подключить `github.com/sagernet/sing-box` актуальной версии и инициализировать сборку с тегами:
   `-tags "with_gvisor with_quic with_dhcp with_wireguard with_ech with_utls"`.
2. **Memory Guard:** В `core/cmd/telifon-cored/main.go` внедрить рантайм-настройки памяти:
   ```go
   debug.SetMemoryLimit(96 * 1024 * 1024)
   debug.SetGCPercent(30)
   ```
3. **Генератор конфига (`core/pkg/config`):**
   * Написать генератор валидного JSON sing-box с `inbound` типа `tun` (`interface_name: utun100`, `auto_route: true`, `stack: system`).
   * Реализовать секцию DNS с Fake-IP пулом `198.18.0.0/15` и правилами раздельного резолвинга (Direct для `.local`, DoH для прокси).
   * Реализовать генераторы `outbound` для **VLESS + Reality (uTLS)** и **Hysteria 2 (Brutal / Salamander)**.
4. **Контроллер движка (`core/pkg/engine`):**
   * Создать обертку над `box.New(box.Options{...})` с методами `Start()`, `Close()`, `Pause()`, `Resume()`.

#### Критерии приемки Phase 1
* Тестовый запуск `sudo ./build/bin/telifon-cored --test-run` поднимает интерфейс `utun100` (видно в выводе `ifconfig utun100`), успешно выполняет хэндшейк VLESS/Hy2 и завершается по сигналу `SIGINT` с корректным удалением интерфейса.

---

### Phase 2: Darwin PCB Inspector и IPC-сервер

#### Задачи
1. **Парсер сокетов ядра (`core/pkg/inspector`):**
   * Реализовать вызов `sysctlbyname("net.inet.tcp.pcblist_n")` и сопоставление `lport -> PID`.
   * Добавить LRU-кэш на `sync.Map` с автоматическим сбросом устаревших записей (TTL 2 секунды).
   * Реализовать получение имени процесса и bundle ID через `libproc` (`proc_pidpath`).
2. **IPC Сервер (`core/pkg/ipc`):**
   * Поднять сервер на Unix Domain Socket по пути `/var/run/telifon/telifon.sock` (с правами `0660` и группой `admin`).
   * Реализовать NDJSON-парсер входящих запросов (`engine.start`, `engine.stop`, `engine.pause`, `node.switch`, `routing.set_mode`).
   * Реализовать потоковую рассылку телеметрии (`telemetry.metrics` каждые 500 мс и `telemetry.connections`).

#### Критерии приемки Phase 2
* Демон запускается под `sudo`.
* Из обычного терминала (без sudo) выполняется команда:
  ```bash
  echo '{"type":"request","id":"1","action":"system.ping"}' | nc -U /var/run/telifon/telifon.sock
  ```
  В ответ приходит валидный JSON со статусом демона и версией sing-box.

---

### Phase 3: Нативный интерфейс (SwiftUI / AppKit без Xcode)

#### Задачи
1. **Модели и IPC-клиент (`ui/Sources/TelifonUI/Services/IPCClient.swift`):**
   * Реализовать асинхронный клиент на Swift `actor` поверх POSIX сокета, поддерживающий чтение NDJSON потока через `AsyncStream`.
   * Написать структуры запросов и ответов согласно `IPC_SPEC.md`.
2. **Жизненный цикл без Interface Builder (`main.swift` & `AppDelegate.swift`):**
   * Инициализация `NSApplication` в режиме строки меню (без окна в Dock по умолчанию).
   * Создание `NSStatusItem` с иконкой и динамическим лейблом скорости (КБ/с, МБ/с).
3. **Menu Bar контроллер (`Menu/StatusBarController.swift`):**
   * Быстрое меню: Switch On/Off, выбор режима (Rule / Global / Direct), селектор узлов с индикацией пинга, кнопки "Dashboard" и "Quit".
4. **Dashboard и График трафика (`Views/`):**
   * Основное окно (SwiftUI, обернутое в `NSHostingView`).
   * Визуализация графика отдачи/загрузки в реальном времени.
   * Экран "Active Connections": виртуализированный список (`Table`), отображающий PID, имя процесса, иконку приложения, сокет назначения и сработавшее правило.
5. **Cadence Telemetry Engine (`Services/TelemetryStreamer.swift`):**
   * Синхронизация отрисовки телеметрии с частотой монитора через `CVDisplayLink`.

#### Критерии приемки Phase 3
* `swift build -c release` завершается без ворнингов.
* Бинарник запускается, появляется в строке меню macOS, корректно подключается к сокету демона и обновляет показатели скорости.

---

### Phase 4: Отказоустойчивость и скрипты бандлинга

#### Задачи
1. **Обработчик сна и роуминга (`Services/SleepWatcher.swift`):**
   * Перехват `NSWorkspace.willSleepNotification` -> отправка `engine.pause` в сокет.
   * Перехват `NSWorkspace.didWakeNotification` -> опрос `NWPathMonitor` -> отправка `engine.resume` после подтверждения сети.
2. **Скрипт бандлинга (`scripts/bundle_app.sh`):**
   * Сборка структуры директорий `Telifon.app/Contents/{MacOS,Resources}`.
   * Генерация `Info.plist` с ключами:
     * `LSUIElement = true` (Menu Bar Agent)
     * `CFBundleIdentifier = com.telifon.proxy`
     * `LSMinimumSystemVersion = 14.0`
3. **Скрипт установки демона (`scripts/install_helper.sh`):**
   * Создание Launchd-манифеста `/Library/LaunchDaemons/com.telifon.cored.plist`.
   * Регистрация демона в системе: `launchctl bootstrap system /Library/LaunchDaemons/com.telifon.cored.plist`.

#### Критерии приемки Phase 4
* Запуск `make app` создает валидный `build/Telifon.app`.
* Приложение открывается штатно по двойному клику или через `open build/Telifon.app`.
* При переводе Mac в режим сна и выходе из него туннель не зависает, сетевые соединения продолжают работать.

---

### Phase 5: Финальное тестирование и верификация

#### Проверочный чеклист
1. **VLESS Reality:** Проверка работы через сервер с Reality (проверка отсутствия блокировок по TLS-отпечатку).
2. **Hysteria 2:** Проверка достижения пиковой скорости на нестабильном канале (тест UDP Brutal CC).
3. **DNS-Leak Test:** Проверка на `browserleaks.com/dns` — должны отсутствовать утечки DNS провайдера, виден только IP прокси.
4. **Memory Footprint:** Проверка через `Activity Monitor` — процесс `TelifonUI` потребляет < 35 МБ RAM, процесс `telifon-cored` < 60 МБ RAM в простое.

---
