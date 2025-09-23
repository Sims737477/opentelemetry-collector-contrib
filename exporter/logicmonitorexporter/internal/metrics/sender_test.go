// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	lmsdkmetrics "github.com/logicmonitor/lm-data-sdk-go/api/metrics"
	"github.com/logicmonitor/lm-data-sdk-go/utils"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/pdata/testdata"
	"go.uber.org/zap"
)

func TestSendMetrics(t *testing.T) {
	authParams := utils.AuthParams{
		AccessID:    "testId",
		AccessKey:   "testKey",
		BearerToken: "testToken",
	}
	t.Run("should not return error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			response := lmsdkmetrics.SendMetricResponse{
				Success: true,
				Message: "Accepted",
			}
			w.WriteHeader(http.StatusAccepted)
			assert.NoError(t, json.NewEncoder(w).Encode(&response))
		}))
		defer ts.Close()

		ctx, cancel := context.WithCancel(t.Context())

		sender, err := NewSender(ctx, ts.URL, ts.Client(), authParams, zap.NewNop())
		assert.NoError(t, err)

		err = sender.SendMetrics(ctx, testdata.GenerateMetrics(1))
		cancel()
		assert.NoError(t, err)
	})

	t.Run("should return permanent failure error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			response := lmsdkmetrics.SendMetricResponse{
				Success: false,
				Message: "The request is invalid. For example, it may be missing headers or the request body is incorrectly formatted.",
			}
			w.WriteHeader(http.StatusBadRequest)
			assert.NoError(t, json.NewEncoder(w).Encode(&response))
		}))
		defer ts.Close()

		ctx, cancel := context.WithCancel(t.Context())

		sender, err := NewSender(ctx, ts.URL, ts.Client(), authParams, zap.NewNop())
		assert.NoError(t, err)

		err = sender.SendMetrics(ctx, testdata.GenerateMetrics(1))
		cancel()
		assert.Error(t, err)
		assert.True(t, consumererror.IsPermanent(err))
	})

	t.Run("should not return permanent failure error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			response := lmsdkmetrics.SendMetricResponse{
				Success: false,
				Message: "A dependency failed to respond within a reasonable time.",
			}
			w.WriteHeader(http.StatusBadGateway)
			assert.NoError(t, json.NewEncoder(w).Encode(&response))
		}))
		defer ts.Close()

		ctx, cancel := context.WithCancel(t.Context())

		sender, err := NewSender(ctx, ts.URL, ts.Client(), authParams, zap.NewNop())
		assert.NoError(t, err)

		err = sender.SendMetrics(ctx, testdata.GenerateMetrics(1))
		cancel()
		assert.Error(t, err)
		assert.False(t, consumererror.IsPermanent(err))
	})
}

func TestGenerateDataSourceName(t *testing.T) {
	tests := []struct {
		name         string
		metricName   string
		expectedName string
	}{
		// Basic delimiter tests
		{
			name:         "metric with underscore delimiter",
			metricName:   "go_gc_cycle",
			expectedName: "Go",
		},
		{
			name:         "metric with dot delimiter",
			metricName:   "jvm.memory.used",
			expectedName: "Jvm",
		},
		{
			name:         "short metric name",
			metricName:   "up",
			expectedName: "Ungroupped",
		},
		{
			name:         "single character metric name",
			metricName:   "x",
			expectedName: "Ungroupped",
		},
		{
			name:         "empty metric name",
			metricName:   "",
			expectedName: "Ungroupped",
		},
		{
			name:         "metric with both delimiters - underscore first",
			metricName:   "http_requests.total",
			expectedName: "Http",
		},
		{
			name:         "metric with both delimiters - dot first",
			metricName:   "app.http_requests",
			expectedName: "App",
		},
		{
			name:         "metric without delimiters - long name",
			metricName:   "temperature",
			expectedName: "Temperature",
		},
		{
			name:         "metric without delimiters - short name",
			metricName:   "cpu",
			expectedName: "Cpu",
		},
		{
			name:         "metric without delimiters - very short name",
			metricName:   "io",
			expectedName: "Ungroupped",
		},
		{
			name:         "metric with first part too short",
			metricName:   "a_long_metric_name",
			expectedName: "Ungroupped",
		},
		{
			name:         "metric with uppercase letters",
			metricName:   "HTTP_REQUESTS_TOTAL",
			expectedName: "Http",
		},

		// LogicMonitor spec compliance tests
		{
			name:         "metric with invalid characters - comma",
			metricName:   "test,name_metric",
			expectedName: "Testname",
		},
		{
			name:         "metric with invalid characters - semicolon",
			metricName:   "test;name_metric",
			expectedName: "Testname",
		},
		{
			name:         "metric with invalid characters - slash",
			metricName:   "test/name_metric",
			expectedName: "Testname",
		},
		{
			name:         "metric with invalid characters - asterisk",
			metricName:   "test*name_metric",
			expectedName: "Testname",
		},
		{
			name:         "metric with invalid characters - brackets",
			metricName:   "test[name]_metric",
			expectedName: "Testname",
		},
		{
			name:         "metric with invalid characters - question mark",
			metricName:   "test?name_metric",
			expectedName: "Testname",
		},
		{
			name:         "metric with invalid characters - quotes",
			metricName:   "test'name\"_metric",
			expectedName: "Testname",
		},
		{
			name:         "metric with invalid characters - backtick",
			metricName:   "test`name_metric",
			expectedName: "Testname",
		},
		{
			name:         "metric with invalid characters - hash",
			metricName:   "test#name_metric",
			expectedName: "Testname",
		},

		// Space handling tests
		{
			name:         "metric with leading spaces",
			metricName:   " test_metric",
			expectedName: "Test",
		},
		{
			name:         "metric with trailing spaces",
			metricName:   "test _metric",
			expectedName: "Test",
		},
		{
			name:         "metric with internal spaces",
			metricName:   "test name_metric",
			expectedName: "Test name",
		},

		// Hyphen handling tests
		{
			name:         "metric with hyphen in middle (should be removed)",
			metricName:   "test-name_metric",
			expectedName: "Testname",
		},
		{
			name:         "metric with hyphen at end (should be kept)",
			metricName:   "testname-_metric",
			expectedName: "Testname-",
		},
		{
			name:         "metric with only hyphen",
			metricName:   "-",
			expectedName: "Ungroupped",
		},
		{
			name:         "metric with hyphen and one character",
			metricName:   "a-",
			expectedName: "Ungroupped", // Less than 2 chars after processing
		},

		// Length limit tests
		{
			name:         "very long metric name (over 64 chars)",
			metricName:   "verylongmetricnamethatexceedssixtyfourcharsandshouldbetruncat_metric",
			expectedName: "Verylongmetricnamethatexceedssixtyfourcharsandshouldbetruncat", // 64 chars
		},

		// Edge cases
		{
			name:         "metric with all invalid characters",
			metricName:   ",;/*[]?'\"`,#",
			expectedName: "Ungroupped",
		},
		{
			name:         "metric with newline characters",
			metricName:   "test\nname_metric",
			expectedName: "Testname",
		},
		{
			name:         "metric with carriage return",
			metricName:   "test\rname_metric",
			expectedName: "Testname",
		},
		{
			name:         "metric becomes empty after cleaning",
			metricName:   ",,;;__metric",
			expectedName: "Ungroupped", // First part becomes empty after cleaning
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateDataSourceName(tt.metricName)
			assert.Equal(t, tt.expectedName, result)

			// Verify spec compliance
			assert.LessOrEqual(t, len(result), 64, "Result should not exceed 64 characters")
			assert.NotEmpty(t, result, "Result should not be empty")

			// Verify no invalid characters in result
			for _, r := range result {
				switch r {
				case ',', ';', '/', '*', '[', ']', '?', '\'', '"', '`', '#', '\n', '\r':
					t.Errorf("Result contains invalid character: %c", r)
				}
			}

			// Verify no leading/trailing spaces
			assert.Equal(t, strings.TrimSpace(result), result, "Result should not have leading/trailing spaces")

			// Verify hyphen rules
			if strings.Contains(result, "-") {
				hyphenIndex := strings.Index(result, "-")
				if hyphenIndex != len(result)-1 {
					t.Errorf("Hyphen found not at the end of result: %s", result)
				}
				if len(result) < 2 {
					t.Errorf("Result with hyphen should have at least 2 characters: %s", result)
				}
			}
		})
	}
}
