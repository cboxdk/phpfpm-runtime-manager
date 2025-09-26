# Docker Setup for PHP-FPM Runtime Manager

Complete production-ready Docker setup with multi-service architecture including Nginx, PHP-FPM pools, monitoring, and observability.

## Architecture Overview

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│     Nginx       │────│   PHP-FPM Web   │────│      Redis      │
│   (Port 80)     │    │   (Port 9000)   │    │   (Port 6379)   │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                       │                       │
         │              ┌─────────────────┐              │
         └──────────────│   PHP-FPM API   │──────────────┘
                        │   (Port 9000)   │
                        └─────────────────┘
                                 │
                     ┌─────────────────┐
                     │   Runtime Mgr   │
                     │   (Port 9090)   │
                     └─────────────────┘
                              │
         ┌────────────────────┼────────────────────┐
         │                    │                    │
┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│   Prometheus    │  │     Grafana     │  │ OpenTelemetry   │
│   (Port 9090)   │  │   (Port 3000)   │  │   Collector     │
└─────────────────┘  └─────────────────┘  └─────────────────┘
```

## Services

- **Nginx**: Reverse proxy and load balancer with rate limiting
- **PHP-FPM Web**: High-traffic web frontend pool
- **PHP-FPM API**: API backend pool with optimized settings
- **Runtime Manager**: PHP-FPM monitoring and scaling service
- **Redis**: Session storage and caching
- **Prometheus**: Metrics collection and alerting
- **Grafana**: Metrics visualization and dashboards
- **OpenTelemetry Collector**: Distributed tracing collection

## Quick Start

### Prerequisites

- Docker 20.10+
- Docker Compose 2.0+
- 4GB+ RAM available
- Ports 80, 3000, 9090 available

### 1. Clone and Setup

```bash
# Navigate to docker example directory
cd examples/docker

# Create required directories
mkdir -p logs data/prometheus data/grafana
```

### 2. Environment Configuration

```bash
# Copy environment template
cp .env.example .env

# Edit configuration (optional)
nano .env
```

### 3. Build and Start

```bash
# Build all images
docker-compose build

# Start all services
docker-compose up -d

# Check service status
docker-compose ps
```

### 4. Verify Installation

```bash
# Test web frontend
curl http://localhost/

# Test API endpoint
curl http://localhost/api/health

# Check manager metrics
curl http://localhost:9090/metrics

# Access Grafana dashboard
open http://localhost:3000
```

## Configuration

### Environment Variables

```bash
# PHP-FPM Pool Configuration
PHP_MAX_CHILDREN=20
PHP_START_SERVERS=5
PHP_MIN_SPARE=3
PHP_MAX_SPARE=8

# Manager Configuration
MANAGER_LOG_LEVEL=info
MANAGER_COLLECT_INTERVAL=5s

# Database Configuration
DB_HOST=redis
DB_PORT=6379

# Monitoring Configuration
PROMETHEUS_RETENTION=30d
GRAFANA_ADMIN_PASSWORD=admin123
```

### Pool Configurations

The setup includes two optimized PHP-FPM pools:

#### Web Pool (`php-fpm-web`)
- **Purpose**: High-traffic web requests
- **Workers**: 10-20 (dynamic scaling)
- **Memory**: 128MB per worker
- **Timeout**: 30s request timeout
- **Use Case**: Web pages, asset serving

#### API Pool (`php-fpm-api`)
- **Purpose**: API backend services
- **Workers**: 5-15 (dynamic scaling)
- **Memory**: 256MB per worker
- **Timeout**: 60s request timeout
- **Use Case**: REST APIs, data processing

### Nginx Configuration

- **Rate Limiting**: 50 req/s for web, 10 req/s for API
- **Load Balancing**: Round-robin with connection pooling
- **Caching**: Static assets cached for 1 year
- **Security**: XSS protection, frame options, content type sniffing

## Monitoring and Observability

### Metrics Collection

The Runtime Manager exposes comprehensive metrics:

```
# Pool metrics
phpfpm_pool_workers_total
phpfpm_pool_workers_active
phpfpm_pool_workers_idle
phpfpm_pool_requests_total
phpfpm_pool_slow_requests_total

# System metrics
phpfpm_memory_usage_bytes
phpfpm_cpu_usage_percent
phpfpm_uptime_seconds
```

### Grafana Dashboards

Access Grafana at `http://localhost:3000`:

- **Username**: admin
- **Password**: admin123 (change in production)

Pre-configured dashboards:
- PHP-FPM Pool Overview
- Request Performance Analysis
- System Resource Monitoring
- Alert Management

### Alerting Rules

Prometheus alerts are configured for:

- High pool utilization (>80%)
- Critical pool utilization (>95%)
- Pool health check failures
- High request queue depth
- Slow request rate increases
- Runtime manager connectivity issues

## Development Usage

### Local Development

```bash
# Start with hot reload
docker-compose -f docker-compose.yml -f docker-compose.dev.yml up

# View logs
docker-compose logs -f phpfpm-manager

# Execute commands in containers
docker-compose exec php-fpm-web bash
docker-compose exec phpfpm-manager sh
```

### Testing

```bash
# Load testing with curl
for i in {1..100}; do
  curl -s http://localhost/ > /dev/null &
done

# Monitor metrics during load
watch -n1 'curl -s http://localhost:9090/metrics | grep phpfpm_pool_workers'

# Check container resource usage
docker stats
```

## Production Deployment

### Security Considerations

1. **Change Default Passwords**:
   ```bash
   # Generate secure passwords
   openssl rand -base64 32 > .grafana_admin_password
   openssl rand -base64 32 > .redis_password
   ```

2. **Enable TLS**:
   ```bash
   # Add SSL certificates to nginx/ssl/
   mkdir -p nginx/ssl
   # Copy certificates: nginx/ssl/cert.pem, nginx/ssl/key.pem
   ```

3. **Firewall Configuration**:
   ```bash
   # Only expose necessary ports
   # 80, 443 (web traffic)
   # 22 (SSH - with key auth only)
   ```

### Resource Requirements

**Minimum Production**:
- CPU: 2 cores
- RAM: 4GB
- Disk: 20GB SSD
- Network: 100Mbps

**Recommended Production**:
- CPU: 4+ cores
- RAM: 8GB+
- Disk: 50GB+ SSD
- Network: 1Gbps

### Scaling

#### Horizontal Scaling

```yaml
# Scale PHP-FPM pools
docker-compose up --scale php-fpm-web=3 --scale php-fpm-api=2

# Load balancer configuration
upstream php_web {
    server php-fpm-web_1:9000;
    server php-fpm-web_2:9000;
    server php-fpm-web_3:9000;
}
```

#### Vertical Scaling

```bash
# Increase pool worker limits
export PHP_MAX_CHILDREN=50
export PHP_START_SERVERS=10

# Restart with new configuration
docker-compose up -d --force-recreate
```

## Troubleshooting

### Common Issues

1. **Port Conflicts**:
   ```bash
   # Check port usage
   netstat -tulpn | grep :80

   # Change ports in docker-compose.yml
   ports:
     - "8080:80"  # Use alternative port
   ```

2. **Memory Issues**:
   ```bash
   # Check container memory usage
   docker stats --no-stream

   # Increase PHP memory limits
   php_admin_value[memory_limit] = 256M
   ```

3. **Permission Issues**:
   ```bash
   # Fix volume permissions
   sudo chown -R $USER:$USER data/ logs/
   chmod -R 755 data/ logs/
   ```

### Health Checks

```bash
# Service health status
docker-compose ps

# Individual service logs
docker-compose logs nginx
docker-compose logs php-fpm-web
docker-compose logs phpfpm-manager

# Manager health endpoint
curl http://localhost:9090/health

# Pool status endpoints
curl http://localhost/status?pool=web&json
curl http://localhost/status?pool=api&json
```

### Performance Tuning

1. **PHP-FPM Optimization**:
   ```ini
   # Increase process limits
   pm.max_children = 50
   pm.start_servers = 10
   pm.min_spare_servers = 5
   pm.max_spare_servers = 15

   # Optimize memory
   php_admin_value[memory_limit] = 256M
   php_admin_value[opcache.memory_consumption] = 256
   ```

2. **Nginx Optimization**:
   ```nginx
   # Increase worker connections
   worker_connections 2048;

   # Enable gzip compression
   gzip_comp_level 6;
   gzip_types text/plain text/css application/json;
   ```

3. **System Optimization**:
   ```bash
   # Increase file descriptor limits
   echo "* soft nofile 65536" >> /etc/security/limits.conf
   echo "* hard nofile 65536" >> /etc/security/limits.conf

   # Optimize kernel parameters
   echo "net.core.somaxconn = 65536" >> /etc/sysctl.conf
   sysctl -p
   ```

## Maintenance

### Backup Procedures

```bash
# Backup configuration
tar -czf backup-config-$(date +%Y%m%d).tar.gz \
  config/ nginx/ php-fpm/ monitoring/

# Backup metrics data
docker run --rm -v prometheus_data:/data -v $(pwd):/backup \
  alpine tar -czf /backup/prometheus-$(date +%Y%m%d).tar.gz /data

# Backup Grafana dashboards
docker-compose exec grafana grafana-cli admin export-dashboard > dashboards-backup.json
```

### Updates

```bash
# Update Docker images
docker-compose pull

# Rebuild custom images
docker-compose build --no-cache

# Rolling update
docker-compose up -d --force-recreate --no-deps nginx
docker-compose up -d --force-recreate --no-deps php-fpm-web
```

## Next Steps

- **Security Hardening**: [Production Security Guide](../security/README.md)
- **Advanced Monitoring**: [Monitoring Setup Guide](../monitoring/README.md)
- **Load Testing**: [Performance Testing Guide](../testing/README.md)
- **Multi-Environment**: [Environment Management](../environments/README.md)