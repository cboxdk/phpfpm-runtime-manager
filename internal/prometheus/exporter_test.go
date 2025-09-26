package prometheus

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"go.uber.org/zap/zaptest"
)

func TestAuthMiddleware(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name           string
		authConfig     config.AuthConfig
		request        func() *http.Request
		expectedStatus int
	}{
		{
			name: "auth disabled - should pass",
			authConfig: config.AuthConfig{
				Enabled: false,
			},
			request: func() *http.Request {
				return httptest.NewRequest("GET", "/metrics", nil)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "api key auth - valid bearer token",
			authConfig: config.AuthConfig{
				Enabled: true,
				Type:    "api_key",
				APIKey:  "test-api-key-32-characters-minimum-length",
			},
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/metrics", nil)
				req.Header.Set("Authorization", "Bearer test-api-key-32-characters-minimum-length")
				return req
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "api key auth - valid X-API-Key header",
			authConfig: config.AuthConfig{
				Enabled: true,
				Type:    "api_key",
				APIKey:  "test-api-key-32-characters-minimum-length",
			},
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/metrics", nil)
				req.Header.Set("X-API-Key", "test-api-key-32-characters-minimum-length")
				return req
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "api key auth - invalid key",
			authConfig: config.AuthConfig{
				Enabled: true,
				Type:    "api_key",
				APIKey:  "test-api-key-32-characters-minimum-length",
			},
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/metrics", nil)
				req.Header.Set("Authorization", "Bearer invalid-key")
				return req
			},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "api key auth - no key provided",
			authConfig: config.AuthConfig{
				Enabled: true,
				Type:    "api_key",
				APIKey:  "test-api-key-32-characters-minimum-length",
			},
			request: func() *http.Request {
				return httptest.NewRequest("GET", "/metrics", nil)
			},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "basic auth - valid credentials",
			authConfig: config.AuthConfig{
				Enabled: true,
				Type:    "basic",
				Basic: config.BasicAuthConfig{
					Username: "admin",
					Password: "password123",
				},
			},
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/metrics", nil)
				req.SetBasicAuth("admin", "password123")
				return req
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "basic auth - invalid credentials",
			authConfig: config.AuthConfig{
				Enabled: true,
				Type:    "basic",
				Basic: config.BasicAuthConfig{
					Username: "admin",
					Password: "password123",
				},
			},
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/metrics", nil)
				req.SetBasicAuth("admin", "wrongpassword")
				return req
			},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "basic auth - no credentials",
			authConfig: config.AuthConfig{
				Enabled: true,
				Type:    "basic",
				Basic: config.BasicAuthConfig{
					Username: "admin",
					Password: "password123",
				},
			},
			request: func() *http.Request {
				return httptest.NewRequest("GET", "/metrics", nil)
			},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "mtls auth - no TLS connection",
			authConfig: config.AuthConfig{
				Enabled: true,
				Type:    "mtls",
			},
			request: func() *http.Request {
				return httptest.NewRequest("GET", "/metrics", nil)
			},
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.ServerConfig{
				BindAddress: "localhost:9090",
				MetricsPath: "/metrics",
				Auth:        tt.authConfig,
			}

			exporter, err := NewExporter(cfg, logger)
			if err != nil {
				t.Fatalf("Failed to create exporter: %v", err)
			}

			// Create a simple handler that returns 200 OK
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Wrap with auth middleware
			authHandler := exporter.authMiddleware(handler)

			// Create test request
			req := tt.request()
			recorder := httptest.NewRecorder()

			// Execute request
			authHandler.ServeHTTP(recorder, req)

			// Check status code
			if recorder.Code != tt.expectedStatus {
				t.Errorf("Expected status code %d, got %d", tt.expectedStatus, recorder.Code)
			}

			// Check WWW-Authenticate header for unauthorized requests
			if recorder.Code == http.StatusUnauthorized {
				authHeader := recorder.Header().Get("WWW-Authenticate")
				if authHeader == "" {
					t.Error("Expected WWW-Authenticate header for unauthorized response")
				}
			}
		})
	}
}

func TestValidateAPIKey(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name       string
		authConfig config.AuthConfig
		request    func() *http.Request
		wantAuth   bool
	}{
		{
			name: "single API key - valid",
			authConfig: config.AuthConfig{
				APIKey: "test-key-32-characters-minimum-length",
			},
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/metrics", nil)
				req.Header.Set("Authorization", "Bearer test-key-32-characters-minimum-length")
				return req
			},
			wantAuth: true,
		},
		{
			name: "multiple API keys - valid first key",
			authConfig: config.AuthConfig{
				APIKeys: []config.APIKeyConfig{
					{
						Name: "key1",
						Key:  "first-key-32-characters-minimum-length",
					},
					{
						Name: "key2",
						Key:  "second-key-32-characters-minimum-length",
					},
				},
			},
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/metrics", nil)
				req.Header.Set("X-API-Key", "first-key-32-characters-minimum-length")
				return req
			},
			wantAuth: true,
		},
		{
			name: "multiple API keys - valid second key",
			authConfig: config.AuthConfig{
				APIKeys: []config.APIKeyConfig{
					{
						Name: "key1",
						Key:  "first-key-32-characters-minimum-length",
					},
					{
						Name: "key2",
						Key:  "second-key-32-characters-minimum-length",
					},
				},
			},
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/metrics", nil)
				req.Header.Set("X-API-Key", "second-key-32-characters-minimum-length")
				return req
			},
			wantAuth: true,
		},
		{
			name: "API key in query parameter (insecure - should fail)",
			authConfig: config.AuthConfig{
				APIKey: "test-key-32-characters-minimum-length",
			},
			request: func() *http.Request {
				return httptest.NewRequest("GET", "/metrics?api_key=test-key-32-characters-minimum-length", nil)
			},
			wantAuth: false, // Changed to false - query parameters are insecure and no longer supported
		},
		{
			name: "invalid API key",
			authConfig: config.AuthConfig{
				APIKey: "test-key-32-characters-minimum-length",
			},
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/metrics", nil)
				req.Header.Set("Authorization", "Bearer invalid-key")
				return req
			},
			wantAuth: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.ServerConfig{
				Auth: tt.authConfig,
			}

			exporter, err := NewExporter(cfg, logger)
			if err != nil {
				t.Fatalf("Failed to create exporter: %v", err)
			}

			req := tt.request()
			authenticated, _ := exporter.validateAPIKey(req)

			if authenticated != tt.wantAuth {
				t.Errorf("Expected authentication result %v, got %v", tt.wantAuth, authenticated)
			}
		})
	}
}

func TestValidateBasicAuth(t *testing.T) {
	logger := zaptest.NewLogger(t)

	authConfig := config.AuthConfig{
		Basic: config.BasicAuthConfig{
			Username: "testuser",
			Password: "testpass123",
		},
	}

	cfg := config.ServerConfig{
		Auth: authConfig,
	}

	exporter, err := NewExporter(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}

	tests := []struct {
		name     string
		username string
		password string
		wantAuth bool
	}{
		{
			name:     "valid credentials",
			username: "testuser",
			password: "testpass123",
			wantAuth: true,
		},
		{
			name:     "invalid username",
			username: "wronguser",
			password: "testpass123",
			wantAuth: false,
		},
		{
			name:     "invalid password",
			username: "testuser",
			password: "wrongpass",
			wantAuth: false,
		},
		{
			name:     "empty credentials",
			username: "",
			password: "",
			wantAuth: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/metrics", nil)
			if tt.username != "" || tt.password != "" {
				req.SetBasicAuth(tt.username, tt.password)
			}

			authenticated, _ := exporter.validateBasicAuth(req)

			if authenticated != tt.wantAuth {
				t.Errorf("Expected authentication result %v, got %v", tt.wantAuth, authenticated)
			}
		})
	}
}

func TestValidateMTLS(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.ServerConfig{
		Auth: config.AuthConfig{
			Type: "mtls",
		},
	}

	exporter, err := NewExporter(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}

	tests := []struct {
		name     string
		request  func() *http.Request
		wantAuth bool
	}{
		{
			name: "no TLS connection",
			request: func() *http.Request {
				return httptest.NewRequest("GET", "/metrics", nil)
			},
			wantAuth: false,
		},
		{
			name: "TLS connection with client certificate",
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/metrics", nil)
				req.TLS = &tls.ConnectionState{
					PeerCertificates: []*x509.Certificate{{}}, // Mock certificate
				}
				return req
			},
			wantAuth: true,
		},
		{
			name: "TLS connection without client certificate",
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/metrics", nil)
				req.TLS = &tls.ConnectionState{
					PeerCertificates: []*x509.Certificate{}, // No certificates
				}
				return req
			},
			wantAuth: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.request()
			authenticated, _ := exporter.validateMTLS(req)

			if authenticated != tt.wantAuth {
				t.Errorf("Expected authentication result %v, got %v", tt.wantAuth, authenticated)
			}
		})
	}
}

func TestGetAuthHeader(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name     string
		authType string
		expected string
	}{
		{
			name:     "api_key",
			authType: "api_key",
			expected: `Bearer realm="PHP-FPM Runtime Manager"`,
		},
		{
			name:     "basic",
			authType: "basic",
			expected: `Basic realm="PHP-FPM Runtime Manager"`,
		},
		{
			name:     "mtls",
			authType: "mtls",
			expected: `Certificate realm="PHP-FPM Runtime Manager"`,
		},
		{
			name:     "unknown",
			authType: "unknown",
			expected: `Bearer realm="PHP-FPM Runtime Manager"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.ServerConfig{
				Auth: config.AuthConfig{
					Type: tt.authType,
				},
			}

			exporter, err := NewExporter(cfg, logger)
			if err != nil {
				t.Fatalf("Failed to create exporter: %v", err)
			}

			header := exporter.getAuthHeader()
			if header != tt.expected {
				t.Errorf("Expected auth header '%s', got '%s'", tt.expected, header)
			}
		})
	}
}

func TestGetTLSConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.ServerConfig{}
	exporter, err := NewExporter(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}

	tlsConfig := exporter.getTLSConfig()

	// Verify TLS configuration
	if tlsConfig.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("Expected ClientAuth to be RequireAndVerifyClientCert, got %v", tlsConfig.ClientAuth)
	}

	if tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("Expected MinVersion to be TLS 1.2, got %v", tlsConfig.MinVersion)
	}

	if !tlsConfig.PreferServerCipherSuites {
		t.Error("Expected PreferServerCipherSuites to be true")
	}

	if len(tlsConfig.CipherSuites) == 0 {
		t.Error("Expected cipher suites to be configured")
	}
}

// Benchmark tests for authentication performance
func BenchmarkAuthMiddleware(b *testing.B) {
	logger := zaptest.NewLogger(b)

	cfg := config.ServerConfig{
		Auth: config.AuthConfig{
			Enabled: true,
			Type:    "api_key",
			APIKey:  "test-api-key-32-characters-minimum-length",
		},
	}

	exporter, err := NewExporter(cfg, logger)
	if err != nil {
		b.Fatalf("Failed to create exporter: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	authHandler := exporter.authMiddleware(handler)

	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer test-api-key-32-characters-minimum-length")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recorder := httptest.NewRecorder()
		authHandler.ServeHTTP(recorder, req)
	}
}
