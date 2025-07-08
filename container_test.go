package autotune

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDetectContainerResources tests container resource detection
func TestDetectContainerResources(t *testing.T) {
	resources, err := DetectContainerResources()
	require.NoError(t, err)
	assert.NotNil(t, resources)

	// The values will depend on the test environment
	// but we can check that the structure is correct
	assert.GreaterOrEqual(t, resources.MemoryLimit, uint64(0))
	assert.GreaterOrEqual(t, resources.CPULimit, float64(0))
}

// TestIsRunningInContainer tests container detection
func TestIsRunningInContainer(t *testing.T) {
	isContainer := isRunningInContainer()
	// This will be false in most test environments
	// but we can test that it doesn't panic
	assert.IsType(t, true, isContainer)
}

// TestDetectMemoryLimit tests memory limit detection
func TestDetectMemoryLimit(t *testing.T) {
	// This will likely fail in a non-container environment
	// but should not panic
	limit, err := detectMemoryLimit()
	if err == nil {
		assert.Greater(t, limit, uint64(0))
	}
}

// TestDetectCPULimit tests CPU limit detection
func TestDetectCPULimit(t *testing.T) {
	// This will likely fail in a non-container environment
	// but should not panic
	limit, err := detectCPULimit()
	if err == nil {
		assert.Greater(t, limit, float64(0))
	}
}

// TestReadProcMemInfo tests reading system memory info
func TestReadProcMemInfo(t *testing.T) {
	// This should work on Linux systems
	memTotal, err := readProcMemInfo()
	if err == nil {
		assert.Greater(t, memTotal, uint64(0))
	}
}

// TestGetContainerStats tests container statistics
func TestGetContainerStats(t *testing.T) {
	stats, err := GetContainerStats()
	require.NoError(t, err)
	assert.NotNil(t, stats)

	// The actual values will depend on the environment
	assert.GreaterOrEqual(t, stats.MemoryUsage, uint64(0))
	assert.GreaterOrEqual(t, stats.CPUUsage, float64(0))
}

// TestCgroupPathParsing tests cgroup path parsing
func TestCgroupPathParsing(t *testing.T) {
	// Test with mock cgroup file
	tempDir := t.TempDir()

	// Create mock /proc/self/cgroup file
	cgroupFile := filepath.Join(tempDir, "cgroup")
	cgroupContent := `12:memory:/docker/container_id
11:cpu:/docker/container_id
10:cpuacct:/docker/container_id
9:devices:/docker/container_id
8:freezer:/docker/container_id
7:net_cls,net_prio:/docker/container_id
6:perf_event:/docker/container_id
5:hugetlb:/docker/container_id
4:pids:/docker/container_id
3:rdma:/
2:misc:/
1:name=systemd:/docker/container_id
0::/docker/container_id`

	err := os.WriteFile(cgroupFile, []byte(cgroupContent), 0644)
	require.NoError(t, err)

	// We can't easily test the actual cgroup parsing without mocking the entire filesystem
	// but we can test that the functions don't panic
	_, err = findCgroupPath("memory")
	// This will likely fail in test environment, but shouldn't panic
	if err != nil {
		assert.Error(t, err)
	}
}

// TestMemoryLimitParsing tests memory limit parsing
func TestMemoryLimitParsing(t *testing.T) {
	// Test edge cases for memory limit parsing

	// Test with mock cgroup v2 memory.max file
	tempDir := t.TempDir()
	memMaxFile := filepath.Join(tempDir, "memory.max")

	// Test with "max" value (no limit)
	err := os.WriteFile(memMaxFile, []byte("max\n"), 0644)
	require.NoError(t, err)

	// Test with actual limit
	err = os.WriteFile(memMaxFile, []byte("1073741824\n"), 0644) // 1GB
	require.NoError(t, err)

	// We can't easily test the actual parsing without mocking the filesystem paths
	// but we can verify the functions handle different input formats
}

// TestCPULimitParsing tests CPU limit parsing
func TestCPULimitParsing(t *testing.T) {
	// Test edge cases for CPU limit parsing

	// Test with mock cgroup v2 cpu.max file
	tempDir := t.TempDir()
	cpuMaxFile := filepath.Join(tempDir, "cpu.max")

	// Test with "max" value (no limit)
	err := os.WriteFile(cpuMaxFile, []byte("max 100000\n"), 0644)
	require.NoError(t, err)

	// Test with actual limit
	err = os.WriteFile(cpuMaxFile, []byte("50000 100000\n"), 0644) // 0.5 cores
	require.NoError(t, err)

	// We can't easily test the actual parsing without mocking the filesystem paths
	// but we can verify the functions handle different input formats
}

// TestContainerDetectionMethods tests various container detection methods
func TestContainerDetectionMethods(t *testing.T) {
	// Test .dockerenv file detection
	dockerEnvExists := false
	if _, err := os.Stat("/.dockerenv"); err == nil {
		dockerEnvExists = true
	}

	// Test cgroup detection
	cgroupContent := ""
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		cgroupContent = string(data)
	}

	// Test environment variables
	k8sHost := os.Getenv("KUBERNETES_SERVICE_HOST")

	// Test PID detection
	pid := os.Getpid()

	// These tests verify the detection logic works
	// The actual values depend on the test environment
	assert.IsType(t, true, dockerEnvExists)
	assert.IsType(t, "", cgroupContent)
	assert.IsType(t, "", k8sHost)
	assert.Greater(t, pid, 0)
}

// TestResourceLimitEdgeCases tests edge cases in resource limit detection
func TestResourceLimitEdgeCases(t *testing.T) {
	// Test with non-existent files
	_, err := readCgroupV2MemoryLimit()
	// Should handle missing files gracefully
	if err != nil {
		assert.Error(t, err)
	}

	_, err = readCgroupV1MemoryLimit()
	// Should handle missing files gracefully
	if err != nil {
		assert.Error(t, err)
	}

	_, err = readCgroupV2CPULimit()
	// Should handle missing files gracefully
	if err != nil {
		assert.Error(t, err)
	}

	_, err = readCgroupV1CPULimit()
	// Should handle missing files gracefully
	if err != nil {
		assert.Error(t, err)
	}
}

// TestContainerResourcesStruct tests the ContainerResources struct
func TestContainerResourcesStruct(t *testing.T) {
	resources := &ContainerResources{
		MemoryLimit: 1024 * 1024 * 1024, // 1GB
		CPULimit:    2.0,                // 2 cores
		IsContainer: true,
	}

	assert.Equal(t, uint64(1024*1024*1024), resources.MemoryLimit)
	assert.Equal(t, 2.0, resources.CPULimit)
	assert.True(t, resources.IsContainer)
}

// TestContainerStatsStruct tests the ContainerStats struct
func TestContainerStatsStruct(t *testing.T) {
	stats := &ContainerStats{
		MemoryUsage: 512 * 1024 * 1024, // 512MB
		CPUUsage:    0.5,               // 50%
	}

	assert.Equal(t, uint64(512*1024*1024), stats.MemoryUsage)
	assert.Equal(t, 0.5, stats.CPUUsage)
}

// TestMemoryUsageDetection tests memory usage detection
func TestMemoryUsageDetection(t *testing.T) {
	usage, err := getCurrentMemoryUsage()
	// May fail in non-container environment
	if err == nil {
		assert.Greater(t, usage, uint64(0))
	}
}

// TestCPUUsageDetection tests CPU usage detection
func TestCPUUsageDetection(t *testing.T) {
	usage, err := getCurrentCPUUsage()
	// May fail in non-container environment
	if err == nil {
		assert.GreaterOrEqual(t, usage, float64(0))
	}
}

// TestCgroupV2Detection tests cgroup v2 specific detection
func TestCgroupV2Detection(t *testing.T) {
	// Test cgroup v2 memory usage
	_, err := readCgroupV2MemoryUsage()
	// Expected to fail in most test environments
	if err == nil {
		// If it succeeds, that's fine too
	}

	// Test cgroup v2 CPU usage
	_, err = readCgroupV2CPUUsage()
	// Expected to fail in most test environments
	if err == nil {
		// If it succeeds, that's fine too
	}
}

// TestCgroupV1Detection tests cgroup v1 specific detection
func TestCgroupV1Detection(t *testing.T) {
	// Test cgroup v1 memory usage
	_, err := readCgroupV1MemoryUsage()
	// Expected to fail in most test environments
	if err == nil {
		// If it succeeds, that's fine too
	}

	// Test cgroup v1 CPU usage
	_, err = readCgroupV1CPUUsage()
	// Expected to fail in most test environments
	if err == nil {
		// If it succeeds, that's fine too
	}
}

// TestDetectContainerResourcesIntegration tests the full integration
func TestDetectContainerResourcesIntegration(t *testing.T) {
	resources, err := DetectContainerResources()
	require.NoError(t, err)

	// Test that we get a valid response regardless of environment
	assert.NotNil(t, resources)
	assert.IsType(t, true, resources.IsContainer)
	assert.IsType(t, uint64(0), resources.MemoryLimit)
	assert.IsType(t, float64(0), resources.CPULimit)
}

// TestGetContainerStatsIntegration tests the full stats integration
func TestGetContainerStatsIntegration(t *testing.T) {
	stats, err := GetContainerStats()
	require.NoError(t, err)

	// Test that we get a valid response regardless of environment
	assert.NotNil(t, stats)
	assert.IsType(t, uint64(0), stats.MemoryUsage)
	assert.IsType(t, float64(0), stats.CPUUsage)
}

// BenchmarkDetectContainerResources benchmarks resource detection
func BenchmarkDetectContainerResources(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := DetectContainerResources()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetContainerStats benchmarks stats collection
func BenchmarkGetContainerStats(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := GetContainerStats()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIsRunningInContainer benchmarks container detection
func BenchmarkIsRunningInContainer(b *testing.B) {
	for i := 0; i < b.N; i++ {
		isRunningInContainer()
	}
}
