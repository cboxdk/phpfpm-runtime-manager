//go:build darwin

package platform

// newLinuxProvider stub for non-Linux platforms
func newLinuxProvider(config *Config) Provider {
	return NewMockProvider(config)
}

// newDarwinProvider creates a platform provider for Darwin/macOS
func newDarwinProvider(config *Config) Provider {
	provider := &DarwinProvider{
		config: config,
	}

	provider.memory = &darwinMemoryProvider{
		config: config,
		cache:  make(map[string]*cachedMemoryInfo),
	}

	provider.process = &darwinProcessProvider{
		config: config,
		cache:  make(map[int]*cachedProcessInfo),
	}

	provider.filesystem = &darwinFileSystemProvider{
		config: config,
	}

	return provider
}

// newWindowsProvider stub for non-Windows platforms
func newWindowsProvider(config *Config) Provider {
	return NewMockProvider(config)
}
