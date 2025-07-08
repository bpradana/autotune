package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/bpradana/autotune"
)

func main() {
	log.Println("Starting basic autotune example...")

	// Create tuner with default configuration
	tuner, err := autotune.NewTuner(nil)
	if err != nil {
		log.Fatal("Failed to create tuner:", err)
	}

	// Set up callbacks for monitoring
	tuner.SetOnTuningDecision(func(decision autotune.TuningDecision) {
		log.Printf("🎯 GC Tuning Decision: %s (confidence: %.2f)",
			decision.Reason, decision.Confidence)
		log.Printf("   GOGC: %d → %d", decision.OldGOGC, decision.NewGOGC)
	})

	tuner.SetOnMetricsUpdate(func(metrics autotune.Metrics) {
		log.Printf("📊 GC Metrics: pause=%.2fms, freq=%.1f/s, pressure=%.1f%%, gogc=%d",
			float64(metrics.GCPauseTime)/1e6,
			metrics.GCFrequency,
			metrics.MemoryPressure*100,
			metrics.CurrentGOGC)
	})

	// Start tuning
	if err := tuner.Start(); err != nil {
		log.Fatal("Failed to start tuner:", err)
	}
	defer func() {
		log.Println("Stopping tuner...")
		tuner.Stop()
	}()

	log.Println("✅ Autotune started successfully!")
	log.Println("📈 Monitoring GC performance and tuning GOGC automatically...")

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

	// Simulate some workload to generate GC activity
	go simulateWorkload(ctx)

	// Wait for shutdown signal
	<-ctx.Done()

	// Print final statistics
	stats := tuner.GetStats()
	log.Printf("📊 Final Statistics:")
	log.Printf("   Total Decisions: %d", stats["total_decisions"])
	log.Printf("   Successful Tunes: %d", stats["successful_tunes"])
	log.Printf("   Current GOGC: %d", stats["current_gogc"])

	log.Println("👋 Goodbye!")
}

// simulateWorkload creates some memory allocation patterns to generate GC activity
func simulateWorkload(ctx context.Context) {
	log.Println("🏃 Starting workload simulation...")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Keep some long-lived data
	longLived := make([][]byte, 0, 1000)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Simulate different allocation patterns

			// Small frequent allocations
			for i := 0; i < 100; i++ {
				data := make([]byte, 1024) // 1KB
				_ = data
			}

			// Medium allocations
			for i := 0; i < 10; i++ {
				data := make([]byte, 64*1024) // 64KB
				_ = data
			}

			// Large allocations (occasionally)
			if time.Now().Unix()%5 == 0 {
				data := make([]byte, 1024*1024) // 1MB
				longLived = append(longLived, data)

				// Occasionally clean up long-lived data
				if len(longLived) > 100 {
					longLived = longLived[:50]
					runtime.GC() // Force a GC to see the effect
				}
			}

			// Small burst of allocations
			if time.Now().Unix()%10 == 0 {
				log.Println("💥 Allocation burst...")
				for i := 0; i < 1000; i++ {
					data := make([]byte, 4096) // 4KB
					_ = data
				}
			}
		}
	}
}
