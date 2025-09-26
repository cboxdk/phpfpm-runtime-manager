# Multi-Pool Setup Example

This example demonstrates a complete multi-pool PHP-FPM setup with different pools optimized for different workloads.

## Pool Architecture

### 🌐 Web Pool (Port 9001)
- **Purpose**: Frontend web requests
- **Characteristics**: High traffic, quick responses
- **Workers**: 10-50 (dynamic scaling)
- **Memory**: 128MB per process
- **Timeout**: 30s

### 🔌 API Pool (Port 9002)
- **Purpose**: API backend requests
- **Characteristics**: Moderate traffic, more processing
- **Workers**: 5-25 (dynamic scaling)
- **Memory**: 256MB per process
- **Timeout**: 60s

### ⚙️ Worker Pool (Port 9003)
- **Purpose**: Background job processing
- **Characteristics**: Low frequency, resource intensive
- **Workers**: 2-10 (dynamic scaling)
- **Memory**: 512MB per process
- **Timeout**: 300s

### 👨‍💼 Admin Pool (Port 9004)
- **Purpose**: Admin panel access
- **Characteristics**: Low traffic, high availability
- **Workers**: 5 (static, always available)
- **Memory**: 128MB per process
- **Timeout**: 45s

## Quick Setup

### 1. Install PHP-FPM Pool Configurations

```bash
# Copy pool configurations to PHP-FPM directory
sudo cp examples/multipool/php-fpm-pools/*.conf /etc/php-fpm.d/

# Create log directories
sudo mkdir -p /var/log/php-fpm
sudo chown www-data:www-data /var/log/php-fpm

# Restart PHP-FPM to load new pools
sudo systemctl restart php-fpm
```

### 2. Verify Pool Status

```bash
# Check that all pools are running
sudo systemctl status php-fpm

# Test pool status endpoints
curl http://127.0.0.1:9001/status?json  # Web pool
curl http://127.0.0.1:9002/status?json  # API pool
curl http://127.0.0.1:9003/status?json  # Worker pool
curl http://127.0.0.1:9004/status?json  # Admin pool
```

### 3. Start PHP-FPM Runtime Manager

```bash
# Copy configuration
cp examples/multipool/config.yaml ./config.yaml

# Create data directory
sudo mkdir -p /var/lib/phpfpm-manager
sudo chown $USER:$USER /var/lib/phpfpm-manager

# Start manager
./phpfpm-manager -config config.yaml
```

### 4. Monitor All Pools

```bash
# Health check
curl http://localhost:9090/health

# View metrics for all pools
curl http://localhost:9090/metrics | grep phpfpm_

# View specific pool metrics
curl http://localhost:9090/metrics | grep 'pool="web"'
curl http://localhost:9090/metrics | grep 'pool="api"'
curl http://localhost:9090/metrics | grep 'pool="worker"'
curl http://localhost:9090/metrics | grep 'pool="admin"'
```

## Configuration Features

### Performance Optimizations
- **String interning** with larger cache (512 entries)
- **JSON buffer pooling** with 2KB buffers
- **Label caching** for 2000 label sets
- **Batch processing** with 100-metric batches

### Adaptive Collection
- **Base interval**: 2 seconds
- **Adaptive scaling** based on load
- **Load threshold**: 80% utilization

### Pool-Specific Scaling
- **Web pool**: Scales 10-50 workers, 70% target utilization
- **API pool**: Scales 5-25 workers, 60% target utilization
- **Worker pool**: Scales 2-10 workers, 80% target utilization
- **Admin pool**: Static 5 workers (no scaling)

### Enhanced Storage
- **Connection pooling** for better performance
- **Granular retention**: 6h raw, 7d minute, 90d hour, 2y daily
- **Health monitoring** for database connections

### Observability
- **OpenTelemetry integration** with OTLP export
- **Event tracking** with 1000-event buffer
- **JSON logging** to `/var/log/phpfpm-manager.log`
- **10% sampling** for production efficiency

## Nginx Configuration Example

For complete setup, configure Nginx to route requests to appropriate pools:

```nginx
# /etc/nginx/sites-available/multipool-app
upstream web_pool {
    server 127.0.0.1:9001;
}

upstream api_pool {
    server 127.0.0.1:9002;
}

upstream admin_pool {
    server 127.0.0.1:9004;
}

server {
    listen 80;
    server_name example.com;
    root /var/www/html;
    index index.php index.html;

    # Web requests to web pool
    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }

    location ~ \.php$ {
        fastcgi_pass web_pool;
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        include fastcgi_params;
    }

    # API requests to API pool
    location /api/ {
        try_files $uri $uri/ /api/index.php?$query_string;
    }

    location ~ ^/api/.*\.php$ {
        fastcgi_pass api_pool;
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        include fastcgi_params;
    }

    # Admin requests to admin pool
    location /admin/ {
        try_files $uri $uri/ /admin/index.php?$query_string;
    }

    location ~ ^/admin/.*\.php$ {
        fastcgi_pass admin_pool;
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        include fastcgi_params;
    }
}
```

## Monitoring Dashboards

### Grafana Query Examples

```promql
# Pool utilization comparison
phpfpm_pool_utilization_ratio

# Request rate by pool
rate(phpfpm_accepted_connections_total[5m])

# Memory usage by pool
phpfpm_process_memory_rss_bytes

# Active processes by pool
phpfpm_active_processes

# Circuit breaker states
phpfpm_circuit_breaker_state
```

### Key Metrics to Watch

- **Web pool**: High request rate, low latency
- **API pool**: Moderate request rate, medium latency
- **Worker pool**: Low request rate, high resource usage
- **Admin pool**: Very low request rate, high availability

## Scaling Behavior

### Automatic Scaling Triggers

1. **Web Pool**: Scales up when >70% of workers are active
2. **API Pool**: Scales up when >60% of workers are active
3. **Worker Pool**: Scales up when >80% of workers are active
4. **Admin Pool**: No automatic scaling (static pool)

### Manual Scaling

```bash
# View current pool status
curl http://localhost:9090/api/v1/pools

# Scale specific pool (if management API is enabled)
curl -X POST http://localhost:9090/api/v1/pools/web/scale -d '{"workers": 30}'
```

## Troubleshooting

### Common Issues

1. **Pool not starting**: Check PHP-FPM configuration syntax
2. **Status endpoint unavailable**: Verify pm.status_path in pool config
3. **High memory usage**: Adjust php_admin_value[memory_limit] in pool configs
4. **Slow responses**: Check request_terminate_timeout settings

### Debugging Commands

```bash
# Check PHP-FPM pool status
sudo php-fpm -t

# Monitor PHP-FPM logs
sudo tail -f /var/log/php-fpm/*.log

# Check manager logs
tail -f /var/log/phpfpm-manager.log

# View detailed pool metrics
curl http://localhost:9090/metrics | grep phpfpm_ | sort
```

## Next Steps

- For production deployment: See [Docker example](../docker/)
- For monitoring setup: See [docs/monitoring.md](../../docs/)
- For troubleshooting: See [docs/troubleshooting.md](../../docs/)