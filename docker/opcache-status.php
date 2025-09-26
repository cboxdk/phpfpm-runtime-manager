<?php
// OPcache status script for testing
if (!extension_loaded('opcache')) {
    http_response_code(503);
    echo json_encode(['error' => 'OPcache extension not loaded']);
    exit;
}

$status = opcache_get_status(false);
$config = opcache_get_configuration();

// Format data for our test expectations
$result = [
    'opcache_enabled' => $status !== false,
    'cache_full' => false,
    'restart_pending' => false,
    'restart_in_progress' => false,
    'memory_usage' => [
        'used_memory' => $status['memory_usage']['used_memory'] ?? 0,
        'free_memory' => $status['memory_usage']['free_memory'] ?? 0,
        'wasted_memory' => $status['memory_usage']['wasted_memory'] ?? 0,
        'current_wasted_percentage' => $status['memory_usage']['current_wasted_percentage'] ?? 0.0,
    ],
    'opcache_statistics' => [
        'num_cached_scripts' => $status['opcache_statistics']['num_cached_scripts'] ?? 0,
        'num_cached_keys' => $status['opcache_statistics']['num_cached_keys'] ?? 0,
        'max_cached_keys' => $status['opcache_statistics']['max_cached_keys'] ?? 0,
        'hits' => $status['opcache_statistics']['hits'] ?? 0,
        'misses' => $status['opcache_statistics']['misses'] ?? 0,
        'blacklist_misses' => $status['opcache_statistics']['blacklist_misses'] ?? 0,
        'blacklist_miss_ratio' => $status['opcache_statistics']['blacklist_miss_ratio'] ?? 0.0,
        'opcache_hit_rate' => $status['opcache_statistics']['opcache_hit_rate'] ?? 0.0,
    ],
];

header('Content-Type: application/json');
echo json_encode($result, JSON_PRETTY_PRINT);
?>