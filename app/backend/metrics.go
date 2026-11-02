package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
)

type ServerInfo struct {
	Hostname      string  `json:"hostname"`
	OS            string  `json:"os"`
	Architecture  string  `json:"architecture"`
	UptimeSeconds float64 `json:"uptimeSeconds"`
}

type CPUInfo struct {
	Cores         int     `json:"cores"`
	LoadAverage1  float64 `json:"loadAverage1"`
	LoadAverage5  float64 `json:"loadAverage5"`
	LoadAverage15 float64 `json:"loadAverage15"`
}

type MemoryInfo struct {
	AppUsedMB         uint64  `json:"appUsedMB"`
	SystemTotalMB     uint64  `json:"systemTotalMB"`
	SystemAvailableMB uint64  `json:"systemAvailableMB"`
	UsagePercent      float64 `json:"usagePercent"`
}

type MetricsResponse struct {
	Server   ServerInfo `json:"server"`
	CPU      CPUInfo    `json:"cpu"`
	Memory   MemoryInfo `json:"memory"`
	ClientIP string     `json:"clientIP"`
}

// MetricsProvider defines an interface for collecting system metrics.
// This allows us to inject fake metrics during unit tests.
type MetricsProvider interface {
	Collect() (MetricsResponse, error)
}

// LinuxMetricsProvider collects real system metrics from the Linux OS.
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
		return MetricsResponse{}, fmt.Errorf("reading memory: %w", err)
	}

	var memoryUsagePercent float64
	if totalMB > 0 && availableMB <= totalMB {
		usedMemory := totalMB - availableMB
		memoryUsagePercent = float64(usedMemory) / float64(totalMB) * 100
	}

	load1, load5, load15, err := getCPULoad()
	if err != nil {
		return MetricsResponse{}, fmt.Errorf("reading CPU load: %w", err)
	}

	uptime, err := getSystemUptime()
	if err != nil {
		return MetricsResponse{}, fmt.Errorf("reading uptime: %w", err)
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

// metricsHandler creates an http.HandlerFunc using the provided MetricsProvider.
func metricsHandler(provider MetricsProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Enforce GET method only
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		metrics, err := provider.Collect()
		if err != nil {
			http.Error(w, "Failed to collect system metrics", http.StatusInternalServerError)
			return
		}

		// Attach the real client IP extracted from proxy headers
		metrics.ClientIP = getClientIP(r)

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(metrics); err != nil {
			logEncodeError(err)
		}
	}
}

// parseSystemMemory parses raw /proc/meminfo bytes into total and available MB.
// Pure function: can be unit-tested without disk access.
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
			val, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("invalid MemTotal: %w", err)
			}
			totalKB = val
			foundTotal = true

		case "MemAvailable:":
			val, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("invalid MemAvailable: %w", err)
			}
			availableKB = val
			foundAvailable = true
		}
	}

	if !foundTotal {
		return 0, 0, fmt.Errorf("MemTotal not found in meminfo")
	}
	if !foundAvailable {
		return 0, 0, fmt.Errorf("MemAvailable not found in meminfo")
	}
	if totalKB == 0 || availableKB > totalKB {
		return 0, 0, fmt.Errorf("invalid memory values: total=%d, available=%d", totalKB, availableKB)
	}

	return totalKB / 1024, availableKB / 1024, nil
}

// getSystemMemory reads /proc/meminfo and passes the bytes to parseSystemMemory.
func getSystemMemory() (uint64, uint64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, fmt.Errorf("reading /proc/meminfo: %w", err)
	}
	return parseSystemMemory(data)
}

// parseCPULoad parses raw /proc/loadavg bytes into 1, 5, and 15-minute load averages.
// Pure function: can be unit-tested without disk access.
func parseCPULoad(data []byte) (float64, float64, float64, error) {
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0, fmt.Errorf("invalid /proc/loadavg format: expected at least 3 fields")
	}

	load1, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid 1-min load: %w", err)
	}

	load5, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid 5-min load: %w", err)
	}

	load15, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid 15-min load: %w", err)
	}

	if load1 < 0 || load5 < 0 || load15 < 0 {
		return 0, 0, 0, fmt.Errorf("CPU load values cannot be negative")
	}

	return load1, load5, load15, nil
}

// getCPULoad reads /proc/loadavg and passes the bytes to parseCPULoad.
func getCPULoad() (float64, float64, float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, fmt.Errorf("reading /proc/loadavg: %w", err)
	}
	return parseCPULoad(data)
}

func parseSystemUptime(data []byte) (float64, error) {
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, fmt.Errorf("invalid /proc/uptime format")
	}
	return strconv.ParseFloat(fields[0], 64)
}

func getSystemUptime() (float64, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, fmt.Errorf("reading /proc/uptime: %w", err)
	}
	return parseSystemUptime(data)
}

func logEncodeError(err error) {
	fmt.Printf("Failed to encode metrics response: %v\n", err)
}

// getClientIP extracts the original client IP from proxy headers, falling back to RemoteAddr.
func getClientIP(r *http.Request) string {
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		parts := strings.Split(forwarded, ",")
		clientIP := strings.TrimSpace(parts[0])
		if clientIP != "" {
			return clientIP
		}
	}

	realIP := strings.TrimSpace(r.Header.Get("X-Real-IP"))
	if realIP != "" {
		return realIP
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
