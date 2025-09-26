# Full Configuration Example

Complete configuration showcasing every available option in the PHP-FPM Runtime Manager.

## Purpose

This example demonstrates the full capability of the runtime manager by explicitly showing every configuration option with detailed explanations. Perfect for:

- **Production deployments** requiring fine-tuned control
- **Enterprise environments** with specific security and compliance needs
- **Learning** the complete feature set and available options
- **Reference** when customizing configurations

## Configuration Sections

### 🌐 Server Configuration
- HTTP server settings (bind address, TLS, authentication)
- Metrics and health endpoints
- Security (API keys, basic auth, mutual TLS)

### 💾 Storage Configuration
- Database settings (file path, in-memory mode)
- Data retention policies (raw, minute, hour aggregations)
- Performance tuning (connection pools, SQLite optimizations)

### ⚙️ PHP-FPM Global Settings
- Global configuration file path
- PHP-FPM binary location
- Emergency restart settings

### 🏊 Pool Configurations
- **Web Pool**: High-traffic frontend with dynamic scaling
- **API Pool**: Moderate-traffic backend optimized for API responses
- **Admin Pool**: Low-traffic admin interface with static scaling

Each pool includes:
- FastCGI endpoints (TCP and Unix socket examples)
- Health checks with custom intervals
- Resource limits and thresholds
- Automatic scaling configuration

### 📊 Monitoring & Performance
- Metrics collection intervals
- Performance optimizations:
  - **String interning** for memory efficiency
  - **JSON buffer pooling** to reduce allocations
  - **Metric batching** for network efficiency
  - **Circuit breaker** for resilience

### 📝 Logging Configuration
- Log levels and formatting options
- File rotation and compression
- Request/response logging (with security considerations)

### 🔍 Observability (Advanced)
- OpenTelemetry distributed tracing
- Prometheus remote write integration
- Service monitoring and sampling

### 🚨 Alerting (Optional)
- Custom alert rules (CPU, pool status)
- Notification channels (webhook, email)
- Severity levels and thresholds

### 🔒 Security Features
- Rate limiting per IP
- CORS configuration for web interfaces
- Request validation and sanitization

## Quick Start

```bash
# Copy the configuration
cp examples/full/config.yaml /etc/phpfpm-runtime-manager/

# Customize for your environment
sudo nano /etc/phpfpm-runtime-manager/config.yaml

# Start with full configuration
phpfpm-runtime-manager run --config=/etc/phpfpm-runtime-manager/config.yaml
```

## Environment-Specific Customization

### Development Environment
```yaml
# Use in-memory database for development
storage:
  database_path: ":memory:"

# Enable debug logging
logging:
  level: "debug"
  format: "console"

# Disable authentication for local dev
server:
  auth:
    enabled: false
```

### Production Environment
```yaml
# Persistent database for production
storage:
  database_path: "/var/lib/phpfpm-runtime-manager/metrics.db"

# Enable TLS and authentication
server:
  tls:
    enabled: true
    cert_file: "/etc/ssl/certs/server.crt"
    key_file: "/etc/ssl/private/server.key"
  auth:
    enabled: true
    type: "api_key"
    api_key: "${PHPFPM_API_KEY}"

# Enable observability
observability:
  tracing:
    enabled: true
    endpoint: "http://jaeger:14268/api/traces"
```

### High-Scale Environment
```yaml
# Optimized for high throughput
monitoring:
  collect_interval: "5s"  # Reduce collection frequency

  string_interning:
    enabled: true
    capacity: 1024  # Increase string pool

  batching:
    enabled: true
    size: 100  # Larger batches
    flush_timeout: "10s"

# Database performance
storage:
  performance:
    max_connections: 25
    pragma:
      cache_size: 50000
```

## Configuration Validation

The runtime manager validates all configuration options at startup:

```bash
# Test configuration syntax
phpfpm-runtime-manager validate --config=examples/full/config.yaml

# Test with specific environment
PHPFPM_API_KEY=test-key phpfpm-runtime-manager validate --config=examples/full/config.yaml
```

## Common Customizations

### 1. FastCGI Endpoints
```yaml
pools:
  - name: "web"
    # TCP endpoint
    fastcgi_endpoint: "127.0.0.1:9000"

  - name: "api"
    # Unix socket endpoint
    fastcgi_endpoint: "unix:/var/run/php-fpm/api.sock"
```

### 2. Health Check Tuning
```yaml
health_check:
  enabled: true
  interval: "15s"    # Check every 15 seconds
  timeout: "3s"      # 3 second timeout
  retries: 2         # Retry twice before marking unhealthy
```

### 3. Scaling Configuration
```yaml
scaling:
  enabled: true
  strategy: "dynamic"           # dynamic, static, ondemand
  scale_up_threshold: 70.0      # Scale up at 70% CPU
  scale_down_threshold: 30.0    # Scale down at 30% CPU
  cooldown: "5m"               # Wait 5 minutes between actions
```

### 4. Performance Optimization
```yaml
monitoring:
  # Memory efficiency
  string_interning:
    enabled: true
    capacity: 512

  # Network efficiency
  batching:
    enabled: true
    size: 75
    flush_timeout: "3s"

  # Resilience
  circuit_breaker:
    enabled: true
    failure_threshold: 3
    timeout: "60s"
```

## Integration Examples

### Prometheus + Grafana
```yaml
# Enable metrics export to Prometheus
observability:
  metrics_export:
    enabled: true
    interval: "15s"
    prometheus:
      - url: "http://prometheus:9090/api/v1/write"
```

### Slack Alerting
```yaml
alerting:
  enabled: true
  notifications:
    webhook:
      enabled: true
      url: "https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK"
```

### OpenTelemetry Tracing
```yaml
observability:
  tracing:
    enabled: true
    endpoint: "http://jaeger:14268/api/traces"
    service_name: "phpfpm-runtime-manager"
    sampling_ratio: 0.1  # Sample 10% of requests
```

## Security Considerations

### API Key Management
```bash
# Generate secure API key
export PHPFPM_API_KEY=$(openssl rand -hex 32)

# Use environment variables in config
api_key: "${PHPFPM_API_KEY}"
```

### TLS Certificate Setup
```bash
# Generate self-signed certificate for testing
openssl req -x509 -newkey rsa:4096 -keyout server.key -out server.crt -days 365 -nodes
```

### Rate Limiting
```yaml
security:
  rate_limiting:
    enabled: true
    requests_per_minute: 100  # Adjust based on expected traffic
    burst: 20
```

## Troubleshooting

### Configuration Errors
```bash
# Validate configuration syntax
phpfpm-runtime-manager validate --config=config.yaml

# Check specific section
phpfpm-runtime-manager validate --config=config.yaml --section=pools
```

### Performance Issues
```bash
# Enable debug logging temporarily
logging:
  level: "debug"

# Monitor metrics collection overhead
curl http://localhost:9090/metrics | grep phpfpm_collection_duration
```

### Connection Problems
```bash
# Test FastCGI endpoint connectivity
echo "SCRIPT_NAME=/status" | cgi-fcgi -bind -connect 127.0.0.1:9000

# Check Unix socket permissions
ls -la /var/run/php-fpm/
```

## Best Practices

1. **Start Simple**: Begin with [minimal example](../minimal/) and add complexity gradually
2. **Environment Variables**: Use environment variables for secrets and environment-specific values
3. **Validation**: Always validate configuration changes before deployment
4. **Monitoring**: Enable detailed metrics in production for better visibility
5. **Security**: Use TLS and authentication in production environments
6. **Performance**: Enable optimizations (string interning, batching) for high-scale deployments

## Next Steps

- **Production Deployment**: See [production guide](../../docs/production.md)
- **Docker Setup**: Check [docker example](../docker/)
- **Monitoring**: Configure [Grafana dashboards](../../docs/monitoring.md)
- **Troubleshooting**: Review [troubleshooting guide](../../docs/troubleshooting.md)