// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"
)

const (
	metricsIngestPath = "/metric/ingest"
)

// MetricsClient is a client for sending metrics to LogicMonitor Push Metrics API
type MetricsClient struct {
	endpoint  string
	accessID  string
	accessKey string
	client    *http.Client
	logger    *zap.Logger
}

// MetricPayload represents the metric data to be sent to LogicMonitor
type MetricPayload struct {
	ResourceName            string                       `json:"resourceName"`
	ResourceIDs             map[string]string            `json:"resourceIds"`
	DataSource              string                       `json:"dataSource,omitempty"`
	DataSourceDisplayName   string                       `json:"dataSourceDisplayName,omitempty"`
	DataSourceGroup         string                       `json:"dataSourceGroup,omitempty"`
	Instances               []MetricInstance             `json:"instances"`
}

// MetricInstance represents an instance within a datasource
type MetricInstance struct {
	InstanceName           string            `json:"instanceName"`
	InstanceDisplayName    string            `json:"instanceDisplayName,omitempty"`
	InstanceProperties     map[string]string `json:"instanceProperties,omitempty"`
	DataPoints             []MetricDataPoint `json:"dataPoints"`
}

// MetricDataPoint represents a single datapoint
type MetricDataPoint struct {
	DataPointName            string            `json:"dataPointName"`
	DataPointDescription     string            `json:"dataPointDescription,omitempty"`
	DataPointType            string            `json:"dataPointType,omitempty"`
	DataPointAggregationType string            `json:"dataPointAggregationType,omitempty"`
	PercentileValue          int               `json:"percentileValue,omitempty"`
	Values                   map[string]interface{} `json:"values"`
}

// MetricResponse represents the response from LogicMonitor API
type MetricResponse struct {
	Success bool               `json:"success"`
	Message string             `json:"message"`
	Errors  []MetricErrorDetail `json:"errors,omitempty"`
}

// MetricErrorDetail represents error details in the response
type MetricErrorDetail struct {
	Code        int               `json:"code,omitempty"`
	Message     string            `json:"message"`
	ResourceIDs map[string]string `json:"resourceIds,omitempty"`
}

// NewMetricsClient creates a new metrics client
func NewMetricsClient(endpoint, accessID, accessKey string, httpClient *http.Client, logger *zap.Logger) *MetricsClient {
	return &MetricsClient{
		endpoint:  endpoint,
		accessID:  accessID,
		accessKey: accessKey,
		client:    httpClient,
		logger:    logger,
	}
}

// SendMetrics sends metric payload to LogicMonitor
func (c *MetricsClient) SendMetrics(ctx context.Context, payload *MetricPayload) (*MetricResponse, error) {
	// Marshal payload to JSON
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create HTTP request
	url := c.endpoint + metricsIngestPath + "?create=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Generate authentication signature
	timestamp := time.Now().UnixMilli()
	auth := c.generateAuth(http.MethodPost, metricsIngestPath, string(body), timestamp)

	// Debug logging for authentication
	c.logger.Debug("LogicMonitor API Request",
		zap.String("url", url),
		zap.String("method", "POST"),
		zap.Int("body_length", len(body)),
		zap.String("body_payload", string(body)),
		zap.Int64("timestamp", timestamp),
		zap.String("access_id", c.accessID),
		zap.String("auth_header", auth))

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", auth)

	// Send request
	resp, err := c.client.Do(req)
	if err != nil {
		c.logger.Error("Failed to send HTTP request",
			zap.Error(err),
			zap.String("url", url))
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Log response details
	c.logger.Debug("LogicMonitor API Response",
		zap.Int("status_code", resp.StatusCode),
		zap.String("status", resp.Status),
		zap.Int("body_length", len(respBody)),
		zap.String("response_body", string(respBody)))

	// Parse response
	var metricResp MetricResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &metricResp); err != nil {
			c.logger.Error("Failed to parse response",
				zap.Error(err),
				zap.Int("status_code", resp.StatusCode),
				zap.String("response_body", string(respBody)))
			return nil, fmt.Errorf("failed to parse response (status %d): %w, body: %s", resp.StatusCode, err, string(respBody))
		}
	}

	// Check for errors
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		if metricResp.Message == "" {
			metricResp.Message = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		}
		metricResp.Success = false
		
		c.logger.Error("LogicMonitor API returned error",
			zap.Int("status_code", resp.StatusCode),
			zap.String("message", metricResp.Message),
			zap.Any("errors", metricResp.Errors))
		
		return &metricResp, fmt.Errorf("API returned status %d: %s", resp.StatusCode, metricResp.Message)
	}

	metricResp.Success = true
	return &metricResp, nil
}

// generateAuth generates LMv1 authentication signature
// Format: LMv1 <AccessId>:<Signature>:<Timestamp>
// Signature = Base64(HMAC-SHA256(Method + Timestamp + Body + Path, AccessKey))
func (c *MetricsClient) generateAuth(method, path, body string, timestamp int64) string {
	// Create string to sign: Method + Timestamp + Body + Path
	stringToSign := method + strconv.FormatInt(timestamp, 10) + body + path

	// Debug log the string to sign components
	c.logger.Debug("Generating LMv1 signature",
		zap.String("method", method),
		zap.String("path", path),
		zap.Int64("timestamp", timestamp),
		zap.Int("body_length", len(body)),
		zap.String("body", body),
		zap.String("access_id", c.accessID),
		zap.Int("access_key_length", len(c.accessKey)),
		zap.String("string_to_sign", stringToSign),
		zap.Int("string_to_sign_length", len(stringToSign)))

	// Generate HMAC SHA256 signature and base64 encode it
	h := hmac.New(sha256.New, []byte(c.accessKey))
	h.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	c.logger.Debug("Generated signature",
		zap.String("signature", signature),
		zap.Int("signature_length", len(signature)))

	// Return formatted auth header
	return fmt.Sprintf("LMv1 %s:%s:%d", c.accessID, signature, timestamp)
}

