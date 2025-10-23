// **** This is an updated sender.go file by the LogicMonitor CTA Team (Ryan) ****
// This updated script will use batching architecture to collect all datapoints for the resource first and send them all with the same instance name
// Date: 10/22/2025
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

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
		lmsdkmetrics.WithMetricBatchingDisabled(),
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

// DataPointBatch holds datapoints to be sent together
type DataPointBatch struct {
	datapoints []model.DataPointInput
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

		// Batch all datapoints for this resource
		datapointBatch := &DataPointBatch{
			datapoints: make([]model.DataPointInput, 0),
		}

		scopeMetrics := resourceMetric.ScopeMetrics()
		for j := 0; j < scopeMetrics.Len(); j++ {
			scopeMetric := scopeMetrics.At(j)
			metrics := scopeMetric.Metrics()

			for k := 0; k < metrics.Len(); k++ {
				metric := metrics.At(k)

				// Collect datapoints instead of sending immediately
				switch metric.Type() {
				case pmetric.MetricTypeGauge:
					s.collectGauge(datapointBatch, metric)
				case pmetric.MetricTypeSum:
					s.collectSum(datapointBatch, metric)
				case pmetric.MetricTypeHistogram:
					s.collectHistogram(datapointBatch, metric)
				case pmetric.MetricTypeSummary:
					s.collectSummary(datapointBatch, metric)
				default:
					s.logger.Warn("Unsupported metric type", zap.String("type", metric.Type().String()))
					continue
				}
			}
		}

		// Send all datapoints for this resource in one call
		if len(datapointBatch.datapoints) > 0 {
			err := s.sendBatch(ctx, resourceName, resourceID, resourceProps, datapointBatch)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Sender) collectGauge(batch *DataPointBatch, metric pmetric.Metric) {
	gauge := metric.Gauge()
	dataPoints := gauge.DataPoints()

	for i := 0; i < dataPoints.Len(); i++ {
		dp := dataPoints.At(i)
		batch.datapoints = append(batch.datapoints, s.createDataPointInput(
			metric.Name(),
			dp.DoubleValue(),
			dp.Timestamp(),
			"GAUGE",
		))
	}
}

func (s *Sender) collectSum(batch *DataPointBatch, metric pmetric.Metric) {
	sum := metric.Sum()
	dataPoints := sum.DataPoints()

	metricType := "COUNTER"
	if !sum.IsMonotonic() {
		metricType = "GAUGE"
	}

	for i := 0; i < dataPoints.Len(); i++ {
		dp := dataPoints.At(i)
		batch.datapoints = append(batch.datapoints, s.createDataPointInput(
			metric.Name(),
			dp.DoubleValue(),
			dp.Timestamp(),
			metricType,
		))
	}
}

func (s *Sender) collectHistogram(batch *DataPointBatch, metric pmetric.Metric) {
	histogram := metric.Histogram()
	dataPoints := histogram.DataPoints()

	for i := 0; i < dataPoints.Len(); i++ {
		dp := dataPoints.At(i)

		// Collect count
		batch.datapoints = append(batch.datapoints, s.createDataPointInput(
			metric.Name()+"_count",
			float64(dp.Count()),
			dp.Timestamp(),
			"COUNTER",
		))

		// Collect sum if available
		if dp.HasSum() {
			batch.datapoints = append(batch.datapoints, s.createDataPointInput(
				metric.Name()+"_sum",
				dp.Sum(),
				dp.Timestamp(),
				"COUNTER",
			))
		}
	}
}

func (s *Sender) collectSummary(batch *DataPointBatch, metric pmetric.Metric) {
	summary := metric.Summary()
	dataPoints := summary.DataPoints()

	for i := 0; i < dataPoints.Len(); i++ {
		dp := dataPoints.At(i)

		// Collect count
		batch.datapoints = append(batch.datapoints, s.createDataPointInput(
			metric.Name()+"_count",
			float64(dp.Count()),
			dp.Timestamp(),
			"COUNTER",
		))

		// Collect sum
		batch.datapoints = append(batch.datapoints, s.createDataPointInput(
			metric.Name()+"_sum",
			dp.Sum(),
			dp.Timestamp(),
			"COUNTER",
		))
	}
}

func (s *Sender) createDataPointInput(metricName string, value float64, timestamp pcommon.Timestamp, metricType string) model.DataPointInput {
	timestampStr := strconv.FormatInt(timestamp.AsTime().Unix(), 10)
	return model.DataPointInput{
		DataPointName:            sanitizeName(metricName),
		DataPointType:            metricType,
		DataPointAggregationType: "none",
		Value:                    map[string]string{timestampStr: strconv.FormatFloat(value, 'f', -1, 64)},
	}
}

func (s *Sender) sendBatch(ctx context.Context, resourceName string, resourceID map[string]string, resourceProps map[string]string, batch *DataPointBatch) error {
	// Merge resourceProps with resourceID
	mergedResourceID := make(map[string]string)
	for k, v := range resourceID {
		mergedResourceID[k] = v
	}
	for k, v := range resourceProps {
		mergedResourceID[k] = v
	}

	// Create resource input
	rInput := model.ResourceInput{
		ResourceName: resourceName,
		ResourceID:   mergedResourceID,
		IsCreate:     true,
	}

	// Create datasource input
	dsInput := model.DatasourceInput{
		DataSourceName:        "PushMetrics",
		DataSourceDisplayName: "PushMetrics",
		DataSourceGroup:       "PushMetrics",
	}

	// Use a single instance name for all datapoints
	// Use "metrics" as the instance name (could also use datasource name or "self")
	instInput := model.InstanceInput{
		InstanceName:       "metrics",
		InstanceProperties: make(map[string]string),
	}

	// Send all datapoints together
	// Note: The SDK's SendMetrics appears to only accept one datapoint at a time
	// So we need to call it multiple times but with the SAME instance name
	for _, dpInput := range batch.datapoints {
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
					"instanceDisplayName": instInput.InstanceName,
					"instanceProperties":  instInput.InstanceProperties,
					"dataPoints": []map[string]interface{}{
						{
							"dataPointName":            dpInput.DataPointName,
							"dataPointType":            dpInput.DataPointType,
							"dataPointAggregationType": "none",
							"values":                   dpInput.Value,
						},
					},
				},
			},
		}

		s.logger.Debug("Sending metric data",
			zap.String("instance", instInput.InstanceName),
			zap.String("datapoint", dpInput.DataPointName),
			zap.Any("body", completePayload))

		// Send to LogicMonitor
		ingestResponse, err := s.metricIngestClient.SendMetrics(ctx, rInput, dsInput, instInput, dpInput)
		if err != nil {
			return s.handleError(ingestResponse, err)
		}
	}

	s.logger.Info("Successfully sent metric batch",
		zap.String("resource", resourceName),
		zap.String("instance", instInput.InstanceName),
		zap.Int("datapoint_count", len(batch.datapoints)))

	return nil
}

// sanitizeName cleans metric and instance names to comply with LogicMonitor naming rules
func sanitizeName(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_]`)
	sanitized := re.ReplaceAllString(name, "_")
	sanitized = strings.ReplaceAll(sanitized, "-", "_")

	if len(sanitized) > 0 && sanitized[0] >= '0' && sanitized[0] <= '9' {
		sanitized = "metric_" + sanitized
	}

	if len(sanitized) == 0 {
		sanitized = "unknown_metric"
	}

	return sanitized
}

func (s *Sender) handleError(ingestResponse *lmsdkmetrics.SendMetricResponse, err error) error {
	if ingestResponse != nil {
		if ingestResponse.StatusCode == http.StatusMultiStatus {
			s.logger.Error("Multi-Status response received from LogicMonitor API",
				zap.Int("status_code", ingestResponse.StatusCode),
				zap.String("response_message", ingestResponse.Message),
				zap.Int("total_errors", len(ingestResponse.MultiStatus)))

			var permanentErrors []string
			var temporaryErrors []string

			for i, status := range ingestResponse.MultiStatus {
				errorCode := int(status.Code)

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

			s.logger.Warn("All errors in Multi-Status response are temporary (will be retried)",
				zap.Int("temporary_error_count", len(temporaryErrors)),
				zap.Strings("temporary_errors", temporaryErrors))

			return fmt.Errorf("temporary failures detected: %v", temporaryErrors)
		}

		if isPermanentClientFailure(ingestResponse.StatusCode) {
			s.logger.Error("Permanent client failure",
				zap.Int("status_code", ingestResponse.StatusCode),
				zap.String("response_message", ingestResponse.Message),
				zap.Error(ingestResponse.Error))
			return consumererror.NewPermanent(ingestResponse.Error)
		}

		s.logger.Warn("Temporary error from LogicMonitor API",
			zap.Int("status_code", ingestResponse.StatusCode),
			zap.String("response_message", ingestResponse.Message),
			zap.Error(ingestResponse.Error))
		return ingestResponse.Error
	}

	if err != nil && strings.Contains(err.Error(), "validation failed") {
		s.logger.Error("Validation error detected", zap.Error(err))
		return consumererror.NewPermanent(err)
	}

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