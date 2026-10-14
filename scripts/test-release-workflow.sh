#!/usr/bin/env bash
set -euo pipefail

workflow="${1:-.github/workflows/go.yml}"

top_level="$(sed -n '/^permissions:/,/^[^ ]/p' "$workflow")"
release="$(sed -n '/^  release:/,$p' "$workflow")"

grep -q '^  contents: read$' <<<"$top_level"
if grep -q 'packages: write' <<<"$top_level"; then
  echo "package write must not be granted at workflow scope" >&2
  exit 1
fi
grep -q '^    permissions:$' <<<"$release"
grep -q '^      contents: read$' <<<"$release"
grep -q '^      packages: write$' <<<"$release"
grep -q "if: github.event_name == 'push' && github.ref == 'refs/heads/main'" <<<"$release"
grep -q '^    needs: \[build, lint\]$' <<<"$release"
grep -q '^    uses: huynle/.github/.github/workflows/container-release.yml@v1$' <<<"$release"

echo "Brain release workflow contract passed"
