import Foundation

// MARK: - Envelopes & Error

public struct IPCError: Codable, Sendable {
    public let code: Int
    public let message: String
}

public struct IPCRequestEnvelope<T: Codable & Sendable>: Codable, Sendable {
    public let type: String
    public let id: String
    public let action: String
    public let payload: T

    public init(id: String = UUID().uuidString, action: String, payload: T) {
        self.type = "request"
        self.id = id
        self.action = action
        self.payload = payload
    }
}

public struct IPCResponseEnvelope<T: Codable & Sendable>: Codable, Sendable {
    public let type: String
    public let id: String?
    public let action: String?
    public let success: Bool?
    public let timestamp: Int64?
    public let payload: T?
    public let error: IPCError?
}

public struct IPCEventEnvelope<T: Codable & Sendable>: Codable, Sendable {
    public let type: String
    public let topic: String
    public let timestamp: Int64
    public let payload: T
}

// MARK: - RPC Payloads

public struct EmptyPayload: Codable, Sendable {
    public init() {}
}

public struct SystemPingResponse: Codable, Sendable {
    public let version: String
    public let singBoxVersion: String
    public let uptimeSeconds: Int64

    enum CodingKeys: String, CodingKey {
        case version
        case singBoxVersion = "sing_box_version"
        case uptimeSeconds = "uptime_seconds"
    }
}

public struct EngineStartRequest: Codable, Sendable {
    public let configPath: String?
    public let configJSON: String?

    enum CodingKeys: String, CodingKey {
        case configPath = "config_path"
        case configJSON = "config_json"
    }

    public init(configPath: String? = nil, configJSON: String? = nil) {
        self.configPath = configPath
        self.configJSON = configJSON
    }
}

public struct EngineStartResponse: Codable, Sendable {
    public let status: String
    public let interface: String
    public let assignedIPv4: String

    enum CodingKeys: String, CodingKey {
        case status
        case interface
        case assignedIPv4 = "assigned_ipv4"
    }
}

public struct EngineStopResponse: Codable, Sendable {
    public let status: String
}

public struct EngineResumeRequest: Codable, Sendable {
    public let rebindInterface: String

    enum CodingKeys: String, CodingKey {
        case rebindInterface = "rebind_interface"
    }

    public init(rebindInterface: String) {
        self.rebindInterface = rebindInterface
    }
}

public struct EngineResumeResponse: Codable, Sendable {
    public let status: String
    public let migrated: Bool
}

public struct NodeSwitchRequest: Codable, Sendable {
    public let outboundTag: String
    public let targetNode: String

    enum CodingKeys: String, CodingKey {
        case outboundTag = "outbound_tag"
        case targetNode = "target_node"
    }

    public init(outboundTag: String = "proxy", targetNode: String) {
        self.outboundTag = outboundTag
        self.targetNode = targetNode
    }
}

public struct NodeSwitchResponse: Codable, Sendable {
    public let activeNode: String

    enum CodingKeys: String, CodingKey {
        case activeNode = "active_node"
    }
}

public struct NodeTestLatencyRequest: Codable, Sendable {
    public let targetNodes: [String]
    public let timeoutMS: Int

    enum CodingKeys: String, CodingKey {
        case targetNodes = "target_nodes"
        case timeoutMS = "timeout_ms"
    }

    public init(targetNodes: [String], timeoutMS: Int = 3000) {
        self.targetNodes = targetNodes
        self.timeoutMS = timeoutMS
    }
}

public struct NodeTestLatencyResponse: Codable, Sendable {
    public let results: [String: Int]
}

public struct RoutingSetModeRequest: Codable, Sendable {
    public let mode: String

    public init(mode: String) {
        self.mode = mode
    }
}

public struct RoutingSetModeResponse: Codable, Sendable {
    public let currentMode: String

    enum CodingKeys: String, CodingKey {
        case currentMode = "current_mode"
    }
}

// MARK: - Telemetry Events

public struct TelemetryMetrics: Codable, Sendable {
    public let uploadBps: Int64
    public let downloadBps: Int64
    public let totalUploadBytes: Int64
    public let totalDownloadBytes: Int64

    enum CodingKeys: String, CodingKey {
        case uploadBps = "upload_bps"
        case downloadBps = "download_bps"
        case totalUploadBytes = "total_upload_bytes"
        case totalDownloadBytes = "total_download_bytes"
    }
}

public struct ConnectionRecord: Codable, Identifiable, Sendable {
    public let id: String
    public let pid: Int32
    public let processName: String
    public let bundleId: String
    public let sourceIp: String?
    public let sourcePort: Int?
    public let destinationDomain: String
    public let destinationIp: String
    public let destinationPort: Int
    public let transport: String?
    public let ruleMatched: String
    public let outbound: String
    public let uploadBytes: Int64
    public let downloadBytes: Int64
    public let durationSeconds: Int64?

    enum CodingKeys: String, CodingKey {
        case id, pid
        case processName = "process_name"
        case bundleId = "bundle_id"
        case sourceIp = "source_ip"
        case sourcePort = "source_port"
        case destinationDomain = "destination_domain"
        case destinationIp = "destination_ip"
        case destinationPort = "destination_port"
        case transport
        case ruleMatched = "rule_matched"
        case outbound
        case uploadBytes = "upload_bytes"
        case downloadBytes = "download_bytes"
        case durationSeconds = "duration_seconds"
    }
}

public struct TelemetryConnections: Codable, Sendable {
    public let connections: [ConnectionRecord]
}

public struct TelemetryLog: Codable, Sendable {
    public let level: String
    public let subsystem: String
    public let message: String
}
