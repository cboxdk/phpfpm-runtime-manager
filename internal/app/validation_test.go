package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"go.uber.org/zap"
)

func TestCheckConfigFileWritable_CreatesDirectory(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create a path to a non-existent directory
	nonExistentDir := filepath.Join(tempDir, "phpfpm-runtime-manager")
	configPath := filepath.Join(nonExistentDir, "php-fpm.conf")

	// Verify directory doesn't exist yet
	if _, err := os.Stat(nonExistentDir); !os.IsNotExist(err) {
		t.Fatal("Directory should not exist yet")
	}

	// Create a minimal manager to test checkConfigFileWritable directly
	logger := zap.NewNop()
	manager := &Manager{
		logger: logger,
		config: &config.Config{},
	}

	// Test that checkConfigFileWritable creates the directory
	err := manager.checkConfigFileWritable(configPath)
	if err != nil {
		t.Fatalf("checkConfigFileWritable should succeed and create directory: %v", err)
	}

	// Check if directory was created
	if _, err := os.Stat(nonExistentDir); os.IsNotExist(err) {
		t.Fatal("Directory should have been created by checkConfigFileWritable")
	}

	// Verify the directory is writable by calling the function again
	err = manager.checkConfigFileWritable(configPath)
	if err != nil {
		t.Fatalf("Config file should be writable after directory creation: %v", err)
	}
}

func TestCheckConfigFileWritable_DatabaseDirectory(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create a path to a non-existent directory for database
	nonExistentDir := filepath.Join(tempDir, "data")
	dbPath := filepath.Join(nonExistentDir, "metrics.db")

	// Verify directory doesn't exist yet
	if _, err := os.Stat(nonExistentDir); !os.IsNotExist(err) {
		t.Fatal("Directory should not exist yet")
	}

	// Create a minimal manager to test database directory validation
	logger := zap.NewNop()
	manager := &Manager{
		logger: logger,
		config: &config.Config{
			Storage: config.StorageConfig{
				DatabasePath: dbPath,
			},
		},
	}

	// Test validateStorageDirectories which should create the database directory
	err := manager.validateStorageDirectories()
	if err != nil {
		t.Fatalf("validateStorageDirectories should succeed and create directory: %v", err)
	}

	// Check if directory was created
	if _, err := os.Stat(nonExistentDir); os.IsNotExist(err) {
		t.Fatal("Database directory should have been created by validateStorageDirectories")
	}
}
