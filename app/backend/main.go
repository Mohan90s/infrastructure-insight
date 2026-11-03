package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config holds validated application runtime configuration.
type Config struct {
	ListenAddr     string
	AllowedOrigins []string
	TrustedProxies []string
}

// isValidHostname validates an RFC 1123 compliant hostname or localhost.
func isValidHostname(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, part := range strings.Split(host, ".") {
		if len(part) == 0 || len(part) > 63 {
			return false
		}
		if part[0] == '-' || part[len(part)-1] == '-' {
			return false
		}
		for _, ch := range part {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-') {
				return false
			}
		}
	}
	return true
}

// loadConfig parses and strictly validates environment variables.
func loadConfig() (*Config, error) {
	// 1. LISTEN_ADDR
	rawListen := os.Getenv("LISTEN_ADDR")
	listenAddr := ":5000"
	if rawListen != "" {
		trimmed := strings.TrimSpace(rawListen)
		if trimmed == "" {
			return nil, fmt.Errorf("invalid LISTEN_ADDR %q: address cannot be empty or whitespace", rawListen)
		}

		host, portStr, err := net.SplitHostPort(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid LISTEN_ADDR %q: %w", trimmed, err)
		}

		// Ensure host is either a valid IP or valid RFC 1123 hostname
		if host != "" {
			if net.ParseIP(host) == nil && !isValidHostname(host) {
				return nil, fmt.Errorf("invalid LISTEN_ADDR %q: host %q is neither a valid IP nor a valid hostname", trimmed, host)
			}
		}

		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid LISTEN_ADDR %q: port %q must be a valid integer", trimmed, portStr)
		}
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid LISTEN_ADDR %q: port %d must be between 1 and 65535", trimmed, port)
		}

		listenAddr = trimmed
	}

	// 2. ALLOWED_ORIGINS
	var allowedOrigins []string
	if raw := os.Getenv("ALLOWED_ORIGINS"); raw != "" {
		seenOrigins := make(map[string]bool)
		for _, o := range strings.Split(raw, ",") {
			trimmed := strings.TrimSpace(o)
			if trimmed == "" {
				return nil, fmt.Errorf("invalid ALLOWED_ORIGINS %q: contains empty origin entry", raw)
			}
			if trimmed == "*" {
				if !seenOrigins["*"] {
					seenOrigins["*"] = true
					allowedOrigins = append(allowedOrigins, "*")
				}
				continue
			}
			parsedURL, err := url.Parse(trimmed)
			if err != nil {
				return nil, fmt.Errorf("invalid allowed origin %q: %w", trimmed, err)
			}
			if parsedURL.Opaque != "" {
				return nil, fmt.Errorf("invalid allowed origin %q: malformed URL", trimmed)
			}
			scheme := strings.ToLower(parsedURL.Scheme)
			if scheme != "http" && scheme != "https" {
				return nil, fmt.Errorf("invalid allowed origin %q: scheme must be http or https", trimmed)
			}
			if parsedURL.Host == "" {
				return nil, fmt.Errorf("invalid allowed origin %q: host must not be empty", trimmed)
			}
			if parsedURL.User != nil {
				return nil, fmt.Errorf("invalid allowed origin %q: must not contain user information", trimmed)
			}
			if parsedURL.RawQuery != "" {
				return nil, fmt.Errorf("invalid allowed origin %q: must not contain query parameters", trimmed)
			}
			if parsedURL.Fragment != "" {
				return nil, fmt.Errorf("invalid allowed origin %q: must not contain URL fragments", trimmed)
			}
			if parsedURL.Path != "" && parsedURL.Path != "/" {
				return nil, fmt.Errorf("invalid allowed origin %q: origins must not include URL paths", trimmed)
			}

			// Validate explicit port if provided
			if portStr := parsedURL.Port(); portStr != "" {
				originPort, err := strconv.Atoi(portStr)
				if err != nil || originPort < 1 || originPort > 65535 {
					return nil, fmt.Errorf("invalid allowed origin %q: port %q must be between 1 and 65535", trimmed, portStr)
				}
			}

			// Normalize origin by stripping trailing slash and lowercasing scheme/host
			normalized := fmt.Sprintf("%s://%s", scheme, strings.ToLower(parsedURL.Host))
			if !seenOrigins[normalized] {
				seenOrigins[normalized] = true
				allowedOrigins = append(allowedOrigins, normalized)
			}
		}
	} else {
		allowedOrigins = []string{"http://192.168.56.10", "http://localhost", "http://127.0.0.1"}
	}

	// 3. TRUSTED_PROXIES
	var trustedProxies []string
	if raw := os.Getenv("TRUSTED_PROXIES"); raw != "" {
		seenProxies := make(map[string]bool)
		for _, p := range strings.Split(raw, ",") {
			trimmed := strings.TrimSpace(p)
			if trimmed == "" {
				return nil, fmt.Errorf("invalid TRUSTED_PROXIES %q: contains empty proxy entry", raw)
			}
			parsedIP := net.ParseIP(trimmed)
			if parsedIP == nil {
				return nil, fmt.Errorf("invalid trusted proxy IP address %q", trimmed)
			}
			canonicalIP := parsedIP.String()
			if !seenProxies[canonicalIP] {
				seenProxies[canonicalIP] = true
				trustedProxies = append(trustedProxies, canonicalIP)
			}
		}
	} else {
		// Production defaults: web-01, web-02, and localhost
		// Production defaults: haproxy, web-01, web-02, and localhost
		trustedProxies = []string{"192.168.56.10", "192.168.56.11", "192.168.56.12", "127.0.0.1", "::1"}
	}

	return &Config{
		ListenAddr:     listenAddr,
		AllowedOrigins: allowedOrigins,
		TrustedProxies: trustedProxies,
	}, nil
}
