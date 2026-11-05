package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// 1. Test default configuration when no environment variables are set
func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("LISTEN_ADDR", "")
	t.Setenv("ALLOWED_ORIGINS", "")
	t.Setenv("TRUSTED_PROXIES", "")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading default config: %v", err)
	}

	if cfg.ListenAddr != ":5000" {
		t.Errorf("expected default ListenAddr ':5000', got %q", cfg.ListenAddr)
	}

	expectedOrigins := []string{"http://192.168.56.10", "http://localhost", "http://127.0.0.1"}
	if !reflect.DeepEqual(cfg.AllowedOrigins, expectedOrigins) {
		t.Errorf("expected default AllowedOrigins %v, got %v", expectedOrigins, cfg.AllowedOrigins)
	}

	expectedProxies := []string{"192.168.56.10", "192.168.56.11", "192.168.56.12", "127.0.0.1", "::1"}
	if !reflect.DeepEqual(cfg.TrustedProxies, expectedProxies) {
		t.Errorf("expected default TrustedProxies %v, got %v", expectedProxies, cfg.TrustedProxies)
	}
}

// 2. Test LISTEN_ADDR validation (ports, ranges, malformed addresses)
func TestLoadConfig_ListenAddr(t *testing.T) {
	tests := []struct {
		name        string
		envVal      string
		expected    string
		expectError bool
		errContains string
	}{
		{
			name:        "valid custom port",
			envVal:      ":8080",
			expected:    ":8080",
			expectError: false,
		},
		{
			name:        "valid host and port",
			envVal:      "127.0.0.1:3000",
			expected:    "127.0.0.1:3000",
			expectError: false,
		},
		{
			name:        "valid all interfaces",
			envVal:      "0.0.0.0:5000",
			expected:    "0.0.0.0:5000",
			expectError: false,
		},
		{
			name:        "valid ipv6 address",
			envVal:      "[::1]:9000",
			expected:    "[::1]:9000",
			expectError: false,
		},
		{
			name:        "empty whitespace only",
			envVal:      "   ",
			expectError: true,
			errContains: "cannot be empty or whitespace",
		},
		{
			name:        "missing port",
			envVal:      "127.0.0.1",
			expectError: true,
			errContains: "missing port in address",
		},
		{
			name:        "non-numeric port",
			envVal:      ":abc",
			expectError: true,
			errContains: "must be a valid integer",
		},
		{
			name:        "port zero invalid",
			envVal:      ":0",
			expectError: true,
			errContains: "between 1 and 65535",
		},
		{
			name:        "port too large",
			envVal:      ":65536",
			expectError: true,
			errContains: "between 1 and 65535",
		},
		{
			name:        "negative port",
			envVal:      ":-1",
			expectError: true,
			errContains: "between 1 and 65535",
		},
		{
			name:        "host containing spaces",
			envVal:      "127.0.0.1 :5000",
			expectError: true,
			errContains: "host cannot contain spaces",
		},
		{
			name:        "invalid host IP",
			envVal:      "999.999.999.999:5000",
			expectError: true,
			errContains: "invalid host IP",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LISTEN_ADDR", tc.envVal)
			t.Setenv("ALLOWED_ORIGINS", "")
			t.Setenv("TRUSTED_PROXIES", "")

			cfg, err := loadConfig()
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error for LISTEN_ADDR=%q, got nil", tc.envVal)
				}
				if !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error to contain %q, got %q", tc.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for LISTEN_ADDR=%q: %v", tc.envVal, err)
				}
				if cfg.ListenAddr != tc.expected {
					t.Errorf("expected ListenAddr %q, got %q", tc.expected, cfg.ListenAddr)
				}
			}
		})
	}
}

// 3. Test ALLOWED_ORIGINS validation (HTTP/HTTPS, trailing slashes, paths, queries)
func TestLoadConfig_AllowedOrigins(t *testing.T) {
	tests := []struct {
		name        string
		envVal      string
		expected    []string
		expectError bool
		errContains string
	}{
		{
			name:        "valid single origin",
			envVal:      "http://example.com",
			expected:    []string{"http://example.com"},
			expectError: false,
		},
		{
			name:        "valid multiple origins with trailing slash normalization and deduplication",
			envVal:      "http://localhost/, https://dashboard.local:8080, http://localhost",
			expected:    []string{"http://localhost", "https://dashboard.local:8080"},
			expectError: false,
		},
		{
			name:        "wildcard origin",
			envVal:      "*",
			expected:    []string{"*"},
			expectError: false,
		},
		{
			name:        "empty entry in comma-separated list",
			envVal:      "http://localhost,,http://example.com",
			expectError: true,
			errContains: "contains empty origin entry",
		},
		{
			name:        "unsupported scheme ftp",
			envVal:      "ftp://example.com",
			expectError: true,
			errContains: "scheme must be http or https",
		},
		{
			name:        "empty host",
			envVal:      "http://",
			expectError: true,
			errContains: "host must not be empty",
		},
		{
			name:        "contains path",
			envVal:      "http://example.com/api/v1",
			expectError: true,
			errContains: "origins must not include URL paths",
		},
		{
			name:        "contains query parameters",
			envVal:      "http://example.com?foo=bar",
			expectError: true,
			errContains: "must not contain query parameters",
		},
		{
			name:        "contains fragment",
			envVal:      "http://example.com#section",
			expectError: true,
			errContains: "must not contain URL fragments",
		},
		{
			name:        "contains user info",
			envVal:      "http://user:pass@example.com",
			expectError: true,
			errContains: "must not contain user information",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LISTEN_ADDR", "")
			t.Setenv("ALLOWED_ORIGINS", tc.envVal)
			t.Setenv("TRUSTED_PROXIES", "")

			cfg, err := loadConfig()
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error for ALLOWED_ORIGINS=%q, got nil", tc.envVal)
				}
				if !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error to contain %q, got %q", tc.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for ALLOWED_ORIGINS=%q: %v", tc.envVal, err)
				}
				if !reflect.DeepEqual(cfg.AllowedOrigins, tc.expected) {
					t.Errorf("expected AllowedOrigins %v, got %v", tc.expected, cfg.AllowedOrigins)
				}
			}
		})
	}
}

// 4. Test TRUSTED_PROXIES validation (IP parsing, whitespace trimming, deduplication)
func TestLoadConfig_TrustedProxies(t *testing.T) {
	tests := []struct {
		name        string
		envVal      string
		expected    []string
		expectError bool
		errContains string
	}{
		{
			name:        "valid ipv4 and ipv6 with trimming and deduplication",
			envVal:      "10.0.0.1,  ::1 , 10.0.0.1, 192.168.1.1",
			expected:    []string{"10.0.0.1", "::1", "192.168.1.1"},
			expectError: false,
		},
		{
			name:        "empty entry in list",
			envVal:      "10.0.0.1,,10.0.0.2",
			expectError: true,
			errContains: "contains empty proxy entry",
		},
		{
			name:        "invalid IP address",
			envVal:      "10.0.0.1, not-an-ip",
			expectError: true,
			errContains: "invalid trusted proxy IP address",
		},
		{
			name:        "out of range IP",
			envVal:      "999.999.999.999",
			expectError: true,
			errContains: "invalid trusted proxy IP address",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LISTEN_ADDR", "")
			t.Setenv("ALLOWED_ORIGINS", "")
			t.Setenv("TRUSTED_PROXIES", tc.envVal)

			cfg, err := loadConfig()
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error for TRUSTED_PROXIES=%q, got nil", tc.envVal)
				}
				if !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error to contain %q, got %q", tc.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for TRUSTED_PROXIES=%q: %v", tc.envVal, err)
				}
				if !reflect.DeepEqual(cfg.TrustedProxies, tc.expected) {
					t.Errorf("expected TrustedProxies %v, got %v", tc.expected, cfg.TrustedProxies)
				}
			}
		})
	}
}

// 5. Test CORS Middleware (Preflight 204, Forbidden 403, and Vary headers)
func TestCorsMiddleware(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	origins := []string{"http://localhost", "https://app.example.com"}
	handler := corsMiddleware(dummyHandler, origins)

	// Preflight OPTIONS from allowed origin -> 204 No Content
	req := httptest.NewRequest(http.MethodOptions, "/metrics", nil)
	req.Header.Set("Origin", "http://localhost")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("OPTIONS allowed origin: expected 204 No Content, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "http://localhost" {
		t.Errorf("expected Access-Control-Allow-Origin header set to origin")
	}

	// Preflight OPTIONS from disallowed origin -> 403 Forbidden
	req = httptest.NewRequest(http.MethodOptions, "/metrics", nil)
	req.Header.Set("Origin", "http://evil-attacker.com")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("OPTIONS disallowed origin: expected 403 Forbidden, got %d", rr.Code)
	}

	// GET with allowed origin -> 200 OK with Vary: Origin
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("GET allowed origin: expected 200 OK, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Errorf("expected Access-Control-Allow-Origin header set on GET")
	}
	if !strings.Contains(rr.Header().Get("Vary"), "Origin") {
		t.Errorf("expected Vary: Origin header")
	}
}

// 6. Test Root Handler routing and method restriction
func TestRootHandler_MethodsAndRouting(t *testing.T) {
	// GET / -> 200 OK
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	rootHandler(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rr.Code)
	}

	// POST / -> 405 Method Not Allowed
	req = httptest.NewRequest(http.MethodPost, "/", nil)
	rr = httptest.NewRecorder()
	rootHandler(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", rr.Code)
	}
	if rr.Header().Get("Allow") != http.MethodGet {
		t.Errorf("expected Allow header: GET")
	}

	// GET /not-root -> 404 Not Found
	req = httptest.NewRequest(http.MethodGet, "/not-root", nil)
	rr = httptest.NewRecorder()
	rootHandler(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d", rr.Code)
	}
}
