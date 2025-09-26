# Configuration Examples

Comprehensive examples demonstrating different deployment scenarios and use cases.

## Quick Reference

| Example | Use Case | Configuration |
|---------|----------|---------------|
| [zero-config/](zero-config/) | Instant start, no setup | Zero config - everything defaults |
| [minimal/](minimal/) | Single pool, quick start | Auto-assigned endpoint, 2 lines |
| [full/](full/) | Complete feature showcase | All options with explanations |
| [multipool/](multipool/) | Multiple pools, production | Mixed TCP/Unix sockets |
| [docker/](docker/) | Container deployment | Full stack with monitoring |
| [inline-pools/](inline-pools/) | Simplified management | Pools in global config |

## Example Architectures

### 1. Single Pool (Minimal)
```
┌─────────────────┐    ┌─────────────────┐
│ Runtime Manager │────│   PHP-FPM Web   │
│   (Port 9090)   │    │ (127.0.0.1:9000)│
└─────────────────┘    └─────────────────┘
```

**Best for:**
- Simple applications
- Getting started
- Single-purpose servers
- Development environments

### 2. Multi-Pool (Production)
```
┌─────────────────┐    ┌─────────────────┐
│                 │────│   PHP-FPM Web   │
│                 │    │ (127.0.0.1:9000)│
│ Runtime Manager │    └─────────────────┘
│   (Port 9090)   │    ┌─────────────────┐
│                 │────│   PHP-FPM API   │
│                 │    │ (127.0.0.1:9001)│
│                 │    └─────────────────┘
│                 │    ┌─────────────────┐
│                 │────│  PHP-FPM Upload │
│                 │    │ (Unix socket)   │
└─────────────────┘    └─────────────────┘
```

**Best for:**
- Production applications
- Different workload types
- Resource optimization
- High-traffic sites

### 3. Container Deployment (Docker)
```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│     Nginx       │────│   PHP-FPM Web   │────│      Redis      │
│   (Port 80)     │    │   Container      │    │   Container     │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                       │                       │
         │              ┌─────────────────┐              │
         └──────────────│   PHP-FPM API   │──────────────┘
                        │   Container     │
                        └─────────────────┘
                                 │
                     ┌─────────────────┐
                     │   Runtime Mgr   │
                     │   Container     │
                     └─────────────────┘
                              │
         ┌────────────────────┼────────────────────┐
         │                    │                    │
┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│   Prometheus    │  │     Grafana     │  │ OpenTelemetry   │
│   Container     │  │   Container     │  │   Container     │
└─────────────────┘  └─────────────────┘  └─────────────────┘
```

**Best for:**
- Microservices architecture
- Scalable deployments
- DevOps workflows
- Cloud environments

### 4. In-Memory (Testing)
```
┌─────────────────┐    ┌─────────────────┐
│ Runtime Manager │────│   PHP-FPM Web   │
│ (:memory: DB)   │    │ (127.0.0.1:9000)│
└─────────────────┘    └─────────────────┘
```

**Best for:**
- Unit testing
- CI/CD pipelines
- Performance testing
- Development workflows

## Configuration Patterns

### FastCGI Endpoint Formats

**TCP Connections:**
```yaml
# IPv4
fastcgi_endpoint: "127.0.0.1:9000"

# IPv6
fastcgi_endpoint: "[::1]:9000"

# Hostname
fastcgi_endpoint: "localhost:9000"

# Container name (Docker)
fastcgi_endpoint: "php-fpm-web:9000"
```

**Unix Socket Connections:**
```yaml
# Standard socket path
fastcgi_endpoint: "unix:/var/run/php-fpm/www.sock"

# Custom socket path
fastcgi_endpoint: "unix:/tmp/php-fpm.sock"

# Pool-specific socket
fastcgi_endpoint: "unix:/var/run/php-fpm/api.sock"
```

### Database Storage Options

**File Database (Production):**
```yaml
storage:
  database_path: "/var/lib/phpfpm-manager/metrics.db"
  retention:
    raw: "12h"
    minute: "168h"  # 7 days
    hour: "2160h"   # 90 days
    daily: "8760h"  # 1 year
```

**In-Memory Database (Testing):**
```yaml
storage:
  database_path: ":memory:"
  retention:
    raw: "1h"
    minute: "6h"
```

**Custom Database Path:**
```yaml
storage:
  database_path: "/opt/monitoring/phpfpm.db"
  # Ensure directory exists and has correct permissions
```

### Security Configurations

**Development (Open):**
```yaml
security:
  auth:
    enabled: false
  rate_limiting:
    enabled: false
```

**Production (Secured):**
```yaml
security:
  auth:
    enabled: true
    type: "api_key"
    api_keys:
      - "secure-api-key-here"

  rate_limiting:
    enabled: true
    requests_per_minute: 1000
    burst: 100

  validation:
    max_request_size: "1MB"
```

### Monitoring Configurations

**High-Frequency (Real-time):**
```yaml
monitoring:
  collect_interval: "1s"

  adaptive_collection:
    enabled: true
    base_interval: "1s"
    max_interval: "5s"
```

**Standard (Production):**
```yaml
monitoring:
  collect_interval: "5s"

  adaptive_collection:
    enabled: true
    base_interval: "5s"
    max_interval: "30s"
    load_threshold: 0.8
```

**Low-Frequency (Resource constrained):**
```yaml
monitoring:
  collect_interval: "30s"

  adaptive_collection:
    enabled: true
    base_interval: "30s"
    max_interval: "300s"
    load_threshold: 0.6
```

## Migration Paths

### From Single to Multi-Pool

1. **Start with minimal configuration**
2. **Add second pool gradually**
3. **Test pool isolation**
4. **Scale based on metrics**

**Step 1: Current single pool**
```yaml
pools:
  - name: "web"
    fastcgi_endpoint: "127.0.0.1:9000"
```

**Step 2: Add API pool**
```yaml
pools:
  - name: "web"
    fastcgi_endpoint: "127.0.0.1:9000"
  - name: "api"
    fastcgi_endpoint: "127.0.0.1:9001"
```

### From Development to Production

1. **Enable authentication**
2. **Configure file-based database**
3. **Set up log rotation**
4. **Add monitoring integration**

**Development:**
```yaml
storage:
  database_path: ":memory:"
security:
  auth:
    enabled: false
logging:
  format: "console"
```

**Production:**
```yaml
storage:
  database_path: "/var/lib/phpfpm-manager/metrics.db"
security:
  auth:
    enabled: true
    api_keys: ["${API_KEY}"]
logging:
  format: "json"
  file: "/var/log/phpfpm-manager/manager.log"
```

## Performance Tuning

### Memory-Optimized
```yaml
monitoring:
  string_interning:
    enabled: true
    capacity: 1024

  label_caching:
    enabled: true
    max_entries: 5000

storage:
  connection_pool:
    max_open_conns: 10
```

### Throughput-Optimized
```yaml
monitoring:
  batching:
    enabled: true
    size: 200
    flush_timeout: "10s"
    max_batches: 8

storage:
  connection_pool:
    max_open_conns: 50
```

### Latency-Optimized
```yaml
monitoring:
  collect_interval: "1s"

  batching:
    enabled: true
    size: 10
    flush_timeout: "100ms"

resilience:
  circuit_breaker:
    timeout: "1s"
```

## Troubleshooting Patterns

### Debug Configuration
```yaml
logging:
  level: "debug"
  format: "console"

monitoring:
  collect_interval: "1s"  # More frequent for debugging
```

### Production Monitoring
```yaml
logging:
  level: "info"
  format: "json"
  rotation:
    max_size: "100MB"
    max_files: 5

telemetry:
  enabled: true
  sampling:
    rate: 0.1
```

## Next Steps

1. **Choose your example** based on use case
2. **Follow the specific README** in each example directory
3. **Customize configuration** for your environment
4. **Test thoroughly** before production deployment
5. **Set up monitoring** for ongoing operations