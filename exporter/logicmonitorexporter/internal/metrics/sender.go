// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package metrics // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/logicmonitorexporter/internal/metrics"

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	lmsdkmetrics "github.com/logicmonitor/lm-data-sdk-go/api/metrics"
	"github.com/logicmonitor/lm-data-sdk-go/model"
	"github.com/logicmonitor/lm-data-sdk-go/utils"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"

	lmutils "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/logicmonitorexporter/internal/utils"
)

type Sender struct {
	logger             *zap.Logger
	metricIngestClient *lmsdkmetrics.LMMetricIngest
}

// NewSender creates a new Sender
func NewSender(ctx context.Context, endpoint string, client *http.Client, authParams utils.AuthParams, logger *zap.Logger) (*Sender, error) {
	options := []lmsdkmetrics.Option{
		//lmsdkmetrics.WithMetricBatchingDisabled(),
		lmsdkmetrics.WithMetricBatchingInterval(1 * time.Second),
		lmsdkmetrics.WithRateLimit(200),
		lmsdkmetrics.WithAuthentication(authParams),
		lmsdkmetrics.WithHTTPClient(client),
		lmsdkmetrics.WithEndpoint(endpoint),
	}

	metricIngestClient, err := lmsdkmetrics.NewLMMetricIngest(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create metricIngestClient: %w", err)
	}
	return &Sender{
		logger:             logger,
		metricIngestClient: metricIngestClient,
	}, nil
}

func (s *Sender) SendMetrics(ctx context.Context, md pmetric.Metrics) error {
	// Convert OpenTelemetry metrics to LogicMonitor format
	resourceMetrics := md.ResourceMetrics()

	for i := 0; i < resourceMetrics.Len(); i++ {
		resourceMetric := resourceMetrics.At(i)
		resource := resourceMetric.Resource()

		// Extract resource information
		resourceName := getResourceName(resource.Attributes())
		resourceID := getResourceID(resource.Attributes())
		resourceProps := convertAttributes(resource.Attributes())

		scopeMetrics := resourceMetric.ScopeMetrics()
		for j := 0; j < scopeMetrics.Len(); j++ {
			scopeMetric := scopeMetrics.At(j)
			metrics := scopeMetric.Metrics()

			for k := 0; k < metrics.Len(); k++ {
				metric := metrics.At(k)

				// Create datasource input with custom DataSourceName based on metric name
				dataSourceName := generateDataSourceName(metric.Name())
				dsInput := model.DatasourceInput{
					DataSourceName: dataSourceName,
				}

				// Process different metric types
				var err error
				switch metric.Type() {
				case pmetric.MetricTypeGauge:
					err = s.processGauge(ctx, resourceName, resourceID, resourceProps, dsInput, metric)
				case pmetric.MetricTypeSum:
					err = s.processSum(ctx, resourceName, resourceID, resourceProps, dsInput, metric)
				case pmetric.MetricTypeHistogram:
					err = s.processHistogram(ctx, resourceName, resourceID, resourceProps, dsInput, metric)
				case pmetric.MetricTypeSummary:
					err = s.processSummary(ctx, resourceName, resourceID, resourceProps, dsInput, metric)
				default:
					s.logger.Warn("Unsupported metric type", zap.String("type", metric.Type().String()))
					continue
				}

				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (s *Sender) processGauge(ctx context.Context, resourceName string, resourceID map[string]string, resourceProps map[string]string, dsInput model.DatasourceInput, metric pmetric.Metric) error {
	gauge := metric.Gauge()
	dataPoints := gauge.DataPoints()

	for i := 0; i < dataPoints.Len(); i++ {
		dp := dataPoints.At(i)
		err := s.sendDataPoint(ctx, resourceName, resourceID, resourceProps, dsInput, metric.Name(), dp.DoubleValue(), dp.Timestamp(), dp.Attributes(), "GAUGE")
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Sender) processSum(ctx context.Context, resourceName string, resourceID map[string]string, resourceProps map[string]string, dsInput model.DatasourceInput, metric pmetric.Metric) error {
	sum := metric.Sum()
	dataPoints := sum.DataPoints()

	metricType := "COUNTER"
	if !sum.IsMonotonic() {
		metricType = "GAUGE"
	}

	for i := 0; i < dataPoints.Len(); i++ {
		dp := dataPoints.At(i)
		err := s.sendDataPoint(ctx, resourceName, resourceID, resourceProps, dsInput, metric.Name(), dp.DoubleValue(), dp.Timestamp(), dp.Attributes(), metricType)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Sender) processHistogram(ctx context.Context, resourceName string, resourceID map[string]string, resourceProps map[string]string, dsInput model.DatasourceInput, metric pmetric.Metric) error {
	histogram := metric.Histogram()
	dataPoints := histogram.DataPoints()

	for i := 0; i < dataPoints.Len(); i++ {
		dp := dataPoints.At(i)
		// Send count
		err := s.sendDataPoint(ctx, resourceName, resourceID, resourceProps, dsInput, metric.Name()+"_count", float64(dp.Count()), dp.Timestamp(), dp.Attributes(), "COUNTER")
		if err != nil {
			return err
		}

		// Send sum if available
		if dp.HasSum() {
			err = s.sendDataPoint(ctx, resourceName, resourceID, resourceProps, dsInput, metric.Name()+"_sum", dp.Sum(), dp.Timestamp(), dp.Attributes(), "COUNTER")
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Sender) processSummary(ctx context.Context, resourceName string, resourceID map[string]string, resourceProps map[string]string, dsInput model.DatasourceInput, metric pmetric.Metric) error {
	summary := metric.Summary()
	dataPoints := summary.DataPoints()

	for i := 0; i < dataPoints.Len(); i++ {
		dp := dataPoints.At(i)
		// Send count
		err := s.sendDataPoint(ctx, resourceName, resourceID, resourceProps, dsInput, metric.Name()+"_count", float64(dp.Count()), dp.Timestamp(), dp.Attributes(), "COUNTER")
		if err != nil {
			return err
		}

		// Send sum
		err = s.sendDataPoint(ctx, resourceName, resourceID, resourceProps, dsInput, metric.Name()+"_sum", dp.Sum(), dp.Timestamp(), dp.Attributes(), "COUNTER")
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Sender) sendDataPoint(ctx context.Context, resourceName string, resourceID map[string]string, resourceProps map[string]string, dsInput model.DatasourceInput, metricName string, value float64, timestamp pcommon.Timestamp, attributes pcommon.Map, metricType string) error {
	// Merge resourceProps with resourceID
	mergedResourceID := make(map[string]string)
	// First copy resourceID
	for k, v := range resourceID {
		mergedResourceID[k] = v
	}
	// Then merge resourceProps (will overwrite any duplicate keys)
	for k, v := range resourceProps {
		mergedResourceID[k] = v
	}

	// Create resource input
	rInput := model.ResourceInput{
		ResourceName: resourceName,
		ResourceID:   mergedResourceID,
		IsCreate:     true,
	}

	// Create instance input from attributes
	instanceName := metricName // Use metric name as instance name
	instInput := model.InstanceInput{
		InstanceName:       sanitizeName(instanceName),
		InstanceProperties: convertAttributes(attributes),
	}

	// Create datapoint input with sanitized name
	timestampStr := strconv.FormatInt(timestamp.AsTime().Unix(), 10)
	dpInput := model.DataPointInput{
		DataPointName:            sanitizeName(metricName),
		DataPointType:            metricType,
		DataPointAggregationType: "none",
		Value:                    map[string]string{timestampStr: strconv.FormatFloat(value, 'f', -1, 64)},
	}

	// Log the complete LogicMonitor API payload structure
	completePayload := map[string]interface{}{
		"resourceName":          resourceName,
		"resourceIds":           mergedResourceID,
		"dataSource":            dsInput.DataSourceName,
		"dataSourceDisplayName": dsInput.DataSourceDisplayName,
		"dataSourceGroup":       dsInput.DataSourceGroup,
		"instances": []map[string]interface{}{
			{
				"instanceName":        instInput.InstanceName,
				"instanceProperties":  instInput.InstanceProperties,
				"dataPoints": []map[string]interface{}{
					{
						"dataPointName":            dpInput.DataPointName,
						"dataPointType":            dpInput.DataPointType,
						"dataPointAggregationType": "none",
						"values":                   map[string]interface{}{timestampStr: value},
					},
				},
			},
		},
	}

	s.logger.Debug("Sending metric data",
		zap.Any("body", completePayload))

	// Send to LogicMonitor
	ingestResponse, err := s.metricIngestClient.SendMetrics(ctx, rInput, dsInput, instInput, dpInput)
	if err != nil {
		return s.handleError(ingestResponse, err)
	}

	return nil
}

// generateDataSourceName creates a DataSourceName from metric name that complies with LogicMonitor spec:
// - 64-character limit
// - Must be unique
// - All characters except , ; / * [ ] ? ' " ` ## and newline are allowed
// - Spaces allowed except at start or end
// - Hyphen allowed only at the end; hyphen must be used with at least one other character
// Uses first part of metric name (delimiter is _ or .) and makes first letter uppercase
// For short metric names (e.g. "up"), returns "Ungroupped"
func generateDataSourceName(metricName string) string {
	if metricName == "" {
		return "Ungroupped"
	}

	// Find the first occurrence of either _ or . (not - as it has special rules)
	firstUnderscore := strings.Index(metricName, "_")
	firstDot := strings.Index(metricName, ".")

	var delimiterPos int = -1

	// Find the first delimiter (excluding hyphen for now)
	if firstUnderscore != -1 {
		delimiterPos = firstUnderscore
	}
	if firstDot != -1 && (delimiterPos == -1 || firstDot < delimiterPos) {
		delimiterPos = firstDot
	}

	var firstPart string
	if delimiterPos == -1 {
		// No delimiter found, use the whole name if it's long enough
		// For metrics without delimiters, only very short names (<=2 chars) should be ungroupped
		if len(metricName) <= 2 {
			return "Ungroupped"
		}
		firstPart = metricName
	} else {
		// Extract the first part
		firstPart = metricName[:delimiterPos]

		// If the first part is too short, return "Ungroupped"
		if len(firstPart) < 2 {
			return "Ungroupped"
		}
	}

	// Clean the first part according to LogicMonitor spec
	cleaned := cleanDataSourceName(firstPart)

	// Trim spaces before further processing
	cleaned = strings.TrimSpace(cleaned)

	// If cleaning resulted in empty or very short string, return "Ungroupped"
	if len(cleaned) < 2 {
		return "Ungroupped"
	}

	// Make first letter uppercase, rest lowercase
	result := strings.ToUpper(string(cleaned[0])) + strings.ToLower(cleaned[1:])

	// Ensure 64-character limit
	if len(result) > 64 {
		result = result[:64]
	}

	// Final validation and cleanup
	return finalizeDataSourceName(result)
}

// cleanDataSourceName removes invalid characters according to LogicMonitor spec
// Invalid characters: , ; / * [ ] ? ' " ` ## and newline
func cleanDataSourceName(input string) string {
	var result strings.Builder
	result.Grow(len(input))

	for _, r := range input {
		// Check if character is invalid
		if isInvalidDataSourceChar(r) {
			continue // Skip invalid characters
		}
		result.WriteRune(r)
	}

	return result.String()
}

// isInvalidDataSourceChar checks if a character is invalid for DataSourceName
func isInvalidDataSourceChar(r rune) bool {
	// Invalid characters: , ; / * [ ] ? ' " ` ## and newline
	switch r {
	case ',', ';', '/', '*', '[', ']', '?', '\'', '"', '`', '#', '\n', '\r':
		return true
	default:
		return false
	}
}

// finalizeDataSourceName applies final rules and cleanup
func finalizeDataSourceName(input string) string {
	if input == "" {
		return "Ungroupped"
	}

	// Trim spaces from start and end
	result := strings.TrimSpace(input)

	// Handle hyphen rules: hyphen allowed only at the end and must be used with at least one other character
	if strings.Contains(result, "-") {
		// Find all hyphens
		lastHyphenIndex := strings.LastIndex(result, "-")

		// If there are hyphens not at the end, remove them
		if lastHyphenIndex != len(result)-1 {
			// Remove all hyphens that are not at the end
			var cleaned strings.Builder
			cleaned.Grow(len(result))

			for i, r := range result {
				if r == '-' && i != len(result)-1 {
					continue // Skip hyphens not at the end
				}
				cleaned.WriteRune(r)
			}
			result = cleaned.String()
		}

		// If the result is just a hyphen or empty, return default
		if result == "-" || result == "" {
			return "Ungroupped"
		}

		// Ensure hyphen at end has at least one other character
		if strings.HasSuffix(result, "-") && len(result) < 2 {
			return "Ungroupped"
		}
	}

	// Final check - if result is empty or too short after all processing
	if len(result) < 1 {
		return "Ungroupped"
	}

	return result
}

// sanitizeName cleans metric and instance names to comply with LogicMonitor naming rules
func sanitizeName(name string) string {
	// LogicMonitor datapoint names cannot contain hyphens and some special characters
	// Replace hyphens with underscores and remove other invalid characters
	re := regexp.MustCompile(`[^a-zA-Z0-9_]`)
	sanitized := re.ReplaceAllString(name, "_")
	sanitized = strings.ReplaceAll(sanitized, "-", "_")

	// Ensure it doesn't start with a number
	if len(sanitized) > 0 && sanitized[0] >= '0' && sanitized[0] <= '9' {
		sanitized = "metric_" + sanitized
	}

	// Ensure minimum length
	if len(sanitized) == 0 {
		sanitized = "unknown_metric"
	}

	return sanitized
}

func (s *Sender) handleError(ingestResponse *lmsdkmetrics.SendMetricResponse, err error) error {
	if ingestResponse != nil {
		if ingestResponse.StatusCode == http.StatusMultiStatus {
			// Log detailed error information for 207 Multi-Status responses
			s.logger.Error("Multi-Status response received from LogicMonitor API",
				zap.Int("status_code", ingestResponse.StatusCode),
				zap.String("response_message", ingestResponse.Message),
				zap.Int("total_errors", len(ingestResponse.MultiStatus)))

			var permanentErrors []string
			var temporaryErrors []string

			// Log each individual error with detailed information
			for i, status := range ingestResponse.MultiStatus {
				errorCode := int(status.Code)

				// Log structured error information for better debugging
				s.logger.Error("LogicMonitor API rejected item",
					zap.Int("item_index", i),
					zap.Int("error_code", errorCode),
					zap.String("error_message", status.Error),
					zap.Bool("is_permanent", isPermanentClientFailure(errorCode)))

				errorMsg := fmt.Sprintf("Item %d: Code=%d, Error=%s", i, errorCode, status.Error)

				if isPermanentClientFailure(errorCode) {
					permanentErrors = append(permanentErrors, errorMsg)
				} else {
					temporaryErrors = append(temporaryErrors, errorMsg)
				}
			}

			// Log summary of error categorization
			if len(permanentErrors) > 0 {
				s.logger.Error("Permanent errors detected in Multi-Status response",
					zap.Int("permanent_error_count", len(permanentErrors)),
					zap.Int("temporary_error_count", len(temporaryErrors)),
					zap.Strings("permanent_errors", permanentErrors))

				if len(temporaryErrors) > 0 {
					s.logger.Warn("Temporary errors also present (will be retried separately)",
						zap.Strings("temporary_errors", temporaryErrors))
				}

				return consumererror.NewPermanent(fmt.Errorf("permanent failure errors detected: %v", permanentErrors))
			}

			// All errors are temporary, log them and return as retryable
			s.logger.Warn("All errors in Multi-Status response are temporary (will be retried)",
				zap.Int("temporary_error_count", len(temporaryErrors)),
				zap.Strings("temporary_errors", temporaryErrors))

			return fmt.Errorf("temporary failures detected: %v", temporaryErrors)
		}

		// Handle non-207 error responses
		if isPermanentClientFailure(ingestResponse.StatusCode) {
			s.logger.Error("Permanent client failure",
				zap.Int("status_code", ingestResponse.StatusCode),
				zap.String("response_message", ingestResponse.Message),
				zap.Error(ingestResponse.Error))
			return consumererror.NewPermanent(ingestResponse.Error)
		}

		// Temporary error
		s.logger.Warn("Temporary error from LogicMonitor API",
			zap.Int("status_code", ingestResponse.StatusCode),
			zap.String("response_message", ingestResponse.Message),
			zap.Error(ingestResponse.Error))
		return ingestResponse.Error
	}

	// Check if this is a validation error (usually not permanent if it's due to data format issues)
	if err != nil && strings.Contains(err.Error(), "validation failed") {
		s.logger.Error("Validation error detected", zap.Error(err))
		return consumererror.NewPermanent(err)
	}

	// Generic error
	if err != nil {
		s.logger.Error("Unexpected error during metric export", zap.Error(err))
	}

	return err
}

func getResourceName(attrs pcommon.Map) string {
	if name, exists := attrs.Get("service.name"); exists {
		return name.Str()
	}
	if name, exists := attrs.Get("host.name"); exists {
		return name.Str()
	}
	return "unknown"
}

func getResourceID(attrs pcommon.Map) map[string]string {
	resourceID := make(map[string]string)
	if name, exists := attrs.Get("service.name"); exists {
		resourceID["service.name"] = name.Str()
	}
	if name, exists := attrs.Get("host.name"); exists {
		resourceID["system.displayname"] = name.Str()
	}
	if len(resourceID) == 0 {
		resourceID["system.displayname"] = "unknown"
	}
	return resourceID
}

func convertAttributes(attrs pcommon.Map) map[string]string {
	return lmutils.ConvertAndNormalizeAttributesToStrings(attrs)
}

// Helper function to get data point count for different metric types
func getMetricDataPointCount(metric pmetric.Metric) int {
	switch metric.Type() {
	case pmetric.MetricTypeGauge:
		return metric.Gauge().DataPoints().Len()
	case pmetric.MetricTypeSum:
		return metric.Sum().DataPoints().Len()
	case pmetric.MetricTypeHistogram:
		return metric.Histogram().DataPoints().Len()
	case pmetric.MetricTypeSummary:
		return metric.Summary().DataPoints().Len()
	case pmetric.MetricTypeExponentialHistogram:
		return metric.ExponentialHistogram().DataPoints().Len()
	default:
		return 0
	}
}

// Does the 'code' indicate a permanent error
func isPermanentClientFailure(code int) bool {
	switch code {
	case http.StatusServiceUnavailable:
		return false
	case http.StatusGatewayTimeout:
		return false
	case http.StatusBadGateway:
		return false
	default:
		return true
	}
}
