#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
THRESHOLDS_FILE="${1:-$ROOT_DIR/tools/goplint/bench/thresholds.toml}"

if [[ ! -f "$THRESHOLDS_FILE" ]]; then
  echo "threshold file not found: $THRESHOLDS_FILE" >&2
  exit 1
fi

declare -A thresholds
while IFS= read -r line; do
  [[ -z "$line" ]] && continue
  [[ "$line" =~ ^[[:space:]]*# ]] && continue
  if [[ "$line" =~ ^([A-Za-z0-9_]+)[[:space:]]*=[[:space:]]*([0-9]+)[[:space:]]*$ ]]; then
    thresholds["${BASH_REMATCH[1]}"]="${BASH_REMATCH[2]}"
  fi
done <"$THRESHOLDS_FILE"

if [[ ${#thresholds[@]} -eq 0 ]]; then
  echo "no benchmark thresholds defined in $THRESHOLDS_FILE" >&2
  exit 1
fi

bench_output="$(
  cd "$ROOT_DIR/tools/goplint"
  go test -run '^$' -bench '^BenchmarkCFGTraversal' -count=1 ./goplint
)"

echo "$bench_output"

status=0
for key in "${!thresholds[@]}"; do
  bench_name="${key%_ns_per_op}"
  threshold="${thresholds[$key]}"
  ns_per_op="$(awk -v bench="$bench_name" '$1 ~ "^"bench {print $3}' <<<"$bench_output" | tail -n1)"
  if [[ -z "$ns_per_op" ]]; then
    echo "missing benchmark result for $bench_name" >&2
    status=1
    continue
  fi
  if (( ns_per_op > threshold )); then
    echo "benchmark $bench_name exceeded threshold: got ${ns_per_op}ns/op > ${threshold}ns/op" >&2
    status=1
  fi
done

exit "$status"
