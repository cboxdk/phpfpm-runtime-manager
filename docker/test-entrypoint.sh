#!/bin/bash
set -e

echo "🚀 Starting PHP-FPM Test Environment"

# Configure PHP-FPM for FastCGI testing (standalone mode)
echo "🔧 Configuring PHP-FPM for FastCGI tests..."
mkdir -p /var/log /tmp

# Create a proper PHP-FPM configuration for testing
cat > /tmp/test-phpfpm.conf << 'EOF'
[global]
error_log = /tmp/php-fpm-error.log
daemonize = yes
pid = /tmp/php-fpm.pid

[test-pool]
user = www-data
group = www-data
listen = 127.0.0.1:9000
pm = dynamic
pm.max_children = 5
pm.start_servers = 2
pm.min_spare_servers = 1
pm.max_spare_servers = 3
pm.status_path = /status
ping.path = /ping

; Logging
access.log = /tmp/php-fpm-access.log
access.format = "%R - %u %t \"%m %r%Q%q\" %s %f %{mili}d %{kilo}M %C%%"

; Performance
pm.max_requests = 500
pm.process_idle_timeout = 10s

; Security
security.limit_extensions = .php

; Slow log for debugging
slowlog = /tmp/php-fpm-slow.log
request_slowlog_timeout = 5s
EOF

echo "📋 PHP-FPM 8.4 available at: $(which php-fpm)"
php-fpm --version | head -1

# Set up signal handling for clean shutdown
cleanup() {
    echo "🛑 Shutting down PHP-FPM..."
    if [ -f /tmp/php-fpm.pid ]; then
        kill -TERM $(cat /tmp/php-fpm.pid) 2>/dev/null || true
        rm -f /tmp/php-fpm.pid
    fi
    echo "✅ Cleanup completed"
}
trap cleanup EXIT INT TERM

# Start PHP-FPM with our test configuration
echo "🚀 Starting PHP-FPM for FastCGI testing..."
php-fpm --fpm-config /tmp/test-phpfpm.conf

# Wait for PHP-FPM to initialize
echo "⏳ Waiting for PHP-FPM to be ready..."
sleep 3

# Verify PHP-FPM is running
if pgrep -f "php-fpm.*test-pool" > /dev/null; then
    echo "✅ PHP-FPM is running with test-pool configuration"
    echo "📊 PHP-FPM processes:"
    pgrep -l php-fpm || true
else
    echo "⚠️  PHP-FPM test-pool not detected, checking all processes..."
    pgrep -l php-fpm || echo "No PHP-FPM processes found"
fi

# Verify listening ports
echo "🔌 Listening ports:"
netstat -tlnp 2>/dev/null | grep -E "(9000)" || true

# Test FastCGI connection
echo "🧪 Testing FastCGI connection..."
if netstat -tln | grep -q ":9000"; then
    echo "✅ FastCGI endpoint available on 127.0.0.1:9000"
    if command -v cgi-fcgi >/dev/null 2>&1; then
        echo "QUERY_STRING=" | cgi-fcgi -bind -connect 127.0.0.1:9000 2>/dev/null || echo "FastCGI connection test completed"
    else
        echo "cgi-fcgi not available, skipping connection test"
    fi
else
    echo "❌ FastCGI endpoint not available on port 9000"
    echo "Available ports:"
    netstat -tln | head -10
fi

echo "🎯 Environment ready! Running tests..."
echo "============================================"

# Execute the command
exec "$@"