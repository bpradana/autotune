package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/bpradana/autotune"
)

func main() {
	log.Println("Starting advanced autotune example with custom configuration...")

	// Create custom configuration
	config := &autotune.Config{
		MonitorInterval:      10 * time.Second,     // Check every 10 seconds
		MinGOGC:              75,                   // Minimum GOGC value
		MaxGOGC:              600,                  // Maximum GOGC value
		TargetLatency:        5 * time.Millisecond, // Target GC pause time
		MemoryLimitPercent:   0.85,                 // Use 85% of container memory
		TuningAggressiveness: 0.4,                  // More aggressive tuning
		StabilizationWindow:  2 * time.Minute,      // Anti-oscillation window
		MaxChangePerInterval: 30,                   // Limit GOGC changes
		Logger:               &customLogger{},      // Custom logger
	}

	tuner, err := autotune.NewTuner(config)
	if err != nil {
		log.Fatal("Failed to create tuner:", err)
	}

	// Set up comprehensive callbacks
	tuner.SetOnTuningDecision(func(decision autotune.TuningDecision) {
		log.Printf("🎯 TUNING DECISION")
		log.Printf("   Time: %s", decision.Timestamp.Format(time.RFC3339))
		log.Printf("   GOGC: %d → %d (change: %+d)",
			decision.OldGOGC, decision.NewGOGC, decision.NewGOGC-decision.OldGOGC)
		log.Printf("   Reason: %s", decision.Reason)
		log.Printf("   Confidence: %.2f", decision.Confidence)
		if decision.Metrics != nil {
			log.Printf("   Metrics: pause=%.2fms, freq=%.1f/s, pressure=%.1f%%",
				float64(decision.Metrics.GCPauseTime)/1e6,
				decision.Metrics.GCFrequency,
				decision.Metrics.MemoryPressure*100)
		}
	})

	tuner.SetOnMetricsUpdate(func(metrics autotune.Metrics) {
		log.Printf("📊 METRICS UPDATE")
		log.Printf("   GC Pause: %.2fms", float64(metrics.GCPauseTime)/1e6)
		log.Printf("   GC Frequency: %.1f/s", metrics.GCFrequency)
		log.Printf("   Memory Pressure: %.1f%%", metrics.MemoryPressure*100)
		log.Printf("   Heap: %s allocated, %s in use",
			formatBytes(metrics.HeapAlloc), formatBytes(metrics.HeapInuse))
		log.Printf("   Current GOGC: %d", metrics.CurrentGOGC)
		if metrics.ContainerMemLimit > 0 {
			log.Printf("   Container Memory Limit: %s", formatBytes(metrics.ContainerMemLimit))
		}
		if metrics.ContainerCPULimit > 0 {
			log.Printf("   Container CPU Limit: %.1f cores", metrics.ContainerCPULimit)
		}
	})

	// Start tuning
	if err := tuner.Start(); err != nil {
		log.Fatal("Failed to start tuner:", err)
	}
	defer func() {
		log.Println("Stopping tuner...")
		tuner.Stop()
	}()

	log.Println("✅ Advanced autotune started successfully!")
	log.Println("🔧 Using custom configuration with aggressive tuning")

	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signals
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		log.Println("🛑 Received interrupt signal, shutting down...")
		cancel()
	}()

	// Start a more complex workload
	go simulateComplexWorkload(ctx)

	// Periodically print statistics
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				printStatistics(tuner)
			}
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()

	// Print final comprehensive statistics
	printFinalStatistics(tuner)
	log.Println("👋 Advanced example completed!")
}

// simulateComplexWorkload creates varied allocation patterns
func simulateComplexWorkload(ctx context.Context) {
	log.Println("🏃 Starting complex workload simulation...")

	// Different phases of allocation patterns
	phases := []string{"startup", "steady", "burst", "cleanup"}
	currentPhase := 0

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	phaseTimer := time.NewTicker(20 * time.Second)
	defer phaseTimer.Stop()

	// Storage for different data types
	shortLived := make([][]byte, 0)
	mediumLived := make([][]byte, 0, 500)
	longLived := make([][]byte, 0, 100)

	for {
		select {
		case <-ctx.Done():
			return
		case <-phaseTimer.C:
			currentPhase = (currentPhase + 1) % len(phases)
			log.Printf("🔄 Switching to phase: %s", phases[currentPhase])
		case <-ticker.C:
			switch phases[currentPhase] {
			case "startup":
				// Gradual ramp-up
				for i := 0; i < 50; i++ {
					data := make([]byte, 2048)
					shortLived = append(shortLived, data)
				}
				// Build up some medium-lived data
				if len(mediumLived) < 200 {
					data := make([]byte, 32*1024)
					mediumLived = append(mediumLived, data)
				}

			case "steady":
				// Steady state allocation
				for i := 0; i < 100; i++ {
					data := make([]byte, 1024)
					shortLived = append(shortLived, data)
				}
				// Occasional medium allocations
				if len(mediumLived) < 300 {
					data := make([]byte, 16*1024)
					mediumLived = append(mediumLived, data)
				}
				// Clean up short-lived data
				if len(shortLived) > 1000 {
					shortLived = shortLived[:100]
				}

			case "burst":
				// High allocation burst
				log.Println("💥 High allocation burst!")
				for i := 0; i < 500; i++ {
					data := make([]byte, 8192)
					shortLived = append(shortLived, data)
				}
				// Large allocations
				for i := 0; i < 5; i++ {
					data := make([]byte, 512*1024)
					longLived = append(longLived, data)
				}

			case "cleanup":
				// Cleanup phase
				log.Println("🧹 Cleanup phase...")
				shortLived = shortLived[:0]
				if len(mediumLived) > 50 {
					mediumLived = mediumLived[:50]
				}
				if len(longLived) > 20 {
					longLived = longLived[:20]
				}
				runtime.GC()
				runtime.GC() // Force thorough cleanup
			}
		}
	}
}

// printStatistics prints current tuning statistics
func printStatistics(tuner *autotune.Tuner) {
	stats := tuner.GetStats()
	metrics := tuner.GetMetrics()

	log.Printf("📈 PERIODIC STATISTICS")
	log.Printf("   Total Decisions: %d", stats["total_decisions"])
	log.Printf("   Successful Tunes: %d", stats["successful_tunes"])
	log.Printf("   Reverted Tunes: %d", stats["reverted_tunes"])
	log.Printf("   Current GOGC: %d", stats["current_gogc"])
	log.Printf("   Stability Count: %d", stats["stability_count"])
	log.Printf("   Running: %v", stats["running"])

	// Runtime statistics
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	log.Printf("   Runtime Stats:")
	log.Printf("     Heap Objects: %d", memStats.HeapObjects)
	log.Printf("     GC Cycles: %d", memStats.NumGC)
	log.Printf("     Last GC: %s ago", time.Since(time.Unix(0, int64(memStats.LastGC))))
	log.Printf("     GC CPU Fraction: %.4f", memStats.GCCPUFraction)

	// Metrics
	log.Printf("   Metrics:")
	log.Printf("      GC Pause Time: %.2fms", float64(metrics.GCPauseTime)/1e6)
	log.Printf("      GC Frequency: %.2f/sec", metrics.GCFrequency)
	log.Printf("      Memory Pressure: %.1f%%", metrics.MemoryPressure*100)
	log.Printf("      Heap Allocated: %s", formatBytes(metrics.HeapAlloc))
	log.Printf("      Heap In Use: %s", formatBytes(metrics.HeapInuse))
	log.Printf("      Heap Size: %s", formatBytes(metrics.HeapSize))
}

// printFinalStatistics prints comprehensive final statistics
func printFinalStatistics(tuner *autotune.Tuner) {
	stats := tuner.GetStats()
	metrics := tuner.GetMetrics()

	log.Printf("📊 FINAL COMPREHENSIVE STATISTICS")
	log.Printf(strings.Repeat("=", 60))
	log.Printf("Tuning Performance:")
	log.Printf("  Total Decisions Made: %d", stats["total_decisions"])
	log.Printf("  Successful Tunes: %d", stats["successful_tunes"])
	log.Printf("  Reverted Tunes: %d", stats["reverted_tunes"])
	if totalDecisions := stats["total_decisions"].(int64); totalDecisions > 0 {
		successRate := float64(stats["successful_tunes"].(int64)) / float64(totalDecisions) * 100
		log.Printf("  Success Rate: %.1f%%", successRate)
	}
	log.Printf("  Final GOGC Value: %d", stats["current_gogc"])
	log.Printf("  Stability Count: %d", stats["stability_count"])

	log.Printf("\nFinal Metrics:")
	log.Printf("  GC Pause Time: %.2fms", float64(metrics.GCPauseTime)/1e6)
	log.Printf("  GC Frequency: %.2f/sec", metrics.GCFrequency)
	log.Printf("  Memory Pressure: %.1f%%", metrics.MemoryPressure*100)
	log.Printf("  Heap Allocated: %s", formatBytes(metrics.HeapAlloc))
	log.Printf("  Heap In Use: %s", formatBytes(metrics.HeapInuse))
	log.Printf("  Heap Size: %s", formatBytes(metrics.HeapSize))

	if metrics.ContainerMemLimit > 0 {
		log.Printf("  Container Memory Limit: %s", formatBytes(metrics.ContainerMemLimit))
		log.Printf("  Memory Utilization: %.1f%%", float64(metrics.HeapInuse)/float64(metrics.ContainerMemLimit)*100)
	}

	if metrics.ContainerCPULimit > 0 {
		log.Printf("  Container CPU Limit: %.1f cores", metrics.ContainerCPULimit)
	}

	log.Printf(strings.Repeat("=", 60))
}

// formatBytes formats byte counts in human-readable format
func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// customLogger implements a custom logger with more detailed output
type customLogger struct{}

func (l *customLogger) Debug(msg string, fields ...interface{}) {
	log.Printf("[DEBUG] "+msg, fields...)
}

func (l *customLogger) Info(msg string, fields ...interface{}) {
	log.Printf("[INFO] "+msg, fields...)
}

func (l *customLogger) Warn(msg string, fields ...interface{}) {
	log.Printf("[WARN] "+msg, fields...)
}

func (l *customLogger) Error(msg string, fields ...interface{}) {
	log.Printf("[ERROR] "+msg, fields...)
}
