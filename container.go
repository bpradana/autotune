package autotune

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ContainerResources holds detected container resource limits
type ContainerResources struct {
	MemoryLimit uint64  // Memory limit in bytes
	CPULimit    float64 // CPU limit in cores
	IsContainer bool    // Whether running in a container
}

// DetectContainerResources attempts to detect container resource limits
func DetectContainerResources() (*ContainerResources, error) {
	resources := &ContainerResources{}

	// Check if we're running in a container
	if isRunningInContainer() {
		resources.IsContainer = true

		// Try to detect memory limit
		if memLimit, err := detectMemoryLimit(); err == nil {
			resources.MemoryLimit = memLimit
		}

		// Try to detect CPU limit
		if cpuLimit, err := detectCPULimit(); err == nil {
			resources.CPULimit = cpuLimit
		}
	}

	return resources, nil
}

// isRunningInContainer checks if the process is running inside a container
func isRunningInContainer() bool {
	// Method 1: Check for /.dockerenv file
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	// Method 2: Check cgroup information
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		content := string(data)
		if strings.Contains(content, "docker") ||
			strings.Contains(content, "kubepods") ||
			strings.Contains(content, "containerd") {
			return true
		}
	}

	// Method 3: Check if we're PID 1 (often the case in containers)
	if os.Getpid() == 1 {
		return true
	}

	// Method 4: Check for Kubernetes environment variables
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}

	return false
}

// detectMemoryLimit attempts to detect the container memory limit
func detectMemoryLimit() (uint64, error) {
	// Try cgroup v2 first
	if limit, err := readCgroupV2MemoryLimit(); err == nil {
		return limit, nil
	}

	// Try cgroup v1
	if limit, err := readCgroupV1MemoryLimit(); err == nil {
		return limit, nil
	}

	// Try /proc/meminfo as fallback
	if limit, err := readProcMemInfo(); err == nil {
		return limit, nil
	}

	return 0, fmt.Errorf("unable to detect memory limit")
}

// readCgroupV2MemoryLimit reads memory limit from cgroup v2
func readCgroupV2MemoryLimit() (uint64, error) {
	// Try unified hierarchy first
	paths := []string{
		"/sys/fs/cgroup/memory.max",
		"/sys/fs/cgroup/memory/memory.limit_in_bytes",
	}

	for _, path := range paths {
		if data, err := os.ReadFile(path); err == nil {
			content := strings.TrimSpace(string(data))
			if content == "max" {
				continue // No limit set
			}

			if limit, err := strconv.ParseUint(content, 10, 64); err == nil {
				// Sanity check - if limit is extremely high, it's probably not set
				if limit < (1<<63) && limit > 0 {
					return limit, nil
				}
			}
		}
	}

	return 0, fmt.Errorf("cgroup v2 memory limit not found")
}

// readCgroupV1MemoryLimit reads memory limit from cgroup v1
func readCgroupV1MemoryLimit() (uint64, error) {
	// First, find the memory cgroup path
	cgroupPath, err := findCgroupPath("memory")
	if err != nil {
		return 0, err
	}

	limitPath := filepath.Join(cgroupPath, "memory.limit_in_bytes")

	data, err := os.ReadFile(limitPath)
	if err != nil {
		return 0, err
	}

	content := strings.TrimSpace(string(data))
	limit, err := strconv.ParseUint(content, 10, 64)
	if err != nil {
		return 0, err
	}

	// Sanity check - if limit is extremely high, it's probably not set
	if limit >= (1<<63) || limit == 0 {
		return 0, fmt.Errorf("no memory limit set")
	}

	return limit, nil
}

// readProcMemInfo reads total memory from /proc/meminfo
func readProcMemInfo() (uint64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					return kb * 1024, nil // Convert KB to bytes
				}
			}
		}
	}

	return 0, fmt.Errorf("MemTotal not found in /proc/meminfo")
}

// detectCPULimit attempts to detect the container CPU limit
func detectCPULimit() (float64, error) {
	// Try cgroup v2 first
	if limit, err := readCgroupV2CPULimit(); err == nil {
		return limit, nil
	}

	// Try cgroup v1
	if limit, err := readCgroupV1CPULimit(); err == nil {
		return limit, nil
	}

	return 0, fmt.Errorf("unable to detect CPU limit")
}

// readCgroupV2CPULimit reads CPU limit from cgroup v2
func readCgroupV2CPULimit() (float64, error) {
	// Try cpu.max first
	if data, err := os.ReadFile("/sys/fs/cgroup/cpu.max"); err == nil {
		content := strings.TrimSpace(string(data))
		if content == "max" {
			return 0, fmt.Errorf("no CPU limit set")
		}

		fields := strings.Fields(content)
		if len(fields) >= 2 {
			quota, err1 := strconv.ParseFloat(fields[0], 64)
			period, err2 := strconv.ParseFloat(fields[1], 64)
			if err1 == nil && err2 == nil && period > 0 {
				return quota / period, nil
			}
		}
	}

	return 0, fmt.Errorf("cgroup v2 CPU limit not found")
}

// readCgroupV1CPULimit reads CPU limit from cgroup v1
func readCgroupV1CPULimit() (float64, error) {
	// First, find the CPU cgroup path
	cgroupPath, err := findCgroupPath("cpu")
	if err != nil {
		return 0, err
	}

	// Read CPU quota and period
	quotaPath := filepath.Join(cgroupPath, "cpu.cfs_quota_us")
	periodPath := filepath.Join(cgroupPath, "cpu.cfs_period_us")

	quotaData, err := os.ReadFile(quotaPath)
	if err != nil {
		return 0, err
	}

	periodData, err := os.ReadFile(periodPath)
	if err != nil {
		return 0, err
	}

	quotaStr := strings.TrimSpace(string(quotaData))
	periodStr := strings.TrimSpace(string(periodData))

	quota, err := strconv.ParseFloat(quotaStr, 64)
	if err != nil {
		return 0, err
	}

	period, err := strconv.ParseFloat(periodStr, 64)
	if err != nil {
		return 0, err
	}

	if quota <= 0 || period <= 0 {
		return 0, fmt.Errorf("no CPU limit set")
	}

	return quota / period, nil
}

// findCgroupPath finds the cgroup path for a given subsystem
func findCgroupPath(subsystem string) (string, error) {
	// First, try to find the cgroup mount point
	mountData, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return "", err
	}

	var cgroupRoot string
	scanner := bufio.NewScanner(strings.NewReader(string(mountData)))
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[2] == "cgroup" {
			cgroupRoot = fields[1]
			break
		}
	}

	if cgroupRoot == "" {
		return "", fmt.Errorf("cgroup mount point not found")
	}

	// Read the current process's cgroup
	cgroupData, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}

	scanner = bufio.NewScanner(strings.NewReader(string(cgroupData)))
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Split(line, ":")
		if len(fields) >= 3 {
			subsystems := strings.Split(fields[1], ",")
			for _, sys := range subsystems {
				if sys == subsystem {
					return filepath.Join(cgroupRoot, subsystem, fields[2]), nil
				}
			}
		}
	}

	return "", fmt.Errorf("cgroup path for %s not found", subsystem)
}

// GetContainerStats returns current container resource usage statistics
func GetContainerStats() (*ContainerStats, error) {
	stats := &ContainerStats{}

	// Get memory usage
	if memUsage, err := getCurrentMemoryUsage(); err == nil {
		stats.MemoryUsage = memUsage
	}

	// Get CPU usage
	if cpuUsage, err := getCurrentCPUUsage(); err == nil {
		stats.CPUUsage = cpuUsage
	}

	return stats, nil
}

// ContainerStats holds current container resource usage
type ContainerStats struct {
	MemoryUsage uint64  // Current memory usage in bytes
	CPUUsage    float64 // Current CPU usage percentage
}

// getCurrentMemoryUsage gets current memory usage from cgroup
func getCurrentMemoryUsage() (uint64, error) {
	// Try cgroup v2
	if usage, err := readCgroupV2MemoryUsage(); err == nil {
		return usage, nil
	}

	// Try cgroup v1
	if usage, err := readCgroupV1MemoryUsage(); err == nil {
		return usage, nil
	}

	return 0, fmt.Errorf("unable to get memory usage")
}

// readCgroupV2MemoryUsage reads current memory usage from cgroup v2
func readCgroupV2MemoryUsage() (uint64, error) {
	data, err := os.ReadFile("/sys/fs/cgroup/memory.current")
	if err != nil {
		return 0, err
	}

	content := strings.TrimSpace(string(data))
	usage, err := strconv.ParseUint(content, 10, 64)
	if err != nil {
		return 0, err
	}

	return usage, nil
}

// readCgroupV1MemoryUsage reads current memory usage from cgroup v1
func readCgroupV1MemoryUsage() (uint64, error) {
	cgroupPath, err := findCgroupPath("memory")
	if err != nil {
		return 0, err
	}

	usagePath := filepath.Join(cgroupPath, "memory.usage_in_bytes")

	data, err := os.ReadFile(usagePath)
	if err != nil {
		return 0, err
	}

	content := strings.TrimSpace(string(data))
	usage, err := strconv.ParseUint(content, 10, 64)
	if err != nil {
		return 0, err
	}

	return usage, nil
}

// getCurrentCPUUsage gets current CPU usage percentage
func getCurrentCPUUsage() (float64, error) {
	// This is a simplified CPU usage calculation
	// In a real implementation, you'd want to sample over time

	// Try cgroup v2
	if usage, err := readCgroupV2CPUUsage(); err == nil {
		return usage, nil
	}

	// Try cgroup v1
	if usage, err := readCgroupV1CPUUsage(); err == nil {
		return usage, nil
	}

	return 0, fmt.Errorf("unable to get CPU usage")
}

// readCgroupV2CPUUsage reads current CPU usage from cgroup v2
func readCgroupV2CPUUsage() (float64, error) {
	data, err := os.ReadFile("/sys/fs/cgroup/cpu.stat")
	if err != nil {
		return 0, err
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "usage_usec") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if usec, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					// Convert microseconds to a percentage (this is simplified)
					// In reality, you'd need to track changes over time
					return float64(usec) / 1000000.0, nil
				}
			}
		}
	}

	return 0, fmt.Errorf("CPU usage not found in cgroup v2")
}

// readCgroupV1CPUUsage reads current CPU usage from cgroup v1
func readCgroupV1CPUUsage() (float64, error) {
	cgroupPath, err := findCgroupPath("cpuacct")
	if err != nil {
		return 0, err
	}

	usagePath := filepath.Join(cgroupPath, "cpuacct.usage")

	data, err := os.ReadFile(usagePath)
	if err != nil {
		return 0, err
	}

	content := strings.TrimSpace(string(data))
	usage, err := strconv.ParseUint(content, 10, 64)
	if err != nil {
		return 0, err
	}

	// Convert nanoseconds to a percentage (this is simplified)
	// In reality, you'd need to track changes over time
	return float64(usage) / 1000000000.0, nil
}
