package main

import (
	"testing"
)

// 1. Test /proc/meminfo parsing
func TestParseSystemMemory(t *testing.T) {
	t.Run("valid meminfo", func(t *testing.T) {
		meminfo := []byte(`MemTotal:        4018348 kB
MemFree:          847320 kB
MemAvailable:    2516582 kB
Buffers:          123456 kB
`)
		total, avail, err := parseSystemMemory(meminfo)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if total != 4018348/1024 {
			t.Errorf("expected total %d MB, got %d MB", 4018348/1024, total)
		}
		if avail != 2516582/1024 {
			t.Errorf("expected avail %d MB, got %d MB", 2516582/1024, avail)
		}
	})

	t.Run("missing kB unit", func(t *testing.T) {
		MemoryInfo := []byte(`MemTotal: 4018348
		MemAvailable: 2516582 kB`)
		_, _, err := parseSystemMemory(MemoryInfo)
		if err == nil {
			t.Fatal("expected error for missing unit, got nil")
		}
	})
	t.Run("Invalid unit MB", func(t *testing.T) {
		MemoryInfo := []byte(`MemTotal: 4018348 MB
		MemAvailable: 2516582 kB`)
		_, _, err := parseSystemMemory(MemoryInfo)
		if err == nil {
			t.Fatal("expected error for missing unit, got nil")
		}
	})
	t.Run("duplicate MemTotal", func(t *testing.T) {
		meminfo := []byte(`MemTotal:        4018348 kB
MemTotal:        4018348 kB
MemAvailable:    2516582 kB`)
		_, _, err := parseSystemMemory(meminfo)
		if err == nil {
			t.Fatal("expected error for duplicate MemTotal, got nil")
		}
	})
	t.Run("available exceeds total", func(t *testing.T) {
		meminfo := []byte(`MemTotal:        1000000 kB
MemAvailable:    2000000 kB`)
		_, _, err := parseSystemMemory(meminfo)
		if err == nil {
			t.Fatal("expected error when available exceeds total, got nil")
		}
	})
}
/ 2. Test /proc/loadavg parsing
func TestParseCPULoad(t *testing.T) {
	t.Run("valid loadavg", func(t *testing.T) {
		data := []byte("0.15 0.25 0.35 1/150 12345\n")
		l1, l5, l15, err := parseCPULoad(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if l1 != 0.15 || l5 != 0.25 || l15 != 0.35 {
			t.Errorf("unexpected load values: %f, %f, %f", l1, l5, l15)
		}
	})
	t.Run("insufficient fields", func(t *testing.T) {
		data := []byte("0.15 0.25\n")
		_, _, _, err := parseCPULoad(data)
		if err == nil {
			t.Fatal("expected error for insufficient fields")
		}
	})
	t.Run("negative load", func(t *testing.T) {
		data := []byte("-0.15 0.25 0.35 1/150 12345\n")
		_, _, _, err := parseCPULoad(data)
		if err == nil {
			t.Fatal("expected error for negative load")
		}
	})
	t.Run("non-numeric load", func(t *testing.T) {
		data := []byte("abc 0.25 0.35 1/150 12345\n")
		_, _, _, err := parseCPULoad(data)
		if err == nil {
			t.Fatal("expected error for non-numeric load")
		}
	})
}
// 3. Test /proc/uptime parsing
func TestParseSystemUptime(t *testing.T) {
	t.Run("valid uptime", func(t *testing.T) {
		data := []byte("12345.67 89012.34\n")
		uptime, err := parseSystemUptime(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if uptime != 12345.67 {
			t.Errorf("expected uptime 12345.67, got %f", uptime)
		}
	})
	t.Run("negative uptime", func(t *testing.T) {
		data := []byte("-123.45 89012.34\n")
		_, err := parseSystemUptime(data)
		if err == nil {
			t.Fatal("expected error for negative uptime")
		}
	})
	t.Run("empty data", func(t *testing.T) {
		data := []byte("")
		_, err := parseSystemUptime(data)
		if err == nil {
			t.Fatal("expected error for empty data")
		}
	})
}
/ 4. Test Client IP extraction and proxy chain security
func TestGetClientIP_Security(t *testing.T) {
	trusted := []string{"192.168.56.10", "192.168.56.11", "192.168.56.12", "127.0.0.1"}
	t.Run("direct connection from untrusted IP ignores spoofed X-Forwarded-For", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.RemoteAddr = "203.0.113.50:45678" // Untrusted direct client
		req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.56.11")
		clientIP := getClientIP(req, trusted)
		if clientIP != "203.0.113.50" {
			t.Errorf("expected direct untrusted IP 203.0.113.50, got %q", clientIP)
		}
	})
	t.Run("full infrastructure chain: HAProxy + Nginx", func(t *testing.T) {
		// Client 203.0.113.99 -> HAProxy (10) -> Nginx (11) -> Backend
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.RemoteAddr = "192.168.56.11:51234" // Nginx
		req.Header.Set("X-Forwarded-For", "203.0.113.99, 192.168.56.10")
		clientIP := getClientIP(req, trusted)
		if clientIP != "203.0.113.99" {
			t.Errorf("expected client IP 203.0.113.99, got %q", clientIP)
		}
	})
	t.Run("right-to-left traversal resists client-injected spoofed headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.RemoteAddr = "192.168.56.11:51234"
		// Spoofed IP: 1.1.1.1; Real Client: 203.0.113.99; Proxy: 192.168.56.10
		req.Header.Set("X-Forwarded-For", "1.1.1.1, 203.0.113.99, 192.168.56.10")
		clientIP := getClientIP(req, trusted)
		if clientIP != "203.0.113.99" {
			t.Errorf("expected rightmost untrusted IP 203.0.113.99, got %q", clientIP)
		}
	})
	t.Run("reject empty entries in X-Forwarded-For", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.RemoteAddr = "192.168.56.11:51234"
		req.Header.Set("X-Forwarded-For", "203.0.113.99, , 192.168.56.10")
		// Malformed chain falls back to direct remote IP
		clientIP := getClientIP(req, trusted)
		if clientIP != "192.168.56.11" {
			t.Errorf("expected fallback to direct remote 192.168.56.11, got %q", clientIP)
		}
	})
}