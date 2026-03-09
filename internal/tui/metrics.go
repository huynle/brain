package tui

import (
	"fmt"

	"github.com/shirou/gopsutil/v3/process"
)

// ResourceMetrics holds CPU, memory, and process count for monitored processes.
type ResourceMetrics struct {
	CPUPercent   float64
	MemoryMB     float64
	ProcessCount int
}

// MetricsCollector tracks OpenCode runner processes and collects metrics.
type MetricsCollector struct {
	trackedPIDs map[int32]*process.Process
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		trackedPIDs: make(map[int32]*process.Process),
	}
}

// TrackProcess adds a PID to monitor.
func (mc *MetricsCollector) TrackProcess(pid int32) error {
	proc, err := process.NewProcess(pid)
	if err != nil {
		return err
	}
	mc.trackedPIDs[pid] = proc
	return nil
}

// UntrackProcess removes a PID from monitoring.
func (mc *MetricsCollector) UntrackProcess(pid int32) {
	delete(mc.trackedPIDs, pid)
}

// Collect gathers current metrics from all tracked processes.
func (mc *MetricsCollector) Collect() ResourceMetrics {
	metrics := ResourceMetrics{}

	// Validate and clean up dead processes
	validPIDs := make(map[int32]*process.Process)

	for pid, proc := range mc.trackedPIDs {
		running, err := proc.IsRunning()
		if err != nil || !running {
			continue // Process died, skip it
		}

		validPIDs[pid] = proc

		// Collect CPU percentage
		cpu, err := proc.CPUPercent()
		if err == nil {
			metrics.CPUPercent += cpu
		}

		// Collect memory info
		mem, err := proc.MemoryInfo()
		if err == nil {
			metrics.MemoryMB += float64(mem.RSS) / 1024 / 1024
		}
	}

	mc.trackedPIDs = validPIDs
	metrics.ProcessCount = len(validPIDs)

	return metrics
}

// Format returns formatted string: "CPU:12.3% Mem:524.2MB 3 procs"
// Memory displays as GB when >= 1000MB (matching TypeScript behavior)
func (m ResourceMetrics) Format() string {
	memDisplay := fmt.Sprintf("%.1fMB", m.MemoryMB)
	if m.MemoryMB >= 1000 {
		memDisplay = fmt.Sprintf("%.1fGB", m.MemoryMB/1024)
	}

	return fmt.Sprintf("CPU:%.1f%% Mem:%s %d procs",
		m.CPUPercent,
		memDisplay,
		m.ProcessCount,
	)
}
