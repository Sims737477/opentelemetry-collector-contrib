// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package logicmonitorexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/logicmonitorexporter"

import (
	"context"
	"fmt"

	"github.com/logicmonitor/lm-data-sdk-go/utils"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/pdata/pmetric"

	metrics "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/logicmonitorexporter/internal/metrics"
	"go.uber.org/zap"
)

type metricsExporter struct {
	config   *Config
	sender   *metrics.Sender
	settings component.TelemetrySettings
	cancel   context.CancelFunc
}

// newMetricsExporter creates new Logicmonitor Metrics Exporter.
func newMetricsExporter(_ context.Context, cfg component.Config, set exporter.Settings) *metricsExporter {
	oCfg := cfg.(*Config)

	// client construction is deferred to start
	return &metricsExporter{
		config:   oCfg,
		settings: set.TelemetrySettings,
	}
}

func (e *metricsExporter) start(ctx context.Context, host component.Host) error {
	client, err := e.config.ToClient(ctx, host, e.settings)
	if err != nil {
		return fmt.Errorf("failed to create http client: %w", err)
	}

	authParams := utils.AuthParams{
		AccessID:    e.config.APIToken.AccessID,
		AccessKey:   string(e.config.APIToken.AccessKey),
		BearerToken: string(e.config.Headers["Authorization"]),
	}

	ctx, e.cancel = context.WithCancel(ctx)
	e.sender, err = metrics.NewSender(ctx, e.config.Endpoint, client, authParams, e.settings.Logger)
	if err != nil {
		return err
	}
	return nil
}

func (e *metricsExporter) PushMetricData(ctx context.Context, md pmetric.Metrics) error {
	// Log detailed metric payload for debugging
	resourceMetrics := md.ResourceMetrics()
	for i := 0; i < resourceMetrics.Len(); i++ {
		resourceMetric := resourceMetrics.At(i)
		resource := resourceMetric.Resource()
		
		// Extract resource information
		resourceMap := make(map[string]any)
		for key, value := range resource.Attributes().All() {
			resourceMap[key] = value.AsRaw()
		}
		
		scopeMetrics := resourceMetric.ScopeMetrics()
		for j := 0; j < scopeMetrics.Len(); j++ {
			scopeMetric := scopeMetrics.At(j)
			metrics := scopeMetric.Metrics()
			
			for k := 0; k < metrics.Len(); k++ {
				metric := metrics.At(k)
				
				// Log each metric with its details
				e.settings.Logger.Debug("Sending metric data",
					zap.String("metric_name", metric.Name()),
					zap.String("metric_type", metric.Type().String()),
					zap.String("metric_unit", metric.Unit()),
					zap.String("metric_description", metric.Description()),
					zap.Any("resource_attributes", resourceMap),
					zap.Int("data_point_count", getDataPointCount(metric)))
			}
		}
	}
	
	return e.sender.SendMetrics(ctx, md)
}

// Helper function to get data point count for different metric types
func getDataPointCount(metric pmetric.Metric) int {
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

func (e *metricsExporter) shutdown(_ context.Context) error {
	if e.cancel != nil {
		e.cancel()
	}

	return nil
}
