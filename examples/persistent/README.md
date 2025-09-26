# Persistent Storage Configuration

Configuration example with file-based database for production environments requiring data persistence across restarts.

## Features

- **File-Based Database**: SQLite database stored on disk
- **Data Persistence**: Metrics survive service restarts
- **Historical Analysis**: Comprehensive retention policies
- **Production Ready**: Optimized for long-running services

## Use Cases

### Production Monitoring
```bash
# Long-running production service
phpfpm-runtime-manager --config=examples/persistent/config.yaml

# Data persists across restarts
sudo systemctl restart phpfpm-runtime-manager

# Historical data still available
curl http://localhost:9090/metrics
```

### Capacity Planning
```bash
# Collect data over weeks/months
# Analyze trends and growth patterns
# Plan infrastructure scaling

# Example: Query weekly worker utilization trends
sqlite3 /var/lib/phpfpm-runtime-manager/metrics.db \
  "SELECT date(timestamp), AVG(active_workers)
   FROM pool_metrics
   WHERE pool='web' AND timestamp > datetime('now', '-30 days')
   GROUP BY date(timestamp);"
```

### Performance Analysis
```bash
# Long-term performance monitoring
# Correlate with deployment timings
# Identify performance regressions

# Example: Find peak usage times
sqlite3 /var/lib/phpfpm-runtime-manager/metrics.db \
  "SELECT strftime('%H', timestamp) as hour, MAX(active_workers)
   FROM pool_metrics
   WHERE pool='web' AND timestamp > datetime('now', '-7 days')
   GROUP BY hour
   ORDER BY hour;"
```

## Configuration Highlights

### Database Path
```yaml
storage:
  database_path: "/var/lib/phpfpm-runtime-manager/metrics.db"
```

### Retention Policies
```yaml
retention:
  raw: "12h"     # Recent detailed data
  minute: "168h"  # Week of minute aggregations (7 days)
  hour: "2160h"   # Quarter of hourly data (90 days)
  daily: "8760h"  # Year of daily summaries (1 year)
```

### Optimizations
```yaml
connection_pool:
  enabled: true
  max_open_conns: 25      # More connections for file I/O
  conn_max_lifetime: "2h" # Regular connection refresh

batching:
  enabled: true
  size: 50               # Batch writes for efficiency
  flush_timeout: "5s"    # Regular flushes
```

## Setup Instructions

### 1. Create Database Directory
```bash
sudo mkdir -p /var/lib/phpfpm-runtime-manager
sudo chown phpfpm-manager:phpfpm-manager /var/lib/phpfpm-runtime-manager
sudo chmod 750 /var/lib/phpfpm-runtime-manager
```

### 2. Configure Log Directory
```bash
sudo mkdir -p /var/log/phpfpm-runtime-manager
sudo chown phpfpm-manager:phpfpm-manager /var/log/phpfpm-runtime-manager
sudo chmod 750 /var/log/phpfpm-runtime-manager
```

### 3. Deploy Configuration
```bash
sudo cp examples/persistent/config.yaml /etc/phpfpm-runtime-manager/
sudo chown phpfpm-manager:phpfpm-manager /etc/phpfpm-runtime-manager/config.yaml
sudo chmod 640 /etc/phpfpm-runtime-manager/config.yaml
```

### 4. Start Service
```bash
sudo systemctl restart phpfpm-runtime-manager
sudo systemctl status phpfpm-runtime-manager
```

## Database Management

### Manual Queries
```bash
# Connect to database
sqlite3 /var/lib/phpfpm-runtime-manager/metrics.db

# Show tables
.tables

# Show recent metrics
SELECT * FROM pool_metrics ORDER BY timestamp DESC LIMIT 10;

# Exit
.quit
```

### Backup Database
```bash
# Create backup
sudo sqlite3 /var/lib/phpfpm-runtime-manager/metrics.db \
  ".backup /backup/phpfpm-metrics-$(date +%Y%m%d).db"

# Restore from backup
sudo sqlite3 /var/lib/phpfpm-runtime-manager/metrics.db \
  ".restore /backup/phpfpm-metrics-20240115.db"
```

### Database Maintenance
```bash
# Analyze database statistics
sudo sqlite3 /var/lib/phpfpm-runtime-manager/metrics.db "ANALYZE;"

# Vacuum to reclaim space
sudo sqlite3 /var/lib/phpfpm-runtime-manager/metrics.db "VACUUM;"

# Check database integrity
sudo sqlite3 /var/lib/phpfpm-runtime-manager/metrics.db "PRAGMA integrity_check;"
```

## Monitoring Database Growth

### Database Size
```bash
# Check database file size
ls -lh /var/lib/phpfpm-runtime-manager/metrics.db

# Check available disk space
df -h /var/lib/phpfpm-runtime-manager/
```

### Record Counts
```bash
sqlite3 /var/lib/phpfpm-runtime-manager/metrics.db \
  "SELECT
     'raw' as type, COUNT(*) as records FROM pool_metrics
   UNION
   SELECT
     'minute' as type, COUNT(*) as records FROM pool_metrics_minute
   UNION
   SELECT
     'hour' as type, COUNT(*) as records FROM pool_metrics_hour
   UNION
   SELECT
     'daily' as type, COUNT(*) as records FROM pool_metrics_daily;"
```

### Cleanup Old Data
```bash
# Manual cleanup (older than retention policy)
sqlite3 /var/lib/phpfpm-runtime-manager/metrics.db \
  "DELETE FROM pool_metrics WHERE timestamp < datetime('now', '-12 hours');"

# Vacuum after cleanup
sqlite3 /var/lib/phpfpm-runtime-manager/metrics.db "VACUUM;"
```

## Performance Characteristics

- **Startup Time**: 1-2 seconds (database initialization)
- **Memory Usage**: 50-200MB (includes database cache)
- **Disk I/O**: Batched writes every 5 seconds
- **Database Growth**: ~10MB per day per pool (typical)

## Migration from Memory

### 1. Update Configuration
```yaml
storage:
  # Change from memory to file
  database_path: "/var/lib/phpfpm-runtime-manager/metrics.db"
```

### 2. Restart Service
```bash
sudo systemctl restart phpfpm-runtime-manager
```

### 3. Verify Persistence
```bash
# Check database was created
ls -la /var/lib/phpfpm-runtime-manager/

# Restart and verify data persists
sudo systemctl restart phpfpm-runtime-manager
curl http://localhost:9090/metrics
```

## Troubleshooting

### Database Permission Issues
```bash
# Check file permissions
ls -la /var/lib/phpfpm-runtime-manager/

# Fix permissions
sudo chown phpfpm-manager:phpfpm-manager /var/lib/phpfpm-runtime-manager/metrics.db
sudo chmod 640 /var/lib/phpfpm-runtime-manager/metrics.db
```

### Database Corruption
```bash
# Check integrity
sqlite3 /var/lib/phpfpm-runtime-manager/metrics.db "PRAGMA integrity_check;"

# If corrupted, restore from backup or delete and restart
sudo rm /var/lib/phpfpm-runtime-manager/metrics.db
sudo systemctl restart phpfpm-runtime-manager
```

### Disk Space Issues
```bash
# Check disk usage
df -h /var/lib/phpfpm-runtime-manager/

# Reduce retention if needed
# Edit config.yaml and restart service
```