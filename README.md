# PHP-FPM Runtime Manager

**Smart autoscaling and monitoring for PHP-FPM applications**

[![Go Version](https://img.shields.io/badge/go-1.21+-blue.svg)](https://golang.org/dl/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/docker-ready-blue.svg)](https://hub.docker.com)

> 🚀 Automatically optimize your PHP-FPM performance with intelligent scaling, real-time monitoring, and zero-downtime deployments.

## ⚠️ **CRITICAL NOTICE**

**This manager uses a dedicated configuration directory** (`/opt/phpfpm-runtime-manager/`) and does NOT modify your system's PHP-FPM configuration. This approach ensures your existing PHP-FPM setup remains untouched.

```bash
# Create dedicated directory:
sudo mkdir -p /opt/phpfpm-runtime-manager/
```

## ✨ What is PHP-FPM Runtime Manager?

A production-ready tool that automatically manages your PHP-FPM worker processes based on real-time metrics. No more guessing optimal worker counts or manual performance tuning.

**Perfect for:**
- 🏢 **Production websites** that need consistent performance
- 📈 **High-traffic applications** with variable load patterns
- 🛡️ **Enterprise environments** requiring monitoring and compliance
- 👨‍💻 **DevOps teams** wanting automated PHP-FPM management

## 🎯 Key Features

### 🔄 **Intelligent Autoscaling**
- Automatically adjusts PHP-FPM worker count based on load
- Considers memory usage, CPU utilization, and queue depth
- Prevents over-provisioning and resource waste

### 📊 **Real-time Monitoring**
- Live metrics dashboard with Prometheus integration
- Process-level tracking and performance insights
- Built-in health checks and alerting

### ⚡ **Zero-downtime Operations**
- Graceful worker scaling without service interruption
- Rolling configuration updates
- Seamless deployment integration

### 🛡️ **Enterprise Ready**
- Multi-pool management for complex applications
- Audit logging and compliance features
- Role-based access control and API authentication

## 🚀 Quick Start

### Using Docker (Recommended)

```bash
# Run with your existing PHP-FPM setup
docker run -d \
  --name phpfpm-manager \
  -p 8080:8080 \
  -v /opt/phpfpm-runtime-manager:/opt/phpfpm-runtime-manager \
  -v /var/run/php-fpm:/var/run/php-fpm \
  cboxdk/phpfpm-runtime-manager:latest
```

### Using Binary

```bash
# Download latest release
curl -L https://github.com/cboxdk/phpfpm-runtime-manager/releases/latest/download/phpfpm-manager-linux-amd64 -o phpfpm-manager
chmod +x phpfpm-manager

# Start with basic configuration
./phpfpm-manager run --config config.yaml
```

### Using Package Manager

```bash
# Ubuntu/Debian
curl -s https://packagecloud.io/install/repositories/cboxdk/phpfpm-runtime-manager/script.deb.sh | sudo bash
sudo apt-get install phpfpm-runtime-manager

# CentOS/RHEL
curl -s https://packagecloud.io/install/repositories/cboxdk/phpfpm-runtime-manager/script.rpm.sh | sudo bash
sudo yum install phpfpm-runtime-manager
```

## ⚙️ Essential Configuration

Create `config.yaml`:

```yaml
# Essential: PHP-FPM global configuration
phpfpm:
  global_config_path: "/opt/phpfpm-runtime-manager/php-fpm.conf"  # Dedicated config file
  # Safe: Uses dedicated directory, doesn't modify system PHP-FPM config

server:
  bind_address: "0.0.0.0:8080"

pools:
  - name: "www"
    config_path: "/opt/phpfpm-runtime-manager/pools.d/www.conf"  # Individual pool config
    fastcgi_endpoint: "127.0.0.1:9000"      # How to reach this pool
    max_workers: 20
    scaling:
      enabled: true
      min_spare: 2
      max_spare: 10

monitoring:
  collect_interval: "10s"

storage:
  database_path: "/var/lib/phpfpm-manager/data.db"
```

### ⚙️ Configuration Notes

#### Safe Configuration Approach
The manager uses a **dedicated configuration directory** and does NOT modify your system's PHP-FPM configuration:

```bash
# Create dedicated directory structure
sudo mkdir -p /opt/phpfpm-runtime-manager/pools.d/

# The manager creates all files in this structured directory:
# /opt/phpfpm-runtime-manager/
# ├── php-fpm.conf              # Main PHP-FPM config
# ├── logs/                     # All log files
# │   ├── phpfpm-manager-error.log
# │   └── phpfpm-pool-*.log
# ├── pids/                     # Process ID files
# │   └── phpfpm-manager.pid
# ├── sockets/                  # Unix socket files (default)
# │   ├── web.sock
# │   └── api.sock
# └── pools.d/                  # Individual pool configs
#     ├── web.conf
#     └── api.conf
#
# Your system's /etc/php-fpm.conf remains untouched
```

#### Required PHP-FPM Setup
Your individual pool configs (e.g., `/opt/phpfpm-runtime-manager/pools.d/www.conf`) must have status enabled:
```ini
# In /opt/phpfpm-runtime-manager/pools.d/www.conf
pm.status_path = /status
ping.path = /ping
listen = 127.0.0.1:9000  # Must match fastcgi_endpoint in manager config
```

#### File Permissions
```bash
# Manager needs write access to its dedicated directory:
sudo chown -R phpfpm-manager:phpfpm-manager /opt/phpfpm-runtime-manager/
```

## 📈 Usage Examples

### View Live Status
```bash
# Check overall system health
curl http://localhost:8080/health

# Get real-time metrics
curl http://localhost:8080/metrics

# View detailed pool information
curl http://localhost:8080/api/v1/pools
```

### Scale Operations
```bash
# Manually scale a pool
curl -X POST http://localhost:8080/api/v1/pools/www/scale \
  -H "Content-Type: application/json" \
  -d '{"workers": 15}'

# Enable/disable autoscaling
curl -X PATCH http://localhost:8080/api/v1/pools/www \
  -H "Content-Type: application/json" \
  -d '{"autoscaling_enabled": true}'
```

### Monitoring Integration
```bash
# Prometheus metrics endpoint
curl http://localhost:8080/metrics

# Grafana dashboard import
curl -L https://raw.githubusercontent.com/cboxdk/phpfpm-runtime-manager/main/examples/monitoring/grafana-dashboard.json
```

## 🔧 Common Use Cases

### High-Traffic WordPress Site
```yaml
pools:
  - name: "wordpress"
    config_file: "/opt/phpfpm-runtime-manager/pools.d/wordpress.conf"
    autoscaling:
      enabled: true
      min_workers: 4
      max_workers: 50
      scale_up_threshold: 80    # Scale up at 80% load
      scale_down_threshold: 20  # Scale down at 20% load
    memory:
      limit_mb: 512
      warning_threshold: 80
```

### Multi-Application Environment
```yaml
pools:
  - name: "api"
    config_file: "/opt/phpfpm-runtime-manager/pools.d/api.conf"
    autoscaling:
      enabled: true
      min_workers: 2
      max_workers: 20

  - name: "web"
    config_file: "/opt/phpfpm-runtime-manager/pools.d/web.conf"
    autoscaling:
      enabled: true
      min_workers: 5
      max_workers: 30
```

### Development Environment
```yaml
# Lightweight setup for development
server:
  bind: "127.0.0.1:8080"

pools:
  - name: "dev"
    config_file: "/opt/phpfpm-runtime-manager/pools.d/dev.conf"
    autoscaling:
      enabled: false  # Manual control during development
      min_workers: 1
      max_workers: 5

monitoring:
  collect_interval: "30s"  # Less frequent collection

storage:
  type: "memory"  # In-memory storage for dev
```

## 📊 Monitoring & Dashboards

### Grafana Integration
Pre-built dashboards available for:
- 📈 Worker pool utilization
- 🏃‍♂️ Request processing times
- 💾 Memory usage patterns
- 🚨 Error rates and alerts

Import dashboard: `examples/monitoring/grafana-dashboard.json`

### Prometheus Metrics
Key metrics exposed:
```
phpfpm_workers_active          # Currently active workers
phpfpm_workers_total           # Total configured workers
phpfpm_requests_per_second     # Request processing rate
phpfpm_memory_usage_bytes      # Memory consumption
phpfpm_queue_length            # Pending request queue
```

### Alerting Rules
```yaml
# Example alert for high queue length
- alert: PHPFPMHighQueue
  expr: phpfpm_queue_length > 10
  for: 2m
  labels:
    severity: warning
  annotations:
    summary: "PHP-FPM queue is backing up"
```

## 🐳 Docker Integration

### Docker Compose
```yaml
version: '3.8'
services:
  web:
    image: nginx:alpine
    ports:
      - "80:80"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf
    depends_on:
      - php-fpm
      - phpfpm-manager

  php-fpm:
    image: php:8.2-fpm-alpine
    volumes:
      - ./app:/var/www/html
      - ./php-fpm.conf:/usr/local/etc/php-fpm.d/www.conf

  phpfpm-manager:
    image: cboxdk/phpfpm-runtime-manager:latest
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/etc/phpfpm-manager/config.yaml
      - /var/run/php-fpm:/var/run/php-fpm
    environment:
      - LOG_LEVEL=info
```

### Kubernetes Deployment
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: phpfpm-manager
spec:
  replicas: 1
  selector:
    matchLabels:
      app: phpfpm-manager
  template:
    metadata:
      labels:
        app: phpfpm-manager
    spec:
      containers:
      - name: phpfpm-manager
        image: cboxdk/phpfpm-runtime-manager:latest
        ports:
        - containerPort: 8080
        env:
        - name: CONFIG_PATH
          value: "/etc/config/config.yaml"
        volumeMounts:
        - name: config
          mountPath: /etc/config
        - name: php-fpm-socket
          mountPath: /var/run/php-fpm
```

## 🛡️ Security Features

### API Authentication
```yaml
server:
  auth:
    api_keys:
      - key: "your-secret-api-key"
        permissions: ["read", "write"]

    basic_auth:
      username: "admin"
      password: "secure-password"

    tls:
      enabled: true
      cert_file: "/path/to/cert.pem"
      key_file: "/path/to/key.pem"
```

### Rate Limiting
```yaml
server:
  rate_limiting:
    enabled: true
    requests_per_minute: 100
    burst_size: 20
```

## 🚨 Troubleshooting

### Common Issues

**Manager can't connect to PHP-FPM**
```bash
# Check PHP-FPM status endpoint is accessible
curl http://localhost/status?json

# Verify socket permissions
ls -la /var/run/php-fpm/

# Check manager logs
docker logs phpfpm-manager
```

**Autoscaling not working**
```bash
# Verify monitoring is collecting metrics
curl http://localhost:8080/api/v1/pools/www/metrics

# Check autoscaling configuration
curl http://localhost:8080/api/v1/pools/www

# Review scaling events in logs
tail -f /var/log/phpfpm-manager.log | grep "scaling"
```

**High memory usage**
```bash
# Check individual worker memory consumption
curl http://localhost:8080/api/v1/pools/www/workers

# Review memory limits in pool config
grep -r "pm.max_children\|memory_limit" /opt/phpfpm-runtime-manager/pools.d/

# Monitor memory trends
curl http://localhost:8080/metrics | grep phpfpm_memory
```

### Performance Tuning

**Optimize collection interval**
```yaml
monitoring:
  collect_interval: "5s"  # More responsive (higher CPU)
  # or
  collect_interval: "30s" # Less responsive (lower CPU)
```

**Adjust scaling sensitivity**
```yaml
pools:
  - name: "www"
    autoscaling:
      scale_up_threshold: 70   # Scale up sooner
      scale_down_threshold: 30 # Scale down later
      cooldown_period: "2m"    # Wait between scaling events
```

## 🤝 Examples & Templates

Explore our comprehensive examples:

- 📁 **[Basic Setup](examples/basic/)** - Simple single-pool configuration
- 📁 **[Full Featured](examples/full/)** - Complete configuration with all options
- 📁 **[Docker](examples/docker/)** - Ready-to-use Docker compose setup
- 📁 **[Kubernetes](examples/kubernetes/)** - Production K8s deployment
- 📁 **[High Memory](examples/memory/)** - Memory-intensive application tuning
- 📁 **[Multi-Pool](examples/multi-pool/)** - Multiple PHP applications

## 📖 Documentation

- 🚀 **[Installation Guide](docs/installation-guide.md)** - Detailed setup instructions
- ⚙️ **[Configuration Reference](docs/configuration-reference.md)** - Complete config options
- 🔧 **[API Documentation](docs/api-reference.md)** - REST API endpoints
- 🛡️ **[Security Guide](docs/security-guide.md)** - Security best practices
- 👨‍💻 **[Developer Guide](docs/developer-reference.md)** - Contributing and development

## 🆘 Getting Help

- 📖 **Documentation**: Comprehensive guides and references
- 💬 **GitHub Discussions**: Community Q&A and feature requests
- 🐛 **Issue Tracker**: Bug reports and technical issues
- 📧 **Enterprise Support**: Commercial support and consulting

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

<div align="center">

**Made with ❤️ for the PHP community**

[⭐ Star us on GitHub](https://github.com/cboxdk/phpfpm-runtime-manager) | [📖 Documentation](docs/) | [🐳 Docker Hub](https://hub.docker.com/r/cboxdk/phpfpm-runtime-manager)

</div>