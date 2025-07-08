package autotune

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ObservabilityConfig holds configuration for observability features
type ObservabilityConfig struct {
	// HTTPPort is the port for the HTTP metrics endpoint
	HTTPPort int
	// MetricsPath is the path for the metrics endpoint
	MetricsPath string
	// EnablePrometheus enables Prometheus metrics export
	EnablePrometheus bool
	// EnableJSONMetrics enables JSON metrics export
	EnableJSONMetrics bool
	// MetricsRetention is how long to keep metrics history
	MetricsRetention time.Duration
}

// DefaultObservabilityConfig returns default observability configuration
func DefaultObservabilityConfig() *ObservabilityConfig {
	return &ObservabilityConfig{
		HTTPPort:          8080,
		MetricsPath:       "/metrics",
		EnablePrometheus:  true,
		EnableJSONMetrics: true,
		MetricsRetention:  24 * time.Hour,
	}
}

// ObservabilityServer provides HTTP endpoints for metrics and health checks
type ObservabilityServer struct {
	config *ObservabilityConfig
	tuner  *Tuner
	server *http.Server
	mu     sync.RWMutex

	// Metrics storage
	metricsHistory []TimestampedMetrics
	maxMetrics     int
}

// TimestampedMetrics holds metrics with a timestamp
type TimestampedMetrics struct {
	Metrics   Metrics   `json:"metrics"`
	Timestamp time.Time `json:"timestamp"`
}

// NewObservabilityServer creates a new observability server
func NewObservabilityServer(config *ObservabilityConfig, tuner *Tuner) *ObservabilityServer {
	if config == nil {
		config = DefaultObservabilityConfig()
	}

	obs := &ObservabilityServer{
		config:     config,
		tuner:      tuner,
		maxMetrics: 1000, // Keep last 1000 metrics
	}

	// Set up HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc(config.MetricsPath, obs.handleMetrics)
	mux.HandleFunc("/health", obs.handleHealth)
	mux.HandleFunc("/stats", obs.handleStats)
	mux.HandleFunc("/config", obs.handleConfig)
	mux.HandleFunc("/decisions", obs.handleDecisions)

	obs.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", config.HTTPPort),
		Handler: mux,
	}

	return obs
}

// Start starts the observability server
func (obs *ObservabilityServer) Start() error {
	// Start collecting metrics
	obs.tuner.SetOnMetricsUpdate(obs.recordMetrics)

	// Start HTTP server
	go func() {
		if err := obs.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			obs.tuner.config.Logger.Error("Observability server error: %v", err)
		}
	}()

	obs.tuner.config.Logger.Info("Observability server started on port %d", obs.config.HTTPPort)
	return nil
}

// Stop stops the observability server
func (obs *ObservabilityServer) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return obs.server.Shutdown(ctx)
}

// recordMetrics records metrics for observability
func (obs *ObservabilityServer) recordMetrics(metrics Metrics) {
	obs.mu.Lock()
	defer obs.mu.Unlock()

	timestamped := TimestampedMetrics{
		Metrics:   metrics,
		Timestamp: time.Now(),
	}

	obs.metricsHistory = append(obs.metricsHistory, timestamped)

	// Remove old metrics
	if len(obs.metricsHistory) > obs.maxMetrics {
		obs.metricsHistory = obs.metricsHistory[1:]
	}

	// Clean up old metrics based on retention policy
	cutoff := time.Now().Add(-obs.config.MetricsRetention)
	for i := 0; i < len(obs.metricsHistory); i++ {
		if obs.metricsHistory[i].Timestamp.After(cutoff) {
			obs.metricsHistory = obs.metricsHistory[i:]
			break
		}
	}
}

// handleMetrics handles the metrics endpoint
func (obs *ObservabilityServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	obs.mu.RLock()
	defer obs.mu.RUnlock()

	format := r.URL.Query().Get("format")

	switch format {
	case "prometheus":
		obs.handlePrometheusMetrics(w, r)
	case "json":
		obs.handleJSONMetrics(w, r)
	default:
		// Default to JSON
		obs.handleJSONMetrics(w, r)
	}
}

// handlePrometheusMetrics handles Prometheus format metrics
func (obs *ObservabilityServer) handlePrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	if !obs.config.EnablePrometheus {
		http.Error(w, "Prometheus metrics disabled", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	// Get current metrics
	currentMetrics := obs.tuner.GetMetrics()
	stats := obs.tuner.GetStats()

	// Write Prometheus metrics
	fmt.Fprintf(w, "# HELP autotune_gc_pause_time_ns Current GC pause time in nanoseconds\n")
	fmt.Fprintf(w, "# TYPE autotune_gc_pause_time_ns gauge\n")
	fmt.Fprintf(w, "autotune_gc_pause_time_ns %d\n", currentMetrics.GCPauseTime.Nanoseconds())

	fmt.Fprintf(w, "# HELP autotune_gc_frequency_per_second Current GC frequency per second\n")
	fmt.Fprintf(w, "# TYPE autotune_gc_frequency_per_second gauge\n")
	fmt.Fprintf(w, "autotune_gc_frequency_per_second %f\n", currentMetrics.GCFrequency)

	fmt.Fprintf(w, "# HELP autotune_heap_size_bytes Current heap size in bytes\n")
	fmt.Fprintf(w, "# TYPE autotune_heap_size_bytes gauge\n")
	fmt.Fprintf(w, "autotune_heap_size_bytes %d\n", currentMetrics.HeapSize)

	fmt.Fprintf(w, "# HELP autotune_heap_alloc_bytes Current heap allocation in bytes\n")
	fmt.Fprintf(w, "# TYPE autotune_heap_alloc_bytes gauge\n")
	fmt.Fprintf(w, "autotune_heap_alloc_bytes %d\n", currentMetrics.HeapAlloc)

	fmt.Fprintf(w, "# HELP autotune_memory_pressure_ratio Current memory pressure ratio\n")
	fmt.Fprintf(w, "# TYPE autotune_memory_pressure_ratio gauge\n")
	fmt.Fprintf(w, "autotune_memory_pressure_ratio %f\n", currentMetrics.MemoryPressure)

	fmt.Fprintf(w, "# HELP autotune_gogc_current Current GOGC value\n")
	fmt.Fprintf(w, "# TYPE autotune_gogc_current gauge\n")
	fmt.Fprintf(w, "autotune_gogc_current %d\n", currentMetrics.CurrentGOGC)

	fmt.Fprintf(w, "# HELP autotune_total_decisions_total Total number of tuning decisions made\n")
	fmt.Fprintf(w, "# TYPE autotune_total_decisions_total counter\n")
	fmt.Fprintf(w, "autotune_total_decisions_total %d\n", stats["total_decisions"])

	fmt.Fprintf(w, "# HELP autotune_successful_tunes_total Number of successful tuning decisions\n")
	fmt.Fprintf(w, "# TYPE autotune_successful_tunes_total counter\n")
	fmt.Fprintf(w, "autotune_successful_tunes_total %d\n", stats["successful_tunes"])

	fmt.Fprintf(w, "# HELP autotune_reverted_tunes_total Number of reverted tuning decisions\n")
	fmt.Fprintf(w, "# TYPE autotune_reverted_tunes_total counter\n")
	fmt.Fprintf(w, "autotune_reverted_tunes_total %d\n", stats["reverted_tunes"])

	if currentMetrics.ContainerMemLimit > 0 {
		fmt.Fprintf(w, "# HELP autotune_container_memory_limit_bytes Container memory limit in bytes\n")
		fmt.Fprintf(w, "# TYPE autotune_container_memory_limit_bytes gauge\n")
		fmt.Fprintf(w, "autotune_container_memory_limit_bytes %d\n", currentMetrics.ContainerMemLimit)
	}

	if currentMetrics.ContainerCPULimit > 0 {
		fmt.Fprintf(w, "# HELP autotune_container_cpu_limit_cores Container CPU limit in cores\n")
		fmt.Fprintf(w, "# TYPE autotune_container_cpu_limit_cores gauge\n")
		fmt.Fprintf(w, "autotune_container_cpu_limit_cores %f\n", currentMetrics.ContainerCPULimit)
	}
}

// handleJSONMetrics handles JSON format metrics
func (obs *ObservabilityServer) handleJSONMetrics(w http.ResponseWriter, r *http.Request) {
	if !obs.config.EnableJSONMetrics {
		http.Error(w, "JSON metrics disabled", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Get current metrics and stats
	currentMetrics := obs.tuner.GetMetrics()
	stats := obs.tuner.GetStats()

	response := map[string]interface{}{
		"current_metrics": currentMetrics,
		"stats":           stats,
		"timestamp":       time.Now(),
	}

	// Include recent metrics history if requested
	if r.URL.Query().Get("history") == "true" {
		response["metrics_history"] = obs.metricsHistory
	}

	json.NewEncoder(w).Encode(response)
}

// handleHealth handles health check endpoint
func (obs *ObservabilityServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	health := map[string]interface{}{
		"status":        "healthy",
		"timestamp":     time.Now(),
		"tuner_running": obs.tuner.running,
	}

	// Check for any critical issues
	currentMetrics := obs.tuner.GetMetrics()
	if currentMetrics.MemoryPressure > 0.95 {
		health["status"] = "warning"
		health["warnings"] = []string{"High memory pressure"}
	}

	if currentMetrics.GCPauseTime > 100*time.Millisecond {
		health["status"] = "warning"
		if health["warnings"] == nil {
			health["warnings"] = []string{}
		}
		health["warnings"] = append(health["warnings"].([]string), "High GC pause time")
	}

	json.NewEncoder(w).Encode(health)
}

// handleStats handles statistics endpoint
func (obs *ObservabilityServer) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	stats := obs.tuner.GetStats()

	// Add observability server stats
	obs.mu.RLock()
	stats["metrics_history_count"] = len(obs.metricsHistory)
	stats["observability_server_running"] = obs.server != nil
	obs.mu.RUnlock()

	json.NewEncoder(w).Encode(stats)
}

// handleConfig handles configuration endpoint
func (obs *ObservabilityServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	config := map[string]interface{}{
		"tuner_config":         obs.tuner.config,
		"observability_config": obs.config,
		"timestamp":            time.Now(),
	}

	json.NewEncoder(w).Encode(config)
}

// handleDecisions handles recent decisions endpoint
func (obs *ObservabilityServer) handleDecisions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	obs.tuner.mu.RLock()
	decisions := obs.tuner.decisionHistory
	obs.tuner.mu.RUnlock()

	response := map[string]interface{}{
		"decisions": decisions,
		"count":     len(decisions),
		"timestamp": time.Now(),
	}

	json.NewEncoder(w).Encode(response)
}

// MetricsExporter provides methods to export metrics to external systems
type MetricsExporter struct {
	tuner *Tuner
}

// NewMetricsExporter creates a new metrics exporter
func NewMetricsExporter(tuner *Tuner) *MetricsExporter {
	return &MetricsExporter{tuner: tuner}
}

// ExportToJSON exports current metrics to JSON format
func (me *MetricsExporter) ExportToJSON() ([]byte, error) {
	metrics := me.tuner.GetMetrics()
	stats := me.tuner.GetStats()

	data := map[string]interface{}{
		"metrics":   metrics,
		"stats":     stats,
		"timestamp": time.Now(),
	}

	return json.MarshalIndent(data, "", "  ")
}

// ExportToPrometheus exports current metrics to Prometheus format
func (me *MetricsExporter) ExportToPrometheus() (string, error) {
	metrics := me.tuner.GetMetrics()
	stats := me.tuner.GetStats()

	var output string

	// Add metrics
	output += fmt.Sprintf("autotune_gc_pause_time_ns %d\n", metrics.GCPauseTime.Nanoseconds())
	output += fmt.Sprintf("autotune_gc_frequency_per_second %f\n", metrics.GCFrequency)
	output += fmt.Sprintf("autotune_heap_size_bytes %d\n", metrics.HeapSize)
	output += fmt.Sprintf("autotune_heap_alloc_bytes %d\n", metrics.HeapAlloc)
	output += fmt.Sprintf("autotune_memory_pressure_ratio %f\n", metrics.MemoryPressure)
	output += fmt.Sprintf("autotune_gogc_current %d\n", metrics.CurrentGOGC)
	output += fmt.Sprintf("autotune_total_decisions_total %d\n", stats["total_decisions"])
	output += fmt.Sprintf("autotune_successful_tunes_total %d\n", stats["successful_tunes"])
	output += fmt.Sprintf("autotune_reverted_tunes_total %d\n", stats["reverted_tunes"])

	if metrics.ContainerMemLimit > 0 {
		output += fmt.Sprintf("autotune_container_memory_limit_bytes %d\n", metrics.ContainerMemLimit)
	}

	if metrics.ContainerCPULimit > 0 {
		output += fmt.Sprintf("autotune_container_cpu_limit_cores %f\n", metrics.ContainerCPULimit)
	}

	return output, nil
}

// AlertManager manages alerts based on metrics thresholds
type AlertManager struct {
	tuner     *Tuner
	observers []AlertObserver
	mu        sync.RWMutex
}

// AlertObserver defines the interface for alert observers
type AlertObserver interface {
	OnAlert(alert Alert)
}

// Alert represents an alert condition
type Alert struct {
	Level      AlertLevel `json:"level"`
	Message    string     `json:"message"`
	Timestamp  time.Time  `json:"timestamp"`
	Metrics    *Metrics   `json:"metrics,omitempty"`
	Resolution string     `json:"resolution,omitempty"`
}

// AlertLevel defines the severity of an alert
type AlertLevel string

const (
	AlertLevelInfo     AlertLevel = "info"
	AlertLevelWarning  AlertLevel = "warning"
	AlertLevelCritical AlertLevel = "critical"
)

// NewAlertManager creates a new alert manager
func NewAlertManager(tuner *Tuner) *AlertManager {
	am := &AlertManager{
		tuner: tuner,
	}

	// Set up metrics monitoring
	tuner.SetOnMetricsUpdate(am.checkAlerts)

	return am
}

// AddObserver adds an alert observer
func (am *AlertManager) AddObserver(observer AlertObserver) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.observers = append(am.observers, observer)
}

// checkAlerts checks for alert conditions
func (am *AlertManager) checkAlerts(metrics Metrics) {
	alerts := []Alert{}

	// High memory pressure alert
	if metrics.MemoryPressure > 0.9 {
		alerts = append(alerts, Alert{
			Level:      AlertLevelCritical,
			Message:    fmt.Sprintf("Critical memory pressure: %.1f%%", metrics.MemoryPressure*100),
			Timestamp:  time.Now(),
			Metrics:    &metrics,
			Resolution: "Consider reducing memory usage or increasing container memory limits",
		})
	} else if metrics.MemoryPressure > 0.8 {
		alerts = append(alerts, Alert{
			Level:      AlertLevelWarning,
			Message:    fmt.Sprintf("High memory pressure: %.1f%%", metrics.MemoryPressure*100),
			Timestamp:  time.Now(),
			Metrics:    &metrics,
			Resolution: "Monitor memory usage and consider optimization",
		})
	}

	// High GC pause time alert
	if metrics.GCPauseTime > 100*time.Millisecond {
		alerts = append(alerts, Alert{
			Level:      AlertLevelCritical,
			Message:    fmt.Sprintf("High GC pause time: %.2fms", float64(metrics.GCPauseTime)/1e6),
			Timestamp:  time.Now(),
			Metrics:    &metrics,
			Resolution: "Consider tuning GOGC or reducing allocation rate",
		})
	} else if metrics.GCPauseTime > 50*time.Millisecond {
		alerts = append(alerts, Alert{
			Level:      AlertLevelWarning,
			Message:    fmt.Sprintf("Elevated GC pause time: %.2fms", float64(metrics.GCPauseTime)/1e6),
			Timestamp:  time.Now(),
			Metrics:    &metrics,
			Resolution: "Monitor GC performance and consider optimization",
		})
	}

	// High GC frequency alert
	if metrics.GCFrequency > 5.0 {
		alerts = append(alerts, Alert{
			Level:      AlertLevelWarning,
			Message:    fmt.Sprintf("High GC frequency: %.1f/sec", metrics.GCFrequency),
			Timestamp:  time.Now(),
			Metrics:    &metrics,
			Resolution: "Consider increasing GOGC or reducing allocation rate",
		})
	}

	// Notify observers
	am.mu.RLock()
	observers := am.observers
	am.mu.RUnlock()

	for _, alert := range alerts {
		for _, observer := range observers {
			observer.OnAlert(alert)
		}
	}
}

// LogAlertObserver logs alerts to the configured logger
type LogAlertObserver struct {
	logger Logger
}

// NewLogAlertObserver creates a new log alert observer
func NewLogAlertObserver(logger Logger) *LogAlertObserver {
	return &LogAlertObserver{logger: logger}
}

// OnAlert handles alert notifications
func (lao *LogAlertObserver) OnAlert(alert Alert) {
	switch alert.Level {
	case AlertLevelInfo:
		lao.logger.Info("Alert: %s", alert.Message)
	case AlertLevelWarning:
		lao.logger.Warn("Alert: %s", alert.Message)
	case AlertLevelCritical:
		lao.logger.Error("Alert: %s", alert.Message)
	}
}
