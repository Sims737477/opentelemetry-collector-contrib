// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package logicmonitorexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/logicmonitorexporter"

import (
	"context"
	"fmt"
	"time"

	"github.com/logicmonitor/lm-data-sdk-go/utils"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/pdata/pmetric"

	metrics "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/logicmonitorexporter/internal/metrics"
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

	// Configure batching parameters with defaults
	batchingConfig := metrics.BatchingConfig{
		Interval:  200 * time.Millisecond, // Default 200ms
		RateLimit: 100,                     // Default 100 requests per second
	}

	// Override with user configuration if provided
	if e.config.Metrics.BatchingInterval > 0 {
		batchingConfig.Interval = e.config.Metrics.BatchingInterval
	}
	if e.config.Metrics.BatchingRateLimit > 0 {
		batchingConfig.RateLimit = e.config.Metrics.BatchingRateLimit
	}

	ctx, e.cancel = context.WithCancel(ctx)
	e.sender, err = metrics.NewSender(ctx, e.config.Endpoint, client, authParams, batchingConfig, e.settings.Logger)
	if err != nil {
		return err
	}
	return nil
}

func (e *metricsExporter) PushMetricData(ctx context.Context, md pmetric.Metrics) error {
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
