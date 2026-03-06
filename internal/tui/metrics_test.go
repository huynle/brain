package tui

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMetricsCollector_Track(t *testing.T) {
	mc := NewMetricsCollector()

	// Track current process (ourselves)
	err := mc.TrackProcess(int32(os.Getpid()))
	assert.NoError(t, err)

	metrics := mc.Collect()

	assert.Equal(t, 1, metrics.ProcessCount)
	assert.Greater(t, metrics.CPUPercent, 0.0)
	assert.Greater(t, metrics.MemoryMB, 0.0)
}

func TestMetricsCollector_Untrack(t *testing.T) {
	mc := NewMetricsCollector()
	pid := int32(os.Getpid())

	// Track then untrack
	err := mc.TrackProcess(pid)
	assert.NoError(t, err)

	mc.UntrackProcess(pid)

	metrics := mc.Collect()
	assert.Equal(t, 0, metrics.ProcessCount)
}

func TestMetricsCollector_DeadProcess(t *testing.T) {
	mc := NewMetricsCollector()

	// Track a non-existent PID (should fail gracefully)
	err := mc.TrackProcess(999999)
	assert.Error(t, err)

	metrics := mc.Collect()
	assert.Equal(t, 0, metrics.ProcessCount)
}

func TestMetricsFormat(t *testing.T) {
	m := ResourceMetrics{
		CPUPercent:   20.1,
		MemoryMB:     524.2,
		ProcessCount: 1,
	}

	formatted := m.Format()
	assert.Equal(t, "CPU:20.1% Mem:524.2MB 1 procs", formatted)
}

func TestMetricsFormat_MultipleProcs(t *testing.T) {
	m := ResourceMetrics{
		CPUPercent:   45.7,
		MemoryMB:     1024.5,
		ProcessCount: 3,
	}

	formatted := m.Format()
	assert.Equal(t, "CPU:45.7% Mem:1024.5MB 3 procs", formatted)
}

func TestMetricsFormat_Zero(t *testing.T) {
	m := ResourceMetrics{
		CPUPercent:   0.0,
		MemoryMB:     0.0,
		ProcessCount: 0,
	}

	formatted := m.Format()
	assert.Equal(t, "CPU:0.0% Mem:0.0MB 0 procs", formatted)
}
