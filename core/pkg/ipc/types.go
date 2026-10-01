package ipc

import (
	"encoding/json"
	"time"
)

// MessageType defines the envelope message kind
type MessageType string

const (
	TypeRequest  MessageType = "request"
	TypeResponse MessageType = "response"
	TypeEvent    MessageType = "event"
)

// Standard error codes defined in IPC_SPEC.md
const (
	ErrCodeMalformedPayload    = 1001
	ErrCodeUnknownAction       = 1002
	ErrCodeTUNCIFailed         = 2001
	ErrCodeRouteConfigFailed   = 2002
	ErrCodeInterfaceRoamingErr = 2003
	ErrCodeConfigParseFailed   = 3001
	ErrCodeNodeUnreachable     = 3002
	ErrCodeInternalError       = 5000
)

// Envelope is the standard wrapper for all IPC socket communications
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

// IPCError represents error details inside a response envelope
type IPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// SystemPingResponse payload for system.ping
type SystemPingResponse struct {
	Version        string `json:"version"`
	SingBoxVersion string `json:"sing_box_version"`
	UptimeSeconds  int64  `json:"uptime_seconds"`
}

// EngineStartRequest payload for engine.start
type EngineStartRequest struct {
	ConfigPath string          `json:"config_path,omitempty"`
	ConfigJSON json.RawMessage `json:"config_json,omitempty"`
}

// EngineStartResponse payload for engine.start
type EngineStartResponse struct {
	Status       string `json:"status"`
	Interface    string `json:"interface"`
	AssignedIPv4 string `json:"assigned_ipv4"`
}

// EngineStopResponse payload for engine.stop
type EngineStopResponse struct {
	Status string `json:"status"`
}

// EngineResumeRequest payload for engine.resume
type EngineResumeRequest struct {
	RebindInterface string `json:"rebind_interface,omitempty"`
}

// EngineResumeResponse payload for engine.resume
type EngineResumeResponse struct {
	Status   string `json:"status"`
	Migrated bool   `json:"migrated"`
}

// NodeSwitchRequest payload for node.switch
type NodeSwitchRequest struct {
	OutboundTag string `json:"outbound_tag"`
	TargetNode  string `json:"target_node"`
}

// NodeSwitchResponse payload for node.switch
type NodeSwitchResponse struct {
	ActiveNode string `json:"active_node"`
}

// NodeTestLatencyRequest payload for node.test_latency
type NodeTestLatencyRequest struct {
	TargetNodes []string `json:"target_nodes"`
	TimeoutMS   int      `json:"timeout_ms"`
}

// NodeTestLatencyResponse payload for node.test_latency
type NodeTestLatencyResponse struct {
	Results map[string]int `json:"results"`
}

// RoutingSetModeRequest payload for routing.set_mode
type RoutingSetModeRequest struct {
	Mode string `json:"mode"` // "rule" | "global" | "direct"
}

// RoutingSetModeResponse payload for routing.set_mode
type RoutingSetModeResponse struct {
	CurrentMode string `json:"current_mode"`
}

// TelemetryMetricsPayload for telemetry.metrics event
type TelemetryMetricsPayload struct {
	UploadBps        int64 `json:"upload_bps"`
	DownloadBps      int64 `json:"download_bps"`
	TotalUploadBytes int64 `json:"total_upload_bytes"`
	TotalDownBytes   int64 `json:"total_download_bytes"`
}

// ConnectionRecord represents an active connection tracked via PCB
type ConnectionRecord struct {
	ID                string `json:"id"`
	PID               int32  `json:"pid"`
	ProcessName       string `json:"process_name"`
	BundleID          string `json:"bundle_id"`
	SourceIP          string `json:"source_ip"`
	SourcePort        int    `json:"source_port"`
	DestinationDomain string `json:"destination_domain"`
	DestinationIP     string `json:"destination_ip"`
	DestinationPort   int    `json:"destination_port"`
	Transport         string `json:"transport"`
	RuleMatched       string `json:"rule_matched"`
	Outbound          string `json:"outbound"`
	UploadBytes       int64  `json:"upload_bytes"`
	DownloadBytes     int64  `json:"download_bytes"`
	DurationSeconds   int64  `json:"duration_seconds"`
}

// TelemetryConnectionsPayload for telemetry.connections event
type TelemetryConnectionsPayload struct {
	Connections []ConnectionRecord `json:"connections"`
}

// TelemetryLogPayload for telemetry.log event
type TelemetryLogPayload struct {
	Level     string `json:"level"` // "DEBUG" | "INFO" | "WARN" | "ERROR"
	Subsystem string `json:"subsystem"`
	Message   string `json:"message"`
}

// Helper constructors
func NewSuccessResponse(id, action string, payload any) (*Envelope, error) {
	var raw json.RawMessage
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		raw = data
	} else {
		raw = json.RawMessage("{}")
	}
	t := true
	return &Envelope{
		Type:      TypeResponse,
		ID:        id,
		Action:    action,
		Success:   &t,
		Timestamp: time.Now().UnixMilli(),
		Payload:   raw,
	}, nil
}

func NewErrorResponse(id, action string, code int, msg string) *Envelope {
	f := false
	return &Envelope{
		Type:      TypeResponse,
		ID:        id,
		Action:    action,
		Success:   &f,
		Timestamp: time.Now().UnixMilli(),
		Error: &IPCError{
			Code:    code,
			Message: msg,
		},
	}
}

func NewEvent(topic string, payload any) (*Envelope, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Envelope{
		Type:      TypeEvent,
		Topic:     topic,
		Timestamp: time.Now().UnixMilli(),
		Payload:   data,
	}, nil
}
