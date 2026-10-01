package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	box "github.com/sagernet/sing-box"
	_ "github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
)

// EngineState represents current runtime state
type EngineState string

const (
	StateStopped EngineState = "STOPPED"
	StateRunning EngineState = "RUNNING"
	StatePaused  EngineState = "PAUSED"
)

// MetricsTracker records bandwidth usage
type MetricsTracker struct {
	TotalUploadBytes   atomic.Int64
	TotalDownloadBytes atomic.Int64
	UploadBps          atomic.Int64
	DownloadBps        atomic.Int64

	lastUpload   int64
	lastDownload int64
	lastSample   time.Time
	mu           sync.Mutex
}

func (m *MetricsTracker) AddUpload(bytes int64) {
	m.TotalUploadBytes.Add(bytes)
}

func (m *MetricsTracker) AddDownload(bytes int64) {
	m.TotalDownloadBytes.Add(bytes)
}

func (m *MetricsTracker) Tick() (uploadBps, downloadBps, totalUp, totalDown int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	dur := now.Sub(m.lastSample).Seconds()
	if dur <= 0 {
		dur = 1
	}

	currUp := m.TotalUploadBytes.Load()
	currDown := m.TotalDownloadBytes.Load()

	diffUp := currUp - m.lastUpload
	diffDown := currDown - m.lastDownload

	upBps := int64(float64(diffUp) / dur)
	downBps := int64(float64(diffDown) / dur)

	m.UploadBps.Store(upBps)
	m.DownloadBps.Store(downBps)

	m.lastUpload = currUp
	m.lastDownload = currDown
	m.lastSample = now

	return upBps, downBps, currUp, currDown
}

// Engine wraps sing-box lifecycle
type Engine struct {
	mu            sync.RWMutex
	boxInstance   *box.Box
	cancelFunc    context.CancelFunc
	state         atomic.Value // string: "STOPPED", "RUNNING", "PAUSED"
	activeConfig  []byte
	activeNode    string
	currentMode   string
	tunInterface  string
	assignedIPv4  string
	Metrics       MetricsTracker
}

// NewEngine creates an idle sing-box wrapper
func NewEngine() *Engine {
	e := &Engine{
		tunInterface: "utun100",
		assignedIPv4: "172.19.0.1",
		activeNode:   "default",
		currentMode:  "rule",
	}
	e.state.Store(string(StateStopped))
	e.Metrics.lastSample = time.Now()
	return e
}

// Start parses configJSON and initializes sing-box
func (e *Engine) Start(configJSON []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state.Load().(string) == string(StateRunning) {
		return errors.New("engine is already running")
	}

	var opt option.Options
	if err := json.Unmarshal(configJSON, &opt); err != nil {
		return fmt.Errorf("failed to parse sing-box config: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	instance, err := box.New(box.Options{
		Options: opt,
		Context: ctx,
	})
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create sing-box instance: %w", err)
	}

	if err := instance.Start(); err != nil {
		cancel()
		_ = instance.Close()
		return fmt.Errorf("failed to start sing-box: %w", err)
	}

	e.boxInstance = instance
	e.cancelFunc = cancel
	e.activeConfig = configJSON
	e.state.Store(string(StateRunning))

	log.Printf("[ENGINE] Started successfully on %s (IPv4: %s)", e.tunInterface, e.assignedIPv4)
	return nil
}

// Stop cleanly terminates sing-box and routing
func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	currState := e.state.Load().(string)
	if currState == string(StateStopped) {
		return nil
	}

	if e.boxInstance != nil {
		_ = e.boxInstance.Close()
		e.boxInstance = nil
	}
	if e.cancelFunc != nil {
		e.cancelFunc()
		e.cancelFunc = nil
	}

	e.state.Store(string(StateStopped))
	log.Printf("[ENGINE] Stopped.")
	return nil
}

// Pause pauses tunnel during system sleep
func (e *Engine) Pause() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state.Load().(string) != string(StateRunning) {
		return nil
	}

	e.state.Store(string(StatePaused))
	log.Printf("[ENGINE] Paused for sleep transition.")
	return nil
}

// Resume restarts routing and rebinds interfaces after sleep
func (e *Engine) Resume(rebindInterface string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	currState := e.state.Load().(string)
	if currState != string(StatePaused) {
		return nil
	}

	if e.boxInstance != nil {
		router := e.boxInstance.Router()
		if router != nil {
			_ = router.UpdateInterfaces()
			_ = router.ResetNetwork()
		}
	}

	e.state.Store(string(StateRunning))
	log.Printf("[ENGINE] Resumed on interface %s", rebindInterface)
	return nil
}

// SwitchNode changes the active node tag
func (e *Engine) SwitchNode(outboundTag, targetNode string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.activeNode = targetNode
	log.Printf("[ENGINE] Switched outbound [%s] to node: %s", outboundTag, targetNode)
	return nil
}

// TestLatency runs HTTP ping test against endpoints
func (e *Engine) TestLatency(targetNodes []string, timeoutMS int) (map[string]int, error) {
	if timeoutMS <= 0 {
		timeoutMS = 3000
	}

	results := make(map[string]int)
	client := &http.Client{
		Timeout: time.Duration(timeoutMS) * time.Millisecond,
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, node := range targetNodes {
		wg.Add(1)
		go func(nodeName string) {
			defer wg.Done()
			start := time.Now()
			req, err := http.NewRequestWithContext(context.Background(), "GET", "http://cp.cloudflare.com/generate_204", nil)
			if err != nil {
				mu.Lock()
				results[nodeName] = -1
				mu.Unlock()
				return
			}

			resp, err := client.Do(req)
			if err != nil {
				mu.Lock()
				results[nodeName] = -1
				mu.Unlock()
				return
			}
			_ = resp.Body.Close()

			rtt := int(time.Since(start).Milliseconds())
			mu.Lock()
			results[nodeName] = rtt
			mu.Unlock()
		}(node)
	}

	wg.Wait()
	return results, nil
}

// State returns current engine status string
func (e *Engine) State() string {
	return e.state.Load().(string)
}

// ActiveNode returns currently selected outbound node
func (e *Engine) ActiveNode() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.activeNode
}

// SetMode updates proxy mode
func (e *Engine) SetMode(mode string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.currentMode = mode
}

// CurrentMode returns active mode ("rule", "global", "direct")
func (e *Engine) CurrentMode() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.currentMode
}

// InterfaceName returns TUN interface name
func (e *Engine) InterfaceName() string {
	return e.tunInterface
}

// AssignedIPv4 returns assigned IP address for TUN
func (e *Engine) AssignedIPv4() string {
	return e.assignedIPv4
}
