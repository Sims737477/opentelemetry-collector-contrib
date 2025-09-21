# LogicMonitor Push Metrics REST API - Developer Guide

## Overview

The LogicMonitor Push Metrics REST API allows you to send custom metrics directly to your LogicMonitor portal. This is ideal for:
- Custom applications and services
- Cloud-native workloads
- Microservices architectures
- Any system that generates metrics not covered by standard LogicMonitor collectors

## API Endpoint

```
POST https://{company}.logicmonitor.com/rest/v2/metric/ingest
```

## Authentication

LogicMonitor supports two authentication methods:

### 1. API Token (Recommended)
```bash
# Generate signature using HMAC-SHA256
auth_string = "POST" + timestamp + request_body + "/metric/ingest"
signature = base64(hmac_sha256(access_key, auth_string))
authorization_header = "LMv1 " + access_id + ":" + signature + ":" + timestamp
```

### 2. Bearer Token
```bash
Authorization: Bearer <your_bearer_token>
```

## Request Structure

### Single Resource Payload

```json
{
  "resourceName": "my-server-01",
  "resourceIds": {
    "system.displayname": "my-server-01",
    "system.ips": "192.168.1.100"
  },
  "resourceProperties": {
    "environment": "production",
    "region": "us-west-2"
  },
  "dataSource": "CustomApp",
  "dataSourceDisplayName": "Custom Application Metrics",
  "dataSourceGroup": "Applications",
  "instances": [
    {
      "instanceName": "app-instance-1",
      "instanceDisplayName": "Application Instance 1",
      "instanceProperties": {
        "version": "2.1.0",
        "port": "8080"
      },
      "dataPoints": [
        {
          "dataPointName": "response_time",
          "dataPointType": "GAUGE",
          "dataPointAggregationType": "avg",
          "dataPointDescription": "Average response time in milliseconds",
          "values": {
            "1640995200": "150.5",
            "1640995260": "142.3"
          }
        }
      ]
    }
  ]
}
```

### Multiple Resources Payload

```json
[
  {
    "resourceName": "server-01",
    "resourceIds": { "system.displayname": "server-01" },
    "dataSource": "CPU",
    "instances": [...]
  },
  {
    "resourceName": "server-02", 
    "resourceIds": { "system.displayname": "server-02" },
    "dataSource": "Memory",
    "instances": [...]
  }
]
```

## Field Reference

### Resource Level Fields

| Field | Type | Required | Description | Constraints |
|-------|------|----------|-------------|-------------|
| `resourceName` | String | **Yes** | Display name for the resource | 255 chars max |
| `resourceIds` | Object | **Yes** | Key-value pairs to identify the resource | Must include `system.displayname` |
| `resourceProperties` | Object | No | Additional metadata for the resource | Key-value pairs |
| `dataSource` | String | **Yes** | Technical name of the data source | 64 chars max, alphanumeric + underscore |
| `dataSourceDisplayName` | String | No | Human-readable data source name | Defaults to `dataSource` value |
| `dataSourceGroup` | String | No | Logical grouping for data sources | 128 chars max |

### Instance Level Fields

| Field | Type | Required | Description | Constraints |
|-------|------|----------|-------------|-------------|
| `instanceName` | String | **Yes** | Technical instance identifier | 255 chars max |
| `instanceDisplayName` | String | No | Human-readable instance name | Defaults to `instanceName` |
| `instanceProperties` | Object | No | Instance-specific metadata | Key-value pairs |

### DataPoint Level Fields

| Field | Type | Required | Description | Valid Values |
|-------|------|----------|-------------|--------------|
| `dataPointName` | String | **Yes** | Metric name | 128 chars max |
| `dataPointType` | String | No | Metric type | `"gauge"`, `"counter"`, `"derive"` (default: `"gauge"`) |
| `dataPointAggregationType` | String | No | How to aggregate values within 1-minute intervals | `"none"`, `"avg"`, `"sum"`, `"min"`, `"max"`, `"percentile"` (default: `"none"`) |
| `dataPointDescription` | String | No | Metric description | 1024 chars max |
| `percentileValue` | Integer | Conditional | Required when aggregationType is `"percentile"` | 0-100 |
| `values` | Object | **Yes** | Timestamp-value pairs | Keys: Unix epoch (seconds), Values: numeric |

## Metric Types Explained

- **`gauge`**: Point-in-time values (CPU usage, memory usage, temperature)
- **`counter`**: Monotonically increasing values (total requests, bytes sent)
- **`derive`**: Rate of change calculations (requests per second)

## Aggregation Types

When multiple values are received within a 1-minute window:

- **`none`**: Use the last received value
- **`avg`**: Calculate average of all values
- **`sum`**: Sum all values
- **`min`**: Use minimum value
- **`max`**: Use maximum value
- **`percentile`**: Calculate specified percentile (requires `percentileValue`)

## Query Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `create` | Boolean | `false` | Auto-create resources/datasources if they don't exist |
| `match` | String | `"displayname"` | How to match existing resources (`"displayname"` or `"resourceids"`) |

## Response Codes

| Code | Status | Description |
|------|--------|-------------|
| `202` | Accepted | Metrics successfully ingested |
| `207` | Multi-Status | Partial success (some metrics failed) |
| `400` | Bad Request | Invalid request format |
| `401` | Unauthorized | Authentication failed |
| `403` | Forbidden | Insufficient permissions |
| `429` | Too Many Requests | Rate limit exceeded |

### Multi-Status Response Example

```json
{
  "success": false,
  "message": "Some events were not accepted. See the 'errors' property for additional information.",
  "errors": [
    {
      "code": 1001,
      "message": "Resource Name is mandatory.",
      "resourceIds": {
        "system.displayname": "server-01"
      }
    }
  ]
}
```

## Code Examples

### Python Example

```python
#!/usr/bin/env python3
import time
import hmac
import hashlib
import base64
import requests
import json

class LogicMonitorPusher:
    def __init__(self, company, access_id, access_key):
        self.company = company
        self.access_id = access_id
        self.access_key = access_key
        self.url = f"https://{company}.logicmonitor.com/rest/v2/metric/ingest"
    
    def _generate_auth(self, timestamp, body):
        """Generate LMv1 authentication header"""
        resource_path = '/v2/metric/ingest'
        req_var = f"POST{timestamp}{body}{resource_path}"
        
        signature = base64.b64encode(
            hmac.new(
                self.access_key.encode('utf-8'),
                req_var.encode('utf-8'),
                hashlib.sha256
            ).digest()
        ).decode('utf-8')
        
        return f"LMv1 {self.access_id}:{signature}:{timestamp}"
    
    def push_metric(self, resource_name, resource_ids, datasource, 
                   instance_name, datapoint_name, value, 
                   datapoint_type="GAUGE", aggregation="none"):
        """Push a single metric to LogicMonitor"""
        
        timestamp = int(time.time())
        
        payload = {
            "resourceName": resource_name,
            "resourceIds": resource_ids,
            "dataSource": datasource,
            "instances": [{
                "instanceName": instance_name,
                "dataPoints": [{
                    "dataPointName": datapoint_name,
                    "dataPointType": datapoint_type,
                    "dataPointAggregationType": aggregation,
                    "values": {
                        str(timestamp): str(value)
                    }
                }]
            }]
        }
        
        body = json.dumps(payload)
        auth_header = self._generate_auth(timestamp * 1000, body)  # LM expects milliseconds
        
        headers = {
            'Content-Type': 'application/json',
            'Authorization': auth_header
        }
        
        response = requests.post(
            self.url,
            headers=headers,
            data=body,
            params={"create": "true"}
        )
        
        if response.status_code == 202:
            print("✅ Metric pushed successfully")
        else:
            print(f"❌ Failed to push metric: {response.status_code} - {response.text}")
        
        return response

# Usage example
if __name__ == "__main__":
    pusher = LogicMonitorPusher(
        company="your-company",
        access_id="your-access-id", 
        access_key="your-access-key"
    )
    
    # Push CPU usage metric
    pusher.push_metric(
        resource_name="web-server-01",
        resource_ids={"system.displayname": "web-server-01"},
        datasource="CustomCPU",
        instance_name="cpu-total",
        datapoint_name="usage_percent",
        value=75.5
    )
```

### Go Example

```go
package main

import (
    "bytes"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "net/http"
    "strconv"
    "time"
)

type LogicMonitorClient struct {
    Company   string
    AccessID  string
    AccessKey string
    BaseURL   string
}

type MetricPayload struct {
    ResourceName string            `json:"resourceName"`
    ResourceIds  map[string]string `json:"resourceIds"`
    DataSource   string            `json:"dataSource"`
    Instances    []Instance        `json:"instances"`
}

type Instance struct {
    InstanceName string      `json:"instanceName"`
    DataPoints   []DataPoint `json:"dataPoints"`
}

type DataPoint struct {
    DataPointName            string            `json:"dataPointName"`
    DataPointType            string            `json:"dataPointType"`
    DataPointAggregationType string            `json:"dataPointAggregationType"`
    Values                   map[string]string `json:"values"`
}

func NewLogicMonitorClient(company, accessID, accessKey string) *LogicMonitorClient {
    return &LogicMonitorClient{
        Company:   company,
        AccessID:  accessID,
        AccessKey: accessKey,
        BaseURL:   fmt.Sprintf("https://%s.logicmonitor.com/rest", company),
    }
}

func (c *LogicMonitorClient) generateAuth(timestamp int64, body string) string {
    resourcePath := "/v2/metric/ingest"
    reqVar := fmt.Sprintf("POST%d%s%s", timestamp, body, resourcePath)
    
    h := hmac.New(sha256.New, []byte(c.AccessKey))
    h.Write([]byte(reqVar))
    signature := base64.StdEncoding.EncodeToString(h.Sum(nil))
    
    return fmt.Sprintf("LMv1 %s:%s:%d", c.AccessID, signature, timestamp)
}

func (c *LogicMonitorClient) PushMetric(resourceName string, resourceIds map[string]string,
    datasource, instanceName, datapointName string, value float64) error {
    
    timestamp := time.Now().Unix() * 1000 // Convert to milliseconds
    
    payload := MetricPayload{
        ResourceName: resourceName,
        ResourceIds:  resourceIds,
        DataSource:   datasource,
        Instances: []Instance{{
            InstanceName: instanceName,
            DataPoints: []DataPoint{{
                DataPointName:            datapointName,
                DataPointType:            "GAUGE",
                DataPointAggregationType: "none",
                Values: map[string]string{
                    strconv.FormatInt(timestamp/1000, 10): fmt.Sprintf("%.2f", value),
                },
            }},
        }},
    }
    
    body, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("failed to marshal payload: %w", err)
    }
    
    auth := c.generateAuth(timestamp, string(body))
    
    req, err := http.NewRequest("POST", c.BaseURL+"/v2/metric/ingest?create=true", 
        bytes.NewBuffer(body))
    if err != nil {
        return fmt.Errorf("failed to create request: %w", err)
    }
    
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", auth)
    
    client := &http.Client{Timeout: 30 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return fmt.Errorf("failed to send request: %w", err)
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != 202 {
        return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
    }
    
    fmt.Println("✅ Metric pushed successfully")
    return nil
}

func main() {
    client := NewLogicMonitorClient("your-company", "your-access-id", "your-access-key")
    
    err := client.PushMetric(
        "api-server-01",
        map[string]string{"system.displayname": "api-server-01"},
        "CustomAPI",
        "endpoint-health",
        "response_time_ms",
        125.7,
    )
    
    if err != nil {
        fmt.Printf("❌ Error: %v\n", err)
    }
}
```

### cURL Example

```bash
#!/bin/bash

# Configuration
COMPANY="your-company"
ACCESS_ID="your-access-id"
ACCESS_KEY="your-access-key"
TIMESTAMP=$(date +%s)000  # Milliseconds

# Payload
PAYLOAD='{
  "resourceName": "test-server",
  "resourceIds": {
    "system.displayname": "test-server"
  },
  "dataSource": "TestMetrics",
  "instances": [{
    "instanceName": "test-instance",
    "dataPoints": [{
      "dataPointName": "test_metric",
      "dataPointType": "GAUGE",
      "values": {
        "'$((TIMESTAMP/1000))'": "42.5"
      }
    }]
  }]
}'

# Generate signature
RESOURCE_PATH="/v2/metric/ingest"
REQUEST_VARS="POST${TIMESTAMP}${PAYLOAD}${RESOURCE_PATH}"
SIGNATURE=$(echo -n "$REQUEST_VARS" | openssl dgst -sha256 -hmac "$ACCESS_KEY" -binary | base64)
AUTH_HEADER="LMv1 ${ACCESS_ID}:${SIGNATURE}:${TIMESTAMP}"

# Send request
curl -X POST \
  "https://${COMPANY}.logicmonitor.com/rest/v2/metric/ingest?create=true" \
  -H "Content-Type: application/json" \
  -H "Authorization: ${AUTH_HEADER}" \
  -d "$PAYLOAD"
```

## Best Practices

### 1. Resource Identification
- Always include `system.displayname` in `resourceIds`
- Use consistent naming conventions across your infrastructure
- Include additional identifiers like IP addresses or hostnames when available

### 2. DataSource Organization
- Use descriptive but concise `dataSource` names
- Group related metrics under the same data source
- Use `dataSourceGroup` to organize data sources logically

### 3. Metric Naming
- Use snake_case for metric names (e.g., `response_time_ms`, `cpu_usage_percent`)
- Include units in the name when helpful
- Keep names consistent across similar metrics

### 4. Performance Optimization
- Batch multiple metrics in a single request when possible
- Use appropriate aggregation types to reduce data volume
- Consider the 1-minute aggregation window when sending high-frequency data

### 5. Error Handling
- Always check response status codes
- Parse 207 Multi-Status responses to identify specific failures
- Implement retry logic with exponential backoff for transient failures
- Log detailed error information for debugging

### 6. Security
- Store API credentials securely (environment variables, secrets management)
- Use HTTPS for all API calls
- Rotate API keys regularly
- Implement proper access controls

## Rate Limits and Constraints

- **Request Rate**: 1000 requests per minute per portal
- **Payload Size**: Maximum 1MB per request
- **Batch Size**: Up to 100 resources per request
- **DataPoints**: Up to 1000 data points per request
- **String Lengths**: See field reference table above

## Troubleshooting

### Common Issues

1. **Authentication Failures (401)**
   - Verify access ID and key are correct
   - Check timestamp is within 5 minutes of current time
   - Ensure signature generation matches LogicMonitor's algorithm

2. **Bad Request (400)**
   - Validate JSON payload structure
   - Check required fields are present
   - Verify field value constraints

3. **Multi-Status (207) Responses**
   - Parse the `errors` array for specific failure reasons
   - Common causes: missing required fields, invalid resource names, duplicate data points

4. **Rate Limiting (429)**
   - Implement exponential backoff
   - Reduce request frequency
   - Consider batching more metrics per request

### Debug Tips

- Use tools like Postman or curl to test API calls manually
- Enable debug logging to capture full request/response details
- Validate your authentication signature using LogicMonitor's signature verification tools
- Start with simple payloads and gradually add complexity

## Integration with OpenTelemetry

This API is used by the LogicMonitor OpenTelemetry exporter. For production deployments, consider using the exporter instead of direct API calls:

```yaml
exporters:
  logicmonitor:
    endpoint: "https://your-company.logicmonitor.com/rest"
    api_token:
      access_id: "your-access-id"
      access_key: "your-access-key"
```

## References

- [Official LogicMonitor Push Metrics API Documentation](https://www.logicmonitor.com/support/push-metrics/ingesting-metrics-with-the-push-metrics-rest-api)
- [LogicMonitor REST API Authentication](https://www.logicmonitor.com/support/rest-api-developers-guide/overview/using-logicmonitors-rest-api)
- [OpenTelemetry LogicMonitor Exporter](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/exporter/logicmonitorexporter)
