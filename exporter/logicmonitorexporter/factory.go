// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:generate mdatagen metadata.yaml

package logicmonitorexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/logicmonitorexporter"

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"

	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/logicmonitorexporter/internal/metadata"
)

// NewFactory creates a LogicMonitor exporter factory
func NewFactory() exporter.Factory {
	return exporter.NewFactory(
		metadata.Type,
		createDefaultConfig,
		exporter.WithLogs(createLogsExporter, metadata.LogsStability),
		exporter.WithTraces(createTracesExporter, metadata.TracesStability),
		exporter.WithMetrics(createMetricsExporter, metadata.MetricsStability),
	)
}

func createDefaultConfig() component.Config {
	// Configure default queue settings to respect LogicMonitor rate limits:
	// https://www.logicmonitor.com/support/push-metrics/rate-limiting-for-push-metrics
	// - Maximum 10,000 requests per minute (~167 requests/second)
	// - Maximum 100 instances per payload
	// - Maximum payload size: 1MB uncompressed, ~102KB compressed
	queueSettings := exporterhelper.NewDefaultQueueConfig()
	queueSettings.QueueSize = 10000 // Match the per-minute request limit
	
	return &Config{
		BackOffConfig: configretry.NewDefaultBackOffConfig(),
		QueueSettings: queueSettings,
	}
}

func createLogsExporter(ctx context.Context, set exporter.Settings, cfg component.Config) (exporter.Logs, error) {
	lmLogExp := newLogsExporter(ctx, cfg, set)
	c := cfg.(*Config)

	return exporterhelper.NewLogs(
		ctx,
		set,
		cfg,
		lmLogExp.PushLogData,
		exporterhelper.WithStart(lmLogExp.start),
		exporterhelper.WithQueue(c.QueueSettings),
		exporterhelper.WithRetry(c.BackOffConfig),
		exporterhelper.WithShutdown(lmLogExp.shutdown),
	)
}

func createTracesExporter(ctx context.Context, set exporter.Settings, cfg component.Config) (exporter.Traces, error) {
	lmTraceExp := newTracesExporter(ctx, cfg, set)
	c := cfg.(*Config)

	return exporterhelper.NewTraces(
		ctx,
		set,
		cfg,
		lmTraceExp.PushTraceData,
		exporterhelper.WithCapabilities(consumer.Capabilities{MutatesData: false}),
		exporterhelper.WithStart(lmTraceExp.start),
		exporterhelper.WithRetry(c.BackOffConfig),
		exporterhelper.WithQueue(c.QueueSettings),
		exporterhelper.WithShutdown(lmTraceExp.shutdown),
	)
}

func createMetricsExporter(ctx context.Context, set exporter.Settings, cfg component.Config) (exporter.Metrics, error) {
	lmMetricExp := newMetricsExporter(ctx, cfg, set)
	c := cfg.(*Config)

	return exporterhelper.NewMetrics(
		ctx,
		set,
		cfg,
		lmMetricExp.PushMetricData,
		exporterhelper.WithCapabilities(consumer.Capabilities{MutatesData: false}),
		exporterhelper.WithStart(lmMetricExp.start),
		exporterhelper.WithRetry(c.BackOffConfig),
		exporterhelper.WithQueue(c.QueueSettings),
		exporterhelper.WithShutdown(lmMetricExp.shutdown),
	)
}
