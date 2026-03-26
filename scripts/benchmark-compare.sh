#!/usr/bin/env bash
# benchmark-compare.sh — Run Go benchmarks and format results.
#
# Usage:
#   ./scripts/benchmark-compare.sh                    # Run all benchmarks
#   ./scripts/benchmark-compare.sh storage             # Run storage benchmarks only
#   ./scripts/benchmark-compare.sh api                 # Run API benchmarks only
#   ./scripts/benchmark-compare.sh service             # Run service benchmarks only
#   ./scripts/benchmark-compare.sh --compare baseline.txt  # Compare against baseline
#   ./scripts/benchmark-compare.sh --save baseline.txt     # Save results as baseline
#
# Requirements:
#   - Go 1.21+
#   - benchstat (optional, for --compare): go install golang.org/x/perf/cmd/benchstat@latest

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Defaults
BENCH_TIME="${BENCH_TIME:-1s}"
BENCH_COUNT="${BENCH_COUNT:-1}"
COMPARE_FILE=""
SAVE_FILE=""
PACKAGE=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        storage)
            PACKAGE="./internal/storage/"
            shift
            ;;
        api)
            PACKAGE="./internal/api/"
            shift
            ;;
        service)
            PACKAGE="./internal/service/"
            shift
            ;;
        --compare)
            COMPARE_FILE="$2"
            shift 2
            ;;
        --save)
            SAVE_FILE="$2"
            shift 2
            ;;
        --count)
            BENCH_COUNT="$2"
            shift 2
            ;;
        --time)
            BENCH_TIME="$2"
            shift 2
            ;;
        -h|--help)
            echo "Usage: $0 [storage|api|service] [--compare FILE] [--save FILE] [--count N] [--time DURATION]"
            echo ""
            echo "Options:"
            echo "  storage          Run storage layer benchmarks only"
            echo "  api              Run API handler benchmarks only"
            echo "  service          Run service layer benchmarks only"
            echo "  --compare FILE   Compare current results against a baseline file"
            echo "  --save FILE      Save benchmark results to a file for later comparison"
            echo "  --count N        Number of benchmark iterations (default: 1)"
            echo "  --time DURATION  Benchmark duration per test (default: 1s)"
            echo ""
            echo "Environment variables:"
            echo "  BENCH_TIME       Benchmark duration (default: 1s)"
            echo "  BENCH_COUNT      Number of iterations (default: 1)"
            exit 0
            ;;
        *)
            echo "Unknown argument: $1"
            exit 1
            ;;
    esac
done

# Default: run all benchmark packages
if [[ -z "$PACKAGE" ]]; then
    PACKAGES=("./internal/storage/" "./internal/api/" "./internal/service/")
else
    PACKAGES=("$PACKAGE")
fi

cd "$PROJECT_ROOT"

echo "╔══════════════════════════════════════════════════════════════╗"
echo "║              Brain API — Go Benchmark Suite                 ║"
echo "╠══════════════════════════════════════════════════════════════╣"
echo "║  Bench time:  $BENCH_TIME"
echo "║  Count:       $BENCH_COUNT"
echo "║  Packages:    ${PACKAGES[*]}"
echo "╚══════════════════════════════════════════════════════════════╝"
echo ""

# Temporary file for results
RESULTS_FILE=$(mktemp /tmp/bench-results-XXXXXX.txt)
trap 'rm -f "$RESULTS_FILE"' EXIT

# Run benchmarks
for pkg in "${PACKAGES[@]}"; do
    echo "━━━ Running benchmarks: $pkg ━━━"
    echo ""
    go test "$pkg" \
        -bench=. \
        -benchmem \
        -benchtime="$BENCH_TIME" \
        -count="$BENCH_COUNT" \
        -run='^$' \
        -timeout=300s \
        2>&1 | tee -a "$RESULTS_FILE"
    echo ""
done

# Print summary table
echo ""
echo "╔══════════════════════════════════════════════════════════════╗"
echo "║                    Benchmark Summary                        ║"
echo "╠══════════════════════════════════════════════════════════════╣"
echo ""

# Parse and format results into a table
printf "%-45s %12s %12s %12s\n" "Benchmark" "ns/op" "B/op" "allocs/op"
printf "%-45s %12s %12s %12s\n" "─────────────────────────────────────────────" "────────────" "────────────" "────────────"

grep -E '^Benchmark' "$RESULTS_FILE" | while read -r line; do
    # Parse: BenchmarkName-N    iterations    ns/op    B/op    allocs/op
    name=$(echo "$line" | awk '{print $1}' | sed 's/-[0-9]*$//')
    nsop=$(echo "$line" | grep -oE '[0-9.]+ ns/op' | awk '{print $1}')
    bop=$(echo "$line" | grep -oE '[0-9]+ B/op' | awk '{print $1}')
    allocsop=$(echo "$line" | grep -oE '[0-9]+ allocs/op' | awk '{print $1}')

    # Truncate long names
    if [[ ${#name} -gt 45 ]]; then
        name="${name:0:42}..."
    fi

    printf "%-45s %12s %12s %12s\n" "$name" "${nsop:-N/A}" "${bop:-N/A}" "${allocsop:-N/A}"
done

echo ""
echo "╚══════════════════════════════════════════════════════════════╝"

# Save results if requested
if [[ -n "$SAVE_FILE" ]]; then
    cp "$RESULTS_FILE" "$SAVE_FILE"
    echo ""
    echo "✅ Results saved to: $SAVE_FILE"
fi

# Compare against baseline if requested
if [[ -n "$COMPARE_FILE" ]]; then
    if ! command -v benchstat &>/dev/null; then
        echo ""
        echo "⚠️  benchstat not found. Install with:"
        echo "   go install golang.org/x/perf/cmd/benchstat@latest"
        echo ""
        echo "Manual comparison: diff $COMPARE_FILE $RESULTS_FILE"
    else
        echo ""
        echo "━━━ Comparison (baseline vs current) ━━━"
        echo ""
        benchstat "$COMPARE_FILE" "$RESULTS_FILE"
    fi
fi
