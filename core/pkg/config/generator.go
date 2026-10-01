package config

import (
	"encoding/json"
	"fmt"
)

// ProxyMode defines routing behavior
type ProxyMode string

const (
	ModeRule   ProxyMode = "rule"
	ModeGlobal ProxyMode = "global"
	ModeDirect ProxyMode = "direct"
)

// VLESSOptions defines settings for VLESS-Reality outbound
type VLESSOptions struct {
	Tag        string `json:"tag"`
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
	UUID       string `json:"uuid"`
	Flow       string `json:"flow,omitempty"` // e.g. "xtls-rprx-vision"
	ServerName string `json:"server_name"`
	PublicKey  string `json:"public_key"`
	ShortID    string `json:"short_id,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"` // "chrome", "safari"
}

// Hysteria2Options defines settings for Hysteria 2 outbound
type Hysteria2Options struct {
	Tag          string `json:"tag"`
	Server       string `json:"server"`
	ServerPort   int    `json:"server_port"`
	Password     string `json:"password"`
	ServerName   string `json:"server_name,omitempty"`
	ObfsPassword string `json:"obfs_password,omitempty"`
	UpMbps       int    `json:"up_mbps,omitempty"`
	DownMbps     int    `json:"down_mbps,omitempty"`
	Ports        string `json:"ports,omitempty"` // port hopping range, e.g. "20000:40000"
}

// Profile holds configuration settings to generate sing-box JSON
type Profile struct {
	Mode         ProxyMode         `json:"mode"`
	TUNInterface string            `json:"tun_interface"` // default "utun100"
	TUNStack     string            `json:"tun_stack"`     // "system" or "gvisor"
	MixedPort    int               `json:"mixed_port"`    // default 10808
	VLESS        *VLESSOptions     `json:"vless,omitempty"`
	Hysteria2    *Hysteria2Options `json:"hysteria2,omitempty"`
}

// DefaultProfile returns a balanced production profile
func DefaultProfile() *Profile {
	return &Profile{
		Mode:         ModeRule,
		TUNInterface: "utun100",
		TUNStack:     "system",
		MixedPort:    10808,
	}
}

// GenerateSingBoxConfig builds a valid sing-box JSON configuration
func GenerateSingBoxConfig(p *Profile) ([]byte, error) {
	if p == nil {
		p = DefaultProfile()
	}

	if p.TUNInterface == "" {
		p.TUNInterface = "utun100"
	}
	if p.TUNStack == "" {
		p.TUNStack = "system"
	}
	if p.MixedPort == 0 {
		p.MixedPort = 10808
	}

	// 1. Inbounds
	inbounds := []map[string]any{
		{
			"type":                       "tun",
			"tag":                        "tun-in",
			"interface_name":             p.TUNInterface,
			"address":                    []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"},
			"mtu":                        9000,
			"auto_route":                 true,
			"strict_route":               true,
			"stack":                      p.TUNStack,
			"sniff":                      true,
			"sniff_override_destination": true,
		},
		{
			"type":        "mixed",
			"tag":         "mixed-in",
			"listen":      "127.0.0.1",
			"listen_port": p.MixedPort,
			"sniff":       true,
		},
	}

	// 2. Outbounds
	var outbounds []map[string]any

	// Active proxy outbound
	if p.VLESS != nil {
		vlessOut := map[string]any{
			"type":        "vless",
			"tag":         "proxy",
			"server":      p.VLESS.Server,
			"server_port": p.VLESS.ServerPort,
			"uuid":        p.VLESS.UUID,
		}
		if p.VLESS.Flow != "" {
			vlessOut["flow"] = p.VLESS.Flow
		}

		tlsMap := map[string]any{
			"enabled":     true,
			"server_name": p.VLESS.ServerName,
			"reality": map[string]any{
				"enabled":    true,
				"public_key": p.VLESS.PublicKey,
				"short_id":   p.VLESS.ShortID,
			},
		}
		fp := p.VLESS.Fingerprint
		if fp == "" {
			fp = "chrome"
		}
		tlsMap["utls"] = map[string]any{
			"enabled":     true,
			"fingerprint": fp,
		}
		vlessOut["tls"] = tlsMap
		outbounds = append(outbounds, vlessOut)
	} else if p.Hysteria2 != nil {
		hy2Out := map[string]any{
			"type":        "hysteria2",
			"tag":         "proxy",
			"server":      p.Hysteria2.Server,
			"server_port": p.Hysteria2.ServerPort,
			"password":    p.Hysteria2.Password,
		}
		if p.Hysteria2.ObfsPassword != "" {
			hy2Out["obfs"] = map[string]any{
				"type":     "salamander",
				"password": p.Hysteria2.ObfsPassword,
			}
		}
		if p.Hysteria2.UpMbps > 0 {
			hy2Out["up_mbps"] = p.Hysteria2.UpMbps
		}
		if p.Hysteria2.DownMbps > 0 {
			hy2Out["down_mbps"] = p.Hysteria2.DownMbps
		}
		if p.Hysteria2.Ports != "" {
			hy2Out["server_ports"] = p.Hysteria2.Ports
		}
		if p.Hysteria2.ServerName != "" {
			hy2Out["tls"] = map[string]any{
				"enabled":     true,
				"server_name": p.Hysteria2.ServerName,
			}
		}
		outbounds = append(outbounds, hy2Out)
	} else {
		// Fallback proxy to direct if no node configured yet
		outbounds = append(outbounds, map[string]any{
			"type": "direct",
			"tag":  "proxy",
		})
	}

	// Standard outbounds
	outbounds = append(outbounds,
		map[string]any{"type": "direct", "tag": "direct"},
		map[string]any{"type": "block", "tag": "block"},
		map[string]any{"type": "dns", "tag": "dns-out"},
	)

	// 3. DNS Configuration
	dnsConfig := map[string]any{
		"servers": []map[string]any{
			{
				"tag":             "dns-remote",
				"address":         "https://1.1.1.1/dns-query",
				"address_resolver": "dns-direct",
				"detour":          "proxy",
			},
			{
				"tag":     "dns-direct",
				"address": "local",
				"detour":  "direct",
			},
			{
				"tag":     "dns-fakeip",
				"address": "fakeip",
			},
		},
		"rules": []map[string]any{
			{
				"outbound": "any",
				"server":   "dns-direct",
			},
			{
				"clash_mode": "Direct",
				"server":     "dns-direct",
			},
			{
				"clash_mode": "Global",
				"server":     "dns-fakeip",
			},
			{
				"domain_suffix": []string{
					".local",
					"apple.com",
					"captive.apple.com",
					"push.apple.com",
					"icloud.com",
				},
				"server": "dns-direct",
			},
			{
				"query_type": []string{"A", "AAAA"},
				"server":     "dns-fakeip",
			},
		},
		"fakeip": map[string]any{
			"enabled":     true,
			"inet4_range": "198.18.0.0/15",
			"inet6_range": "fc00::/18",
		},
		"independent_cache": true,
	}

	// 4. Routing Configuration
	routeRules := []map[string]any{
		{
			"protocol": "dns",
			"outbound": "dns-out",
		},
		{
			"port":     []int{53},
			"outbound": "dns-out",
		},
		{
			"ip_is_private": true,
			"outbound":      "direct",
		},
	}

	switch p.Mode {
	case ModeDirect:
		// Send everything direct
		routeRules = append(routeRules, map[string]any{
			"outbound": "direct",
		})
	case ModeGlobal:
		// Send everything proxy
		routeRules = append(routeRules, map[string]any{
			"outbound": "proxy",
		})
	default: // ModeRule
		routeRules = append(routeRules,
			map[string]any{
				"domain_suffix": []string{".ru", ".su", ".xn--p1ai", "yandex.net", "vk.com", "tinkoff.ru", "sberbank.ru", "gosuslugi.ru"},
				"outbound":      "direct",
			},
			map[string]any{
				"geoip":    []string{"ru", "private"},
				"outbound": "direct",
			},
			map[string]any{
				"outbound": "proxy",
			},
		)
	}

	routeConfig := map[string]any{
		"rules":                routeRules,
		"auto_detect_interface": true,
	}

	// 5. Root Configuration
	root := map[string]any{
		"log": map[string]any{
			"level":     "info",
			"timestamp": true,
		},
		"dns":       dnsConfig,
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"route":     routeConfig,
	}

	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal sing-box config: %w", err)
	}

	return data, nil
}
