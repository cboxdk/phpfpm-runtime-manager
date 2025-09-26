package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/app"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/autoscaler"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	Version = "1.0.0-dev"
)

// CLI represents the command line interface
type CLI struct {
	args []string
}

// Command represents a CLI command
type Command struct {
	Name        string
	Description string
	Usage       string
	Run         func(args []string) error
}

func main() {
	cli := &CLI{args: os.Args[1:]}

	commands := map[string]*Command{
		"run":            {Name: "run", Description: "Start the PHP-FPM runtime manager", Usage: "run [--config path] [--log-level level]", Run: cli.runCommand},
		"validate":       {Name: "validate", Description: "Validate configuration file", Usage: "validate [--config path]", Run: cli.validateCommand},
		"version":        {Name: "version", Description: "Show version information", Usage: "version", Run: cli.versionCommand},
		"help":           {Name: "help", Description: "Show help information", Usage: "help [command]", Run: cli.helpCommand},
		"example-config": {Name: "example-config", Description: "Generate example configuration file", Usage: "example-config [--output path]", Run: cli.exampleConfigCommand},
	}

	if len(cli.args) == 0 {
		cli.printUsage(commands)
		os.Exit(1)
	}

	commandName := cli.args[0]

	// Handle help flag
	if commandName == "--help" || commandName == "-h" {
		cli.printUsage(commands)
		return
	}

	// Default to run command if not a recognized command
	if _, exists := commands[commandName]; !exists {
		// Check if it's a flag for the run command
		if strings.HasPrefix(commandName, "--") {
			commandName = "run"
		} else {
			fmt.Fprintf(os.Stderr, "Error: Unknown command '%s'\n\n", commandName)
			cli.printUsage(commands)
			os.Exit(1)
		}
	} else {
		// Remove command name from args
		cli.args = cli.args[1:]
	}

	cmd := commands[commandName]
	if err := cmd.Run(cli.args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func (cli *CLI) printUsage(commands map[string]*Command) {
	fmt.Printf("PHP-FPM Runtime Manager v%s\n", Version)
	fmt.Println("A high-performance PHP-FPM process manager with monitoring and auto-scaling capabilities.")
	fmt.Println()
	fmt.Println("USAGE:")
	fmt.Printf("  %s <command> [options]\n", os.Args[0])
	fmt.Println()
	fmt.Println("COMMANDS:")

	commandOrder := []string{"run", "validate", "example-config", "version", "help"}
	for _, name := range commandOrder {
		if cmd, exists := commands[name]; exists {
			fmt.Printf("  %-15s %s\n", cmd.Name, cmd.Description)
		}
	}

	fmt.Println()
	fmt.Println("GLOBAL OPTIONS:")
	fmt.Println("  --help, -h       Show help information")
	fmt.Println()
	fmt.Println("Use \"phpfpm-manager help <command>\" for more information about a command.")
	fmt.Println()
	fmt.Println("EXAMPLES:")
	fmt.Printf("  %s run --config /opt/phpfpm-runtime-manager/config.yaml\n", os.Args[0])
	fmt.Printf("  %s validate --config ./config.yaml\n", os.Args[0])
	fmt.Printf("  %s example-config --output ./phpfpm-manager.yaml\n", os.Args[0])
}

func (cli *CLI) parseFlags(args []string, flags map[string]*string) []string {
	var remaining []string

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if strings.HasPrefix(arg, "--") {
			flagName := strings.TrimPrefix(arg, "--")

			// Handle --flag=value format
			if strings.Contains(flagName, "=") {
				parts := strings.SplitN(flagName, "=", 2)
				flagName = parts[0]
				if flagVar, exists := flags[flagName]; exists {
					*flagVar = parts[1]
				}
				continue
			}

			// Handle --flag value format
			if flagVar, exists := flags[flagName]; exists {
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
					*flagVar = args[i+1]
					i++ // Skip the value
				} else {
					// Boolean flag or missing value
					*flagVar = "true"
				}
				continue
			}
		}

		remaining = append(remaining, arg)
	}

	return remaining
}

func (cli *CLI) runCommand(args []string) error {
	var configPath string
	var logLevel = "info"
	var phpfpmBinary string
	var useDefaultConfig = true

	flags := map[string]*string{
		"config":         &configPath,
		"log-level":      &logLevel,
		"php-fpm-binary": &phpfpmBinary,
	}

	remaining := cli.parseFlags(args, flags)

	// Check if --config flag was explicitly provided
	for _, arg := range args {
		if strings.HasPrefix(arg, "--config") {
			useDefaultConfig = false
			break
		}
	}

	// Check for help
	for _, arg := range remaining {
		if arg == "--help" || arg == "-h" {
			cli.printRunHelp()
			return nil
		}
	}

	// Create logger with specified level
	logger, err := cli.createLogger(logLevel)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Sync()

	// Load configuration
	var cfg *config.Config
	if useDefaultConfig {
		logger.Info("Running in zero-config mode with intelligent defaults")
		cfg, err = config.LoadDefault()
		if err != nil {
			return fmt.Errorf("failed to load default configuration: %w", err)
		}
	} else {
		// Validate config path
		if err := cli.validateConfigPath(configPath); err != nil {
			return err
		}
		fmt.Printf("Loading configuration from: %s\n", configPath)
		cfg, err = config.Load(configPath)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}
	}

	// Create main application manager
	var manager *app.Manager
	if phpfpmBinary != "" {
		manager, err = app.NewManagerWithPHPFPMBinary(cfg, phpfpmBinary, logger)
	} else {
		manager, err = app.NewManager(cfg, logger)
	}
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	// Set up signal handling
	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGUSR2)

	go func() {
		sig := <-sigChan
		logger.Info("Received signal", zap.String("signal", sig.String()))

		switch sig {
		case syscall.SIGHUP:
			fmt.Println("Reloading configuration...")
			if err := manager.Reload(ctx); err != nil {
				logger.Error("Failed to reload configuration", zap.Error(err))
				fmt.Printf("Failed to reload configuration: %v\n", err)
			} else {
				fmt.Println("Configuration reloaded successfully")
			}
		case syscall.SIGUSR2:
			fmt.Println("Performing zero-downtime restart...")
			if err := manager.Restart(ctx); err != nil {
				logger.Error("Failed to restart", zap.Error(err))
				fmt.Printf("Failed to restart: %v\n", err)
			} else {
				fmt.Println("Restart completed successfully")
			}
		default:
			logger.Info("Shutting down gracefully")
			cancel()
		}
	}()

	logger.Info("Starting PHP-FPM Runtime Manager",
		zap.String("version", Version),
		zap.Int("pools_configured", len(cfg.Pools)),
		zap.String("monitoring_interval", cfg.Monitoring.CollectInterval.String()),
		zap.String("server_address", cfg.Server.BindAddress))

	// Run the manager
	if err := manager.Run(ctx); err != nil {
		logger.Error("Manager stopped with error", zap.Error(err))
		return fmt.Errorf("manager stopped with error: %w", err)
	}

	logger.Info("PHP-FPM Runtime Manager stopped")
	return nil
}

func (cli *CLI) validateCommand(args []string) error {
	var configPath string
	var verbose = false

	flags := map[string]*string{
		"config": &configPath,
		"verbose": func() *string {
			s := "false"
			if verbose {
				s = "true"
			}
			return &s
		}(),
	}

	remaining := cli.parseFlags(args, flags)
	verbose = flags["verbose"] != nil && *flags["verbose"] == "true"

	// Check for help
	for _, arg := range remaining {
		if arg == "--help" || arg == "-h" {
			cli.printValidateHelp()
			return nil
		}
	}

	// Load and validate configuration
	var cfg *config.Config
	var err error
	var validationResult *config.ValidationResult

	if configPath == "" {
		fmt.Println("🔍 Validating zero-config mode with intelligent defaults")
		cfg, err = config.LoadDefault()
		if err != nil {
			// Try to get detailed validation results even if loading failed
			if cfg != nil {
				validationResult = config.GetValidationResult(cfg)
				cli.printValidationResults(validationResult, verbose)
			}
			return fmt.Errorf("default configuration validation failed: %w", err)
		}
	} else {
		// Validate config path exists
		if err := cli.validateConfigPath(configPath); err != nil {
			return err
		}

		fmt.Printf("🔍 Validating configuration file: %s\n", configPath)
		cfg, err = config.Load(configPath)
		if err != nil {
			return fmt.Errorf("configuration validation failed: %w", err)
		}
	}

	// Get detailed validation results
	validationResult = config.GetValidationResult(cfg)

	// Print validation results with detailed error reporting
	cli.printValidationResults(validationResult, verbose)

	// If there are errors, exit with failure
	if !validationResult.Valid {
		fmt.Printf("\n❌ Configuration validation failed with %d error(s)\n", len(validationResult.Errors))
		return fmt.Errorf("configuration validation failed")
	}

	// Show warnings summary if any
	if len(validationResult.Warnings) > 0 {
		fmt.Printf("\n⚠️  Found %d warning(s) - configuration is valid but could be improved\n", len(validationResult.Warnings))
	}

	// Additional informational output for valid configs
	cli.printConfigurationSummary(cfg)

	fmt.Println("\n✅ Configuration validation completed successfully!")
	return nil
}

// printValidationResults prints detailed validation results
func (cli *CLI) printValidationResults(result *config.ValidationResult, verbose bool) {
	if len(result.Errors) == 0 && len(result.Warnings) == 0 {
		fmt.Println("✅ Configuration passes all validation checks")
		return
	}

	// Print errors
	if len(result.Errors) > 0 {
		fmt.Printf("\n❌ VALIDATION ERRORS (%d):\n", len(result.Errors))
		for i, err := range result.Errors {
			fmt.Printf("  %d. Field: %s\n", i+1, err.Field)
			fmt.Printf("     Error: %s\n", err.Message)
			if err.Suggestion != "" {
				fmt.Printf("     Fix: %s\n", err.Suggestion)
			}
			if verbose && err.Value != nil {
				fmt.Printf("     Current value: %v\n", err.Value)
			}
			fmt.Println()
		}
	}

	// Print warnings
	if len(result.Warnings) > 0 {
		fmt.Printf("\n⚠️  VALIDATION WARNINGS (%d):\n", len(result.Warnings))
		for i, warning := range result.Warnings {
			fmt.Printf("  %d. Field: %s\n", i+1, warning.Field)
			fmt.Printf("     Warning: %s\n", warning.Message)
			if warning.Suggestion != "" {
				fmt.Printf("     Suggestion: %s\n", warning.Suggestion)
			}
			if verbose && warning.Value != nil {
				fmt.Printf("     Current value: %v\n", warning.Value)
			}
			fmt.Println()
		}
	}
}

// printConfigurationSummary prints a summary of valid configuration
func (cli *CLI) printConfigurationSummary(cfg *config.Config) {
	fmt.Println("\n📋 CONFIGURATION SUMMARY:")

	// Server configuration
	fmt.Printf("🌐 Server:\n")
	fmt.Printf("   Bind Address: %s\n", cfg.Server.BindAddress)
	fmt.Printf("   Metrics Path: %s\n", cfg.Server.MetricsPath)
	fmt.Printf("   Health Path: %s\n", cfg.Server.HealthPath)
	if cfg.Server.TLS.Enabled {
		fmt.Printf("   TLS: ✅ Enabled\n")
	} else {
		fmt.Printf("   TLS: ⚠️  Disabled\n")
	}
	if cfg.Server.Auth.Enabled {
		fmt.Printf("   Authentication: ✅ %s\n", cfg.Server.Auth.Type)
	} else {
		fmt.Printf("   Authentication: ⚠️  Disabled\n")
	}

	// Storage configuration
	fmt.Printf("\n💾 Storage:\n")
	fmt.Printf("   Database: %s\n", cfg.Storage.DatabasePath)
	fmt.Printf("   Retention: Raw=%s, Minute=%s, Hour=%s\n",
		cfg.Storage.Retention.Raw, cfg.Storage.Retention.Minute, cfg.Storage.Retention.Hour)

	// Pools configuration
	fmt.Printf("\n🏊 Pools (%d configured):\n", len(cfg.Pools))
	for _, pool := range cfg.Pools {
		fmt.Printf("   📦 Pool '%s':\n", pool.Name)
		fmt.Printf("      FastCGI Endpoint: %s\n", pool.FastCGIEndpoint)
		fmt.Printf("      Status Path: %s\n", pool.FastCGIStatusPath)

		// Show calculated worker configuration
		if pool.MaxWorkers > 0 {
			fmt.Printf("      Max Workers: %d (explicit limit)\n", pool.MaxWorkers)
		} else {
			// Use autoscaler to calculate optimal worker configuration
			calculatedMax, startServers, minSpare, maxSpare, err := autoscaler.CalculateOptimalWorkers(pool.MaxWorkers)
			if err != nil {
				fmt.Printf("      Max Workers: Error calculating (fallback: %d): %v\n", 4, err)
			} else {
				// Show memory information for transparency
				totalMB, availableMB, reservedMB, memErr := autoscaler.GetSystemMemoryInfo()
				if memErr == nil {
					fmt.Printf("      Max Workers: %d (autoscaled: %dMB available ÷ 64MB per worker)\n",
						calculatedMax, availableMB)
					fmt.Printf("      System Memory: %dMB total, %dMB reserved for OS, %dMB available for PHP-FPM\n",
						totalMB, reservedMB, availableMB)
				} else {
					fmt.Printf("      Max Workers: %d (autoscaled)\n", calculatedMax)
				}
				fmt.Printf("      Start Servers: %d\n", startServers)
				fmt.Printf("      Min Spare: %d\n", minSpare)
				fmt.Printf("      Max Spare: %d\n", maxSpare)
			}
		}

		fmt.Printf("      Health Check: every %s\n", pool.HealthCheck.Interval)

		// Show what will be written to PHP-FPM config
		if strings.HasPrefix(pool.FastCGIEndpoint, "unix:") {
			socketPath := strings.TrimPrefix(pool.FastCGIEndpoint, "unix:")
			fmt.Printf("      PHP-FPM listen: %s (Unix socket)\n", socketPath)
		} else {
			fmt.Printf("      PHP-FPM listen: %s (TCP socket)\n", pool.FastCGIEndpoint)
		}

		// Show config location
		if pool.ConfigPath != "" {
			if _, err := os.Stat(pool.ConfigPath); os.IsNotExist(err) {
				fmt.Printf("      ⚠️  Config File: %s (file not found - will be written to global config)\n", pool.ConfigPath)
			} else {
				fmt.Printf("      Config File: %s ✅\n", pool.ConfigPath)
			}
		} else {
			fmt.Printf("      Config: Written to global config (%s)\n", cfg.PHPFPM.GlobalConfigPath)
		}

		if pool.Scaling.Enabled {
			fmt.Printf("      Auto-scaling: ✅ Min=%d, Max=%d, Target=%.0f%%\n",
				pool.Scaling.MinWorkers, pool.Scaling.MaxWorkers, pool.Scaling.TargetUtilization*100)
		} else {
			fmt.Printf("      Auto-scaling: ⚠️  Disabled\n")
		}
	}

	// Monitoring configuration
	fmt.Printf("\n📊 Monitoring:\n")
	fmt.Printf("   Collection Interval: %s\n", cfg.Monitoring.CollectInterval)
	fmt.Printf("   Health Check Interval: %s\n", cfg.Monitoring.HealthCheckInterval)
	fmt.Printf("   CPU Threshold: %.1f%%\n", cfg.Monitoring.CPUThreshold)
	fmt.Printf("   Memory Threshold: %.1f%%\n", cfg.Monitoring.MemoryThreshold)
	if cfg.Monitoring.EnableOpcache {
		fmt.Printf("   OPcache Metrics: ✅ Enabled\n")
	} else {
		fmt.Printf("   OPcache Metrics: ⚠️  Disabled\n")
	}

	// Telemetry configuration
	if cfg.Telemetry.Enabled {
		fmt.Printf("\n🔭 Telemetry: ✅ Enabled (%s exporter)\n", cfg.Telemetry.Exporter.Type)
		fmt.Printf("   Service: %s v%s (%s)\n", cfg.Telemetry.ServiceName, cfg.Telemetry.ServiceVersion, cfg.Telemetry.Environment)
		fmt.Printf("   Sampling Rate: %.1f%%\n", cfg.Telemetry.Sampling.Rate*100)
	} else {
		fmt.Printf("\n🔭 Telemetry: ⚠️  Disabled\n")
	}
}

func (cli *CLI) versionCommand(args []string) error {
	fmt.Printf("PHP-FPM Runtime Manager version %s\n", Version)
	fmt.Println("Built with Go")
	fmt.Println("https://github.com/cboxdk/phpfpm-runtime-manager")
	return nil
}

func (cli *CLI) helpCommand(args []string) error {
	commands := map[string]*Command{
		"run":            {Name: "run", Description: "Start the PHP-FPM runtime manager", Usage: "run [--config path] [--log-level level]", Run: cli.runCommand},
		"validate":       {Name: "validate", Description: "Validate configuration file", Usage: "validate [--config path]", Run: cli.validateCommand},
		"version":        {Name: "version", Description: "Show version information", Usage: "version", Run: cli.versionCommand},
		"help":           {Name: "help", Description: "Show help information", Usage: "help [command]", Run: cli.helpCommand},
		"example-config": {Name: "example-config", Description: "Generate example configuration file", Usage: "example-config [--output path]", Run: cli.exampleConfigCommand},
	}

	if len(args) == 0 {
		cli.printUsage(commands)
		return nil
	}

	commandName := args[0]
	switch commandName {
	case "run":
		cli.printRunHelp()
	case "validate":
		cli.printValidateHelp()
	case "example-config":
		cli.printExampleConfigHelp()
	case "version":
		fmt.Println("USAGE: phpfpm-manager version")
		fmt.Println("Show version information and build details.")
	default:
		fmt.Printf("Unknown command: %s\n\n", commandName)
		cli.printUsage(commands)
	}

	return nil
}

func (cli *CLI) exampleConfigCommand(args []string) error {
	var outputPath = "phpfpm-manager.yaml"

	flags := map[string]*string{
		"output": &outputPath,
	}

	remaining := cli.parseFlags(args, flags)

	// Check for help
	for _, arg := range remaining {
		if arg == "--help" || arg == "-h" {
			cli.printExampleConfigHelp()
			return nil
		}
	}

	// Check if file already exists
	if _, err := os.Stat(outputPath); err == nil {
		return fmt.Errorf("file already exists: %s (use a different path or remove the existing file)", outputPath)
	}

	// Copy the example config
	sourceConfig := filepath.Join("configs", "example.yaml")

	// Read source
	data, err := os.ReadFile(sourceConfig)
	if err != nil {
		return fmt.Errorf("failed to read example config: %w", err)
	}

	// Write to output
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("Example configuration written to: %s\n", outputPath)
	fmt.Println("Edit the file to match your environment and use:")
	fmt.Printf("  phpfpm-manager validate --config %s\n", outputPath)
	return nil
}

func (cli *CLI) validateConfigPath(path string) error {
	if path == "" {
		return fmt.Errorf("config path cannot be empty")
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("config file does not exist: %s", path)
	}

	return nil
}

func (cli *CLI) createLogger(level string) (*zap.Logger, error) {
	var zapLevel zapcore.Level
	switch strings.ToLower(level) {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn", "warning":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		return nil, fmt.Errorf("invalid log level: %s (valid: debug, info, warn, error)", level)
	}

	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zapLevel)
	return config.Build()
}

func (cli *CLI) printRunHelp() {
	fmt.Println("USAGE: phpfpm-manager run [options]")
	fmt.Println("Start the PHP-FPM runtime manager with monitoring and auto-scaling.")
	fmt.Println()
	fmt.Println("OPTIONS:")
	fmt.Println("  --config path          Configuration file path (default: zero-config mode)")
	fmt.Println("  --log-level level      Log level: debug, info, warn, error (default: info)")
	fmt.Println("  --php-fpm-binary path  Path to PHP-FPM binary (e.g. php84-fpm, /usr/local/bin/php-fpm)")
	fmt.Println("  --help, -h             Show this help message")
	fmt.Println()
	fmt.Println("SIGNALS:")
	fmt.Println("  SIGINT/SIGTERM    Graceful shutdown")
	fmt.Println("  SIGHUP            Reload configuration")
	fmt.Println("  SIGUSR2           Zero-downtime restart")
	fmt.Println()
	fmt.Println("EXAMPLES:")
	fmt.Println("  phpfpm-manager run")
	fmt.Println("  phpfpm-manager run --config /opt/phpfpm-runtime-manager/config.yaml")
	fmt.Println("  phpfpm-manager run --log-level debug")
	fmt.Println("  phpfpm-manager run --php-fpm-binary php84-fpm  # For Laravel Herd on macOS")
	fmt.Println("  phpfpm-manager run --php-fpm-binary /usr/local/bin/php-fpm")
}

func (cli *CLI) printValidateHelp() {
	fmt.Println("USAGE: phpfpm-manager validate [options]")
	fmt.Println("Validate configuration file without starting the service.")
	fmt.Println()
	fmt.Println("Performs comprehensive validation with detailed error reporting and fix suggestions.")
	fmt.Println()
	fmt.Println("OPTIONS:")
	fmt.Println("  --config path  Configuration file path (default: zero-config mode)")
	fmt.Println("  --verbose      Show detailed validation output including current values")
	fmt.Println("  --help, -h     Show this help message")
	fmt.Println()
	fmt.Println("VALIDATION FEATURES:")
	fmt.Println("  • Comprehensive field validation with type checking")
	fmt.Println("  • Network address and URL format validation")
	fmt.Println("  • File path and directory accessibility checks")
	fmt.Println("  • Duration and percentage range validation")
	fmt.Println("  • Configuration consistency and dependency checks")
	fmt.Println("  • Detailed error messages with fix suggestions")
	fmt.Println("  • Performance and security warnings")
	fmt.Println()
	fmt.Println("EXAMPLES:")
	fmt.Println("  phpfpm-manager validate")
	fmt.Println("  phpfpm-manager validate --config ./config.yaml")
	fmt.Println("  phpfpm-manager validate --config ./config.yaml --verbose")
}

func (cli *CLI) printExampleConfigHelp() {
	fmt.Println("USAGE: phpfpm-manager example-config [options]")
	fmt.Println("Generate an example configuration file.")
	fmt.Println()
	fmt.Println("OPTIONS:")
	fmt.Println("  --output path  Output file path (default: phpfpm-manager.yaml)")
	fmt.Println("  --help, -h     Show this help message")
	fmt.Println()
	fmt.Println("EXAMPLES:")
	fmt.Println("  phpfpm-manager example-config")
	fmt.Println("  phpfpm-manager example-config --output /opt/phpfpm-runtime-manager/config.yaml")
}
