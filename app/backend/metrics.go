package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// ServerInfo holds basic system metadata.
type ServerInfo struct {
	Hostname      string  `json:"hostname"`
	OS            string  `json:"os"`
	Architecture  string  `json:"architecture"`
	UptimeSeconds float64 `json:"uptimeSeconds"`
}

// CPUInfo holds CPU core counts and standard UNIX load averages.
// Note: Load averages represent runnable queue length, not CPU utilization percentage.
type CPUInfo struct {
	Cores         int     `json:"cores"`
	LoadAverage1  float64 `json:"loadAverage1"`
	LoadAverage5  float64 `json:"loadAverage5"`
	LoadAverage15 float64 `json:"loadAverage15"`
}

// MemoryInfo holds system and Go runtime memory statistics in megabytes.
// Note: AppUsedMB measures active Go heap allocations (runtime.MemStats.Alloc),
// not the complete Resident Set Size (RSS) of the OS process.
type MemoryInfo struct {
	AppUsedMB         uint64  `json:"appUsedMB"`
	SystemTotalMB     uint64  `json:"systemTotalMB"`
	SystemAvailableMB uint64  `json:"systemAvailableMB"`
	UsagePercent      float64 `json:"usagePercent"`
}

// MetricsResponse is the top-level JSON structure sent to the dashboard.
type MetricsResponse struct {
	Server   ServerInfo `json:"server"`
	CPU      CPUInfo    `json:"cpu"`
	Memory   MemoryInfo `json:"memory"`
	ClientIP string     `json:"clientIP"`
}

// MetricsProvider defines an abstraction for collecting system metrics.
// Allows injecting mock providers during unit tests without requiring Linux.
type MetricsProvider interface {
	Collect() (MetricsResponse, error)
}

// LinuxMetricsProvider collects live telemetry from the Linux OS /proc filesystem.
// Note: This provider is intentionally Linux-specific and requires /proc/meminfo,
// /proc/loadavg, and /proc/uptime to be mounted.
type LinuxMetricsProvider struct{}

func (p LinuxMetricsProvider) Collect() (MetricsResponse, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	appMemoryUsedMB := memStats.Alloc / 1024 / 1024

	totalMB, availableMB, err := getSystemMemory()
	if err != nil {
		return MetricsResponse{}, fmt.Errorf("collecting system memory: %w", err)
	}

	var memoryUsagePercent float64
	if totalMB > 0 && availableMB <= totalMB {
		usedMemory := totalMB - availableMB
		memoryUsagePercent = float64(usedMemory) / float64(totalMB) * 100
	}

	load1, load5, load15, err := getCPULoad()
	if err != nil {
		return MetricsResponse{}, fmt.Errorf("collecting CPU load: %w", err)
	}

	uptime, err := getSystemUptime()
	if err != nil {
		return MetricsResponse{}, fmt.Errorf("collecting uptime: %w", err)
	}

	return MetricsResponse{
		Server: ServerInfo{
			Hostname:      hostname,
			OS:            runtime.GOOS,
			Architecture:  runtime.GOARCH,
			UptimeSeconds: uptime,
		},
		CPU: CPUInfo{
			Cores:         runtime.NumCPU(),
			LoadAverage1:  load1,
			LoadAverage5:  load5,
			LoadAverage15: load15,
		},
		Memory: MemoryInfo{
			AppUsedMB:         appMemoryUsedMB,
			SystemTotalMB:     totalMB,
			SystemAvailableMB: availableMB,
			UsagePercent:      memoryUsagePercent,
		},
	}, nil
}

// metricsHandler creates an HTTP handler utilizing the supplied MetricsProvider.
func metricsHandler(provider MetricsProvider, trustedProxies []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		metrics, err := provider.Collect()
		if err != nil {
			log.Printf("ERROR: Failed to collect metrics: %v", err)
			http.Error(w, "Failed to collect system metrics", http.StatusInternalServerError)
			return
		}

		metrics.ClientIP = getClientIP(r, trustedProxies)

		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(metrics); err != nil {
			log.Printf("ERROR: Failed to encode metrics response: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	}
}

// parseSystemMemory parses raw /proc/meminfo bytes into total and available MB.
// Enforces required "kB" unit and rejects duplicate entries.
func parseSystemMemory(data []byte) (uint64, uint64, error) {
	var totalKB uint64
	var availableKB uint64
	foundTotal := false
	foundAvailable := false

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		switch fields[0] {
		case "MemTotal:":
			if foundTotal {
				return 0, 0, fmt.Errorf("duplicate MemTotal field in meminfo")
			}
			if len(fields) < 3 {
				return 0, 0, fmt.Errorf("missing unit for MemTotal")
			}
			if !strings.EqualFold(fields[2], "kb") {
				return 0, 0, fmt.Errorf("unexpected unit for MemTotal: %s (expected kB)", fields[2])
			}
			val, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("invalid numeric value for MemTotal: %w", err)
			}
			totalKB = val
			foundTotal = true

		case "MemAvailable:":
			if foundAvailable {
				return 0, 0, fmt.Errorf("duplicate MemAvailable field in meminfo")
			}
			if len(fields) < 3 {
				return 0, 0, fmt.Errorf("missing unit for MemAvailable")
			}
			if !strings.EqualFold(fields[2], "kb") {
				return 0, 0, fmt.Errorf("unexpected unit for MemAvailable: %s (expected kB)", fields[2])
			}
			val, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("invalid numeric value for MemAvailable: %w", err)
			}
			availableKB = val
			foundAvailable = true
		}
	}

	if !foundTotal {
		return 0, 0, fmt.Errorf("required field MemTotal not found in meminfo")
	}
	if !foundAvailable {
		return 0, 0, fmt.Errorf("required field MemAvailable not found in meminfo")
	}
	if totalKB == 0 {
		return 0, 0, fmt.Errorf("system total memory cannot be zero")
	}
	if availableKB > totalKB {
		return 0, 0, fmt.Errorf("available memory (%d kB) exceeds total memory (%d kB)", availableKB, totalKB)
	}

	return totalKB / 1024, availableKB / 1024, nil
}

func getSystemMemory() (uint64, uint64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, fmt.Errorf("reading /proc/meminfo: %w", err)
	}
	return parseSystemMemory(data)
}

// parseCPULoad parses /proc/loadavg into 1, 5, and 15-minute load values.
// Rejects non-numeric, negative, NaN, and Inf values.
func parseCPULoad(data []byte) (float64, float64, float64, error) {
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0, fmt.Errorf("invalid /proc/loadavg format: expected at least 3 fields, got %d", len(fields))
	}

	parseVal := func(s, label string) (float64, error) {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid numeric %s: %w", label, err)
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, fmt.Errorf("%s cannot be NaN or infinite", label)
		}
		if v < 0 {
			return 0, fmt.Errorf("%s cannot be negative (%f)", label, v)
		}
		return v, nil
	}

	l1, err := parseVal(fields[0], "1-min load")
	if err != nil {
		return 0, 0, 0, err
	}
	l5, err := parseVal(fields[1], "5-min load")
	if err != nil {
		return 0, 0, 0, err
	}
	l15, err := parseVal(fields[2], "15-min load")
	if err != nil {
		return 0, 0, 0, err
	}

	return l1, l5, l15, nil
}

func getCPULoad() (float64, float64, float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0, fmt.Errorf("reading /proc/loadavg: %w", err)
	}
	return parseCPULoad(data)
}

// parseSystemUptime parses /proc/uptime into seconds.
// Rejects empty, negative, NaN, and Inf values.
func parseSystemUptime(data []byte) (float64, error) {
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, fmt.Errorf("invalid /proc/uptime format: empty input")
	}

	uptime, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric uptime: %w", err)
	}
	if math.IsNaN(uptime) || math.IsInf(uptime, 0) {
		return 0, fmt.Errorf("uptime cannot be NaN or infinite")
	}
	if uptime < 0 {
		return 0, fmt.Errorf("uptime cannot be negative (%f)", uptime)
	}

	return uptime, nil
}

func getSystemUptime() (float64, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, fmt.Errorf("reading /proc/uptime: %w", err)
	}
	return parseSystemUptime(data)
}

// isIPInList checks if targetIP matches any IP in the provided whitelist string slice.
func isIPInList(target net.IP, list []string) bool {
	if target == nil {
		return false
	}
	for _, item := range list {
		parsed := net.ParseIP(strings.TrimSpace(item))
		if parsed != nil && parsed.Equal(target) {
			return true
		}
	}
	return false
}

// / getClientIP extracts the original client IP using right-to-left proxy chain traversal.
// Forwarding headers are only inspected if the direct connection originates from a trusted proxy.
func getClientIP(r *http.Request, trustedProxies []string) string {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}

	directIP := net.ParseIP(strings.TrimSpace(remoteHost))
	if directIP == nil {
		return r.RemoteAddr
	}

	// 1. If direct connection is not from a trusted proxy, ignore all forwarding headers.
	if !isIPInList(directIP, trustedProxies) {
		return directIP.String()
	}

	// 2. Inspect X-Forwarded-For using strict right-to-left traversal
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		var validIPs []net.IP
		validChain := true

		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			// Reject empty entries: a valid proxy chain must never have empty tokens
			if trimmed == "" {
				validChain = false
				break
			}
			parsed := net.ParseIP(trimmed)
			if parsed == nil {
				validChain = false
				break
			}
			validIPs = append(validIPs, parsed)
		}

		// Only trust the chain if every entry parsed without error
		if validChain && len(validIPs) > 0 {
			// Walk backwards from right to left (most recent proxy to earliest)
			for i := len(validIPs) - 1; i >= 0; i-- {
				if !isIPInList(validIPs[i], trustedProxies) {
					// Found the first untrusted IP from the right: genuine client IP
					return validIPs[i].String()
				}
			}
			// If all IPs in XFF were trusted proxies, return the leftmost one
			return validIPs[0].String()
		}
	}

	// 3. Fallback to X-Real-IP only if valid and NOT a trusted proxy IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		trimmed := strings.TrimSpace(xri)
		if parsed := net.ParseIP(trimmed); parsed != nil {
			// Do not return an internal proxy IP as the client IP
			if !isIPInList(parsed, trustedProxies) {
				return parsed.String()
			}
		}
	}

	// 4. Safe fallback: direct connection IP
	return directIP.String()
}
