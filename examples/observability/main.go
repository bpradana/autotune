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
	log.Println("Starting autotune with full observability example...")

	// Create tuner configuration
	tunerConfig := &autotune.Config{
		MonitorInterval:      15 * time.Second,
		MinGOGC:              50,
		MaxGOGC:              500,
		TargetLatency:        8 * time.Millisecond,
		MemoryLimitPercent:   0.8,
		TuningAggressiveness: 0.3,
		StabilizationWindow:  3 * time.Minute,
		MaxChangePerInterval: 40,
		Logger:               &observabilityLogger{},
	}

	tuner, err := autotune.NewTuner(tunerConfig)
	if err != nil {
		log.Fatal("Failed to create tuner:", err)
	}

	// Create observability configuration
	obsConfig := &autotune.ObservabilityConfig{
		HTTPPort:          8080,
		MetricsPath:       "/metrics",
		EnablePrometheus:  true,
		EnableJSONMetrics: true,
		MetricsRetention:  2 * time.Hour,
	}

	// Create observability server
	obsServer := autotune.NewObservabilityServer(obsConfig, tuner)

	// Create alert manager with custom observers
	alertManager := autotune.NewAlertManager(tuner)
	alertManager.AddObserver(autotune.NewLogAlertObserver(tunerConfig.Logger))
	alertManager.AddObserver(&customAlertObserver{})

	// Set up tuner callbacks
	tuner.SetOnTuningDecision(func(decision autotune.TuningDecision) {
		log.Printf("🎯 Tuning Decision: GOGC %d → %d (%s) [confidence: %.2f]",
			decision.OldGOGC, decision.NewGOGC, decision.Reason, decision.Confidence)
	})

	tuner.SetOnMetricsUpdate(func(metrics autotune.Metrics) {
		log.Printf("📊 Metrics: pause=%.1fms, freq=%.1f/s, pressure=%.0f%%, gogc=%d",
			float64(metrics.GCPauseTime)/1e6,
			metrics.GCFrequency,
			metrics.MemoryPressure*100,
			metrics.CurrentGOGC)
	})

	// Start all services
	if err := obsServer.Start(); err != nil {
		log.Fatal("Failed to start observability server:", err)
	}
	defer obsServer.Stop()

	if err := tuner.Start(); err != nil {
		log.Fatal("Failed to start tuner:", err)
	}
	defer tuner.Stop()

	log.Println("✅ All services started successfully!")
	log.Println("🌐 Observability endpoints available:")
	log.Println("   📊 Metrics (JSON):       http://localhost:8080/metrics?format=json")
	log.Println("   📈 Metrics (Prometheus): http://localhost:8080/metrics?format=prometheus")
	log.Println("   ❤️  Health Check:        http://localhost:8080/health")
	log.Println("   📋 Statistics:           http://localhost:8080/stats")
	log.Println("   ⚙️  Configuration:       http://localhost:8080/config")
	log.Println("   🎯 Recent Decisions:     http://localhost:8080/decisions")

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

	// Start workload simulation
	go simulateObservabilityWorkload(ctx)

	// Start periodic reporting
	go periodicReporting(ctx, tuner)

	// Start metrics export demonstration
	go demonstrateMetricsExport(ctx, tuner)

	// Wait for shutdown signal
	<-ctx.Done()

	// Print final observability report
	printFinalObservabilityReport(tuner, obsServer)
	log.Println("👋 Observability example completed!")
}

// simulateObservabilityWorkload creates workload patterns that trigger various alerts and metrics
func simulateObservabilityWorkload(ctx context.Context) {
	log.Println("🏃 Starting observability workload simulation...")

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// Different workload scenarios
	scenarios := []string{"normal", "memory_pressure", "allocation_burst", "gc_pressure", "recovery"}
	currentScenario := 0
	scenarioTimer := time.NewTicker(45 * time.Second)
	defer scenarioTimer.Stop()

	var workloadData [][]byte

	for {
		select {
		case <-ctx.Done():
			return
		case <-scenarioTimer.C:
			currentScenario = (currentScenario + 1) % len(scenarios)
			log.Printf("🔄 Switching to scenario: %s", scenarios[currentScenario])
		case <-ticker.C:
			scenario := scenarios[currentScenario]

			switch scenario {
			case "normal":
				// Normal allocation pattern
				for i := 0; i < 50; i++ {
					data := make([]byte, 4096)
					workloadData = append(workloadData, data)
				}
				// Clean up old data
				if len(workloadData) > 500 {
					workloadData = workloadData[:100]
				}

			case "memory_pressure":
				// High memory pressure scenario
				log.Println("🔴 Simulating high memory pressure...")
				for i := 0; i < 100; i++ {
					data := make([]byte, 64*1024) // 64KB allocations
					workloadData = append(workloadData, data)
				}

			case "allocation_burst":
				// Allocation burst scenario
				log.Println("💥 Simulating allocation burst...")
				for i := 0; i < 1000; i++ {
					data := make([]byte, 8192) // 8KB allocations
					workloadData = append(workloadData, data)
				}

			case "gc_pressure":
				// GC pressure scenario - frequent allocations
				log.Println("⚡ Simulating GC pressure...")
				for i := 0; i < 2000; i++ {
					data := make([]byte, 1024) // Many small allocations
					_ = data                   // Don't keep references - should trigger frequent GC
				}

			case "recovery":
				// Recovery scenario - cleanup
				log.Println("🧹 Simulating recovery (cleanup)...")
				workloadData = workloadData[:0]
				runtime.GC()
				runtime.GC() // Force cleanup
			}
		}
	}
}

// periodicReporting demonstrates periodic metrics reporting
func periodicReporting(ctx context.Context, tuner *autotune.Tuner) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	reportCounter := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reportCounter++
			log.Printf("📊 PERIODIC REPORT #%d", reportCounter)

			// Get current metrics
			metrics := tuner.GetMetrics()
			stats := tuner.GetStats()

			log.Printf("   Current GOGC: %d", metrics.CurrentGOGC)
			log.Printf("   GC Pause: %.2fms", float64(metrics.GCPauseTime)/1e6)
			log.Printf("   Memory Pressure: %.1f%%", metrics.MemoryPressure*100)
			log.Printf("   Total Decisions: %d", stats["total_decisions"])
			log.Printf("   Success Rate: %.1f%%", calculateSuccessRate(stats))

			// Memory statistics
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)
			log.Printf("   Heap Objects: %d", memStats.HeapObjects)
			log.Printf("   GC CPU Fraction: %.4f", memStats.GCCPUFraction)
		}
	}
}

// demonstrateMetricsExport shows how to export metrics in different formats
func demonstrateMetricsExport(ctx context.Context, tuner *autotune.Tuner) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	exporter := autotune.NewMetricsExporter(tuner)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			log.Println("📤 METRICS EXPORT DEMONSTRATION")

			// Export to JSON
			jsonData, err := exporter.ExportToJSON()
			if err != nil {
				log.Printf("❌ JSON export failed: %v", err)
			} else {
				log.Printf("✅ JSON export successful (%d bytes)", len(jsonData))
				// In a real application, you might send this to a monitoring system
			}

			// Export to Prometheus format
			promData, err := exporter.ExportToPrometheus()
			if err != nil {
				log.Printf("❌ Prometheus export failed: %v", err)
			} else {
				log.Printf("✅ Prometheus export successful (%d bytes)", len(promData))
				// Show a sample of the Prometheus data
				lines := splitLines(promData)
				if len(lines) > 0 {
					log.Printf("   Sample metric: %s", lines[0])
				}
			}
		}
	}
}

// printFinalObservabilityReport prints a comprehensive final report
func printFinalObservabilityReport(tuner *autotune.Tuner, obsServer *autotune.ObservabilityServer) {
	log.Printf("📊 FINAL OBSERVABILITY REPORT")
	log.Printf(strings.Repeat("=", 60))

	// Tuner statistics
	stats := tuner.GetStats()
	metrics := tuner.GetMetrics()

	log.Printf("Tuning Statistics:")
	log.Printf("  Total Decisions: %d", stats["total_decisions"])
	log.Printf("  Successful Tunes: %d", stats["successful_tunes"])
	log.Printf("  Reverted Tunes: %d", stats["reverted_tunes"])
	log.Printf("  Success Rate: %.1f%%", calculateSuccessRate(stats))
	log.Printf("  Final GOGC: %d", stats["current_gogc"])

	log.Printf("\nFinal Metrics:")
	log.Printf("  GC Pause Time: %.2fms", float64(metrics.GCPauseTime)/1e6)
	log.Printf("  GC Frequency: %.2f/sec", metrics.GCFrequency)
	log.Printf("  Memory Pressure: %.1f%%", metrics.MemoryPressure*100)
	log.Printf("  Heap Utilization: %s / %s",
		formatBytes(metrics.HeapInuse), formatBytes(metrics.HeapSize))

	if metrics.ContainerMemLimit > 0 {
		log.Printf("  Container Memory: %s", formatBytes(metrics.ContainerMemLimit))
	}

	// Runtime statistics
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	log.Printf("\nRuntime Statistics:")
	log.Printf("  Total GC Cycles: %d", memStats.NumGC)
	log.Printf("  GC CPU Fraction: %.4f", memStats.GCCPUFraction)
	log.Printf("  Heap Objects: %d", memStats.HeapObjects)

	// Export final metrics for external systems
	exporter := autotune.NewMetricsExporter(tuner)
	if jsonData, err := exporter.ExportToJSON(); err == nil {
		log.Printf("\nFinal JSON Export: %d bytes", len(jsonData))
	}
	if promData, err := exporter.ExportToPrometheus(); err == nil {
		log.Printf("Final Prometheus Export: %d bytes", len(promData))
	}

	log.Printf(strings.Repeat("=", 60))
}

// Helper functions

func calculateSuccessRate(stats map[string]interface{}) float64 {
	total := stats["total_decisions"].(int64)
	if total == 0 {
		return 0
	}
	successful := stats["successful_tunes"].(int64)
	return float64(successful) / float64(total) * 100
}

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

func splitLines(s string) []string {
	var lines []string
	for _, line := range []string{} {
		if line != "" {
			lines = append(lines, line)
		}
	}
	// Simple split by newline for demonstration
	current := ""
	for _, char := range s {
		if char == '\n' {
			if current != "" {
				lines = append(lines, current)
				current = ""
			}
		} else {
			current += string(char)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// Custom implementations

type observabilityLogger struct{}

func (l *observabilityLogger) Debug(msg string, fields ...interface{}) {
	log.Printf("[🔍 DEBUG] "+msg, fields...)
}

func (l *observabilityLogger) Info(msg string, fields ...interface{}) {
	log.Printf("[ℹ️  INFO] "+msg, fields...)
}

func (l *observabilityLogger) Warn(msg string, fields ...interface{}) {
	log.Printf("[⚠️  WARN] "+msg, fields...)
}

func (l *observabilityLogger) Error(msg string, fields ...interface{}) {
	log.Printf("[❌ ERROR] "+msg, fields...)
}

type customAlertObserver struct{}

func (c *customAlertObserver) OnAlert(alert autotune.Alert) {
	emoji := "ℹ️"
	switch alert.Level {
	case autotune.AlertLevelWarning:
		emoji = "⚠️"
	case autotune.AlertLevelCritical:
		emoji = "🚨"
	}

	log.Printf("%s ALERT [%s]: %s", emoji, alert.Level, alert.Message)
	if alert.Resolution != "" {
		log.Printf("   💡 Resolution: %s", alert.Resolution)
	}

	// In a real implementation, you might:
	// - Send to external alerting system (PagerDuty, Slack, etc.)
	// - Write to metrics database
	// - Trigger automated remediation
}
