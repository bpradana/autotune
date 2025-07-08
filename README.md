# Autotune: Production-Ready Go GC Tuning for Containers

[![Go Reference](https://pkg.go.dev/badge/github.com/bpradana/autotune.svg)](https://pkg.go.dev/github.com/bpradana/autotune)
[![Go Report Card](https://goreportcard.com/badge/github.com/bpradana/autotune)](https://goreportcard.com/report/github.com/bpradana/autotune)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Autotune is a production-ready Go package that automatically optimizes garbage collection (GC) performance in containerized environments. It monitors real-time application metrics and dynamically adjusts the `GOGC` value to optimize for both latency and throughput while respecting container resource constraints.

## Features

- **Automatic GC Tuning**: Continuously monitors and adjusts GOGC based on application performance
- **Container-Aware**: Detects and respects Docker/Kubernetes memory and CPU limits
- **Anti-Oscillation**: Prevents rapid back-and-forth tuning decisions
- **Thread-Safe**: Designed for concurrent access in multi-threaded applications
- **Low Overhead**: Minimal performance impact on your application
- **Observability**: Built-in metrics, alerts, and HTTP endpoints for monitoring
- **Production-Ready**: Comprehensive testing, error handling, and safety measures

## Quick Start

### Installation

```bash
go get github.com/bpradana/autotune
```

### Basic Usage

```go
package main

import (
    "log"
    "time"
    
    "github.com/bpradana/autotune"
)

func main() {
    // Create tuner with default configuration
    tuner, err := autotune.NewTuner(nil)
    if err != nil {
        log.Fatal(err)
    }
    
    // Start automatic tuning
    if err := tuner.Start(); err != nil {
        log.Fatal(err)
    }
    defer tuner.Stop()
    
    // Your application code here
    // The tuner will automatically optimize GC in the background
    
    // Run your application
    select {}
}
```

### Advanced Configuration

```go
package main

import (
    "log"
    "time"
    
    "github.com/bpradana/autotune"
)

func main() {
    // Create custom configuration
    config := &autotune.Config{
        MonitorInterval:      15 * time.Second,  // Check every 15 seconds
        MinGOGC:              100,               // Minimum GOGC value
        MaxGOGC:              500,               // Maximum GOGC value
        TargetLatency:        5 * time.Millisecond, // Target GC pause time
        MemoryLimitPercent:   0.85,              // Use 85% of container memory
        TuningAggressiveness: 0.5,               // More aggressive tuning
        StabilizationWindow:  3 * time.Minute,   // Anti-oscillation window
        MaxChangePerInterval: 25,                // Limit GOGC changes
    }
    
    tuner, err := autotune.NewTuner(config)
    if err != nil {
        log.Fatal(err)
    }
    
    // Set up callbacks for monitoring
    tuner.SetOnTuningDecision(func(decision autotune.TuningDecision) {
        log.Printf("GC tuning: %s (confidence: %.2f)", 
            decision.Reason, decision.Confidence)
    })
    
    tuner.SetOnMetricsUpdate(func(metrics autotune.Metrics) {
        log.Printf("GC metrics: pause=%.2fms, freq=%.1f/s, pressure=%.1f%%",
            float64(metrics.GCPauseTime)/1e6,
            metrics.GCFrequency,
            metrics.MemoryPressure*100)
    })
    
    // Start tuning
    if err := tuner.Start(); err != nil {
        log.Fatal(err)
    }
    defer tuner.Stop()
    
    // Your application code here
    select {}
}
```

## Observability

### Built-in HTTP Endpoints

Enable observability with HTTP endpoints:

```go
package main

import (
    "log"
    
    "github.com/bpradana/autotune"
)

func main() {
    tuner, err := autotune.NewTuner(nil)
    if err != nil {
        log.Fatal(err)
    }
    
    // Set up observability server
    obsConfig := autotune.DefaultObservabilityConfig()
    obsConfig.HTTPPort = 8080
    obsServer := autotune.NewObservabilityServer(obsConfig, tuner)
    
    // Start both tuner and observability server
    if err := obsServer.Start(); err != nil {
        log.Fatal(err)
    }
    defer obsServer.Stop()
    
    if err := tuner.Start(); err != nil {
        log.Fatal(err)
    }
    defer tuner.Stop()
    
    log.Println("Autotune running with observability on :8080")
    select {}
}
```

### Available Endpoints

- `GET /metrics` - Prometheus or JSON metrics
- `GET /metrics?format=prometheus` - Prometheus format
- `GET /metrics?format=json` - JSON format
- `GET /metrics?format=json&history=true` - JSON with history
- `GET /health` - Health check
- `GET /stats` - Tuning statistics
- `GET /config` - Current configuration
- `GET /decisions` - Recent tuning decisions

### Prometheus Metrics

```bash
# Example Prometheus metrics
curl http://localhost:8080/metrics?format=prometheus

# Output:
# HELP autotune_gc_pause_time_ns Current GC pause time in nanoseconds
# TYPE autotune_gc_pause_time_ns gauge
autotune_gc_pause_time_ns 2500000

# HELP autotune_gogc_current Current GOGC value
# TYPE autotune_gogc_current gauge
autotune_gogc_current 150
```

### JSON Metrics

```bash
curl http://localhost:8080/metrics?format=json | jq
```

```json
{
  "current_metrics": {
    "gc_pause_time": "2.5ms",
    "gc_frequency": 1.2,
    "heap_size": 104857600,
    "memory_pressure": 0.45,
    "current_gogc": 150
  },
  "stats": {
    "total_decisions": 25,
    "successful_tunes": 23,
    "avg_improvement": 0.15
  }
}
```

## Container Deployment

### Docker

```dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o myapp .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

# Copy the binary
COPY --from=builder /app/myapp .

# Expose observability port
EXPOSE 8080

CMD ["./myapp"]
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
spec:
  replicas: 3
  selector:
    matchLabels:
      app: myapp
  template:
    metadata:
      labels:
        app: myapp
    spec:
      containers:
      - name: myapp
        image: myapp:latest
        ports:
        - containerPort: 8080
          name: metrics
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        env:
        - name: AUTOTUNE_ENABLE
          value: "true"
        - name: AUTOTUNE_PORT
          value: "8080"
---
apiVersion: v1
kind: Service
metadata:
  name: myapp-metrics
spec:
  selector:
    app: myapp
  ports:
  - port: 8080
    targetPort: metrics
    name: metrics
```

### Prometheus Monitoring

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: myapp-autotune
spec:
  selector:
    matchLabels:
      app: myapp
  endpoints:
  - port: metrics
    path: /metrics
    params:
      format: ['prometheus']
```

## Configuration Reference

### Core Configuration

```go
type Config struct {
    // How often to collect metrics and evaluate tuning (default: 30s)
    MonitorInterval time.Duration
    
    // Minimum allowed GOGC value (default: 50)
    MinGOGC int
    
    // Maximum allowed GOGC value (default: 800)
    MaxGOGC int
    
    // Target GC pause time (default: 10ms)
    TargetLatency time.Duration
    
    // Percentage of container memory to use as threshold (default: 0.8)
    MemoryLimitPercent float64
    
    // How aggressively to tune - 0.1 conservative, 1.0 aggressive (default: 0.3)
    TuningAggressiveness float64
    
    // Time window for anti-oscillation logic (default: 5min)
    StabilizationWindow time.Duration
    
    // Maximum GOGC change per interval (default: 50)
    MaxChangePerInterval int
    
    // Logger interface for debugging
    Logger Logger
}
```

### Tuning Algorithm

The autotune package uses a sophisticated algorithm that considers multiple factors:

1. **Latency Factor**: Adjusts GOGC based on GC pause time vs target
2. **Memory Pressure Factor**: Considers container memory usage
3. **Frequency Factor**: Accounts for GC frequency
4. **Exponential Smoothing**: Prevents rapid oscillations
5. **Confidence Scoring**: Only applies changes with high confidence

## Performance Impact

Autotune is designed to have minimal performance impact:

- **CPU Overhead**: < 0.1% in typical workloads
- **Memory Overhead**: < 1MB additional memory usage
- **Monitoring Frequency**: Configurable (default: 30 seconds)
- **Thread Safety**: Lock-free metrics collection, minimal lock contention

## Safety Features

### Anti-Oscillation

Prevents rapid back-and-forth tuning decisions:

```go
// Example: Detects alternating increase/decrease patterns
// and skips tuning during unstable periods
```

### Bounds Checking

```go
// Always respects configured bounds
if targetGOGC < config.MinGOGC {
    targetGOGC = config.MinGOGC
}
if targetGOGC > config.MaxGOGC {
    targetGOGC = config.MaxGOGC
}
```

### Confidence Scoring

Only applies changes when confidence is high:

```go
if confidence < 0.6 {
    // Skip tuning due to low confidence
    return nil
}
```

## Troubleshooting

### Common Issues

1. **No Tuning Decisions**: Check if application has sufficient GC activity
2. **Oscillating GOGC**: Increase `StabilizationWindow` or decrease `TuningAggressiveness`
3. **Container Detection Failed**: Ensure proper cgroup permissions
4. **High Memory Usage**: Decrease `MemoryLimitPercent` or `MaxGOGC`

### Debug Logging

```go
config := autotune.DefaultConfig()
config.Logger = &customLogger{} // Implement Logger interface

tuner, err := autotune.NewTuner(config)
```

### Metrics Analysis

```bash
# Check current GOGC value
curl http://localhost:8080/metrics?format=json | jq '.current_metrics.current_gogc'

# Monitor tuning decisions
curl http://localhost:8080/decisions | jq '.decisions[-5:]'

# Check health status
curl http://localhost:8080/health
```

## Testing

Run the test suite:

```bash
go test ./...
```

Run benchmarks:

```bash
go test -bench=. -benchmem
```

Run with race detection:

```bash
go test -race ./...
```

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Development Setup

```bash
git clone https://github.com/bpradana/autotune.git
cd autotune
go mod tidy
go test ./...
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- Go team for the excellent runtime introspection APIs
- Kubernetes community for container resource detection patterns
- Prometheus community for metrics format standards

## Support

- 📖 [Documentation](https://pkg.go.dev/github.com/bpradana/autotune)
- 🐛 [Issues](https://github.com/bpradana/autotune/issues)
- 💬 [Discussions](https://github.com/bpradana/autotune/discussions)

---

**Note**: This package is designed for production use but should be thoroughly tested in your specific environment before deployment. Monitor the tuning decisions and adjust configuration as needed for your workload characteristics.