package inspector

/*
#include <libproc.h>
#include <stdlib.h>
#include <sys/sysctl.h>
#include <sys/types.h>
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// ProcessInfo contains resolved process metadata
type ProcessInfo struct {
	PID         int32
	ProcessName string
	BundleID    string
	Path        string
	ResolvedAt  time.Time
}

// PortKey maps protocol and local port
type PortKey struct {
	Transport string // "tcp" or "udp"
	Port      int
}

// Inspector queries and caches Darwin kernel socket ownership
type Inspector struct {
	cacheTTL  time.Duration
	portCache sync.Map // map[PortKey]ProcessInfo
	pidCache  sync.Map // map[int32]ProcessInfo
}

// NewInspector creates a new Darwin PCB inspector with 2-second cache TTL
func NewInspector(ttl time.Duration) *Inspector {
	if ttl <= 0 {
		ttl = 2 * time.Second
	}
	return &Inspector{
		cacheTTL: ttl,
	}
}

// LookupProcess resolves process metadata for a given transport and local port
func (ins *Inspector) LookupProcess(transport string, localPort int) ProcessInfo {
	key := PortKey{Transport: strings.ToLower(transport), Port: localPort}

	// 1. Check port cache
	if val, ok := ins.portCache.Load(key); ok {
		info := val.(ProcessInfo)
		if time.Since(info.ResolvedAt) < ins.cacheTTL {
			return info
		}
		ins.portCache.Delete(key)
	}

	// 2. Query kernel PCB list
	pid := ins.findPIDForPort(key.Transport, key.Port)
	if pid <= 0 {
		return ProcessInfo{
			PID:         0,
			ProcessName: "unknown",
			BundleID:    "com.apple.unknown",
			ResolvedAt:  time.Now(),
		}
	}

	// 3. Resolve PID to ProcessInfo
	info := ins.ResolvePID(pid)
	ins.portCache.Store(key, info)
	return info
}

// ResolvePID inspects proc_pidpath to extract process name and bundle identifier
func (ins *Inspector) ResolvePID(pid int32) ProcessInfo {
	if val, ok := ins.pidCache.Load(pid); ok {
		cached := val.(ProcessInfo)
		if time.Since(cached.ResolvedAt) < (ins.cacheTTL * 5) {
			return cached
		}
	}

	buf := make([]byte, 4096)
	ret := C.proc_pidpath(C.int(pid), unsafe.Pointer(&buf[0]), C.uint32_t(len(buf)))
	if ret <= 0 {
		info := ProcessInfo{
			PID:         pid,
			ProcessName: fmt.Sprintf("pid-%d", pid),
			BundleID:    "com.apple.unknown",
			ResolvedAt:  time.Now(),
		}
		ins.pidCache.Store(pid, info)
		return info
	}

	// Convert null-terminated C string
	n := 0
	for ; n < len(buf) && buf[n] != 0; n++ {
	}
	execPath := string(buf[:n])
	procName := filepath.Base(execPath)
	bundleID := ins.extractBundleID(execPath, procName)

	info := ProcessInfo{
		PID:         pid,
		ProcessName: procName,
		BundleID:    bundleID,
		Path:        execPath,
		ResolvedAt:  time.Now(),
	}
	ins.pidCache.Store(pid, info)
	return info
}

// extractBundleID looks for .app bundle Info.plist or creates a sensible bundle identifier
func (ins *Inspector) extractBundleID(execPath, procName string) string {
	idx := strings.Index(execPath, ".app/")
	if idx != -1 {
		appRoot := execPath[:idx+4]
		plistPath := filepath.Join(appRoot, "Contents", "Info.plist")
		if data, err := os.ReadFile(plistPath); err == nil {
			content := string(data)
			if bIdx := strings.Index(content, "<key>CFBundleIdentifier</key>"); bIdx != -1 {
				sub := content[bIdx:]
				if sIdx := strings.Index(sub, "<string>"); sIdx != -1 {
					if eIdx := strings.Index(sub[sIdx+8:], "</string>"); eIdx != -1 {
						return strings.TrimSpace(sub[sIdx+8 : sIdx+8+eIdx])
					}
				}
			}
		}
		appName := filepath.Base(appRoot)
		appName = strings.TrimSuffix(appName, ".app")
		return "com.apple." + strings.ToLower(appName)
	}

	return "com.process." + strings.ToLower(procName)
}

// findPIDForPort queries sysctlbyname for net.inet.tcp.pcblist_n or net.inet.udp.pcblist_n
func (ins *Inspector) findPIDForPort(transport string, port int) int32 {
	sysctlName := "net.inet.tcp.pcblist_n"
	if transport == "udp" {
		sysctlName = "net.inet.udp.pcblist_n"
	}

	cName := C.CString(sysctlName)
	defer C.free(unsafe.Pointer(cName))

	var size C.size_t
	if ret := C.sysctlbyname(cName, nil, &size, nil, 0); ret != 0 || size == 0 {
		return 0
	}

	buf := make([]byte, size)
	if ret := C.sysctlbyname(cName, unsafe.Pointer(&buf[0]), &size, nil, 0); ret != 0 {
		return 0
	}

	return ins.parsePCBList(buf, port)
}

// parsePCBList iterates through the Darwin PCB list structures
func (ins *Inspector) parsePCBList(buf []byte, targetPort int) int32 {
	if len(buf) < 16 {
		return 0
	}

	offset := 0
	// Skip top-level header xinpgen (first 16 bytes: xig_len, xig_count, etc.)
	hdrLen := binary.LittleEndian.Uint32(buf[offset : offset+4])
	if hdrLen > 0 && int(hdrLen) <= len(buf) {
		offset = int(hdrLen)
	} else {
		offset = 16
	}

	targetPortBE := uint16(targetPort)

	for offset+32 < len(buf) {
		itemLen := binary.LittleEndian.Uint32(buf[offset : offset+4])
		if itemLen == 0 || offset+int(itemLen) > len(buf) {
			break
		}

		item := buf[offset : offset+int(itemLen)]

		// In Darwin xsocket_n / xinpcb_n:
		// Local port is stored in network byte order (big endian)
		// We scan for local port matching targetPortBE
		for i := 4; i < len(item)-8; i += 2 {
			p := binary.BigEndian.Uint16(item[i : i+2])
			if p == targetPortBE {
				// Search within item for effective PID (e_pid)
				// In xsocket_n, so_e_pid is at the tail
				for j := len(item) - 16; j <= len(item)-4; j += 4 {
					pid := int32(binary.LittleEndian.Uint32(item[j : j+4]))
					if pid > 0 && pid < 200000 {
						return pid
					}
				}
			}
		}

		offset += int(itemLen)
	}

	return 0
}
