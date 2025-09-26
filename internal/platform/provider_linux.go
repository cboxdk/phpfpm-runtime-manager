//go:build linux

package platform

// newLinuxProvider creates a platform provider for Linux
func newLinuxProvider(config *Config) Provider {
	return &LinuxProvider{
		config:     config,
		memory:     newLinuxMemoryProvider(config),
		process:    newLinuxProcessProvider(config),
		filesystem: newLinuxFileSystemProvider(),
	}
}

// newDarwinProvider stub for non-Darwin platforms
func newDarwinProvider(config *Config) Provider {
	return NewMockProvider(config)
}

// newWindowsProvider stub for non-Windows platforms
func newWindowsProvider(config *Config) Provider {
	return NewMockProvider(config)
}
