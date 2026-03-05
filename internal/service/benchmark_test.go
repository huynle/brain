package service

import (
	"fmt"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// ---------------------------------------------------------------------------
// Benchmark helpers
// ---------------------------------------------------------------------------

// generateTasks creates n tasks with realistic dependency structures.
// Tasks are organized in chains: each task depends on the previous one,
// with some cross-chain dependencies for complexity.
func generateTasks(n int) []types.BrainEntry {
	tasks := make([]types.BrainEntry, n)
	for i := 0; i < n; i++ {
		tasks[i] = types.BrainEntry{
			ID:       fmt.Sprintf("task%04d", i),
			Path:     fmt.Sprintf("projects/bench/task/task-%04d.md", i),
			Title:    fmt.Sprintf("Task %d: implement feature %d", i, i%20),
			Type:     "task",
			Status:   "pending",
			Priority: []string{"high", "medium", "low"}[i%3],
			Created:  fmt.Sprintf("2024-01-%02dT10:00:00Z", (i%28)+1),
		}

		// Create dependency chains: each task depends on the previous one
		// (except the first in each chain of 10)
		if i%10 != 0 && i > 0 {
			tasks[i].DependsOn = []string{fmt.Sprintf("task%04d", i-1)}
		}

		// Add cross-chain dependencies every 15 tasks
		if i > 10 && i%15 == 0 {
			tasks[i].DependsOn = append(tasks[i].DependsOn, fmt.Sprintf("task%04d", i-10))
		}
	}
	return tasks
}

// generateTasksWithCycles creates tasks that include dependency cycles.
// Creates mutual dependency cycles: in each chain of 10, the first task
// depends on the last, creating A→B→...→J→A cycles in the adjacency list.
func generateTasksWithCycles(n int) []types.BrainEntry {
	tasks := generateTasks(n)

	// Create cycles: first task in each chain of 10 depends on the last.
	// Since tasks[1] depends on tasks[0], tasks[2] on tasks[1], etc.,
	// adding tasks[0] depends on tasks[9] creates:
	//   adj[task0000] = [task0009]
	//   adj[task0001] = [task0000]
	//   ...
	//   adj[task0009] = [task0008]
	// Following adj from task0000: task0009 → task0008 → ... → task0001 → task0000 (cycle!)
	for i := 0; i < n; i += 10 {
		last := i + 9
		if last >= n {
			break
		}
		tasks[i].DependsOn = append(tasks[i].DependsOn, fmt.Sprintf("task%04d", last))
	}

	return tasks
}

// generateTasksWithFeatures creates tasks grouped into features.
func generateTasksWithFeatures(n int, numFeatures int) []types.BrainEntry {
	tasks := generateTasks(n)
	for i := range tasks {
		featureIdx := i % numFeatures
		tasks[i].FeatureID = fmt.Sprintf("feature-%02d", featureIdx)
		tasks[i].FeaturePriority = []string{"high", "medium", "low"}[featureIdx%3]

		// Feature dependencies: each feature depends on the previous one
		if featureIdx > 0 {
			tasks[i].FeatureDependsOn = []string{fmt.Sprintf("feature-%02d", featureIdx-1)}
		}
	}
	return tasks
}

// ---------------------------------------------------------------------------
// Task Dependency Resolution Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkTaskDependencyResolution_10(b *testing.B) {
	benchmarkResolveDeps(b, 10)
}

func BenchmarkTaskDependencyResolution_100(b *testing.B) {
	benchmarkResolveDeps(b, 100)
}

func BenchmarkTaskDependencyResolution_500(b *testing.B) {
	benchmarkResolveDeps(b, 500)
}

func benchmarkResolveDeps(b *testing.B, n int) {
	tasks := generateTasks(n)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := ResolveDependencies(tasks)
		if result.Count != n {
			b.Fatalf("count = %d, want %d", result.Count, n)
		}
	}
}

// ---------------------------------------------------------------------------
// Cycle Detection Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkCycleDetection_10(b *testing.B) {
	benchmarkCycleDetection(b, 10)
}

func BenchmarkCycleDetection_100(b *testing.B) {
	benchmarkCycleDetection(b, 100)
}

func BenchmarkCycleDetection_500(b *testing.B) {
	benchmarkCycleDetection(b, 500)
}

func benchmarkCycleDetection(b *testing.B, n int) {
	tasks := generateTasksWithCycles(n)
	maps := BuildLookupMaps(tasks)
	adjacency := BuildAdjacencyList(tasks, maps)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cycles := FindCycles(adjacency)
		if len(cycles) == 0 {
			b.Fatal("expected cycles, got none")
		}
	}
}

// BenchmarkCycleDetection_NoCycles benchmarks cycle detection when no cycles exist.
func BenchmarkCycleDetection_NoCycles_100(b *testing.B) {
	tasks := generateTasks(100) // No cycles
	maps := BuildLookupMaps(tasks)
	adjacency := BuildAdjacencyList(tasks, maps)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cycles := FindCycles(adjacency)
		_ = cycles
	}
}

// ---------------------------------------------------------------------------
// Lookup Map Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkBuildLookupMaps_100(b *testing.B) {
	tasks := generateTasks(100)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		maps := BuildLookupMaps(tasks)
		if len(maps.ByID) != 100 {
			b.Fatalf("ByID len = %d, want 100", len(maps.ByID))
		}
	}
}

func BenchmarkBuildLookupMaps_500(b *testing.B) {
	tasks := generateTasks(500)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		maps := BuildLookupMaps(tasks)
		if len(maps.ByID) != 500 {
			b.Fatalf("ByID len = %d, want 500", len(maps.ByID))
		}
	}
}

// ---------------------------------------------------------------------------
// Dependency Resolution (ResolveDep) Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkResolveDep_DirectID(b *testing.B) {
	tasks := generateTasks(500)
	maps := BuildLookupMaps(tasks)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ref := fmt.Sprintf("task%04d", i%500)
		id := ResolveDep(ref, maps)
		if id == "" {
			b.Fatalf("ResolveDep(%q) returned empty", ref)
		}
	}
}

func BenchmarkResolveDep_ProjectPrefixed(b *testing.B) {
	tasks := generateTasks(500)
	maps := BuildLookupMaps(tasks)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ref := fmt.Sprintf("bench:task%04d", i%500)
		id := ResolveDep(ref, maps)
		if id == "" {
			b.Fatalf("ResolveDep(%q) returned empty", ref)
		}
	}
}

func BenchmarkResolveDep_FullPath(b *testing.B) {
	tasks := generateTasks(500)
	maps := BuildLookupMaps(tasks)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ref := fmt.Sprintf("projects/bench/task/task%04d.md", i%500)
		id := ResolveDep(ref, maps)
		if id == "" {
			b.Fatalf("ResolveDep(%q) returned empty", ref)
		}
	}
}

func BenchmarkResolveDep_TitleMatch(b *testing.B) {
	tasks := generateTasks(500)
	maps := BuildLookupMaps(tasks)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ref := fmt.Sprintf("Task %d: implement feature %d", i%500, (i%500)%20)
		id := ResolveDep(ref, maps)
		if id == "" {
			b.Fatalf("ResolveDep(%q) returned empty", ref)
		}
	}
}

// ---------------------------------------------------------------------------
// Adjacency List Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkBuildAdjacencyList_100(b *testing.B) {
	tasks := generateTasks(100)
	maps := BuildLookupMaps(tasks)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		adj := BuildAdjacencyList(tasks, maps)
		if len(adj) != 100 {
			b.Fatalf("adj len = %d, want 100", len(adj))
		}
	}
}

func BenchmarkBuildAdjacencyList_500(b *testing.B) {
	tasks := generateTasks(500)
	maps := BuildLookupMaps(tasks)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		adj := BuildAdjacencyList(tasks, maps)
		if len(adj) != 500 {
			b.Fatalf("adj len = %d, want 500", len(adj))
		}
	}
}

// ---------------------------------------------------------------------------
// Task Classification Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkClassifyTask(b *testing.B) {
	tasks := generateTasks(100)
	maps := BuildLookupMaps(tasks)
	adjacency := BuildAdjacencyList(tasks, maps)
	inCycle := FindCycles(adjacency)

	effectiveStatus := make(map[string]string, len(tasks))
	for _, task := range tasks {
		effectiveStatus[task.ID] = task.Status
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		task := &tasks[i%len(tasks)]
		resolvedDeps := adjacency[task.ID]
		classification, _, _ := ClassifyTask(task, resolvedDeps, effectiveStatus, inCycle)
		_ = classification
	}
}

// ---------------------------------------------------------------------------
// Priority Sorting Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkSortByPriority_100(b *testing.B) {
	tasks := generateTasks(100)
	result := ResolveDependencies(tasks)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sorted := SortByPriority(result.Tasks)
		if len(sorted) != len(result.Tasks) {
			b.Fatalf("sorted len = %d, want %d", len(sorted), len(result.Tasks))
		}
	}
}

func BenchmarkSortByPriority_500(b *testing.B) {
	tasks := generateTasks(500)
	result := ResolveDependencies(tasks)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sorted := SortByPriority(result.Tasks)
		if len(sorted) != len(result.Tasks) {
			b.Fatalf("sorted len = %d, want %d", len(sorted), len(result.Tasks))
		}
	}
}

// ---------------------------------------------------------------------------
// GetReadyTasks / GetNextTask Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkGetReadyTasks_100(b *testing.B) {
	tasks := generateTasks(100)
	result := ResolveDependencies(tasks)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ready := GetReadyTasks(result)
		_ = ready
	}
}

func BenchmarkGetReadyTasks_500(b *testing.B) {
	tasks := generateTasks(500)
	result := ResolveDependencies(tasks)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ready := GetReadyTasks(result)
		_ = ready
	}
}

func BenchmarkGetNextTask_100(b *testing.B) {
	tasks := generateTasks(100)
	result := ResolveDependencies(tasks)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		next := GetNextTask(result)
		_ = next
	}
}

func BenchmarkGetNextTask_500(b *testing.B) {
	tasks := generateTasks(500)
	result := ResolveDependencies(tasks)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		next := GetNextTask(result)
		_ = next
	}
}

// ---------------------------------------------------------------------------
// Downstream Tasks Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkGetDownstreamTasks_100(b *testing.B) {
	tasks := generateTasks(100)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		downstream := GetDownstreamTasks("task0000", tasks)
		_ = downstream
	}
}

func BenchmarkGetDownstreamTasks_500(b *testing.B) {
	tasks := generateTasks(500)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		downstream := GetDownstreamTasks("task0000", tasks)
		_ = downstream
	}
}

// ---------------------------------------------------------------------------
// Feature Computation Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkFeatureComputation_10Features(b *testing.B) {
	benchmarkFeatureComputation(b, 100, 10)
}

func BenchmarkFeatureComputation_50Features(b *testing.B) {
	benchmarkFeatureComputation(b, 500, 50)
}

func benchmarkFeatureComputation(b *testing.B, numTasks, numFeatures int) {
	tasks := generateTasksWithFeatures(numTasks, numFeatures)
	result := ResolveDependencies(tasks)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		features := ComputeFeatures(result.Tasks)
		if len(features) != numFeatures {
			b.Fatalf("features = %d, want %d", len(features), numFeatures)
		}
	}
}

// ---------------------------------------------------------------------------
// Feature Dependency Resolution Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkFeatureDependencyResolution_10(b *testing.B) {
	benchmarkFeatureDepResolution(b, 100, 10)
}

func BenchmarkFeatureDependencyResolution_50(b *testing.B) {
	benchmarkFeatureDepResolution(b, 500, 50)
}

func benchmarkFeatureDepResolution(b *testing.B, numTasks, numFeatures int) {
	tasks := generateTasksWithFeatures(numTasks, numFeatures)
	result := ResolveDependencies(tasks)
	features := ComputeFeatures(result.Tasks)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolved := ResolveFeatureDependencies(features)
		if len(resolved) != numFeatures {
			b.Fatalf("resolved = %d, want %d", len(resolved), numFeatures)
		}
	}
}

// ---------------------------------------------------------------------------
// End-to-end: ComputeAndResolveFeatures
// ---------------------------------------------------------------------------

func BenchmarkComputeAndResolveFeatures_10(b *testing.B) {
	benchmarkComputeAndResolve(b, 100, 10)
}

func BenchmarkComputeAndResolveFeatures_50(b *testing.B) {
	benchmarkComputeAndResolve(b, 500, 50)
}

func benchmarkComputeAndResolve(b *testing.B, numTasks, numFeatures int) {
	tasks := generateTasksWithFeatures(numTasks, numFeatures)
	result := ResolveDependencies(tasks)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		featureResult := ComputeAndResolveFeatures(result.Tasks)
		if len(featureResult.Features) != numFeatures {
			b.Fatalf("features = %d, want %d", len(featureResult.Features), numFeatures)
		}
	}
}
