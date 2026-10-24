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

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		clientIP = r.RemoteAddr
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	appMemoryUsedMB := memStats.Alloc / 1024 / 1024

	totalMB, availableMB, err := getSystemMemory()
	if err != nil {
		http.Error(w, "Failed to read system memory",
			http.StatusInternalServerError)
		return
	}

	var memoryUsagePercent float64
	if totalMB > 0 && availableMB <= totalMB {
		usedMemory := totalMB - availableMB
		memoryUsagePercent = float64(usedMemory) /
			float64(totalMB) * 100
	}

	load1, load5, load15, err := getCPULoad()
	if err != nil {
		http.Error(w, "Failed to read CPU load",
			http.StatusInternalServerError)
		return
	}

	uptime, err := getSystemUptime()
	if err != nil {
		http.Error(w, "Failed to read system uptime",
			http.StatusInternalServerError)
		return
	}

	metrics := MetricsResponse{
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
		ClientIP: clientIP,
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		logEncodeError(err)
	}
}

func getSystemMemory() (uint64, uint64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}

	var totalKB uint64
	var availableKB uint64

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		switch fields[0] {
		case "MemTotal:":
			totalKB, err = strconv.ParseUint(fields[1], 10, 64)
		case "MemAvailable:":
			availableKB, err = strconv.ParseUint(fields[1], 10, 64)
		default:
			continue
		}

		if err != nil {
			return 0, 0, err
		}
	}

	if totalKB == 0 {
		return 0, 0, fmt.Errorf("total system memory not found")
	}

	return totalKB / 1024, availableKB / 1024, nil
}

func getCPULoad() (float64, float64, float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0, err
	}

	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0, fmt.Errorf("invalid /proc/loadavg format")
	}

	load1, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, 0, 0, err
	}

	load5, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, 0, 0, err
	}

	load15, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return 0, 0, 0, err
	}

	return load1, load5, load15, nil
}

func getSystemUptime() (float64, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}

	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, fmt.Errorf("invalid /proc/uptime format")
	}

	return strconv.ParseFloat(fields[0], 64)
}

func logEncodeError(err error) {
	fmt.Printf("Failed to encode metrics response: %v\n", err)
}
