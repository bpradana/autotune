package autotune

import (
	"runtime/debug"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultConfig tests the default configuration
func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, 30*time.Second, config.MonitorInterval)
	assert.Equal(t, 50, config.MinGOGC)
	assert.Equal(t, 800, config.MaxGOGC)
	assert.Equal(t, 10*time.Millisecond, config.TargetLatency)
	assert.Equal(t, 0.8, config.MemoryLimitPercent)
	assert.Equal(t, 0.3, config.TuningAggressiveness)
	assert.Equal(t, 5*time.Minute, config.StabilizationWindow)
	assert.Equal(t, 50, config.MaxChangePerInterval)
	assert.NotNil(t, config.Logger)
}

// TestConfigValidation tests configuration validation
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "invalid monitor interval",
			config: &Config{
				MonitorInterval:      500 * time.Millisecond,
				MinGOGC:              50,
				MaxGOGC:              800,
				TargetLatency:        10 * time.Millisecond,
				MemoryLimitPercent:   0.8,
				TuningAggressiveness: 0.3,
				StabilizationWindow:  5 * time.Minute,
				MaxChangePerInterval: 50,
				Logger:               &defaultLogger{},
			},
			wantErr: true,
		},
		{
			name: "invalid GOGC range",
			config: &Config{
				MonitorInterval:      30 * time.Second,
				MinGOGC:              900,
				MaxGOGC:              800,
				TargetLatency:        10 * time.Millisecond,
				MemoryLimitPercent:   0.8,
				TuningAggressiveness: 0.3,
				StabilizationWindow:  5 * time.Minute,
				MaxChangePerInterval: 50,
				Logger:               &defaultLogger{},
			},
			wantErr: true,
		},
		{
			name: "invalid tuning aggressiveness",
			config: &Config{
				MonitorInterval:      30 * time.Second,
				MinGOGC:              50,
				MaxGOGC:              800,
				TargetLatency:        10 * time.Millisecond,
				MemoryLimitPercent:   0.8,
				TuningAggressiveness: 3.0,
				StabilizationWindow:  5 * time.Minute,
				MaxChangePerInterval: 50,
				Logger:               &defaultLogger{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestNewTuner tests tuner creation
func TestNewTuner(t *testing.T) {
	tuner, err := NewTuner(nil)
	require.NoError(t, err)
	assert.NotNil(t, tuner)
	assert.NotNil(t, tuner.config)
	assert.NotNil(t, tuner.ctx)
	assert.NotNil(t, tuner.cancel)
	assert.False(t, tuner.running)

	// Test with custom config
	config := DefaultConfig()
	config.MinGOGC = 100
	tuner2, err := NewTuner(config)
	require.NoError(t, err)
	assert.Equal(t, 100, tuner2.config.MinGOGC)
}

// TestTunerStartStop tests starting and stopping the tuner
func TestTunerStartStop(t *testing.T) {
	config := DefaultConfig()
	config.MonitorInterval = 1000 * time.Millisecond

	tuner, err := NewTuner(config)
	require.NoError(t, err)

	// Test starting
	err = tuner.Start()
	assert.NoError(t, err)
	assert.True(t, tuner.running)

	// Test starting again should fail
	err = tuner.Start()
	assert.Error(t, err)

	// Wait a bit for monitoring to occur
	time.Sleep(200 * time.Millisecond)

	// Test stopping
	err = tuner.Stop()
	assert.NoError(t, err)
	assert.False(t, tuner.running)

	// Test stopping again should fail
	err = tuner.Stop()
	assert.Error(t, err)
}

// TestMetricsCollection tests metrics collection
func TestMetricsCollection(t *testing.T) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	metrics := tuner.GetMetrics()

	assert.NotZero(t, metrics.Timestamp)
	assert.NotZero(t, metrics.HeapSize)
	assert.NotZero(t, metrics.HeapAlloc)
	assert.GreaterOrEqual(t, metrics.CurrentGOGC, 0)
}

// TestTuningDecision tests tuning decision making
func TestTuningDecision(t *testing.T) {
	config := DefaultConfig()
	config.MinGOGC = 50
	config.MaxGOGC = 800
	config.TargetLatency = 10 * time.Millisecond
	config.TuningAggressiveness = 0.5

	tuner, err := NewTuner(config)
	require.NoError(t, err)

	// Add some history
	for i := 0; i < 5; i++ {
		metrics := Metrics{
			GCPauseTime:    20 * time.Millisecond, // High pause time
			GCFrequency:    1.0,
			MemoryPressure: 0.5,
			CurrentGOGC:    100,
			Timestamp:      time.Now(),
		}
		tuner.metricsHistory = append(tuner.metricsHistory, metrics)
	}

	// Test decision making
	currentMetrics := Metrics{
		GCPauseTime:    25 * time.Millisecond, // Even higher pause time
		GCFrequency:    1.2,
		MemoryPressure: 0.6,
		CurrentGOGC:    100,
		Timestamp:      time.Now(),
	}

	decision := tuner.makeTuningDecision(currentMetrics)

	if decision != nil {
		assert.Equal(t, 100, decision.OldGOGC)
		assert.NotEqual(t, 100, decision.NewGOGC)
		assert.NotEmpty(t, decision.Reason)
		assert.Greater(t, decision.Confidence, 0.0)
		assert.LessOrEqual(t, decision.Confidence, 1.0)
	}
}

// TestAntiOscillation tests anti-oscillation logic
func TestAntiOscillation(t *testing.T) {
	config := DefaultConfig()
	config.StabilizationWindow = 1 * time.Second

	tuner, err := NewTuner(config)
	require.NoError(t, err)

	// Add alternating decisions to history
	now := time.Now()
	decisions := []TuningDecision{
		{OldGOGC: 100, NewGOGC: 150, Timestamp: now.Add(-800 * time.Millisecond)},
		{OldGOGC: 150, NewGOGC: 100, Timestamp: now.Add(-600 * time.Millisecond)},
		{OldGOGC: 100, NewGOGC: 150, Timestamp: now.Add(-400 * time.Millisecond)},
		{OldGOGC: 150, NewGOGC: 100, Timestamp: now.Add(-200 * time.Millisecond)},
	}

	tuner.decisionHistory = decisions

	// Should skip due to oscillation
	shouldSkip := tuner.shouldSkipDueToOscillation()
	assert.True(t, shouldSkip)

	// Test with older decisions (outside window)
	oldDecisions := []TuningDecision{
		{OldGOGC: 100, NewGOGC: 150, Timestamp: now.Add(-2 * time.Second)},
		{OldGOGC: 150, NewGOGC: 100, Timestamp: now.Add(-3 * time.Second)},
	}

	tuner.decisionHistory = oldDecisions
	shouldSkip = tuner.shouldSkipDueToOscillation()
	assert.False(t, shouldSkip)
}

// TestCalculateTargetGOGC tests GOGC calculation
func TestCalculateTargetGOGC(t *testing.T) {
	config := DefaultConfig()
	tuner, err := NewTuner(config)
	require.NoError(t, err)

	// Test with high pause time (should increase GOGC)
	metrics := Metrics{
		GCPauseTime:    50 * time.Millisecond, // 5x target
		GCFrequency:    1.0,
		MemoryPressure: 0.5,
		CurrentGOGC:    100,
	}

	targetGOGC := tuner.calculateTargetGOGC(metrics)
	assert.Greater(t, targetGOGC, 100)

	// Test with low pause time and high memory pressure (should decrease GOGC)
	metrics = Metrics{
		GCPauseTime:    2 * time.Millisecond, // Below target
		GCFrequency:    1.0,
		MemoryPressure: 0.9, // High pressure
		CurrentGOGC:    100,
	}

	targetGOGC = tuner.calculateTargetGOGC(metrics)
	assert.Less(t, targetGOGC, 100)
}

// TestCalculateConfidence tests confidence calculation
func TestCalculateConfidence(t *testing.T) {
	config := DefaultConfig()
	tuner, err := NewTuner(config)
	require.NoError(t, err)

	// Test with insufficient history
	metrics := Metrics{CurrentGOGC: 100}
	confidence := tuner.calculateConfidence(metrics)
	assert.Less(t, confidence, 1.0)

	// Test with stable history
	for i := 0; i < 10; i++ {
		tuner.metricsHistory = append(tuner.metricsHistory, Metrics{
			GCPauseTime: 10 * time.Millisecond,
			CurrentGOGC: 100,
		})
	}

	confidence = tuner.calculateConfidence(metrics)
	assert.Greater(t, confidence, 0.5)
}

// TestCallbacks tests callback functionality
func TestCallbacks(t *testing.T) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	var receivedMetrics Metrics
	var receivedDecision TuningDecision
	var metricsCallbackCalled bool
	var decisionCallbackCalled bool

	tuner.SetOnMetricsUpdate(func(metrics Metrics) {
		receivedMetrics = metrics
		metricsCallbackCalled = true
	})

	tuner.SetOnTuningDecision(func(decision TuningDecision) {
		receivedDecision = decision
		decisionCallbackCalled = true
	})

	// Trigger metrics callback
	if tuner.onMetricsUpdate != nil {
		testMetrics := Metrics{CurrentGOGC: 100}
		tuner.onMetricsUpdate(testMetrics)
	}

	// Trigger decision callback
	if tuner.onTuningDecision != nil {
		testDecision := TuningDecision{OldGOGC: 100, NewGOGC: 150}
		tuner.onTuningDecision(testDecision)
	}

	assert.True(t, metricsCallbackCalled)
	assert.True(t, decisionCallbackCalled)
	assert.Equal(t, 100, receivedMetrics.CurrentGOGC)
	assert.Equal(t, 100, receivedDecision.OldGOGC)
	assert.Equal(t, 150, receivedDecision.NewGOGC)
}

// TestThreadSafety tests thread safety
func TestThreadSafety(t *testing.T) {
	config := DefaultConfig()
	config.MonitorInterval = 1000 * time.Millisecond

	tuner, err := NewTuner(config)
	require.NoError(t, err)

	var wg sync.WaitGroup

	// Start the tuner
	err = tuner.Start()
	require.NoError(t, err)

	// Run multiple goroutines accessing tuner methods
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				metrics := tuner.GetMetrics()
				assert.NotZero(t, metrics.Timestamp)

				stats := tuner.GetStats()
				assert.NotNil(t, stats)

				// Small delay to allow other goroutines to run
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()

	err = tuner.Stop()
	assert.NoError(t, err)
}

// TestStatistics tests statistics collection
func TestStatistics(t *testing.T) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	stats := tuner.GetStats()

	assert.Contains(t, stats, "total_decisions")
	assert.Contains(t, stats, "successful_tunes")
	assert.Contains(t, stats, "reverted_tunes")
	assert.Contains(t, stats, "current_gogc")
	assert.Contains(t, stats, "running")

	assert.Equal(t, int64(0), stats["total_decisions"])
	assert.Equal(t, int64(0), stats["successful_tunes"])
	assert.Equal(t, int64(0), stats["reverted_tunes"])
	assert.Equal(t, false, stats["running"])
}

// TestRealGOGCApplication tests that GOGC is actually applied
func TestRealGOGCApplication(t *testing.T) {
	originalGOGC := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(originalGOGC)

	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	// Create a decision
	decision := TuningDecision{
		OldGOGC:    100,
		NewGOGC:    200,
		Reason:     "Test",
		Confidence: 0.8,
		Timestamp:  time.Now(),
	}

	tuner.applyTuningDecision(decision)

	// Check that GOGC was actually set
	currentGOGC := debug.SetGCPercent(-1)
	assert.Equal(t, 200, currentGOGC)

	// Check that decision was recorded
	assert.Len(t, tuner.decisionHistory, 1)
	assert.Equal(t, int64(1), tuner.totalDecisions)
}

// TestHelperFunctions tests helper functions
func TestHelperFunctions(t *testing.T) {
	// Test abs function
	assert.Equal(t, 5, abs(5))
	assert.Equal(t, 5, abs(-5))
	assert.Equal(t, 0, abs(0))

	// Test joinStrings function
	assert.Equal(t, "", joinStrings([]string{}, ", "))
	assert.Equal(t, "a", joinStrings([]string{"a"}, ", "))
	assert.Equal(t, "a, b, c", joinStrings([]string{"a", "b", "c"}, ", "))

	// Test calculateVariation function
	metrics := []Metrics{
		{GCPauseTime: 10 * time.Millisecond},
		{GCPauseTime: 20 * time.Millisecond},
		{GCPauseTime: 30 * time.Millisecond},
	}

	variation := calculateVariation(metrics, func(m Metrics) float64 {
		return float64(m.GCPauseTime)
	})
	assert.Greater(t, variation, 0.0)

	// Test with empty metrics
	variation = calculateVariation([]Metrics{}, func(m Metrics) float64 {
		return float64(m.GCPauseTime)
	})
	assert.Equal(t, 0.0, variation)
}

// TestBoundaryConditions tests boundary conditions
func TestBoundaryConditions(t *testing.T) {
	config := DefaultConfig()
	config.MinGOGC = 50
	config.MaxGOGC = 800
	config.MaxChangePerInterval = 50

	tuner, err := NewTuner(config)
	require.NoError(t, err)

	// Test that GOGC is bounded
	metrics := Metrics{
		GCPauseTime:    1 * time.Millisecond, // Very low, should want to decrease GOGC
		GCFrequency:    0.1,
		MemoryPressure: 0.9, // High pressure, should want to decrease GOGC
		CurrentGOGC:    60,  // Close to minimum
	}

	// Add some history
	for i := 0; i < 5; i++ {
		tuner.metricsHistory = append(tuner.metricsHistory, metrics)
	}

	decision := tuner.makeTuningDecision(metrics)

	if decision != nil {
		assert.GreaterOrEqual(t, decision.NewGOGC, config.MinGOGC)
		assert.LessOrEqual(t, decision.NewGOGC, config.MaxGOGC)
		assert.LessOrEqual(t, abs(decision.NewGOGC-decision.OldGOGC), config.MaxChangePerInterval)
	}
}

// TestDefaultLogger tests the default logger
func TestDefaultLogger(t *testing.T) {
	logger := &defaultLogger{}

	// These should not panic
	logger.Debug("test debug")
	logger.Info("test info")
	logger.Warn("test warn")
	logger.Error("test error")

	// Test with fields
	logger.Debug("test debug %s", "field")
	logger.Info("test info %d", 42)
	logger.Warn("test warn %f", 3.14)
	logger.Error("test error %v", []int{1, 2, 3})
}

// BenchmarkMetricsCollection benchmarks metrics collection
func BenchmarkMetricsCollection(b *testing.B) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tuner.collectMetrics()
	}
}

// BenchmarkTuningDecision benchmarks tuning decision making
func BenchmarkTuningDecision(b *testing.B) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(b, err)

	// Add some history
	for i := 0; i < 10; i++ {
		metrics := Metrics{
			GCPauseTime:    10 * time.Millisecond,
			GCFrequency:    1.0,
			MemoryPressure: 0.5,
			CurrentGOGC:    100,
			Timestamp:      time.Now(),
		}
		tuner.metricsHistory = append(tuner.metricsHistory, metrics)
	}

	testMetrics := Metrics{
		GCPauseTime:    15 * time.Millisecond,
		GCFrequency:    1.2,
		MemoryPressure: 0.6,
		CurrentGOGC:    100,
		Timestamp:      time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tuner.makeTuningDecision(testMetrics)
	}
}

// TestEdgeCases tests various edge cases
func TestEdgeCases(t *testing.T) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	// Test with zero values
	metrics := Metrics{}
	decision := tuner.makeTuningDecision(metrics)
	// Should not panic and should return nil or valid decision

	// Test with extreme values
	metrics = Metrics{
		GCPauseTime:    1 * time.Hour, // Extremely high
		GCFrequency:    1000,          // Extremely high
		MemoryPressure: 1.0,           // Maximum
		CurrentGOGC:    100,
		Timestamp:      time.Now(),
	}

	// Add some history
	for i := 0; i < 5; i++ {
		tuner.metricsHistory = append(tuner.metricsHistory, metrics)
	}

	decision = tuner.makeTuningDecision(metrics)
	// Should not panic and should return reasonable values
	if decision != nil {
		assert.GreaterOrEqual(t, decision.NewGOGC, tuner.config.MinGOGC)
		assert.LessOrEqual(t, decision.NewGOGC, tuner.config.MaxGOGC)
	}
}

// TestConcurrentAccess tests concurrent access to tuner
func TestConcurrentAccess(t *testing.T) {
	tuner, err := NewTuner(DefaultConfig())
	require.NoError(t, err)

	var wg sync.WaitGroup

	// Start multiple goroutines that access tuner concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				tuner.GetMetrics()
				tuner.GetStats()

				// Simulate some work
				time.Sleep(time.Microsecond)
			}
		}()
	}

	wg.Wait()
	// Should not panic or race
}
