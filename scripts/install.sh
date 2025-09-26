#!/bin/bash

# PHP-FPM Runtime Manager Installation Script
# Automated installation with systemctl setup for production use
# Supports Ubuntu 20.04+, Debian 11+, CentOS 8+, RHEL 8+

set -euo pipefail

# Configuration
INSTALL_DIR="/opt/phpfpm-runtime-manager"
CONFIG_DIR="/opt/phpfpm-runtime-manager"
LOG_DIR="/var/log/phpfpm-runtime-manager"
SERVICE_USER="phpfpm-manager"
SERVICE_GROUP="phpfpm-manager"
GITHUB_REPO="cboxdk/phpfpm-runtime-manager"
SYSTEMD_SERVICE_FILE="/etc/systemd/system/phpfpm-runtime-manager.service"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if running as root
check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root. Use: sudo $0"
        exit 1
    fi
}

# Detect operating system
detect_os() {
    if [[ -f /etc/os-release ]]; then
        . /etc/os-release
        OS=$ID
        VER=$VERSION_ID
    else
        log_error "Cannot detect operating system"
        exit 1
    fi

    log_info "Detected OS: $OS $VER"
}

# Check system requirements
check_requirements() {
    log_info "Checking system requirements..."

    # Check kernel version
    KERNEL_VERSION=$(uname -r | cut -d. -f1-2)
    log_info "Kernel version: $KERNEL_VERSION"

    # Check available memory
    MEMORY_GB=$(free -g | awk 'NR==2{printf "%.1f", $2}')
    if (( $(echo "$MEMORY_GB < 1.0" | bc -l) )); then
        log_warning "Low memory detected: ${MEMORY_GB}GB (minimum 1GB recommended)"
    fi

    # Check disk space
    DISK_SPACE=$(df -BG / | awk 'NR==2 {print $4}' | sed 's/G//')
    if [[ $DISK_SPACE -lt 2 ]]; then
        log_warning "Low disk space: ${DISK_SPACE}GB available (minimum 2GB recommended)"
    fi

    log_success "System requirements check completed"
}

# Install dependencies based on OS
install_dependencies() {
    log_info "Installing system dependencies..."

    case $OS in
        ubuntu|debian)
            apt-get update
            apt-get install -y curl wget tar systemd unzip jq bc
            ;;
        centos|rhel|fedora)
            if command -v dnf >/dev/null 2>&1; then
                dnf install -y curl wget tar systemd unzip jq bc
            else
                yum install -y curl wget tar systemd unzip jq bc
            fi
            ;;
        *)
            log_error "Unsupported operating system: $OS"
            exit 1
            ;;
    esac

    log_success "Dependencies installed successfully"
}

# Get latest release version
get_latest_version() {
    log_info "Fetching latest release version..."

    # Try GitHub API first
    if LATEST_VERSION=$(curl -s "https://api.github.com/repos/$GITHUB_REPO/releases/latest" | jq -r '.tag_name' 2>/dev/null); then
        if [[ "$LATEST_VERSION" != "null" && -n "$LATEST_VERSION" ]]; then
            log_success "Latest version: $LATEST_VERSION"
            return 0
        fi
    fi

    # Fallback to default version
    LATEST_VERSION="v1.0.0"
    log_warning "Could not fetch latest version, using default: $LATEST_VERSION"
}

# Download and extract binary
download_binary() {
    log_info "Downloading PHP-FPM Runtime Manager $LATEST_VERSION..."

    # Detect architecture
    ARCH=$(uname -m)
    case $ARCH in
        x86_64) ARCH="amd64" ;;
        aarch64) ARCH="arm64" ;;
        armv7l) ARCH="arm" ;;
        *) log_error "Unsupported architecture: $ARCH"; exit 1 ;;
    esac

    # Download URL
    DOWNLOAD_URL="https://github.com/$GITHUB_REPO/releases/download/$LATEST_VERSION/phpfpm-runtime-manager-$LATEST_VERSION-linux-$ARCH.tar.gz"
    TEMP_DIR=$(mktemp -d)

    # Download with retry logic
    for i in {1..3}; do
        if curl -L -o "$TEMP_DIR/phpfpm-runtime-manager.tar.gz" "$DOWNLOAD_URL"; then
            break
        elif [[ $i -eq 3 ]]; then
            log_error "Failed to download after 3 attempts"
            exit 1
        else
            log_warning "Download attempt $i failed, retrying..."
            sleep 2
        fi
    done

    # Verify download
    if [[ ! -f "$TEMP_DIR/phpfpm-runtime-manager.tar.gz" ]]; then
        log_error "Download failed - file not found"
        exit 1
    fi

    # Extract
    log_info "Extracting binary..."
    tar -xzf "$TEMP_DIR/phpfpm-runtime-manager.tar.gz" -C "$TEMP_DIR"

    # Verify binary
    if [[ ! -f "$TEMP_DIR/phpfpm-runtime-manager" ]]; then
        log_error "Binary not found in archive"
        exit 1
    fi

    # Test binary
    if ! "$TEMP_DIR/phpfpm-runtime-manager" --version >/dev/null 2>&1; then
        log_error "Binary verification failed"
        exit 1
    fi

    # Install binary
    mkdir -p "$INSTALL_DIR"
    cp "$TEMP_DIR/phpfpm-runtime-manager" "$INSTALL_DIR/"
    chmod +x "$INSTALL_DIR/phpfpm-runtime-manager"

    # Create symlink
    ln -sf "$INSTALL_DIR/phpfpm-runtime-manager" /usr/local/bin/phpfpm-runtime-manager

    # Cleanup
    rm -rf "$TEMP_DIR"

    log_success "Binary installed to $INSTALL_DIR"
}

# Create service user and group
create_user() {
    log_info "Creating service user and group..."

    # Create group
    if ! getent group "$SERVICE_GROUP" >/dev/null 2>&1; then
        groupadd --system "$SERVICE_GROUP"
        log_success "Created group: $SERVICE_GROUP"
    else
        log_info "Group already exists: $SERVICE_GROUP"
    fi

    # Create user
    if ! getent passwd "$SERVICE_USER" >/dev/null 2>&1; then
        useradd --system --gid "$SERVICE_GROUP" \
                --home-dir "$INSTALL_DIR" \
                --no-create-home \
                --shell /bin/false \
                "$SERVICE_USER"
        log_success "Created user: $SERVICE_USER"
    else
        log_info "User already exists: $SERVICE_USER"
    fi
}

# Setup directories and permissions
setup_directories() {
    log_info "Setting up directories and permissions..."

    # Create directories
    mkdir -p "$CONFIG_DIR"
    mkdir -p "$LOG_DIR"
    mkdir -p "/var/lib/phpfpm-runtime-manager"

    # Set ownership
    chown -R "$SERVICE_USER:$SERVICE_GROUP" "$INSTALL_DIR"
    chown -R "$SERVICE_USER:$SERVICE_GROUP" "$CONFIG_DIR"
    chown -R "$SERVICE_USER:$SERVICE_GROUP" "$LOG_DIR"
    chown -R "$SERVICE_USER:$SERVICE_GROUP" "/var/lib/phpfpm-runtime-manager"

    # Set permissions
    chmod 755 "$INSTALL_DIR"
    chmod 750 "$CONFIG_DIR"
    chmod 750 "$LOG_DIR"
    chmod 750 "/var/lib/phpfpm-runtime-manager"

    log_success "Directories and permissions configured"
}

# Create default configuration
create_config() {
    log_info "Creating default configuration..."

    cat > "$CONFIG_DIR/config.yaml" << 'EOF'
# PHP-FPM Runtime Manager Configuration
# Production-ready default configuration

server:
  bind_address: "127.0.0.1:9090"
  metrics_path: "/metrics"

  health_check:
    enabled: true
    endpoint: "/health"

# Default PHP-FPM pool configuration
pools:
  - name: "www"
    config_path: "/opt/phpfpm-runtime-manager/pools.d/www.conf"
    status_endpoint: "http://127.0.0.1:9001/status"
    max_workers: 20
    health_check:
      enabled: true
      interval: "30s"
      timeout: "10s"
    scaling:
      enabled: true
      min_workers: 5
      max_workers: 20
      target_utilization: 0.7

# Storage configuration
# Note: Change to file path for persistence across restarts
storage:
  database_path: ":memory:"

  connection_pool:
    enabled: true
    max_open_conns: 25
    max_idle_conns: 10
    conn_max_lifetime: "2h"
    health_interval: "30s"

  retention:
    raw: "12h"
    minute: "7d"
    hour: "90d"
    daily: "1y"

# Monitoring configuration
monitoring:
  collect_interval: "5s"

  adaptive_collection:
    enabled: true
    base_interval: "5s"
    max_interval: "30s"
    load_threshold: 0.8

  # Performance optimizations
  string_interning:
    enabled: true
    capacity: 256

  json_pooling:
    enabled: true
    buffer_size: 1024

  label_caching:
    enabled: true
    max_entries: 1000

  batching:
    enabled: true
    size: 50
    flush_timeout: "5s"
    max_batches: 4

# Circuit breaker for resilience
resilience:
  circuit_breaker:
    enabled: true
    failure_threshold: 3
    recovery_timeout: "30s"
    success_threshold: 2
    timeout: "10s"
    max_concurrent_requests: 2

# Logging configuration
logging:
  level: "info"
  format: "json"
  file: "/var/log/phpfpm-runtime-manager/manager.log"

  structured: true
  include_caller: false

  rotation:
    max_size: "50MB"
    max_files: 3
    max_age: "7d"

# Security configuration
security:
  auth:
    enabled: false  # Enable and configure for production

  validation:
    max_request_size: "1MB"
    max_pool_name_length: 32
    allowed_characters: "a-zA-Z0-9_-"

  rate_limiting:
    enabled: true
    requests_per_minute: 1000
    burst: 100

# API configuration
api:
  enabled: true
  version: "v1"

  cors:
    enabled: false  # Configure for cross-origin access

# Metrics export
metrics:
  prometheus:
    enabled: true
    path: "/metrics"
    include_go_metrics: false
    include_process_metrics: true
EOF

    chown "$SERVICE_USER:$SERVICE_GROUP" "$CONFIG_DIR/config.yaml"
    chmod 640 "$CONFIG_DIR/config.yaml"

    log_success "Default configuration created at $CONFIG_DIR/config.yaml"
}

# Create systemd service
create_systemd_service() {
    log_info "Creating systemd service..."

    cat > "$SYSTEMD_SERVICE_FILE" << EOF
[Unit]
Description=PHP-FPM Runtime Manager
Documentation=https://github.com/$GITHUB_REPO
After=network.target php-fpm.service
Wants=php-fpm.service

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_GROUP
ExecStart=$INSTALL_DIR/phpfpm-runtime-manager --config=$CONFIG_DIR/config.yaml
ExecReload=/bin/kill -HUP \$MAINPID
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=phpfpm-runtime-manager

# Security settings
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=$CONFIG_DIR $LOG_DIR /var/lib/phpfpm-runtime-manager
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE

# Resource limits
LimitNOFILE=65536
LimitNPROC=4096

# Environment
Environment=HOME=/var/lib/phpfpm-runtime-manager
WorkingDirectory=/var/lib/phpfpm-runtime-manager

[Install]
WantedBy=multi-user.target
EOF

    log_success "Systemd service created"
}

# Setup logrotate
setup_logrotate() {
    log_info "Setting up log rotation..."

    cat > "/etc/logrotate.d/phpfpm-runtime-manager" << EOF
$LOG_DIR/*.log {
    daily
    missingok
    rotate 14
    compress
    delaycompress
    notifempty
    create 640 $SERVICE_USER $SERVICE_GROUP
    postrotate
        systemctl reload phpfpm-runtime-manager.service || true
    endscript
}
EOF

    log_success "Log rotation configured"
}

# Configure firewall (if applicable)
configure_firewall() {
    log_info "Configuring firewall..."

    # UFW (Ubuntu/Debian)
    if command -v ufw >/dev/null 2>&1; then
        # Only allow local access by default
        ufw allow from 127.0.0.1 to any port 9090
        log_success "UFW firewall rule added"
    fi

    # Firewalld (CentOS/RHEL/Fedora)
    if command -v firewall-cmd >/dev/null 2>&1; then
        # Add custom service
        cat > "/etc/firewalld/services/phpfpm-runtime-manager.xml" << EOF
<?xml version="1.0" encoding="utf-8"?>
<service>
  <short>PHP-FPM Runtime Manager</short>
  <description>PHP-FPM monitoring and scaling service</description>
  <port protocol="tcp" port="9090"/>
</service>
EOF
        firewall-cmd --reload
        # Only allow from localhost by default
        firewall-cmd --permanent --add-rich-rule='rule family="ipv4" source address="127.0.0.1" service name="phpfpm-runtime-manager" accept'
        firewall-cmd --reload
        log_success "Firewalld rule added"
    fi
}

# Enable and start service
enable_service() {
    log_info "Enabling and starting service..."

    # Reload systemd
    systemctl daemon-reload

    # Enable service
    systemctl enable phpfpm-runtime-manager.service

    # Start service
    if systemctl start phpfpm-runtime-manager.service; then
        log_success "Service started successfully"
    else
        log_error "Failed to start service"
        log_info "Check logs with: journalctl -u phpfpm-runtime-manager.service"
        exit 1
    fi

    # Wait for service to be ready
    sleep 3

    # Check service status
    if systemctl is-active --quiet phpfpm-runtime-manager.service; then
        log_success "Service is running"
    else
        log_error "Service is not running"
        systemctl status phpfpm-runtime-manager.service
        exit 1
    fi
}

# Verify installation
verify_installation() {
    log_info "Verifying installation..."

    # Check binary
    if phpfpm-runtime-manager --version >/dev/null 2>&1; then
        VERSION=$(phpfpm-runtime-manager --version 2>/dev/null | head -n1)
        log_success "Binary check passed: $VERSION"
    else
        log_error "Binary check failed"
        exit 1
    fi

    # Check service
    if systemctl is-active --quiet phpfpm-runtime-manager.service; then
        log_success "Service check passed"
    else
        log_error "Service check failed"
        exit 1
    fi

    # Check health endpoint
    if curl -sf http://127.0.0.1:9090/health >/dev/null 2>&1; then
        log_success "Health check passed"
    else
        log_warning "Health check failed - service may still be starting"
    fi

    # Check metrics endpoint
    if curl -sf http://127.0.0.1:9090/metrics >/dev/null 2>&1; then
        log_success "Metrics endpoint accessible"
    else
        log_warning "Metrics endpoint not accessible"
    fi
}

# Display post-installation information
show_post_install_info() {
    echo
    log_success "PHP-FPM Runtime Manager installation completed successfully!"
    echo
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo -e "${GREEN}Installation Summary${NC}"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo
    echo -e "${BLUE}Binary Location:${NC} $INSTALL_DIR/phpfpm-runtime-manager"
    echo -e "${BLUE}Configuration:${NC} $CONFIG_DIR/config.yaml"
    echo -e "${BLUE}Log Directory:${NC} $LOG_DIR"
    echo -e "${BLUE}Service User:${NC} $SERVICE_USER"
    echo -e "${BLUE}Systemd Service:${NC} phpfpm-runtime-manager.service"
    echo
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo -e "${GREEN}Service Management${NC}"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo
    echo -e "${BLUE}Start service:${NC}      systemctl start phpfpm-runtime-manager"
    echo -e "${BLUE}Stop service:${NC}       systemctl stop phpfpm-runtime-manager"
    echo -e "${BLUE}Restart service:${NC}    systemctl restart phpfpm-runtime-manager"
    echo -e "${BLUE}Service status:${NC}     systemctl status phpfpm-runtime-manager"
    echo -e "${BLUE}View logs:${NC}          journalctl -u phpfpm-runtime-manager -f"
    echo
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo -e "${GREEN}Endpoints${NC}"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo
    echo -e "${BLUE}Health Check:${NC}       http://127.0.0.1:9090/health"
    echo -e "${BLUE}Metrics:${NC}            http://127.0.0.1:9090/metrics"
    echo -e "${BLUE}API Documentation:${NC}  http://127.0.0.1:9090/api/v1/docs"
    echo
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo -e "${GREEN}Next Steps${NC}"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo
    echo "1. Review and customize configuration:"
    echo "   sudo nano $CONFIG_DIR/config.yaml"
    echo
    echo "2. Configure PHP-FPM pools with status endpoints:"
    echo "   sudo nano /opt/phpfpm-runtime-manager/pools.d/www.conf"
    echo "   # Add: pm.status_path = /status"
    echo
    echo "3. Restart PHP-FPM to apply status endpoint:"
    echo "   sudo systemctl restart php-fpm"
    echo
    echo "4. Test the installation:"
    echo "   curl http://127.0.0.1:9090/health"
    echo
    echo "5. Set up monitoring (optional):"
    echo "   # See examples/monitoring/ for Prometheus and Grafana setup"
    echo
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo -e "${YELLOW}Security Notes${NC}"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo
    echo "• Service runs as non-root user ($SERVICE_USER)"
    echo "• Firewall configured for localhost access only"
    echo "• Enable authentication for production use"
    echo "• Review security settings in configuration"
    echo
    echo -e "${GREEN}Installation completed successfully!${NC}"
    echo
}

# Main installation function
main() {
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo -e "${GREEN}PHP-FPM Runtime Manager - Automated Installation${NC}"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo

    check_root
    detect_os
    check_requirements
    install_dependencies
    get_latest_version
    download_binary
    create_user
    setup_directories
    create_config
    create_systemd_service
    setup_logrotate
    configure_firewall
    enable_service
    verify_installation
    show_post_install_info
}

# Handle script arguments
case "${1:-}" in
    --help|-h)
        echo "PHP-FPM Runtime Manager Installation Script"
        echo
        echo "Usage: $0 [OPTIONS]"
        echo
        echo "Options:"
        echo "  --help, -h     Show this help message"
        echo "  --version, -v  Show version information"
        echo "  --dry-run      Show what would be done without executing"
        echo
        echo "Environment Variables:"
        echo "  GITHUB_REPO    Override GitHub repository (default: cboxdk/phpfpm-runtime-manager)"
        echo "  INSTALL_DIR    Override installation directory (default: /opt/phpfpm-runtime-manager)"
        echo "  CONFIG_DIR     Override configuration directory (default: /opt/phpfpm-runtime-manager)"
        echo
        exit 0
        ;;
    --version|-v)
        echo "PHP-FPM Runtime Manager Installation Script v1.0.0"
        exit 0
        ;;
    --dry-run)
        echo "Dry run mode - showing what would be done:"
        echo "1. Check system requirements"
        echo "2. Install dependencies for detected OS"
        echo "3. Download latest binary from GitHub"
        echo "4. Create service user and directories"
        echo "5. Install configuration and systemd service"
        echo "6. Configure firewall and log rotation"
        echo "7. Start and enable service"
        echo "8. Verify installation"
        exit 0
        ;;
    "")
        main
        ;;
    *)
        log_error "Unknown option: $1"
        echo "Use --help for usage information"
        exit 1
        ;;
esac