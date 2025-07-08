package autotune

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultObservabilityConfig tests default observability configuration
func TestDefaultObservabilityConfig(t *testing.T) {
	config := DefaultObservabilityConfig()

	assert.Equal(t, 8080, config.HTTPPort)
	assert.Equal(t, "/metrics", config.MetricsPath)
	assert.True(t, config.EnablePrometheus)
	assert.True(t, config.EnableJSONMetrics)
	assert.Equal(t, 24*time.Hour, config.MetricsRetention)
}

// TestNewObservabilityServer tests observability server creation
func TestNewObservabilityServer(t *testing.T) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	// Test with default config
	obs := NewObservabilityServer(nil, tuner)
	assert.NotNil(t, obs)
	assert.NotNil(t, obs.config)
	assert.NotNil(t, obs.tuner)
	assert.NotNil(t, obs.server)
	assert.Equal(t, 1000, obs.maxMetrics)

	// Test with custom config
	config := DefaultObservabilityConfig()
	config.HTTPPort = 9090
	obs2 := NewObservabilityServer(config, tuner)
	assert.Equal(t, 9090, obs2.config.HTTPPort)
}

// TestObservabilityServerStartStop tests starting and stopping the server
func TestObservabilityServerStartStop(t *testing.T) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	config := DefaultObservabilityConfig()
	config.HTTPPort = 0 // Use random port
	obs := NewObservabilityServer(config, tuner)

	// Start server
	err = obs.Start()
	assert.NoError(t, err)

	// Stop server
	err = obs.Stop()
	assert.NoError(t, err)
}

// TestMetricsRecording tests metrics recording
func TestMetricsRecording(t *testing.T) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	obs := NewObservabilityServer(DefaultObservabilityConfig(), tuner)

	// Test recording metrics
	testMetrics := Metrics{
		GCPauseTime:    10 * time.Millisecond,
		GCFrequency:    1.0,
		MemoryPressure: 0.5,
		CurrentGOGC:    100,
		Timestamp:      time.Now(),
	}

	obs.recordMetrics(testMetrics)

	obs.mu.RLock()
	assert.Len(t, obs.metricsHistory, 1)
	assert.Equal(t, testMetrics.GCPauseTime, obs.metricsHistory[0].Metrics.GCPauseTime)
	obs.mu.RUnlock()
}

// TestMetricsRetention tests metrics retention policy
func TestMetricsRetention(t *testing.T) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	config := DefaultObservabilityConfig()
	config.MetricsRetention = 100 * time.Millisecond
	obs := NewObservabilityServer(config, tuner)

	// Add old metrics
	oldTime := time.Now().Add(-200 * time.Millisecond)
	testMetrics := Metrics{
		GCPauseTime: 10 * time.Millisecond,
		Timestamp:   oldTime,
	}

	obs.recordMetrics(testMetrics)

	// Wait for retention period
	time.Sleep(150 * time.Millisecond)

	// Add new metrics
	newMetrics := Metrics{
		GCPauseTime: 20 * time.Millisecond,
		Timestamp:   time.Now(),
	}

	obs.recordMetrics(newMetrics)

	// Old metrics should be cleaned up
	obs.mu.RLock()
	assert.Len(t, obs.metricsHistory, 1)
	assert.Equal(t, 20*time.Millisecond, obs.metricsHistory[0].Metrics.GCPauseTime)
	obs.mu.RUnlock()
}

// TestHTTPEndpoints tests HTTP endpoints
func TestHTTPEndpoints(t *testing.T) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	obs := NewObservabilityServer(DefaultObservabilityConfig(), tuner)

	// Test health endpoint
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	obs.handleHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var health map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &health)
	require.NoError(t, err)
	assert.Equal(t, "healthy", health["status"])

	// Test stats endpoint
	req = httptest.NewRequest("GET", "/stats", nil)
	w = httptest.NewRecorder()
	obs.handleStats(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var stats map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &stats)
	require.NoError(t, err)
	assert.Contains(t, stats, "total_decisions")

	// Test config endpoint
	req = httptest.NewRequest("GET", "/config", nil)
	w = httptest.NewRecorder()
	obs.handleConfig(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var config map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &config)
	require.NoError(t, err)
	assert.Contains(t, config, "tuner_config")
	assert.Contains(t, config, "observability_config")
}

// TestPrometheusMetrics tests Prometheus metrics endpoint
func TestPrometheusMetrics(t *testing.T) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	obs := NewObservabilityServer(DefaultObservabilityConfig(), tuner)

	// Test Prometheus format
	req := httptest.NewRequest("GET", "/metrics?format=prometheus", nil)
	w := httptest.NewRecorder()
	obs.handleMetrics(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")

	body := w.Body.String()
	assert.Contains(t, body, "autotune_gc_pause_time_ns")
	assert.Contains(t, body, "autotune_gc_frequency_per_second")
	assert.Contains(t, body, "autotune_heap_size_bytes")
	assert.Contains(t, body, "autotune_gogc_current")
	assert.Contains(t, body, "# HELP")
	assert.Contains(t, body, "# TYPE")

	// Test with disabled Prometheus
	config := DefaultObservabilityConfig()
	config.EnablePrometheus = false
	obs2 := NewObservabilityServer(config, tuner)

	req = httptest.NewRequest("GET", "/metrics?format=prometheus", nil)
	w = httptest.NewRecorder()
	obs2.handleMetrics(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestJSONMetrics tests JSON metrics endpoint
func TestJSONMetrics(t *testing.T) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	obs := NewObservabilityServer(DefaultObservabilityConfig(), tuner)

	// Test JSON format
	req := httptest.NewRequest("GET", "/metrics?format=json", nil)
	w := httptest.NewRecorder()
	obs.handleMetrics(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response, "current_metrics")
	assert.Contains(t, response, "stats")
	assert.Contains(t, response, "timestamp")

	// Test with history
	req = httptest.NewRequest("GET", "/metrics?format=json&history=true", nil)
	w = httptest.NewRecorder()
	obs.handleMetrics(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response, "metrics_history")

	// Test with disabled JSON
	config := DefaultObservabilityConfig()
	config.EnableJSONMetrics = false
	obs2 := NewObservabilityServer(config, tuner)

	req = httptest.NewRequest("GET", "/metrics?format=json", nil)
	w = httptest.NewRecorder()
	obs2.handleMetrics(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestDecisionsEndpoint tests decisions endpoint
func TestDecisionsEndpoint(t *testing.T) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	obs := NewObservabilityServer(DefaultObservabilityConfig(), tuner)

	// Add some decisions to history
	decision := TuningDecision{
		OldGOGC:    100,
		NewGOGC:    150,
		Reason:     "Test decision",
		Confidence: 0.8,
		Timestamp:  time.Now(),
	}

	tuner.mu.Lock()
	tuner.decisionHistory = append(tuner.decisionHistory, decision)
	tuner.mu.Unlock()

	req := httptest.NewRequest("GET", "/decisions", nil)
	w := httptest.NewRecorder()
	obs.handleDecisions(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response, "decisions")
	assert.Contains(t, response, "count")
	assert.Equal(t, float64(1), response["count"])
}

// TestMetricsExporter tests metrics exporter
func TestMetricsExporter(t *testing.T) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	exporter := NewMetricsExporter(tuner)
	assert.NotNil(t, exporter)

	// Test JSON export
	jsonData, err := exporter.ExportToJSON()
	require.NoError(t, err)
	assert.NotEmpty(t, jsonData)

	var data map[string]interface{}
	err = json.Unmarshal(jsonData, &data)
	require.NoError(t, err)
	assert.Contains(t, data, "metrics")
	assert.Contains(t, data, "stats")
	assert.Contains(t, data, "timestamp")

	// Test Prometheus export
	promData, err := exporter.ExportToPrometheus()
	require.NoError(t, err)
	assert.NotEmpty(t, promData)
	assert.Contains(t, promData, "autotune_gc_pause_time_ns")
	assert.Contains(t, promData, "autotune_gogc_current")
}

// TestAlertManager tests alert manager
func TestAlertManager(t *testing.T) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	alertManager := NewAlertManager(tuner)
	assert.NotNil(t, alertManager)

	// Test adding observer
	var receivedAlerts []Alert
	observer := &mockAlertObserver{
		alerts: &receivedAlerts,
	}

	alertManager.AddObserver(observer)

	// Test alert generation
	highPressureMetrics := Metrics{
		MemoryPressure: 0.95,                   // Should trigger critical alert
		GCPauseTime:    150 * time.Millisecond, // Should trigger critical alert
		GCFrequency:    6.0,                    // Should trigger warning alert
	}

	alertManager.checkAlerts(highPressureMetrics)

	// Should have received alerts
	assert.Greater(t, len(receivedAlerts), 0)

	// Check alert types
	foundCritical := false
	foundWarning := false
	for _, alert := range receivedAlerts {
		if alert.Level == AlertLevelCritical {
			foundCritical = true
		}
		if alert.Level == AlertLevelWarning {
			foundWarning = true
		}
	}

	assert.True(t, foundCritical)
	assert.True(t, foundWarning)
}

// TestLogAlertObserver tests log alert observer
func TestLogAlertObserver(t *testing.T) {
	logger := &mockLogger{}
	observer := NewLogAlertObserver(logger)

	// Test different alert levels
	infoAlert := Alert{
		Level:     AlertLevelInfo,
		Message:   "Info alert",
		Timestamp: time.Now(),
	}

	warningAlert := Alert{
		Level:     AlertLevelWarning,
		Message:   "Warning alert",
		Timestamp: time.Now(),
	}

	criticalAlert := Alert{
		Level:     AlertLevelCritical,
		Message:   "Critical alert",
		Timestamp: time.Now(),
	}

	observer.OnAlert(infoAlert)
	observer.OnAlert(warningAlert)
	observer.OnAlert(criticalAlert)

	// Verify logs were called
	assert.Equal(t, 1, logger.infoCalls)
	assert.Equal(t, 1, logger.warnCalls)
	assert.Equal(t, 1, logger.errorCalls)
}

// TestTimestampedMetrics tests timestamped metrics struct
func TestTimestampedMetrics(t *testing.T) {
	metrics := Metrics{
		GCPauseTime: 10 * time.Millisecond,
		CurrentGOGC: 100,
	}

	timestamped := TimestampedMetrics{
		Metrics:   metrics,
		Timestamp: time.Now(),
	}

	assert.Equal(t, metrics.GCPauseTime, timestamped.Metrics.GCPauseTime)
	assert.Equal(t, metrics.CurrentGOGC, timestamped.Metrics.CurrentGOGC)
	assert.False(t, timestamped.Timestamp.IsZero())
}

// TestAlertStruct tests alert struct
func TestAlertStruct(t *testing.T) {
	metrics := &Metrics{
		GCPauseTime: 10 * time.Millisecond,
		CurrentGOGC: 100,
	}

	alert := Alert{
		Level:      AlertLevelWarning,
		Message:    "Test alert",
		Timestamp:  time.Now(),
		Metrics:    metrics,
		Resolution: "Test resolution",
	}

	assert.Equal(t, AlertLevelWarning, alert.Level)
	assert.Equal(t, "Test alert", alert.Message)
	assert.Equal(t, "Test resolution", alert.Resolution)
	assert.Equal(t, metrics, alert.Metrics)
	assert.False(t, alert.Timestamp.IsZero())
}

// TestMetricsHistoryLimit tests metrics history limit
func TestMetricsHistoryLimit(t *testing.T) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	obs := NewObservabilityServer(DefaultObservabilityConfig(), tuner)
	obs.maxMetrics = 3 // Set small limit for testing

	// Add more metrics than the limit
	for i := 0; i < 5; i++ {
		testMetrics := Metrics{
			GCPauseTime: time.Duration(i) * time.Millisecond,
			Timestamp:   time.Now(),
		}
		obs.recordMetrics(testMetrics)
	}

	obs.mu.RLock()
	assert.Equal(t, 3, len(obs.metricsHistory))
	// Should keep the most recent ones
	assert.Equal(t, 2*time.Millisecond, obs.metricsHistory[0].Metrics.GCPauseTime)
	assert.Equal(t, 3*time.Millisecond, obs.metricsHistory[1].Metrics.GCPauseTime)
	assert.Equal(t, 4*time.Millisecond, obs.metricsHistory[2].Metrics.GCPauseTime)
	obs.mu.RUnlock()
}

// BenchmarkMetricsRecording benchmarks metrics recording
func BenchmarkMetricsRecording(b *testing.B) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(b, err)

	obs := NewObservabilityServer(DefaultObservabilityConfig(), tuner)

	testMetrics := Metrics{
		GCPauseTime:    10 * time.Millisecond,
		GCFrequency:    1.0,
		MemoryPressure: 0.5,
		CurrentGOGC:    100,
		Timestamp:      time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		obs.recordMetrics(testMetrics)
	}
}

// BenchmarkPrometheusExport benchmarks Prometheus metrics export
func BenchmarkPrometheusExport(b *testing.B) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(b, err)

	exporter := NewMetricsExporter(tuner)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := exporter.ExportToPrometheus()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkJSONExport benchmarks JSON metrics export
func BenchmarkJSONExport(b *testing.B) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(b, err)

	exporter := NewMetricsExporter(tuner)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := exporter.ExportToJSON()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Mock implementations for testing

type mockAlertObserver struct {
	alerts *[]Alert
}

func (m *mockAlertObserver) OnAlert(alert Alert) {
	*m.alerts = append(*m.alerts, alert)
}

type mockLogger struct {
	infoCalls  int
	warnCalls  int
	errorCalls int
	debugCalls int
}

func (m *mockLogger) Debug(msg string, fields ...interface{}) { m.debugCalls++ }
func (m *mockLogger) Info(msg string, fields ...interface{})  { m.infoCalls++ }
func (m *mockLogger) Warn(msg string, fields ...interface{})  { m.warnCalls++ }
func (m *mockLogger) Error(msg string, fields ...interface{}) { m.errorCalls++ }

// TestIntegrationObservabilityWithTuner tests integration between observability and tuner
func TestIntegrationObservabilityWithTuner(t *testing.T) {
	config := DefaultConfig()
	config.MonitorInterval = 1000 * time.Millisecond

	tuner, err := NewTuner(config)
	require.NoError(t, err)

	obsConfig := DefaultObservabilityConfig()
	obsConfig.HTTPPort = 0 // Random port
	obs := NewObservabilityServer(obsConfig, tuner)

	// Start both
	err = obs.Start()
	require.NoError(t, err)

	err = tuner.Start()
	require.NoError(t, err)

	// Let them run for a bit
	time.Sleep(5000 * time.Millisecond)

	// Check that metrics were recorded
	obs.mu.RLock()
	metricsCount := len(obs.metricsHistory)
	obs.mu.RUnlock()

	assert.Greater(t, metricsCount, 0)

	// Stop both
	err = tuner.Stop()
	assert.NoError(t, err)

	err = obs.Stop()
	assert.NoError(t, err)
}

// TestObservabilityServerContextCancellation tests proper context cancellation
func TestObservabilityServerContextCancellation(t *testing.T) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	config := DefaultObservabilityConfig()
	config.HTTPPort = 0
	obs := NewObservabilityServer(config, tuner)

	// Start server
	err = obs.Start()
	require.NoError(t, err)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Stop server with context
	obs.server.BaseContext = func(net.Listener) context.Context {
		return ctx
	}

	err = obs.Stop()
	assert.NoError(t, err)
}
