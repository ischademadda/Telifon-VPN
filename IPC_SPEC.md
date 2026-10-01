# Inter-Process Communication (IPC) Specification
## Project Name: Apex (macOS High-Performance Proxy Client)

---

## 1. Транспорт и модель подключения

Взаимодействие между непривилегированным интерфейсом (`ApexUI`) и корневым системным демоном (`apex-cored`) осуществляется через **Unix Domain Socket (UDS)** с потоковым кадрированием через перевод строки (`\n` framing / Newline-Delimited JSON).

### 1.1. Параметры сокета
* **Путь:** `/var/run/apex/apex.sock`
* **Фолбек путь (dev-режим без root):** `~/.apex/apex.sock`
* **Права доступа:** `0660` (`rw-rw----`)
* **Владелец:** `root:admin`
* **Формат данных:** UTF-8 encoded JSON. Каждое сообщение обязательно завершается символом перевода строки `\n` (`0x0A`).

```
 +------------------+                           +--------------------+
 | Swift UI Client  |                           |  apex-cored Daemon |
 +------------------+                           +--------------------+
          |                                                |
          |-------- [Request] id: 1, action: "status" ---->|
          |<------- [Response] id: 1, payload: {...} ------|
          |                                                |
          |<~~~~~~~ [Event] "telemetry.metrics" (cadence) ~|
          |<~~~~~~~ [Event] "telemetry.connections" ~~~~~~~|
          |<~~~~~~~ [Event] "telemetry.log" ~~~~~~~~~~~~~~~|
```

---

## 2. Базовые структуры конвертов (Message Envelopes)

Все сообщения внутри сокета принадлежат одному из трех типов: `request`, `response`, `event`.

### 2.1. Запрос от GUI к Демону (Request)
```json
{
  "type": "request",
  "id": "req-1001",
  "action": "engine.start",
  "payload": {}
}
```

### 2.2. Ответ от Демона к GUI (Response)
```json
{
  "type": "response",
  "id": "req-1001",
  "action": "engine.start",
  "success": true,
  "payload": {},
  "error": null
}
```
В случае ошибки поле `success` равно `false`, а объект `error` заполнен:
```json
{
  "type": "response",
  "id": "req-1001",
  "action": "engine.start",
  "success": false,
  "payload": null,
  "error": {
    "code": 2001,
    "message": "Failed to create utun100 interface: permission denied"
  }
}
```

### 2.3. Асинхронное событие от Демона к GUI (Event / Telemetry)
События не содержат `id` запроса и рассылаются демоном всем подключенным клиентам в режиме реального времени:
```json
{
  "type": "event",
  "topic": "telemetry.metrics",
  "timestamp": 1714567890123,
  "payload": {}
}
```

---

## 3. Командный протокол (Control Plane RPC)

### 3.1. Управление жизненным циклом туннеля

#### `system.ping`
Проверка доступности демона и версии API.
* **Payload:** `{}`
* **Response Payload:**
  ```json
  {
    "version": "1.0.0",
    "sing_box_version": "1.9.0",
    "uptime_seconds": 3620
  }
  ```

#### `engine.start`
Запуск туннеля `/dev/utun`, настройка маршрутов и запуск sing-box.
* **Payload:**
  ```json
  {
    "config_path": "/var/run/apex/active_config.json"
  }
  ```
* **Response Payload:**
  ```json
  {
    "status": "RUNNING",
    "interface": "utun100",
    "assigned_ipv4": "172.19.0.1"
  }
  ```

#### `engine.stop`
Полная остановка туннеля, корректное удаление системных маршрутов и возврат шлюза по умолчанию на физический адаптер.
* **Payload:** `{}`
* **Response Payload:**
  ```json
  {
    "status": "STOPPED"
  }
  ```

#### `engine.pause` (Sleep Transition)
Замораживает чтение из TUN и QUIC-keepalive перед сном macOS.
* **Payload:** `{}`
* **Response Payload:**
  ```json
  {
    "status": "PAUSED"
  }
  ```

#### `engine.resume` (Wake Transition)
Возобновляет работу туннеля после стабилизации физического сетевого интерфейса при выходе из сна.
* **Payload:**
  ```json
  {
    "rebind_interface": "en0"
  }
  ```
* **Response Payload:**
  ```json
  {
    "status": "RUNNING",
    "migrated": true
  }
  ```

---

### 3.2. Управление узлами и маршрутизацией

#### `node.switch`
Бесшовное переключение исходящего прокси-узла без разрыва `/dev/utun`.
* **Payload:**
  ```json
  {
    "outbound_tag": "proxy",
    "target_node": "vless-reality-frankfurt-01"
  }
  ```
* **Response Payload:**
  ```json
  {
    "active_node": "vless-reality-frankfurt-01"
  }
  ```

#### `node.test_latency`
Запуск измерения реального времени задержки (URL-Test через `generate_204`).
* **Payload:**
  ```json
  {
    "target_nodes": ["vless-reality-frankfurt-01", "hy2-helsinki-02"],
    "timeout_ms": 3000
  }
  ```
* **Response Payload:**
  ```json
  {
    "results": {
      "vless-reality-frankfurt-01": 84,
      "hy2-helsinki-02": 42
    }
  }
  ```

#### `routing.set_mode`
Смена глобального режима фильтрации.
* **Payload:**
  ```json
  {
    "mode": "rule" // допустимо: "rule" | "global" | "direct"
  }
  ```
* **Response Payload:**
  ```json
  {
    "current_mode": "rule"
  }
  ```

---

## 4. Потоковая телеметрия (Telemetry Events)

Демон транслирует телеметрию с каденцией, не перегружающей CPU (по умолчанию каждые 500 мс для метрик и немедленно для логов).

### 4.1. Метрики полосы пропускания (`telemetry.metrics`)
```json
{
  "type": "event",
  "topic": "telemetry.metrics",
  "timestamp": 1714567891000,
  "payload": {
    "upload_bps": 1245000,
    "download_bps": 48200000,
    "total_upload_bytes": 104857600,
    "total_download_bytes": 1073741824
  }
}
```

### 4.2. Инспектор соединений ядра (`telemetry.connections`)
Передает снапшот активных L3/L4 сессий, сопоставленных с PID через Darwin PCB:
```json
{
  "type": "event",
  "topic": "telemetry.connections",
  "timestamp": 1714567891000,
  "payload": {
    "connections": [
      {
        "id": "c-4819",
        "pid": 89412,
        "process_name": "Safari",
        "bundle_id": "com.apple.Safari",
        "source_ip": "172.19.0.1",
        "source_port": 54210,
        "destination_domain": "api.github.com",
        "destination_ip": "140.82.121.6",
        "destination_port": 443,
        "transport": "tcp",
        "rule_matched": "geosite-github",
        "outbound": "vless-reality-frankfurt-01",
        "upload_bytes": 1420,
        "download_bytes": 89200,
        "duration_seconds": 12
      }
    ]
  }
}
```

### 4.3. Системные логи (`telemetry.log`)
```json
{
  "type": "event",
  "topic": "telemetry.log",
  "timestamp": 1714567891240,
  "payload": {
    "level": "INFO", // "DEBUG" | "INFO" | "WARN" | "ERROR"
    "subsystem": "router",
    "message": "match[domain_suffix=youtube.com] -> proxy[vless-reality-frankfurt-01]"
  }
}
```

---

## 5. Контракты структур в коде

### 5.1. Swift (Клиент)
```swift
import Foundation

public struct IPCMessage: Codable {
    public let type: String
    public var id: String?
    public var action: String?
    public var topic: String?
    public var success: Bool?
    public var timestamp: Int64?
    public var payload: AnyCodable?
    public var error: IPCError?
}

public struct IPCError: Codable {
    public let code: Int
    public let message: String
}

public struct ConnectionRecord: Codable, Identifiable {
    public let id: String
    public let pid: Int32
    public let processName: String
    public let bundleId: String
    public let destinationDomain: String
    public let destinationIp: String
    public let destinationPort: Int
    public let ruleMatched: String
    public let outbound: String
    public let uploadBytes: Int64
    public let downloadBytes: Int64
}
```

### 5.2. Go (Демон)
```go
package ipc

type MessageType string

const (
	TypeRequest  MessageType = "request"
	TypeResponse MessageType = "response"
	TypeEvent    MessageType = "event"
)

type Envelope struct {
	Type      MessageType     `json:"type"`
	ID        string          `json:"id,omitempty"`
	Action    string          `json:"action,omitempty"`
	Topic     string          `json:"topic,omitempty"`
	Success   *bool           `json:"success,omitempty"`
	Timestamp int64           `json:"timestamp,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Error     *IPCError       `json:"error,omitempty"`
}

type IPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
```

---

## 6. Коды ошибок

| Код | Идентификатор | Описание |
| :--- | :--- | :--- |
| `1001` | `MALFORMED_PAYLOAD` | Получен некорректный JSON или нарушено NDJSON-кадрирование. |
| `1002` | `UNKNOWN_ACTION` | Запрошенный метод `action` не поддерживается сервером. |
| `2001` | `TUN_CREATION_FAILED` | Ошибка создания виртуального интерфейса `/dev/utun`. |
| `2002` | `ROUTE_CONFIG_FAILED` | Не удалось настроить системную таблицу маршрутов Darwin. |
| `2003` | `INTERFACE_ROAMING_ERR`| Сбой перепривязки сокета к новому сетевому адаптеру. |
| `3001` | `CONFIG_PARSE_FAILED` | Ошибка парсинга конфигурации sing-box (JSON/SRS). |
| `3002` | `NODE_UNREACHABLE` | Узел не прошел проверку хэндшейка при переключении. |
```