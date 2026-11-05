package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
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

		// Reject spaces in host
		if strings.Contains(host, " ") {
			return nil, fmt.Errorf("invalid LISTEN_ADDR %q: host cannot contain spaces", trimmed)
		}

		if host != "" {
			// If host has 4 parts, check if every part consists entirely of digits
			parts := strings.Split(host, ".")
			isIPv4Candidate := len(parts) == 4
			if isIPv4Candidate {
				for _, p := range parts {
					if len(p) == 0 {
						isIPv4Candidate = false
						break
					}
					for _, ch := range p {
						if ch < '0' || ch > '9' {
							isIPv4Candidate = false
							break
						}
					}
					if !isIPv4Candidate {
						break
					}
				}
			}
			if isIPv4Candidate {
				if net.ParseIP(host) == nil {
					return nil, fmt.Errorf("invalid LISTEN_ADDR %q: invalid host IP %q", trimmed, host)
				}
			} else {
				if net.ParseIP(host) == nil && !isValidHostname(host) {
					return nil, fmt.Errorf("invalid LISTEN_ADDR %q: host %q is neither a valid IP nor a valid hostname", trimmed, host)
				}
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

// corsMiddleware enforces allowed origins, adds Vary headers, and returns 403 on disallowed preflights.
func corsMiddleware(next http.Handler, allowedOrigins []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Always set Vary: Origin when Origin is supplied by client
		if origin != "" {
			w.Header().Add("Vary", "Origin")
		}

		originAllowed := false
		for _, allowed := range allowedOrigins {
			if allowed == "*" || allowed == origin {
				originAllowed = true
				break
			}
		}

		// Handle OPTIONS preflight requests
		if r.Method == http.MethodOptions {
			if origin != "" && !originAllowed {
				http.Error(w, "CORS origin forbidden", http.StatusForbidden)
				return
			}
			if originAllowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
				w.Header().Add("Vary", "Access-Control-Request-Method")
				w.Header().Add("Vary", "Access-Control-Request-Headers")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Standard GET requests
		if originAllowed && origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}

		next.ServeHTTP(w, r)
	})
}

// rootHandler ensures only exact "/" GET requests succeed.
func rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintln(w, "Infrastructure Insight")
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	provider := LinuxMetricsProvider{}

	mux := http.NewServeMux()
	mux.HandleFunc("/", rootHandler)
	mux.Handle("/metrics", metricsHandler(provider, cfg.TrustedProxies))
	mux.Handle("/api/metrics", metricsHandler(provider, cfg.TrustedProxies))

	wrappedHandler := corsMiddleware(mux, cfg.AllowedOrigins)

	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      wrappedHandler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	serverErrChan := make(chan error, 1)
	go func() {
		log.Printf("Server starting on %s", cfg.ListenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrChan <- err
		}
	}()

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrChan:
		log.Fatalf("Server startup failed: %v", err)
	case sig := <-stopChan:
		log.Printf("Received signal %v, initiating graceful shutdown...", sig)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server gracefully stopped.")
}
