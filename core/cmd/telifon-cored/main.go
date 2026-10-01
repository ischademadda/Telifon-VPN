package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"telifon-core/pkg/config"
	"telifon-core/pkg/engine"
	"telifon-core/pkg/inspector"
	"telifon-core/pkg/ipc"
)

const (
	Version        = "1.0.0"
	SingBoxVersion = "1.10.7"
)

func init() {
	// Darwin RSS optimization: soft memory limit at 96 MB
	debug.SetMemoryLimit(96 * 1024 * 1024)
	// More frequent GC to release memory aggressively under Darwin
	debug.SetGCPercent(30)
}

func main() {
	socketFlag := flag.String("socket", "", "Path to Unix Domain Socket")
	testRunFlag := flag.Bool("test-run", false, "Test run daemon initialization and exit cleanly")
	versionFlag := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("telifon-cored v%s (sing-box v%s)\n", Version, SingBoxVersion)
		return
	}

	log.Printf("[CORE] Starting telifon-cored v%s (sing-box v%s)", Version, SingBoxVersion)
	startTime := time.Now()

	eng := engine.NewEngine()
	ins := inspector.NewInspector(2 * time.Second)

	if *testRunFlag {
		log.Println("[CORE] Running test-run verification...")
		cfgBytes, err := config.GenerateSingBoxConfig(config.DefaultProfile())
		if err != nil {
			log.Fatalf("[CORE] Failed to generate test config: %v", err)
		}
		log.Printf("[CORE] Generated valid sing-box config (%d bytes)", len(cfgBytes))
		log.Println("[CORE] Test run completed successfully!")
		return
	}

	ipcServer := ipc.NewServer(*socketFlag)

	// Register IPC RPC Action Handlers
	registerHandlers(ipcServer, eng, ins, startTime)

	if err := ipcServer.Start(); err != nil {
		log.Fatalf("[CORE] Failed to start IPC server: %v", err)
	}

	// Start Telemetry Broadcast Loop
	stopTelemetry := make(chan struct{})
	go runTelemetry(ipcServer, eng, ins, stopTelemetry)

	// Wait for termination signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	log.Printf("[CORE] Received signal %v, shutting down...", sig)

	close(stopTelemetry)
	_ = eng.Stop()
	_ = ipcServer.Stop()

	log.Println("[CORE] Shutdown complete.")
}

func registerHandlers(srv *ipc.Server, eng *engine.Engine, ins *inspector.Inspector, startTime time.Time) {
	// system.ping
	srv.RegisterHandler("system.ping", func(req *ipc.Envelope) (*ipc.Envelope, error) {
		uptime := int64(time.Since(startTime).Seconds())
		resp := ipc.SystemPingResponse{
			Version:        Version,
			SingBoxVersion: SingBoxVersion,
			UptimeSeconds:  uptime,
		}
		return ipc.NewSuccessResponse(req.ID, req.Action, resp)
	})

	// engine.start
	srv.RegisterHandler("engine.start", func(req *ipc.Envelope) (*ipc.Envelope, error) {
		var startReq ipc.EngineStartRequest
		if len(req.Payload) > 0 && string(req.Payload) != "{}" {
			_ = json.Unmarshal(req.Payload, &startReq)
		}

		var cfgBytes []byte
		var err error

		if len(startReq.ConfigJSON) > 0 && string(startReq.ConfigJSON) != "null" {
			cfgBytes = startReq.ConfigJSON
		} else if startReq.ConfigPath != "" {
			cfgBytes, err = os.ReadFile(startReq.ConfigPath)
			if err != nil {
				return ipc.NewErrorResponse(req.ID, req.Action, ipc.ErrCodeConfigParseFailed, fmt.Sprintf("Failed to read config file: %v", err)), nil
			}
		} else {
			// Generate default profile
			cfgBytes, err = config.GenerateSingBoxConfig(config.DefaultProfile())
			if err != nil {
				return ipc.NewErrorResponse(req.ID, req.Action, ipc.ErrCodeConfigParseFailed, err.Error()), nil
			}
		}

		if err := eng.Start(cfgBytes); err != nil {
			return ipc.NewErrorResponse(req.ID, req.Action, ipc.ErrCodeTUNCIFailed, err.Error()), nil
		}

		resp := ipc.EngineStartResponse{
			Status:       eng.State(),
			Interface:    eng.InterfaceName(),
			AssignedIPv4: eng.AssignedIPv4(),
		}
		return ipc.NewSuccessResponse(req.ID, req.Action, resp)
	})

	// engine.stop
	srv.RegisterHandler("engine.stop", func(req *ipc.Envelope) (*ipc.Envelope, error) {
		if err := eng.Stop(); err != nil {
			return ipc.NewErrorResponse(req.ID, req.Action, ipc.ErrCodeInternalError, err.Error()), nil
		}
		return ipc.NewSuccessResponse(req.ID, req.Action, ipc.EngineStopResponse{Status: eng.State()})
	})

	// engine.pause
	srv.RegisterHandler("engine.pause", func(req *ipc.Envelope) (*ipc.Envelope, error) {
		if err := eng.Pause(); err != nil {
			return ipc.NewErrorResponse(req.ID, req.Action, ipc.ErrCodeInternalError, err.Error()), nil
		}
		return ipc.NewSuccessResponse(req.ID, req.Action, map[string]string{"status": eng.State()})
	})

	// engine.resume
	srv.RegisterHandler("engine.resume", func(req *ipc.Envelope) (*ipc.Envelope, error) {
		var resumeReq ipc.EngineResumeRequest
		if len(req.Payload) > 0 {
			_ = json.Unmarshal(req.Payload, &resumeReq)
		}
		if err := eng.Resume(resumeReq.RebindInterface); err != nil {
			return ipc.NewErrorResponse(req.ID, req.Action, ipc.ErrCodeInterfaceRoamingErr, err.Error()), nil
		}
		resp := ipc.EngineResumeResponse{
			Status:   eng.State(),
			Migrated: true,
		}
		return ipc.NewSuccessResponse(req.ID, req.Action, resp)
	})

	// node.switch
	srv.RegisterHandler("node.switch", func(req *ipc.Envelope) (*ipc.Envelope, error) {
		var switchReq ipc.NodeSwitchRequest
		if err := json.Unmarshal(req.Payload, &switchReq); err != nil {
			return ipc.NewErrorResponse(req.ID, req.Action, ipc.ErrCodeMalformedPayload, err.Error()), nil
		}
		if err := eng.SwitchNode(switchReq.OutboundTag, switchReq.TargetNode); err != nil {
			return ipc.NewErrorResponse(req.ID, req.Action, ipc.ErrCodeNodeUnreachable, err.Error()), nil
		}
		return ipc.NewSuccessResponse(req.ID, req.Action, ipc.NodeSwitchResponse{ActiveNode: eng.ActiveNode()})
	})

	// node.test_latency
	srv.RegisterHandler("node.test_latency", func(req *ipc.Envelope) (*ipc.Envelope, error) {
		var latencyReq ipc.NodeTestLatencyRequest
		if err := json.Unmarshal(req.Payload, &latencyReq); err != nil {
			return ipc.NewErrorResponse(req.ID, req.Action, ipc.ErrCodeMalformedPayload, err.Error()), nil
		}
		results, err := eng.TestLatency(latencyReq.TargetNodes, latencyReq.TimeoutMS)
		if err != nil {
			return ipc.NewErrorResponse(req.ID, req.Action, ipc.ErrCodeInternalError, err.Error()), nil
		}
		return ipc.NewSuccessResponse(req.ID, req.Action, ipc.NodeTestLatencyResponse{Results: results})
	})

	// routing.set_mode
	srv.RegisterHandler("routing.set_mode", func(req *ipc.Envelope) (*ipc.Envelope, error) {
		var modeReq ipc.RoutingSetModeRequest
		if err := json.Unmarshal(req.Payload, &modeReq); err != nil {
			return ipc.NewErrorResponse(req.ID, req.Action, ipc.ErrCodeMalformedPayload, err.Error()), nil
		}
		eng.SetMode(modeReq.Mode)
		return ipc.NewSuccessResponse(req.ID, req.Action, ipc.RoutingSetModeResponse{CurrentMode: eng.CurrentMode()})
	})
}

func runTelemetry(srv *ipc.Server, eng *engine.Engine, ins *inspector.Inspector, stop <-chan struct{}) {
	metricsTicker := time.NewTicker(500 * time.Millisecond)
	defer metricsTicker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-metricsTicker.C:
			upBps, downBps, totalUp, totalDown := eng.Metrics.Tick()
			metrics := ipc.TelemetryMetricsPayload{
				UploadBps:        upBps,
				DownloadBps:      downBps,
				TotalUploadBytes: totalUp,
				TotalDownBytes:   totalDown,
			}
			if event, err := ipc.NewEvent("telemetry.metrics", metrics); err == nil {
				srv.Broadcast(event)
			}
		}
	}
}
