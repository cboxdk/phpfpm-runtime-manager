# Inline Pool Configuration

Example showing pools configured directly in the global PHP-FPM configuration file instead of separate pool files.

## Architecture

```
/opt/phpfpm-runtime-manager/php-fpm.conf (global config)
├── [global settings]
├── [web pool config inline]
└── [api pool config inline]

Runtime Manager reads:
├── Global config: /opt/phpfpm-runtime-manager/php-fpm.conf
├── Pool 'web': inline configuration
└── Pool 'api': inline configuration
```

## Use Cases

- **Simple setups** where separate pool files add complexity
- **Container environments** with minimal file structure
- **Legacy systems** with existing inline pool configurations
- **Development environments** with simplified configuration management

## Configuration Structure

### Runtime Manager Config
```yaml
phpfpm:
  global_config_path: "/opt/phpfpm-runtime-manager/php-fpm.conf"  # Contains all pool configs

pools:
  - name: "web"
    # config_path: omitted - pool is inline in global config
    fastcgi_endpoint: "127.0.0.1:9000"
    fastcgi_status_path: "/status"

  - name: "api"
    # config_path: omitted - pool is inline in global config
    fastcgi_endpoint: "127.0.0.1:9001"
    fastcgi_status_path: "/status"
```

### Global PHP-FPM Config Example

`/opt/phpfpm-runtime-manager/php-fpm.conf`:
```ini
; Global PHP-FPM configuration
[global]
pid = /var/run/php-fpm.pid
error_log = /var/log/php-fpm.log
log_level = notice
emergency_restart_threshold = 10
emergency_restart_interval = 1m
process_control_timeout = 10s
daemonize = yes

; Web pool - inline configuration
[web]
user = www-data
group = www-data
listen = 127.0.0.1:9000
listen.owner = www-data
listen.group = www-data
listen.mode = 0660

pm = dynamic
pm.max_children = 20
pm.start_servers = 5
pm.min_spare_servers = 3
pm.max_spare_servers = 8
pm.max_requests = 1000

; Status and monitoring
pm.status_path = /status
ping.path = /ping
ping.response = pong

; API pool - inline configuration
[api]
user = www-data
group = www-data
listen = 127.0.0.1:9001
listen.owner = www-data
listen.group = www-data
listen.mode = 0660

pm = dynamic
pm.max_children = 15
pm.start_servers = 3
pm.min_spare_servers = 2
pm.max_spare_servers = 5
pm.max_requests = 500

; Status and monitoring
pm.status_path = /status
ping.path = /ping
ping.response = pong

; API-specific settings
php_admin_value[memory_limit] = 256M
php_admin_value[max_execution_time] = 60
```

## Setup Instructions

### 1. Create Global PHP-FPM Config

```bash
sudo mkdir -p /opt/phpfpm-runtime-manager
sudo tee /opt/phpfpm-runtime-manager/php-fpm.conf > /dev/null <<'EOF'
[global]
pid = /var/run/php-fpm.pid
error_log = /var/log/php-fpm.log
log_level = notice
emergency_restart_threshold = 10
emergency_restart_interval = 1m
process_control_timeout = 10s
daemonize = yes

[web]
user = www-data
group = www-data
listen = 127.0.0.1:9000
listen.owner = www-data
listen.group = www-data
listen.mode = 0660

pm = dynamic
pm.max_children = 20
pm.start_servers = 5
pm.min_spare_servers = 3
pm.max_spare_servers = 8
pm.max_requests = 1000

pm.status_path = /status
ping.path = /ping
ping.response = pong

[api]
user = www-data
group = www-data
listen = 127.0.0.1:9001
listen.owner = www-data
listen.group = www-data
listen.mode = 0660

pm = dynamic
pm.max_children = 15
pm.start_servers = 3
pm.min_spare_servers = 2
pm.max_spare_servers = 5
pm.max_requests = 500

pm.status_path = /status
ping.path = /ping
ping.response = pong

php_admin_value[memory_limit] = 256M
php_admin_value[max_execution_time] = 60
EOF
```

### 2. Remove Separate Pool Files

```bash
# Backup existing pool files
sudo mkdir -p /etc/php-fpm.d.backup
sudo mv /etc/php-fpm.d/*.conf /etc/php-fpm.d.backup/ 2>/dev/null || true

# Or disable include in php-fpm.conf
# Note: Using dedicated config path, no need to modify system php-fpm.conf
```

### 3. Test PHP-FPM Configuration

```bash
# Test configuration syntax
sudo php-fpm -t

# Start PHP-FPM
sudo systemctl restart php-fpm
sudo systemctl status php-fpm
```

### 4. Deploy Runtime Manager Config

```bash
sudo cp examples/inline-pools/config.yaml /etc/phpfpm-runtime-manager/
sudo systemctl restart phpfpm-runtime-manager
```

### 5. Verify Setup

```bash
# Check PHP-FPM pools
curl http://localhost:9090/metrics | grep phpfpm_pool

# Test both pools
echo "SCRIPT_NAME=/status" | cgi-fcgi -bind -connect 127.0.0.1:9000
echo "SCRIPT_NAME=/status" | cgi-fcgi -bind -connect 127.0.0.1:9001
```

## Advantages

### Simplified Management
- **Single file** contains all pool configurations
- **No includes** to manage or track
- **Easier deployment** with single configuration file

### Container Friendly
- **Minimal files** in container image
- **Single config** volume mount needed
- **Easier templating** for dynamic environments

### Legacy Compatibility
- **Existing setups** often use inline pools
- **Migration friendly** for older configurations
- **Standard format** supported by all PHP-FPM versions

## Disadvantages

### Limited Flexibility
- **All pools** must be in one file
- **Harder to manage** large numbers of pools
- **No selective include** of specific pools

### Operational Complexity
- **Restart required** for any pool change
- **Larger config file** harder to review
- **No modular** pool management

## Migration Strategies

### From Separate Files to Inline

```bash
# 1. Backup current setup
sudo cp /etc/php-fpm.conf /etc/php-fpm.conf.backup
sudo cp -r /etc/php-fpm.d /etc/php-fpm.d.backup

# 2. Merge pool configs into global config
for pool in /etc/php-fpm.d/*.conf; do
  echo "# Pool from $pool" >> /etc/php-fpm.conf.new
  cat "$pool" >> /etc/php-fpm.conf.new
  echo "" >> /etc/php-fpm.conf.new
done

# 3. Replace configuration
sudo mv /etc/php-fpm.conf.new /etc/php-fpm.conf

# 4. Test and restart
sudo php-fpm -t
sudo systemctl restart php-fpm
```

### From Inline to Separate Files

```bash
# 1. Extract pools from global config
sudo mkdir -p /etc/php-fpm.d

# 2. Create separate pool files (manual process)
# Extract each [poolname] section to /etc/php-fpm.d/poolname.conf

# 3. Add include directive
echo "include=/etc/php-fpm.d/*.conf" >> /etc/php-fpm.conf

# 4. Remove inline pools from global config
# Edit /etc/php-fpm.conf to remove [pool] sections

# 5. Test and restart
sudo php-fpm -t
sudo systemctl restart php-fpm
```

## Best Practices

### Configuration Organization
```ini
[global]
; Global settings first

[pool1]
; Pool 1 configuration

[pool2]
; Pool 2 configuration
```

### Documentation
```ini
; Web frontend pool - high traffic
[web]
; Configuration here

; API backend pool - moderate traffic
[api]
; Configuration here
```

### Validation
```bash
# Always test before restart
sudo php-fpm -t

# Check for syntax errors
sudo php-fpm -tt
```