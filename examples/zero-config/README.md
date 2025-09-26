# Zero Configuration Example

The ultimate minimal setup - run PHP-FPM Runtime Manager with ZERO configuration!

## What You Get

When you run with an empty configuration file (or no file at all), the runtime manager automatically provides:

### 🚀 **Automatic Pool Creation**
- **Pool Name**: `web`
- **FastCGI Endpoint**: `127.0.0.1:9000`
- **Status Path**: `/status`

### 🌐 **Server Defaults**
- **HTTP Server**: `0.0.0.0:9090`
- **Metrics Endpoint**: `http://localhost:9090/metrics`
- **Health Endpoint**: `http://localhost:9090/health`
- **Admin Endpoint**: `http://localhost:9090/admin`

### 💾 **Storage Defaults**
- **Database**: In-memory (`:memory:`)
- **Raw Data Retention**: 24 hours
- **Minute Data Retention**: 7 days
- **Hour Data Retention**: 1 year

### ⚙️ **PHP-FPM Defaults**
- **Global Config**: `/opt/phpfpm-runtime-manager/php-fpm.conf`
- **Collection Interval**: 1 second
- **Health Checks**: Enabled every 30 seconds

### 📊 **Performance Optimizations**
- **String Interning**: Enabled for memory efficiency
- **JSON Buffer Pooling**: Enabled to reduce allocations
- **Metric Batching**: Enabled with 50-metric batches
- **Circuit Breaker**: Enabled for resilience

## Quick Start

```bash
# Create empty config file (optional)
touch config.yaml

# Or create minimal config
echo "# Zero config - everything uses defaults" > config.yaml

# Start the runtime manager
phpfpm-runtime-manager run --config=config.yaml

# Or run without any config file (uses built-in defaults)
phpfpm-runtime-manager run
```

## Verify It Works

```bash
# Check health
curl http://localhost:9090/health

# View metrics
curl http://localhost:9090/metrics

# Check admin status
curl http://localhost:9090/admin/pools
```

## What This Example Shows

This demonstrates the **ultimate ease-of-use**:

1. **No Learning Curve**: Start immediately without reading documentation
2. **No Configuration Errors**: Impossible to misconfigure when there's no config
3. **Intelligent Defaults**: Every default is carefully chosen for common use cases
4. **Production Ready**: Defaults include monitoring, health checks, and optimizations

## Requirements

Your PHP-FPM pool must have status endpoint enabled in `/opt/phpfpm-runtime-manager/pools.d/www.conf`:

```ini
; Enable status page
pm.status_path = /status
ping.path = /ping
```

## Next Steps

Start here, then:

1. **Need more pools?** → See [minimal example](../minimal/) to add pools
2. **Custom settings?** → See [full example](../full/) for all options
3. **Production ready?** → See [docker example](../docker/) for complete stack
4. **Multiple pools?** → See [multipool example](../multipool/) for scaling

## Design Philosophy

**Zero-Config First**: The system should work perfectly out of the box for 80% of use cases, with progressive configuration for advanced needs.

This example proves that philosophy - you can monitor PHP-FPM with literally zero configuration!