package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
)

const (
	DefaultSocketPath = "/var/run/telifon/telifon.sock"
	FallbackDir       = ".telifon"
	FallbackSocket    = "telifon.sock"
)

// ActionHandler handles an incoming RPC request
type ActionHandler func(req *Envelope) (*Envelope, error)

// Server is the Unix Domain Socket IPC server
type Server struct {
	socketPath string
	listener   net.Listener
	handlers   map[string]ActionHandler
	clients    map[net.Conn]struct{}
	mu         sync.RWMutex
	broadcast  chan *Envelope
	quit       chan struct{}
	wg         sync.WaitGroup
}

// NewServer initializes the IPC server with preferred or fallback socket path
func NewServer(preferredPath string) *Server {
	if preferredPath == "" {
		preferredPath = DefaultSocketPath
	}
	return &Server{
		socketPath: preferredPath,
		handlers:   make(map[string]ActionHandler),
		clients:    make(map[net.Conn]struct{}),
		broadcast:  make(chan *Envelope, 256),
		quit:       make(chan struct{}),
	}
}

// RegisterHandler registers an action callback
func (s *Server) RegisterHandler(action string, handler ActionHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[action] = handler
}

// Start begins listening on the Unix Domain Socket
func (s *Server) Start() error {
	path, err := s.prepareSocketPath(s.socketPath)
	if err != nil {
		// Fallback to user home directory if root permissions fail
		fallbackPath := s.getFallbackSocketPath()
		log.Printf("[IPC] Preferred socket path %s failed (%v), falling back to %s", s.socketPath, err, fallbackPath)
		path, err = s.prepareSocketPath(fallbackPath)
		if err != nil {
			return fmt.Errorf("failed to prepare fallback socket path: %w", err)
		}
		s.socketPath = fallbackPath
	}

	listener, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("failed to listen on unix socket %s: %w", path, err)
	}
	s.listener = listener

	// Set socket permissions: 0660 (rw-rw----)
	_ = os.Chmod(path, 0660)

	// Attempt to set group to "admin" if running as root
	s.chownSocket(path)

	log.Printf("[IPC] Listening on Unix Domain Socket: %s", path)

	s.wg.Add(2)
	go s.broadcastLoop()
	go s.acceptLoop()

	return nil
}

// Stop cleanly terminates the IPC server and removes the socket file
func (s *Server) Stop() error {
	close(s.quit)

	s.mu.Lock()
	if s.listener != nil {
		_ = s.listener.Close()
	}
	for conn := range s.clients {
		_ = conn.Close()
		delete(s.clients, conn)
	}
	s.mu.Unlock()

	s.wg.Wait()

	if s.socketPath != "" {
		_ = os.Remove(s.socketPath)
	}
	log.Printf("[IPC] Server stopped.")
	return nil
}

// Broadcast sends an asynchronous event envelope to all connected clients
func (s *Server) Broadcast(event *Envelope) {
	select {
	case s.broadcast <- event:
	default:
		// Drop telemetry if buffer is full to prevent blocking engine
	}
}

func (s *Server) prepareSocketPath(path string) (string, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0775); err != nil {
		return "", err
	}

	// Remove stale socket if it exists
	if fi, err := os.Stat(path); err == nil {
		if fi.Mode()&os.ModeSocket != 0 || fi.Mode().IsRegular() {
			_ = os.Remove(path)
		}
	}
	return path, nil
}

func (s *Server) getFallbackSocketPath() string {
	u, err := user.Current()
	if err == nil && u.HomeDir != "" {
		return filepath.Join(u.HomeDir, FallbackDir, FallbackSocket)
	}
	return filepath.Join(os.TempDir(), FallbackSocket)
}

func (s *Server) chownSocket(path string) {
	if os.Geteuid() == 0 {
		// Look up admin group on Darwin
		adminGroup, err := user.LookupGroup("admin")
		if err == nil {
			if gid, err := strconv.Atoi(adminGroup.Gid); err == nil {
				_ = syscall.Chown(path, 0, gid)
			}
		}
	}
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return
			default:
				if errors.Is(err, net.ErrClosed) {
					return
				}
				log.Printf("[IPC] Accept error: %v", err)
				continue
			}
		}

		s.mu.Lock()
		s.clients[conn] = struct{}{}
		s.mu.Unlock()

		go s.handleClient(conn)
	}
}

func (s *Server) handleClient(conn net.Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.clients, conn)
		s.mu.Unlock()
		_ = conn.Close()
	}()

	scanner := bufio.NewScanner(conn)
	// Support messages up to 2MB (for configs/rules)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Envelope
		if err := json.Unmarshal(line, &req); err != nil {
			errResp := NewErrorResponse("", "", ErrCodeMalformedPayload, "Malformed JSON envelope")
			_ = s.writeEnvelope(conn, errResp)
			continue
		}

		if req.Type != TypeRequest {
			continue
		}

		s.mu.RLock()
		handler, exists := s.handlers[req.Action]
		s.mu.RUnlock()

		if !exists {
			errResp := NewErrorResponse(req.ID, req.Action, ErrCodeUnknownAction, fmt.Sprintf("Unknown action: %s", req.Action))
			_ = s.writeEnvelope(conn, errResp)
			continue
		}

		resp, err := handler(&req)
		if err != nil {
			errResp := NewErrorResponse(req.ID, req.Action, ErrCodeInternalError, err.Error())
			_ = s.writeEnvelope(conn, errResp)
			continue
		}

		if resp != nil {
			if err := s.writeEnvelope(conn, resp); err != nil {
				log.Printf("[IPC] Write error to client: %v", err)
				break
			}
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, net.ErrClosed) {
		log.Printf("[IPC] Connection scan error: %v", err)
	}
}

func (s *Server) broadcastLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.quit:
			return
		case event := <-s.broadcast:
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			data = append(data, '\n')

			s.mu.RLock()
			for conn := range s.clients {
				_, _ = conn.Write(data)
			}
			s.mu.RUnlock()
		}
	}
}

func (s *Server) writeEnvelope(conn net.Conn, env *Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = conn.Write(data)
	return err
}

// SocketPath returns the active path of the socket
func (s *Server) SocketPath() string {
	return s.socketPath
}
