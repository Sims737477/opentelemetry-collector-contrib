// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package logicmonitorexporter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	lmsdkmetrics "github.com/logicmonitor/lm-data-sdk-go/api/metrics"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/exporter/exportertest"
	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/logicmonitorexporter/internal/metadata"
)

func Test_NewMetricsExporter(t *testing.T) {
	t.Run("should create Metrics exporter", func(t *testing.T) {
		config := &Config{
			ClientConfig: confighttp.ClientConfig{
				Endpoint: "http://example.logicmonitor.com/rest",
			},
			APIToken: APIToken{AccessID: "testid", AccessKey: "testkey"},
		}
		set := exportertest.NewNopSettings(metadata.Type)
		exp := newMetricsExporter(t.Context(), config, set)
		assert.NotNil(t, exp)
	})
}

func TestPushMetricData(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := lmsdkmetrics.SendMetricResponse{
			Success: true,
			Message: "",
		}
		assert.NoError(t, json.NewEncoder(w).Encode(&response))
	}))
	defer ts.Close()

	params := exportertest.NewNopSettings(metadata.Type)
	f := NewFactory()
	config := &Config{
		ClientConfig: confighttp.ClientConfig{
			Endpoint: ts.URL,
		},
		APIToken: APIToken{AccessID: "testid", AccessKey: "testkey"},
	}
	ctx := t.Context()
	exp, err := f.CreateMetrics(ctx, params, config)
	assert.NoError(t, err)
	assert.NoError(t, exp.Start(ctx, componenttest.NewNopHost()))
	defer func() { assert.NoError(t, exp.Shutdown(ctx)) }()

	testMetrics := pmetric.NewMetrics()
	generateMetrics().CopyTo(testMetrics)
	err = exp.ConsumeMetrics(t.Context(), testMetrics)
	assert.NoError(t, err)
}

func generateMetrics() pmetric.Metrics {
	metrics := pmetric.NewMetrics()
	resourceMetrics := metrics.ResourceMetrics()
	rm := resourceMetrics.AppendEmpty()
	sm := rm.ScopeMetrics().AppendEmpty()
	metric := sm.Metrics().AppendEmpty()
	metric.SetName("test_metric")
	metric.SetDescription("A test metric")
	metric.SetUnit("unit")

	// Create a gauge metric
	gauge := metric.SetEmptyGauge()
	dp := gauge.DataPoints().AppendEmpty()
	dp.SetDoubleValue(42.0)
	dp.SetTimestamp(1234567890)

	return metrics
}
