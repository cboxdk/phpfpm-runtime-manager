package security

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
)

// Input validation patterns and helpers for security hardening

var (
	// Pool name validation: alphanumeric, dashes, underscores only
	poolNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

	// API key validation: alphanumeric and safe characters only
	apiKeyRegex = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

	// Safe characters for event summaries
	eventSummaryRegex = regexp.MustCompile(`^[a-zA-Z0-9\s\.\-_:,()]+$`)
)

// ValidatePoolName validates pool name for security and length constraints
func ValidatePoolName(name string, securityConfig *config.SecurityConfig) error {
	if name == "" {
		return fmt.Errorf("pool name cannot be empty")
	}

	maxLength := securityConfig.MaxPoolNameLength
	if maxLength == 0 {
		maxLength = 64 // fallback default
	}

	if len(name) > maxLength {
		return fmt.Errorf("pool name too long: %d > %d", len(name), maxLength)
	}

	if !poolNameRegex.MatchString(name) {
		return fmt.Errorf("pool name contains invalid characters: %s", name)
	}

	return nil
}

// ValidateAPIKey validates API key format and length
func ValidateAPIKey(key string, securityConfig *config.SecurityConfig) error {
	if key == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	maxLength := securityConfig.MaxAPIKeyLength
	if maxLength == 0 {
		maxLength = 128 // fallback default
	}

	if len(key) > maxLength {
		return fmt.Errorf("API key too long: %d > %d", len(key), maxLength)
	}

	if !apiKeyRegex.MatchString(key) {
		return fmt.Errorf("API key contains invalid characters")
	}

	return nil
}

// ValidateEventSummary validates event summary content for security
func ValidateEventSummary(summary string, securityConfig *config.SecurityConfig) error {
	maxLength := securityConfig.MaxEventSummaryLength
	if maxLength == 0 {
		maxLength = 1024 // fallback default
	}

	if len(summary) > maxLength {
		return fmt.Errorf("event summary too long: %d > %d", len(summary), maxLength)
	}

	if !utf8.ValidString(summary) {
		return fmt.Errorf("event summary contains invalid UTF-8")
	}

	// Allow empty summaries
	if summary == "" {
		return nil
	}

	if !eventSummaryRegex.MatchString(summary) {
		return fmt.Errorf("event summary contains potentially unsafe characters")
	}

	return nil
}

// SanitizeUserInput removes potentially dangerous characters from user input
func SanitizeUserInput(input string) string {
	// Remove control characters
	input = strings.Map(func(r rune) rune {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			return -1
		}
		return r
	}, input)

	// Limit length for safety
	if len(input) > 1000 {
		input = input[:1000]
	}

	return strings.TrimSpace(input)
}

// ValidateURL validates that a URL is safe and properly formatted
func ValidateURL(rawURL string, securityConfig *config.SecurityConfig) error {
	if rawURL == "" {
		return fmt.Errorf("URL cannot be empty")
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	// Validate scheme against allowed schemes
	allowedSchemes := securityConfig.AllowedSchemes
	if len(allowedSchemes) == 0 {
		allowedSchemes = []string{"http", "https"} // fallback default
	}

	schemeAllowed := false
	for _, scheme := range allowedSchemes {
		if parsedURL.Scheme == scheme {
			schemeAllowed = true
			break
		}
	}
	if !schemeAllowed {
		return fmt.Errorf("unsupported URL scheme: %s (allowed: %v)", parsedURL.Scheme, allowedSchemes)
	}

	// Check internal network access based on configuration
	if !securityConfig.AllowInternalNetworks {
		host := parsedURL.Hostname()
		if strings.HasPrefix(host, "127.") ||
			strings.HasPrefix(host, "10.") ||
			strings.HasPrefix(host, "192.168.") ||
			strings.HasPrefix(host, "172.16.") ||
			host == "localhost" {
			return fmt.Errorf("access to internal networks not allowed: %s (set allow_internal_networks: true for containers)", host)
		}
	}

	return nil
}

// SanitizeFilePath prevents directory traversal attacks
func SanitizeFilePath(path string) error {
	if strings.Contains(path, "..") {
		return fmt.Errorf("path contains directory traversal: %s", path)
	}

	if strings.Contains(path, "\x00") {
		return fmt.Errorf("path contains null bytes: %s", path)
	}

	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must be absolute: %s", path)
	}

	return nil
}
