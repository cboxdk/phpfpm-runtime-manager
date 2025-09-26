# In-Memory Configuration

Perfect for testing, development, or temporary monitoring where persistence is not required.

## Features

- **In-Memory Database**: Uses SQLite `:memory:` database
- **High-Frequency Collection**: 1-second intervals for real-time monitoring
- **Fast Startup**: No disk I/O for database initialization
- **Development-Friendly**: Console logging and debug output

## Use Cases

### Testing and Development
```bash
# Quick test run
phpfpm-runtime-manager --config=examples/memory/config.yaml

# No cleanup needed - data vanishes on stop
```

### Temporary Monitoring
```bash
# Monitor a deployment temporarily
phpfpm-runtime-manager --config=examples/memory/config.yaml &
PID=$!

# Do your deployment work...
curl http://localhost:9090/metrics

# Stop when done
kill $PID
```

### CI/CD Pipelines
```yaml
# GitHub Actions example
- name: Test PHP-FPM Performance
  run: |
    phpfpm-runtime-manager --config=examples/memory/config.yaml &
    MANAGER_PID=$!

    # Run performance tests
    ./run-performance-tests.sh

    # Collect final metrics
    curl http://localhost:9090/metrics > performance-report.txt

    # Cleanup automatic on job end
    kill $MANAGER_PID
```

## Benefits

- **Zero Persistence**: No files created, perfect for containers
- **Fast Startup**: Immediate ready state
- **Memory Efficient**: Optimized for RAM-only operation
- **Clean Shutdown**: No cleanup required

## Limitations

- **Data Loss**: All metrics lost on restart
- **Memory Usage**: Limited by available RAM
- **No Historical Analysis**: Can't analyze trends across restarts

## Performance Characteristics

- **Startup Time**: < 100ms
- **Memory Usage**: ~10-50MB depending on retention settings
- **Collection Overhead**: < 1ms per collection cycle
- **Concurrent Connections**: Up to 10 with connection pooling