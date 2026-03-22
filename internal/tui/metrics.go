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

// Collect gathers current metrics from all tracked processes and their
// child process trees. For tmux-spawned tasks, the tracked PID is the
// shell process, but the actual work is done by child processes
// (opencode → node, etc.), so we must aggregate the full tree.
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

		// Collect metrics for this process and all its descendants
		cpu, mem := mc.collectTree(proc)
		metrics.CPUPercent += cpu
		metrics.MemoryMB += mem
	}

	mc.trackedPIDs = validPIDs
	metrics.ProcessCount = len(validPIDs)

	return metrics
}

// collectTree collects aggregate CPU and memory for a process and all its
// descendants (children, grandchildren, etc.).
func (mc *MetricsCollector) collectTree(proc *process.Process) (cpuTotal float64, memTotalMB float64) {
	// Collect this process's metrics
	if cpu, err := proc.CPUPercent(); err == nil {
		cpuTotal += cpu
	}
	if mem, err := proc.MemoryInfo(); err == nil {
		memTotalMB += float64(mem.RSS) / 1024 / 1024
	}

	// Recursively collect children
	children, err := proc.Children()
	if err != nil {
		return // No children or error — just return this process's metrics
	}
	for _, child := range children {
		childCPU, childMem := mc.collectTree(child)
		cpuTotal += childCPU
		memTotalMB += childMem
	}

	return
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
