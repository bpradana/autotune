// Package autotune provides automatic garbage collection tuning for Go applications
// in containerized environments. It monitors real-time application metrics and
// dynamically adjusts the GOGC value to optimize for latency and throughput.
package autotune

import (
	"context"
	"fmt"
	"log"
	"math"
	"runtime"
	"runtime/debug"
	"sync"
	"time"
)

// Config holds configuration for the autotune package
type Config struct {
	// MonitorInterval is how often to collect metrics and evaluate tuning
	MonitorInterval time.Duration
	// MinGOGC is the minimum GOGC value allowed
	MinGOGC int
	// MaxGOGC is the maximum GOGC value allowed
	MaxGOGC int
	// TargetLatency is the target GC pause time in nanoseconds
	TargetLatency time.Duration
	// MemoryLimitPercent is the percentage of container memory limit to use as threshold
	MemoryLimitPercent float64
	// TuningAggressiveness controls how quickly GOGC is adjusted (0.1 = conservative, 1.0 = aggressive)
	TuningAggressiveness float64
	// StabilizationWindow is the time window for anti-oscillation logic
	StabilizationWindow time.Duration
	// MaxChangePerInterval limits how much GOGC can change in one interval
	MaxChangePerInterval int
	// Logger for debugging and observability
	Logger Logger
}

// DefaultConfig returns a production-ready default configuration
func DefaultConfig() *Config {
	return &Config{
		MonitorInterval:      30 * time.Second,
		MinGOGC:              50,
		MaxGOGC:              800,
		TargetLatency:        10 * time.Millisecond,
		MemoryLimitPercent:   0.8,
		TuningAggressiveness: 0.3,
		StabilizationWindow:  5 * time.Minute,
		MaxChangePerInterval: 50,
		Logger:               &defaultLogger{},
	}
}

// Logger interface for customizable logging
type Logger interface {
	Debug(msg string, fields ...interface{})
	Info(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
}

type defaultLogger struct{}

func (l *defaultLogger) Debug(msg string, fields ...interface{}) {
	log.Printf("DEBUG: "+msg, fields...)
}
func (l *defaultLogger) Info(msg string, fields ...interface{}) { log.Printf("INFO: "+msg, fields...) }
func (l *defaultLogger) Warn(msg string, fields ...interface{}) { log.Printf("WARN: "+msg, fields...) }
func (l *defaultLogger) Error(msg string, fields ...interface{}) {
	log.Printf("ERROR: "+msg, fields...)
}

// Metrics holds runtime metrics for GC tuning decisions
type Metrics struct {
	// GC metrics
	GCPauseTime time.Duration
	GCFrequency float64 // GCs per second
	HeapSize    uint64
	HeapAlloc   uint64
	HeapInuse   uint64
	NextGC      uint64
	LastGC      time.Time
	NumGC       uint32

	// Memory metrics
	MemoryLimit    uint64
	MemoryUsage    uint64
	MemoryPressure float64 // 0.0 to 1.0

	// Performance metrics
	CPUUsage   float64
	Throughput float64 // requests per second (app-specific)

	// Container metrics
	ContainerMemLimit uint64
	ContainerCPULimit float64

	// Current GOGC value
	CurrentGOGC int

	Timestamp time.Time
}

// TuningDecision represents a decision made by the tuning algorithm
type TuningDecision struct {
	OldGOGC    int
	NewGOGC    int
	Reason     string
	Confidence float64 // 0.0 to 1.0
	Timestamp  time.Time
	Metrics    *Metrics
}

// Tuner manages automatic GC tuning
type Tuner struct {
	config  *Config
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
	running bool

	// Metrics history for decision-making
	metricsHistory []Metrics
	maxHistory     int

	// Decision history for anti-oscillation
	decisionHistory []TuningDecision
	maxDecisions    int

	// Container resource detection
	containerResources *ContainerResources

	// Callbacks
	onTuningDecision func(decision TuningDecision)
	onMetricsUpdate  func(metrics Metrics)

	// Internal state
	lastGOGC       int
	stabilityCount int

	// Metrics for observability
	totalDecisions  int64
	successfulTunes int64
	revertedTunes   int64
	avgImprovement  float64
}

// NewTuner creates a new GC tuner with the given configuration
func NewTuner(config *Config) (*Tuner, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	containerResources, err := DetectContainerResources()
	if err != nil {
		config.Logger.Warn("Failed to detect container resources: %v", err)
	}

	tuner := &Tuner{
		config:             config,
		ctx:                ctx,
		cancel:             cancel,
		maxHistory:         100,
		maxDecisions:       50,
		containerResources: containerResources,
		lastGOGC:           debug.SetGCPercent(-1), // Get current GOGC
	}

	// Restore original GOGC
	debug.SetGCPercent(tuner.lastGOGC)

	return tuner, nil
}

// Start begins the automatic tuning process
func (t *Tuner) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running {
		return fmt.Errorf("tuner is already running")
	}

	t.running = true
	t.config.Logger.Info("Starting GC autotuner")

	go t.monitorLoop()

	return nil
}

// Stop stops the automatic tuning process
func (t *Tuner) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return fmt.Errorf("tuner is not running")
	}

	t.running = false
	t.cancel()
	t.config.Logger.Info("Stopping GC autotuner")

	return nil
}

// GetMetrics returns the current metrics
func (t *Tuner) GetMetrics() Metrics {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.collectMetrics()
}

// SetOnTuningDecision sets a callback for when tuning decisions are made
func (t *Tuner) SetOnTuningDecision(callback func(TuningDecision)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onTuningDecision = callback
}

// SetOnMetricsUpdate sets a callback for when metrics are updated
func (t *Tuner) SetOnMetricsUpdate(callback func(Metrics)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onMetricsUpdate = callback
}

// GetStats returns statistics about the tuner's performance
func (t *Tuner) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return map[string]interface{}{
		"total_decisions":  t.totalDecisions,
		"successful_tunes": t.successfulTunes,
		"reverted_tunes":   t.revertedTunes,
		"avg_improvement":  t.avgImprovement,
		"current_gogc":     debug.SetGCPercent(-1), // Get current without changing
		"stability_count":  t.stabilityCount,
		"metrics_history":  len(t.metricsHistory),
		"decision_history": len(t.decisionHistory),
		"running":          t.running,
	}
}

// monitorLoop is the main monitoring and tuning loop
func (t *Tuner) monitorLoop() {
	ticker := time.NewTicker(t.config.MonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			t.performTuningCycle()
		}
	}
}

// performTuningCycle performs one complete tuning cycle
func (t *Tuner) performTuningCycle() {
	defer func() {
		if r := recover(); r != nil {
			t.config.Logger.Error("Panic in tuning cycle: %v", r)
		}
	}()

	// Collect current metrics
	metrics := t.collectMetrics()

	t.mu.Lock()
	// Store metrics history
	t.metricsHistory = append(t.metricsHistory, metrics)
	if len(t.metricsHistory) > t.maxHistory {
		t.metricsHistory = t.metricsHistory[1:]
	}
	t.mu.Unlock()

	// Trigger metrics callback
	if t.onMetricsUpdate != nil {
		t.onMetricsUpdate(metrics)
	}

	// Make tuning decision
	decision := t.makeTuningDecision(metrics)

	if decision != nil {
		t.applyTuningDecision(*decision)
	}
}

// collectMetrics gathers all relevant metrics for tuning decisions
func (t *Tuner) collectMetrics() Metrics {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	var gcStats debug.GCStats
	debug.ReadGCStats(&gcStats)

	metrics := Metrics{
		HeapSize:    m.HeapSys,
		HeapAlloc:   m.HeapAlloc,
		HeapInuse:   m.HeapInuse,
		NextGC:      m.NextGC,
		NumGC:       m.NumGC,
		CurrentGOGC: debug.SetGCPercent(-1), // Get current without changing
		Timestamp:   time.Now(),
	}

	// Calculate GC pause time (average of recent pauses)
	if len(gcStats.Pause) > 0 {
		var totalPause time.Duration
		count := len(gcStats.Pause)
		if count > 10 {
			count = 10 // Use last 10 pauses
		}
		for i := 0; i < count; i++ {
			totalPause += gcStats.Pause[i]
		}
		metrics.GCPauseTime = totalPause / time.Duration(count)
	}

	// Calculate GC frequency
	if len(t.metricsHistory) > 0 {
		prev := t.metricsHistory[len(t.metricsHistory)-1]
		timeDiff := metrics.Timestamp.Sub(prev.Timestamp).Seconds()
		if timeDiff > 0 {
			gcDiff := float64(metrics.NumGC - prev.NumGC)
			metrics.GCFrequency = gcDiff / timeDiff
		}
	}

	// Add container resource information
	if t.containerResources != nil {
		metrics.ContainerMemLimit = t.containerResources.MemoryLimit
		metrics.ContainerCPULimit = t.containerResources.CPULimit
		if t.containerResources.MemoryLimit > 0 {
			metrics.MemoryPressure = float64(metrics.HeapInuse) / float64(t.containerResources.MemoryLimit)
		}
	}

	// Calculate memory usage and pressure
	if metrics.ContainerMemLimit > 0 {
		metrics.MemoryUsage = metrics.HeapInuse
		metrics.MemoryLimit = uint64(float64(metrics.ContainerMemLimit) * t.config.MemoryLimitPercent)
		metrics.MemoryPressure = float64(metrics.MemoryUsage) / float64(metrics.MemoryLimit)
	}

	return metrics
}

// makeTuningDecision analyzes metrics and decides whether to adjust GOGC
func (t *Tuner) makeTuningDecision(metrics Metrics) *TuningDecision {
	currentGOGC := metrics.CurrentGOGC

	// Check if we have enough data to make a decision
	if len(t.metricsHistory) < 2 {
		return nil
	}

	// Anti-oscillation check
	if t.shouldSkipDueToOscillation() {
		t.config.Logger.Debug("Skipping tuning due to oscillation prevention")
		return nil
	}

	// Calculate target GOGC based on multiple factors
	targetGOGC := t.calculateTargetGOGC(metrics)

	// Check if change is significant enough
	change := targetGOGC - currentGOGC
	if abs(change) < 10 { // Minimum change threshold
		t.stabilityCount++
		return nil
	}

	// Limit the change per interval
	if abs(change) > t.config.MaxChangePerInterval {
		if change > 0 {
			targetGOGC = currentGOGC + t.config.MaxChangePerInterval
		} else {
			targetGOGC = currentGOGC - t.config.MaxChangePerInterval
		}
	}

	// Ensure bounds
	if targetGOGC < t.config.MinGOGC {
		targetGOGC = t.config.MinGOGC
	}
	if targetGOGC > t.config.MaxGOGC {
		targetGOGC = t.config.MaxGOGC
	}

	// Calculate confidence based on metrics stability and clarity
	confidence := t.calculateConfidence(metrics)

	// Only proceed if confidence is high enough
	if confidence < 0.6 {
		t.config.Logger.Debug("Skipping tuning due to low confidence: %.2f", confidence)
		return nil
	}

	reason := t.buildReasonString(metrics, currentGOGC, targetGOGC)

	decision := &TuningDecision{
		OldGOGC:    currentGOGC,
		NewGOGC:    targetGOGC,
		Reason:     reason,
		Confidence: confidence,
		Timestamp:  time.Now(),
		Metrics:    &metrics,
	}

	return decision
}

// calculateTargetGOGC computes the optimal GOGC value based on current metrics
func (t *Tuner) calculateTargetGOGC(metrics Metrics) int {
	currentGOGC := metrics.CurrentGOGC

	// Factor 1: Latency-based adjustment
	latencyFactor := 1.0
	if metrics.GCPauseTime > t.config.TargetLatency {
		// Pause time too high, increase GOGC to reduce GC frequency
		ratio := float64(metrics.GCPauseTime) / float64(t.config.TargetLatency)
		latencyFactor = 1.0 + (ratio-1.0)*t.config.TuningAggressiveness
	} else {
		// Pause time acceptable, might be able to decrease GOGC for better memory usage
		ratio := float64(t.config.TargetLatency) / float64(metrics.GCPauseTime)
		latencyFactor = 1.0 - (ratio-1.0)*t.config.TuningAggressiveness*0.5
	}

	// Factor 2: Memory pressure adjustment
	memoryFactor := 1.0
	if metrics.MemoryPressure > 0.8 {
		// High memory pressure, decrease GOGC to collect more frequently
		memoryFactor = 1.0 - (metrics.MemoryPressure-0.8)*2.0*t.config.TuningAggressiveness
	} else if metrics.MemoryPressure < 0.4 {
		// Low memory pressure, can increase GOGC for better performance
		memoryFactor = 1.0 + (0.4-metrics.MemoryPressure)*1.5*t.config.TuningAggressiveness
	}

	// Factor 3: GC frequency adjustment
	frequencyFactor := 1.0
	if metrics.GCFrequency > 2.0 {
		// Too frequent GCs, increase GOGC
		frequencyFactor = 1.0 + (metrics.GCFrequency-2.0)*0.1*t.config.TuningAggressiveness
	} else if metrics.GCFrequency < 0.1 {
		// Very infrequent GCs, might decrease GOGC
		frequencyFactor = 1.0 - (0.1-metrics.GCFrequency)*0.5*t.config.TuningAggressiveness
	}

	// Combine factors
	combinedFactor := (latencyFactor + memoryFactor + frequencyFactor) / 3.0

	// Apply exponential smoothing to avoid rapid changes
	alpha := 0.3 // Smoothing factor
	smoothedFactor := alpha*combinedFactor + (1-alpha)*1.0

	targetGOGC := int(float64(currentGOGC) * smoothedFactor)

	return targetGOGC
}

// calculateConfidence determines confidence in the tuning decision
func (t *Tuner) calculateConfidence(metrics Metrics) float64 {
	confidence := 1.0

	// Reduce confidence if we don't have enough history
	if len(t.metricsHistory) < 5 {
		confidence *= 0.7
	}

	// Reduce confidence if metrics are unstable
	if len(t.metricsHistory) >= 3 {
		recent := t.metricsHistory[len(t.metricsHistory)-3:]
		pauseVariation := calculateVariation(recent, func(m Metrics) float64 {
			return float64(m.GCPauseTime)
		})

		if pauseVariation > 0.3 {
			confidence *= 0.8
		}
	}

	// Reduce confidence if we're near limits
	if metrics.CurrentGOGC <= t.config.MinGOGC+20 || metrics.CurrentGOGC >= t.config.MaxGOGC-20 {
		confidence *= 0.9
	}

	// Reduce confidence if memory pressure is extreme
	if metrics.MemoryPressure > 0.95 || metrics.MemoryPressure < 0.05 {
		confidence *= 0.8
	}

	return confidence
}

// buildReasonString creates a human-readable reason for the tuning decision
func (t *Tuner) buildReasonString(metrics Metrics, oldGOGC, newGOGC int) string {
	reasons := []string{}

	if metrics.GCPauseTime > t.config.TargetLatency {
		reasons = append(reasons, fmt.Sprintf("GC pause %.2fms > target %.2fms",
			float64(metrics.GCPauseTime)/1e6, float64(t.config.TargetLatency)/1e6))
	}

	if metrics.MemoryPressure > 0.8 {
		reasons = append(reasons, fmt.Sprintf("High memory pressure %.1f%%", metrics.MemoryPressure*100))
	}

	if metrics.GCFrequency > 2.0 {
		reasons = append(reasons, fmt.Sprintf("High GC frequency %.1f/sec", metrics.GCFrequency))
	}

	direction := "increasing"
	if newGOGC < oldGOGC {
		direction = "decreasing"
	}

	if len(reasons) == 0 {
		return fmt.Sprintf("Optimizing performance by %s GOGC %d -> %d", direction, oldGOGC, newGOGC)
	}

	return fmt.Sprintf("%s GOGC %d -> %d due to: %s",
		direction, oldGOGC, newGOGC, joinStrings(reasons, ", "))
}

// applyTuningDecision applies the tuning decision and records it
func (t *Tuner) applyTuningDecision(decision TuningDecision) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Apply the GOGC change
	oldGOGC := debug.SetGCPercent(decision.NewGOGC)
	decision.OldGOGC = oldGOGC // Ensure we have the actual old value

	// Record the decision
	t.decisionHistory = append(t.decisionHistory, decision)
	if len(t.decisionHistory) > t.maxDecisions {
		t.decisionHistory = t.decisionHistory[1:]
	}

	t.totalDecisions++
	t.lastGOGC = decision.NewGOGC
	t.stabilityCount = 0

	t.config.Logger.Info("Applied GC tuning: %s (confidence: %.2f)",
		decision.Reason, decision.Confidence)

	// Trigger callback
	if t.onTuningDecision != nil {
		t.onTuningDecision(decision)
	}
}

// shouldSkipDueToOscillation checks if we should skip tuning to prevent oscillation
func (t *Tuner) shouldSkipDueToOscillation() bool {
	if len(t.decisionHistory) < 4 {
		return false
	}

	// Check for rapid back-and-forth changes
	recent := t.decisionHistory[len(t.decisionHistory)-4:]

	// Look for alternating increase/decrease pattern
	increaseCount := 0
	decreaseCount := 0

	for i := 0; i < len(recent); i++ {
		if recent[i].NewGOGC > recent[i].OldGOGC {
			increaseCount++
		} else {
			decreaseCount++
		}
	}

	// If we have both increases and decreases in recent history, we might be oscillating
	if increaseCount > 0 && decreaseCount > 0 {
		// Check if decisions are within the stabilization window
		now := time.Now()
		oldestDecision := recent[0].Timestamp

		if now.Sub(oldestDecision) < t.config.StabilizationWindow {
			t.config.Logger.Debug("Detected potential oscillation, skipping tuning")
			return true
		}
	}

	return false
}

// Helper functions

func validateConfig(config *Config) error {
	if config.MonitorInterval < time.Second {
		return fmt.Errorf("monitor interval must be at least 1 second")
	}
	if config.MinGOGC < 10 || config.MinGOGC > 1000 {
		return fmt.Errorf("min GOGC must be between 10 and 1000")
	}
	if config.MaxGOGC < config.MinGOGC || config.MaxGOGC > 2000 {
		return fmt.Errorf("max GOGC must be between min GOGC and 2000")
	}
	if config.TuningAggressiveness < 0.1 || config.TuningAggressiveness > 2.0 {
		return fmt.Errorf("tuning aggressiveness must be between 0.1 and 2.0")
	}
	if config.MemoryLimitPercent < 0.1 || config.MemoryLimitPercent > 1.0 {
		return fmt.Errorf("memory limit percent must be between 0.1 and 1.0")
	}
	return nil
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func calculateVariation(metrics []Metrics, extractor func(Metrics) float64) float64 {
	if len(metrics) < 2 {
		return 0
	}

	values := make([]float64, len(metrics))
	for i, m := range metrics {
		values[i] = extractor(m)
	}

	// Calculate coefficient of variation
	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))

	if mean == 0 {
		return 0
	}

	variance := 0.0
	for _, v := range values {
		variance += (v - mean) * (v - mean)
	}
	variance /= float64(len(values))

	stdDev := math.Sqrt(variance)
	return stdDev / mean
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	if len(strs) == 1 {
		return strs[0]
	}

	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
