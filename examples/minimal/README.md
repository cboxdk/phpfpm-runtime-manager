# Minimal Configuration

The smallest possible configuration that explicitly defines a pool - just 2 lines!

> **Even simpler?** See [zero-config example](../zero-config/) for setup with ZERO configuration!

## Configuration

```yaml
pools:
  - name: "web"
```

That's it! Everything else uses intelligent defaults, including:
- **FastCGI Endpoint**: Auto-assigned to `127.0.0.1:9000` (first available port)
- **Status Path**: `/status`
- **All other settings**: Smart defaults for monitoring, health checks, etc.

## What This Gives You

### 🚀 **Automatic Defaults:**
- **Server**: `127.0.0.1:9090` with `/metrics` endpoint
- **PHP-FPM**: Global config at `/opt/phpfpm-runtime-manager/php-fpm.conf`
- **Storage**: In-memory database (`:memory:`)
- **Status Path**: `/status` for pool status
- **Max Workers**: Inherited from PHP-FPM config
- **Monitoring**: 5-second collection interval
- **Health Checks**: Enabled with 30-second intervals
- **Optimizations**: String interning, JSON pooling, batching
- **Logging**: Info level, console format
- **Circuit Breaker**: Basic protection enabled

### 🎯 **Perfect For:**
- Quick testing and development
- Single PHP-FPM pool setups
- Getting started immediately
- Learning the system

## Quick Start

```bash
# Save the minimal config
cat > config.yaml << EOF
pools:
  - name: "web"
    fastcgi_endpoint: "127.0.0.1:9000"
EOF

# Start the manager
phpfpm-runtime-manager run --config=config.yaml
```

## Prerequisites

Your PHP-FPM pool must have status endpoint enabled:

```ini
; In /etc/php-fpm.d/www.conf
pm.status_path = /status
ping.path = /ping
```

## Verify It Works

```bash
# Health check
curl http://127.0.0.1:9090/health

# Metrics
curl http://127.0.0.1:9090/metrics
```

## Next Steps

- **Even simpler**: See [zero-config example](../zero-config/) for NO configuration
- **More pools**: See [multipool example](../multipool/)
- **All options**: See [full example](../full/) for comprehensive configuration
- **Production**: See [docker example](../docker/) for complete stack

## Default Values Reference

For reference, this minimal config is equivalent to:

<details>
<summary>Click to see full equivalent configuration</summary>

```yaml
server:
  bind_address: "127.0.0.1:9090"
  metrics_path: "/metrics"

phpfpm:
  global_config_path: "/opt/phpfpm-runtime-manager/php-fpm.conf"

pools:
  - name: "web"
    fastcgi_endpoint: "127.0.0.1:9000"
    fastcgi_status_path: "/status"
    health_check:
      enabled: true
      interval: "30s"
      timeout: "5s"
      retries: 3

storage:
  database_path: ":memory:"
  retention:
    raw: "24h"
    minute: "168h"
    hour: "8760h"

monitoring:
  collect_interval: "1s"
  string_interning:
    enabled: true
    capacity: 256
  json_pooling:
    enabled: true
    buffer_size: 1024
  batching:
    enabled: true
    size: 50
    flush_timeout: "5s"

logging:
  level: "info"
  format: "json"
```

</details>