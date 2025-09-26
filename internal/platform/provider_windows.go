//go:build windows

package platform

// newLinuxProvider stub for non-Linux platforms
func newLinuxProvider(config *Config) Provider {
	return NewMockProvider(config)
}

// newDarwinProvider stub for non-Darwin platforms
func newDarwinProvider(config *Config) Provider {
	return NewMockProvider(config)
}

// newWindowsProvider creates a platform provider for Windows
func newWindowsProvider(config *Config) Provider {
	provider := &WindowsProvider{
		config: config,
	}

	provider.memory = &windowsMemoryProvider{
		config: config,
		cache:  make(map[string]*cachedMemoryInfo),
	}

	provider.process = &windowsProcessProvider{
		config: config,
		cache:  make(map[int]*cachedProcessInfo),
	}

	provider.filesystem = &windowsFileSystemProvider{
		config: config,
	}

	return provider
}
