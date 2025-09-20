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

	lmsdkmetrics "github.com/logicmonitor/lm-data-sdk-go/api/metrics"
	"github.com/logicmonitor/lm-data-sdk-go/model"
	"github.com/logicmonitor/lm-data-sdk-go/utils"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
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
				
				// Create datasource input
				dsInput := model.DatasourceInput{
					DataSourceName:        "OpenTelemetry",
					DataSourceDisplayName: "OpenTelemetry Collector",
					DataSourceGroup:       "Telemetry",
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
	// Create resource input
	rInput := model.ResourceInput{
		ResourceName:       resourceName,
		ResourceID:         resourceID,
		ResourceProperties: resourceProps,
	}
	
	// Create instance input from attributes
	instanceName := getInstanceName(attributes)
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
	
	// Send to LogicMonitor
	ingestResponse, err := s.metricIngestClient.SendMetrics(ctx, rInput, dsInput, instInput, dpInput)
	if err != nil {
		return s.handleError(ingestResponse, err)
	}
	
	return nil
}

// sanitizeName cleans metric and instance names to comply with LogicMonitor naming rules
func sanitizeName(name string) string {
	// LogicMonitor datapoint names cannot contain hyphens and some special characters
	// Replace hyphens with underscores and remove other invalid characters
	re := regexp.MustCompile(`[^a-zA-Z0-9_.]`)
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
			for _, status := range ingestResponse.MultiStatus {
				if isPermanentClientFailure(int(status.Code)) {
					return consumererror.NewPermanent(fmt.Errorf("permanent failure error %s, complete error log %w", status.Error, ingestResponse.Error))
				}
			}
		}
		if isPermanentClientFailure(ingestResponse.StatusCode) {
			return consumererror.NewPermanent(ingestResponse.Error)
		}
		return ingestResponse.Error
	}
	
	// Check if this is a validation error (usually not permanent if it's due to data format issues)
	if err != nil && strings.Contains(err.Error(), "validation failed") {
		return consumererror.NewPermanent(err)
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

func getInstanceName(attrs pcommon.Map) string {
	if name, exists := attrs.Get("instance"); exists {
		return name.Str()
	}
	if name, exists := attrs.Get("endpoint"); exists {
		return name.Str()
	}
	return "default"
}

func convertAttributes(attrs pcommon.Map) map[string]string {
	result := make(map[string]string)
	attrs.Range(func(k string, v pcommon.Value) bool {
		result[k] = v.AsString()
		return true
	})
	return result
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
