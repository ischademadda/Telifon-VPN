import Foundation
import Network

public actor IPCClient {
    public enum ConnectionState: Sendable {
        case disconnected
        case connecting
        case connected
    }

    private let preferredSocketPath: String
    private var socketFD: Int32 = -1
    private var isConnected: Bool = false
    private var activeContinuations: [String: CheckedContinuation<Data, Error>] = [:]
    private var metricsContinuations: [UUID: AsyncStream<TelemetryMetrics>.Continuation] = [:]

    public init(socketPath: String = "/var/run/telifon/telifon.sock") {
        self.preferredSocketPath = socketPath
    }

    public func connect() throws {
        if isConnected { return }

        var targetPath = preferredSocketPath
        if !FileManager.default.fileExists(atPath: targetPath) {
            let home = FileManager.default.homeDirectoryForCurrentUser.path
            let fallback = "\(home)/.telifon/telifon.sock"
            if FileManager.default.fileExists(atPath: fallback) {
                targetPath = fallback
            }
        }

        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else {
            throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
        }

        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        
        let pathBytes = targetPath.utf8CString
        guard pathBytes.count < MemoryLayout.size(ofValue: addr.sun_path) else {
            close(fd)
            throw POSIXError(.ENAMETOOLONG)
        }

        withUnsafeMutablePointer(to: &addr.sun_path.0) { ptr in
            _ = pathBytes.withUnsafeBufferPointer { buf in
                memcpy(ptr, buf.baseAddress!, buf.count)
            }
        }

        let addrLen = socklen_t(MemoryLayout<sa_family_t>.size + pathBytes.count)
        let connectResult = withUnsafePointer(to: &addr) { ptr in
            ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sa in
                Darwin.connect(fd, sa, addrLen)
            }
        }

        guard connectResult == 0 else {
            close(fd)
            throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .ECONNREFUSED)
        }

        self.socketFD = fd
        self.isConnected = true

        Task.detached { [weak self] in
            await self?.readLoop(fd: fd)
        }
    }

    public func disconnect() {
        guard isConnected else { return }
        isConnected = false
        if socketFD >= 0 {
            close(socketFD)
            socketFD = -1
        }
        for (_, cont) in activeContinuations {
            cont.resume(throwing: POSIXError(.ECONNRESET))
        }
        activeContinuations.removeAll()
        for (_, cont) in metricsContinuations {
            cont.finish()
        }
        metricsContinuations.removeAll()
    }

    public func sendRequest<P: Codable & Sendable, R: Codable & Sendable>(action: String, payload: P) async throws -> R {
        if !isConnected {
            try connect()
        }

        let reqID = UUID().uuidString
        let env = IPCRequestEnvelope(id: reqID, action: action, payload: payload)
        var data = try JSONEncoder().encode(env)
        data.append(0x0A) // '\n' newline framing

        let rawData: Data = try await withCheckedThrowingContinuation { continuation in
            self.activeContinuations[reqID] = continuation

            let written = data.withUnsafeBytes { ptr in
                Darwin.write(self.socketFD, ptr.baseAddress!, data.count)
            }

            if written < 0 {
                self.activeContinuations.removeValue(forKey: reqID)
                continuation.resume(throwing: POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO))
            }
        }

        return try JSONDecoder().decode(R.self, from: rawData)
    }

    // MARK: - High-level RPC methods

    public func ping() async throws -> SystemPingResponse {
        let resp: IPCResponseEnvelope<SystemPingResponse> = try await sendRequest(action: "system.ping", payload: EmptyPayload())
        guard let p = resp.payload else {
            throw NSError(domain: "IPC", code: resp.error?.code ?? -1, userInfo: [NSLocalizedDescriptionKey: resp.error?.message ?? "Empty ping payload"])
        }
        return p
    }

    public func startEngine(configJSON: String? = nil) async throws -> EngineStartResponse {
        let req = EngineStartRequest(configJSON: configJSON)
        let resp: IPCResponseEnvelope<EngineStartResponse> = try await sendRequest(action: "engine.start", payload: req)
        guard let p = resp.payload else {
            throw NSError(domain: "IPC", code: resp.error?.code ?? -1, userInfo: [NSLocalizedDescriptionKey: resp.error?.message ?? "Failed to start engine"])
        }
        return p
    }

    public func stopEngine() async throws -> EngineStopResponse {
        let resp: IPCResponseEnvelope<EngineStopResponse> = try await sendRequest(action: "engine.stop", payload: EmptyPayload())
        guard let p = resp.payload else {
            throw NSError(domain: "IPC", code: resp.error?.code ?? -1, userInfo: [NSLocalizedDescriptionKey: resp.error?.message ?? "Failed to stop engine"])
        }
        return p
    }

    public func switchNode(tag: String = "proxy", node: String) async throws -> String {
        let req = NodeSwitchRequest(outboundTag: tag, targetNode: node)
        let resp: IPCResponseEnvelope<NodeSwitchResponse> = try await sendRequest(action: "node.switch", payload: req)
        return resp.payload?.activeNode ?? node
    }

    public func setRoutingMode(mode: String) async throws -> String {
        let req = RoutingSetModeRequest(mode: mode)
        let resp: IPCResponseEnvelope<RoutingSetModeResponse> = try await sendRequest(action: "routing.set_mode", payload: req)
        return resp.payload?.currentMode ?? mode
    }

    public func metricsStream() -> AsyncStream<TelemetryMetrics> {
        let id = UUID()
        return AsyncStream { continuation in
            self.metricsContinuations[id] = continuation
            continuation.onTermination = { [weak self] _ in
                Task { [weak self] in
                    await self?.removeMetricsContinuation(id: id)
                }
            }
        }
    }

    private func removeMetricsContinuation(id: UUID) {
        metricsContinuations.removeValue(forKey: id)
    }

    private func readLoop(fd: Int32) {
        var buffer = Data()
        let chunk = UnsafeMutablePointer<UInt8>.allocate(capacity: 8192)
        defer { chunk.deallocate() }

        while true {
            let bytesRead = Darwin.read(fd, chunk, 8192)
            guard bytesRead > 0 else {
                break
            }

            buffer.append(chunk, count: bytesRead)

            while let newlineIndex = buffer.firstIndex(of: 0x0A) {
                let lineData = buffer.subdata(in: 0..<newlineIndex)
                buffer.removeSubrange(0...newlineIndex)

                if lineData.isEmpty { continue }

                self.handleIncomingLine(lineData)
            }
        }

        self.disconnect()
    }

    private func handleIncomingLine(_ data: Data) {
        // Check if message is an event or response
        guard let jsonObject = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let type = jsonObject["type"] as? String else {
            return
        }

        if type == "response", let id = jsonObject["id"] as? String {
            if let continuation = activeContinuations.removeValue(forKey: id) {
                continuation.resume(returning: data)
            }
        } else if type == "event", let topic = jsonObject["topic"] as? String {
            if topic == "telemetry.metrics" {
                if let event = try? JSONDecoder().decode(IPCEventEnvelope<TelemetryMetrics>.self, from: data) {
                    for continuation in metricsContinuations.values {
                        continuation.yield(event.payload)
                    }
                }
            }
        }
    }
}
