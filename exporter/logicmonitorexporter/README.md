# LogicMonitor Exporter

This exporter supports sending metrics, logs, and traces to [LogicMonitor](https://www.logicmonitor.com/).

## Configuration

The LogicMonitor exporter follows OpenTelemetry Collector configuration standards, using the same configuration options and naming conventions as the [OTLP HTTP Exporter](https://github.com/open-telemetry/opentelemetry-collector/tree/main/exporter/otlphttpexporter).

### Required Configuration

```yaml
exporters:
  logicmonitor:
    endpoint: https://company.logicmonitor.com/rest
    api_token:
      access_id: "your_access_id"
      access_key: "your_access_key"
```

### Full Configuration Example

```yaml
exporters:
  logicmonitor:
    # Required: LogicMonitor API endpoint (must include /rest path)
    endpoint: https://company.logicmonitor.com/rest
    
    # Required: API authentication credentials
    api_token:
      access_id: "your_access_id"
      access_key: "your_access_key"
    
    # HTTP Client Configuration (confighttp.ClientConfig)
    # These options follow OpenTelemetry standards
    timeout: 30s                      # HTTP request timeout (default: 30s)
    read_buffer_size: 0               # Read buffer size in bytes (default: 0)
    write_buffer_size: 524288         # Write buffer size in bytes (default: 512KB)
    max_idle_conns: 100               # Maximum idle connections (default: 100)
    max_idle_conns_per_host: 0        # Maximum idle connections per host (default: unlimited)
    max_conns_per_host: 0             # Maximum connections per host (default: unlimited)
    idle_conn_timeout: 90s            # Idle connection timeout (default: 90s)
    compression: gzip                 # Compression: none, gzip, snappy, zstd (default: none)
    
    # TLS Configuration
    tls:
      insecure: false                 # Skip TLS verification (default: false)
      insecure_skip_verify: false     # Skip certificate verification (default: false)
      ca_file: /path/to/ca.crt        # Path to CA certificate
      cert_file: /path/to/cert.crt    # Path to client certificate
      key_file: /path/to/key.key      # Path to client key
      min_version: "1.2"              # Minimum TLS version
      max_version: "1.3"              # Maximum TLS version
    
    # Custom HTTP Headers
    headers:
      X-Custom-Header: "value"
    
    # Retry Configuration (configretry.BackOffConfig)
    retry_on_failure:
      enabled: true                   # Enable retry on failure (default: true)
      initial_interval: 5s            # Initial retry delay (default: 5s)
      randomization_factor: 0.5       # Randomization factor (default: 0.5)
      multiplier: 1.5                 # Backoff multiplier (default: 1.5)
      max_interval: 30s               # Maximum retry delay (default: 30s)
      max_elapsed_time: 300s          # Maximum total retry time (default: 300s)
    
    # Queue Configuration (exporterhelper.QueueBatchConfig)
    sending_queue:
      enabled: true                   # Enable sending queue (default: true)
      num_consumers: 10               # Number of parallel consumers (default: 10)
      queue_size: 10000               # Maximum queue size (default: 10000, matches LogicMonitor's 10k req/min limit)
      storage: file_storage           # Optional: persistent storage extension
    
    # Logs Configuration
    logs:
      resource_mapping_op: "and"      # Resource mapping operation: "and" or "or" (default: "and")
```

## Authentication

The exporter supports two authentication methods:

### 1. API Token (Recommended)

```yaml
exporters:
  logicmonitor:
    endpoint: https://company.logicmonitor.com/rest
    api_token:
      access_id: "your_access_id"
      access_key: "your_access_key"
```

The exporter automatically generates LMv1 authentication signatures using HMAC-SHA256:

```
Authorization: LMv1 <AccessId>:<Signature>:<Timestamp>
```

### 2. Bearer Token

```yaml
exporters:
  logicmonitor:
    endpoint: https://company.logicmonitor.com/rest
    headers:
      Authorization: "Bearer <your_token>"
```

## Metrics Export

Metrics are sent to the [LogicMonitor Push Metrics REST API](https://www.logicmonitor.com/support/push-metrics/ingesting-metrics-with-the-push-metrics-rest-api).

### Rate Limiting Compliance

The exporter automatically batches metrics to comply with [LogicMonitor's rate limiting rules](https://www.logicmonitor.com/support/push-metrics/rate-limiting-for-push-metrics):

- **Maximum 100 instances per payload** - Metrics are batched into payloads with up to 100 instances
- **Maximum payload size**: 1 MB uncompressed, ~102 KB compressed - Payloads are sized to stay within limits
- **Ingestion frequency**: 10,000 requests per minute (~167 requests/second) - Default queue size matches this limit
- **Automatic batching** - The exporter intelligently groups metrics by resource and datasource

The exporter uses a conservative 80% safety margin on payload sizes to ensure reliable delivery.

### DataSource Name Generation

The exporter automatically generates DataSource names from metric names following LogicMonitor specifications:

- Takes the first part of the metric name (delimited by `_` or `.`)
- Capitalizes the first letter
- Short metric names (≤2 characters) are grouped as "Ungroupped"
- Enforces 64-character limit
- Removes invalid characters: `, ; / * [ ] ? ' " ` ## and newline`
- Ensures proper spacing and hyphen usage

**Examples:**
- `go_gc_cycle` → `Go`
- `jvm.memory.used` → `Jvm`
- `http_requests_total` → `Http`
- `up` → `Ungroupped`

### Metric Types

The exporter supports all OpenTelemetry metric types:

- **Gauge**: Maps to LogicMonitor GAUGE
- **Sum**: Maps to COUNTER (monotonic) or GAUGE (non-monotonic)
- **Histogram**: Exports count and sum as separate metrics
- **Summary**: Exports count and sum as separate metrics

## HTTP Client Configuration

The exporter uses OpenTelemetry's standard `confighttp.ClientConfig`, providing the same configuration options as other HTTP-based exporters:

### Timeout Configuration

```yaml
exporters:
  logicmonitor:
    timeout: 30s                    # Overall request timeout
```

### Connection Pooling

```yaml
exporters:
  logicmonitor:
    max_idle_conns: 100             # Maximum idle connections
    max_idle_conns_per_host: 10     # Maximum idle connections per host
    max_conns_per_host: 100         # Maximum total connections per host
    idle_conn_timeout: 90s          # How long idle connections stay open
```

### Compression

```yaml
exporters:
  logicmonitor:
    compression: gzip               # Options: none, gzip, snappy, zstd
```

### TLS Configuration

```yaml
exporters:
  logicmonitor:
    tls:
      insecure: false               # Skip TLS verification
      ca_file: /path/to/ca.crt      # Custom CA certificate
      cert_file: /path/to/cert.crt  # Client certificate
      key_file: /path/to/key.key    # Client key
      min_version: "1.2"            # Minimum TLS version (1.0, 1.1, 1.2, 1.3)
```

## Retry and Queue Configuration

### Retry Configuration

The exporter uses exponential backoff for retries:

```yaml
exporters:
  logicmonitor:
    retry_on_failure:
      enabled: true                 # Enable retries
      initial_interval: 5s          # Wait 5s before first retry
      max_interval: 30s             # Maximum wait between retries
      max_elapsed_time: 300s        # Give up after 5 minutes
      multiplier: 1.5               # Increase wait time by 1.5x each retry
```

### Sending Queue

The queue buffers data before sending:

```yaml
exporters:
  logicmonitor:
    sending_queue:
      enabled: true                 # Enable buffering
      num_consumers: 10             # Number of parallel senders
      queue_size: 1000              # Buffer up to 1000 batches
```

## Environment Variables

You can use environment variables in the configuration:

```yaml
exporters:
  logicmonitor:
    endpoint: "https://${env:LM_COMPANY}.logicmonitor.com/rest"
    api_token:
      access_id: "${env:LM_ACCESS_ID}"
      access_key: "${env:LM_ACCESS_KEY}"
```

## Complete Pipeline Example

```yaml
receivers:
  prometheus:
    config:
      scrape_configs:
        - job_name: 'my-app'
          scrape_interval: 30s
          static_configs:
            - targets: ['localhost:8080']

processors:
  batch:
    timeout: 10s
    send_batch_size: 1024

exporters:
  logicmonitor:
    endpoint: "https://company.logicmonitor.com/rest"
    api_token:
      access_id: "${env:LM_ACCESS_ID}"
      access_key: "${env:LM_ACCESS_KEY}"
    timeout: 30s
    compression: gzip
    retry_on_failure:
      enabled: true
      initial_interval: 5s
      max_interval: 30s
      max_elapsed_time: 300s
    sending_queue:
      enabled: true
      num_consumers: 10
      queue_size: 1000

service:
  pipelines:
    metrics:
      receivers: [prometheus]
      processors: [batch]
      exporters: [logicmonitor]
```

## References

- [LogicMonitor Push Metrics REST API Documentation](https://www.logicmonitor.com/support/push-metrics/ingesting-metrics-with-the-push-metrics-rest-api)
- [OpenTelemetry Collector Configuration](https://opentelemetry.io/docs/collector/configuration/)
- [OTLP HTTP Exporter](https://github.com/open-telemetry/opentelemetry-collector/tree/main/exporter/otlphttpexporter)
