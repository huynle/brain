"""Matched P4.9 boot measurement; no checkout edits, commits or live services.

Usage: python3 scripts/compare-single-boot.py BASELINE_CHECKOUT OUTPUT_DIRECTORY
Creates Go overlays so EXACTLY this checkout's benchmark source runs in both
versions. Output directory must not exist. Uses 30 pairs per fixture, alternating
AB/BA order; two unrecorded warmup pairs per fixture. No parallel measurements.
"""

import hashlib
import json
import os
from pathlib import Path
import random
import re
import statistics
import subprocess
import sys


def main():
    current = Path(__file__).resolve().parents[1]
    baseline = Path(sys.argv[1]).resolve()
    output = Path(sys.argv[2]).resolve()
    output.mkdir()  # Refuse overwriting previous evidence.
    source = current / "internal/apiserver/single_boot_comparison_test.go"
    env = {"PATH": os.environ["PATH"], "HOME": str(output),
           "TMPDIR": str(output), "CI": "1", "GOMAXPROCS": "14"}
    binaries = {}
    for name, checkout in (("baseline", baseline), ("current", current)):
        overlay = output / (name + "-overlay.json")
        overlay.write_text(json.dumps({"Replace": {
            str(checkout / source.relative_to(current)): str(source)}}))
        binary = output / (name + ".test")
        subprocess.run(["go", "test", "-c", "-overlay", str(overlay),
                        "-o", str(binary), "./internal/apiserver"],
                       cwd=checkout, check=True)
        binaries[name] = binary
    samples = []
    pattern = re.compile(r"^BenchmarkSingleBootComparison/\S+\s+1\s+(\d+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op", re.M)
    for fixture in ("fresh-empty", "existing-empty"):
        for pair in range(-2, 30):
            order = ("baseline", "current") if pair % 2 == 0 else ("current", "baseline")
            for version in order:
                result = subprocess.run([str(binaries[version]), "-test.run=^$",
                    "-test.bench=^BenchmarkSingleBootComparison$/^" + fixture + "$",
                    "-test.benchtime=1x", "-test.count=1", "-test.timeout=1m"],
                    cwd=output, env=env, text=True, stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT, check=False)
                (output / f"{fixture}-{pair}-{version}.txt").write_text(result.stdout)
                if result.returncode:
                    raise RuntimeError(result.stdout)
                match = pattern.search(result.stdout)
                if not match:
                    raise RuntimeError(result.stdout)
                row = dict(fixture=fixture, pair=pair, version=version,
                           ns=int(match[1]), bytes=int(match[2]), allocs=int(match[3]))
                print(json.dumps(row), flush=True)
                if pair >= 0:
                    samples.append(row)
    report = {"source_sha256": hashlib.sha256(source.read_bytes()).hexdigest(),
              "samples": samples, "summary": {}}
    for fixture in ("fresh-empty", "existing-empty"):
        values = {v: [r for r in samples if r["fixture"] == fixture and r["version"] == v]
                  for v in binaries}
        summary = {}
        for version, rows in values.items():
            ns = sorted(r["ns"] for r in rows)
            summary[version] = {"median_ns": statistics.median(ns),
                "min_ns": min(ns), "max_ns": max(ns), "p95_ns": ns[28],
                "median_bytes": statistics.median(r["bytes"] for r in rows),
                "median_allocs": statistics.median(r["allocs"] for r in rows)}
        delta = summary["current"]["median_ns"] - summary["baseline"]["median_ns"]
        rng = random.Random(49)
        bootstrap = []
        for _ in range(10000):
            indices = rng.choices(range(30), k=30)
            bootstrap.append(statistics.median(values["current"][i]["ns"] for i in indices)
                             - statistics.median(values["baseline"][i]["ns"] for i in indices))
        bootstrap.sort()
        summary.update(delta_ns=delta, delta_percent=100 * delta / summary["baseline"]["median_ns"],
                       paired_bootstrap_95_delta_ns=[bootstrap[250], bootstrap[9749]])
        report["summary"][fixture] = summary
    (output / "results.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report["summary"], indent=2))


if __name__ == "__main__":
    main()
