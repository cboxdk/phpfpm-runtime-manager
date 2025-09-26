package platform

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"
)

// DefaultProvider creates a platform provider using the current runtime
func DefaultProvider() (Provider, error) {
	return NewProvider(&Config{
		MemoryUpdateInterval:  30 * time.Second,
		ProcessUpdateInterval: 5 * time.Second,
		TimeoutDuration:       10 * time.Second,
		CacheEnabled:          true,
		CacheTTL:              1 * time.Minute,
	})
}

// NewProvider creates a platform provider with the specified configuration
func NewProvider(config *Config) (Provider, error) {
	if config == nil {
		config = &Config{}
	}

	// Apply defaults
	if config.MemoryUpdateInterval == 0 {
		config.MemoryUpdateInterval = 30 * time.Second
	}
	if config.ProcessUpdateInterval == 0 {
		config.ProcessUpdateInterval = 5 * time.Second
	}
	if config.TimeoutDuration == 0 {
		config.TimeoutDuration = 10 * time.Second
	}
	if config.CacheTTL == 0 {
		config.CacheTTL = 1 * time.Minute
	}

	// Enable mock provider for testing if requested
	if config.EnableMockProvider {
		return NewMockProvider(config), nil
	}

	// Select platform implementation
	platformName := config.PreferredPlatform
	if platformName == "" {
		platformName = runtime.GOOS
	}

	switch platformName {
	case "linux":
		return newLinuxProvider(config), nil
	case "darwin":
		return newDarwinProvider(config), nil
	case "windows":
		return newWindowsProvider(config), nil
	default:
		// Fallback to mock provider for unsupported platforms
		return NewMockProvider(config), nil
	}
}

// platformProvider implements the Provider interface with platform-specific adapters
type platformProvider struct {
	config     *Config
	memory     MemoryProvider
	process    ProcessProvider
	filesystem FileSystemProvider
	platform   string
	mu         sync.RWMutex
}

// Memory returns the memory provider
func (p *platformProvider) Memory() MemoryProvider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.memory
}

// Process returns the process provider
func (p *platformProvider) Process() ProcessProvider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.process
}

// FileSystem returns the filesystem provider
func (p *platformProvider) FileSystem() FileSystemProvider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.filesystem
}

// Platform returns the platform identifier
func (p *platformProvider) Platform() string {
	return p.platform
}

// IsSupported returns true if the platform is fully supported
func (p *platformProvider) IsSupported() bool {
	return p.memory.IsSupported() && p.process.IsSupported()
}

// DetectPlatform returns detailed platform information
func DetectPlatform() *PlatformInfo {
	return &PlatformInfo{
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		NumCPU:       runtime.NumCPU(),
		Version:      runtime.Version(),
		Compiler:     runtime.Compiler,
	}
}

// PlatformInfo contains detailed platform information
type PlatformInfo struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	NumCPU       int    `json:"num_cpu"`
	Version      string `json:"version"`
	Compiler     string `json:"compiler"`
}

// Validate validates the platform configuration
func (c *Config) Validate() error {
	if c.MemoryUpdateInterval < 0 {
		return fmt.Errorf("memory update interval cannot be negative")
	}
	if c.ProcessUpdateInterval < 0 {
		return fmt.Errorf("process update interval cannot be negative")
	}
	if c.TimeoutDuration < 0 {
		return fmt.Errorf("timeout duration cannot be negative")
	}
	if c.CacheTTL < 0 {
		return fmt.Errorf("cache TTL cannot be negative")
	}
	return nil
}

// Platform-specific provider implementations are available in:
// - linux.go: Linux platform provider with /proc filesystem support
// - darwin.go: macOS platform provider with sysctl integration
// - windows.go: Windows platform provider with Windows API support

// globalProvider holds the default provider instance
var (
	globalProvider Provider
	providerOnce   sync.Once
	providerErr    error
)

// GetProvider returns the global platform provider instance
func GetProvider() (Provider, error) {
	providerOnce.Do(func() {
		globalProvider, providerErr = DefaultProvider()
	})
	return globalProvider, providerErr
}

// SetProvider sets the global platform provider (useful for testing)
func SetProvider(provider Provider) {
	globalProvider = provider
	providerOnce = sync.Once{} // Reset to allow re-initialization
}

// ResetProvider resets the global provider (useful for testing)
func ResetProvider() {
	globalProvider = nil
	providerOnce = sync.Once{}
	providerErr = nil
}

// WithContext creates a new context with a platform provider
func WithContext(ctx context.Context, provider Provider) context.Context {
	return context.WithValue(ctx, providerContextKey, provider)
}

// FromContext retrieves a platform provider from context
func FromContext(ctx context.Context) (Provider, bool) {
	provider, ok := ctx.Value(providerContextKey).(Provider)
	return provider, ok
}

type contextKey string

const providerContextKey contextKey = "platform.provider"
