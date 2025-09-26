package security

import (
	"testing"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
)

func TestValidatePoolName(t *testing.T) {
	tests := []struct {
		name      string
		poolName  string
		expectErr bool
	}{
		{
			name:      "valid pool name",
			poolName:  "web-pool",
			expectErr: false,
		},
		{
			name:      "valid pool name with underscores",
			poolName:  "api_pool_v1",
			expectErr: false,
		},
		{
			name:      "valid pool name alphanumeric",
			poolName:  "pool123",
			expectErr: false,
		},
		{
			name:      "empty pool name",
			poolName:  "",
			expectErr: true,
		},
		{
			name:      "pool name with spaces",
			poolName:  "web pool",
			expectErr: true,
		},
		{
			name:      "pool name with special characters",
			poolName:  "web@pool",
			expectErr: true,
		},
		{
			name:      "pool name with directory traversal",
			poolName:  "../etc/passwd",
			expectErr: true,
		},
		{
			name:      "pool name too long",
			poolName:  string(make([]byte, 200)), // exceeds MaxPoolNameLength
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			securityConfig := &config.SecurityConfig{
				MaxPoolNameLength: 64,
			}
			err := ValidatePoolName(tt.poolName, securityConfig)
			if (err != nil) != tt.expectErr {
				t.Errorf("ValidatePoolName() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}

func TestValidateAPIKey(t *testing.T) {
	tests := []struct {
		name      string
		apiKey    string
		expectErr bool
	}{
		{
			name:      "valid API key",
			apiKey:    "abc123_def.456-xyz",
			expectErr: false,
		},
		{
			name:      "empty API key",
			apiKey:    "",
			expectErr: true,
		},
		{
			name:      "API key with spaces",
			apiKey:    "abc 123",
			expectErr: true,
		},
		{
			name:      "API key with special characters",
			apiKey:    "abc@123",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			securityConfig := &config.SecurityConfig{
				MaxAPIKeyLength: 128,
			}
			err := ValidateAPIKey(tt.apiKey, securityConfig)
			if (err != nil) != tt.expectErr {
				t.Errorf("ValidateAPIKey() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}

func TestSanitizeUserInput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal text",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "text with control characters",
			input:    "hello\x00world\x01",
			expected: "helloworld",
		},
		{
			name:     "text with leading/trailing spaces",
			input:    "  hello world  ",
			expected: "hello world",
		},
		{
			name:     "text with tabs and newlines",
			input:    "hello\tworld\n",
			expected: "hello\tworld", // TrimSpace removes trailing newlines
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeUserInput(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeUserInput() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestValidateEventSummary(t *testing.T) {
	tests := []struct {
		name      string
		summary   string
		expectErr bool
	}{
		{
			name:      "valid summary",
			summary:   "Pool started successfully",
			expectErr: false,
		},
		{
			name:      "valid summary with numbers and punctuation",
			summary:   "Pool-1 restarted: 5 workers, avg response 200ms",
			expectErr: false,
		},
		{
			name:      "empty summary",
			summary:   "",
			expectErr: false, // Empty summaries are allowed
		},
		{
			name:      "summary with unsafe characters",
			summary:   "Pool started <script>alert('xss')</script>",
			expectErr: true,
		},
		{
			name:      "summary too long",
			summary:   string(make([]byte, 2000)), // exceeds MaxEventSummaryLength
			expectErr: true,
		},
		{
			name:      "summary with invalid UTF-8",
			summary:   "Pool started \xff\xfe",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			securityConfig := &config.SecurityConfig{
				MaxEventSummaryLength: 1024,
			}
			err := ValidateEventSummary(tt.summary, securityConfig)
			if (err != nil) != tt.expectErr {
				t.Errorf("ValidateEventSummary() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		expectErr bool
	}{
		{
			name:      "valid HTTPS URL",
			url:       "https://example.com/api",
			expectErr: false,
		},
		{
			name:      "valid HTTP URL",
			url:       "http://example.com",
			expectErr: false,
		},
		{
			name:      "localhost URL (should be blocked)",
			url:       "http://localhost:8080",
			expectErr: true,
		},
		{
			name:      "internal IP (should be blocked)",
			url:       "http://192.168.1.1",
			expectErr: true,
		},
		{
			name:      "FTP URL (unsupported scheme)",
			url:       "ftp://example.com",
			expectErr: true,
		},
		{
			name:      "empty URL",
			url:       "",
			expectErr: true,
		},
		{
			name:      "invalid URL format",
			url:       "not-a-url",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			securityConfig := &config.SecurityConfig{
				AllowInternalNetworks: false,
				AllowedSchemes:        []string{"http", "https"},
			}
			err := ValidateURL(tt.url, securityConfig)
			if (err != nil) != tt.expectErr {
				t.Errorf("ValidateURL() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}
