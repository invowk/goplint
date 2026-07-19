#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
THRESHOLDS_FILE="${1:-$ROOT_DIR/tools/goplint/bench/thresholds.toml}"

if [[ ! -f "$THRESHOLDS_FILE" ]]; then
  echo "threshold file not found: $THRESHOLDS_FILE" >&2
  exit 1
fi

toml_root_value() {
  local key="$1"
  awk -F '=' -v key="$key" '
    /^\[/ { exit }
    $1 ~ "^[[:space:]]*" key "[[:space:]]*$" {
      value=$2
      gsub(/^[[:space:]"]+|[[:space:]"]+$/, "", value)
      print value
      exit
    }
  ' "$THRESHOLDS_FILE"
}

toml_section_value() {
  local section="$1"
  local key="$2"
  awk -F '=' -v section="[$section]" -v key="$key" '
    $0 == section { active=1; next }
    active && /^\[/ { exit }
    active && $1 ~ "^[[:space:]]*" key "[[:space:]]*$" {
      value=$2
      gsub(/^[[:space:]"]+|[[:space:]"]+$/, "", value)
      print value
      exit
    }
  ' "$THRESHOLDS_FILE"
}

median_five() {
  if [[ $# -ne 5 ]]; then
    echo "median_five requires exactly five samples, got $#" >&2
    return 1
  fi
  printf '%s\n' "$@" | sort -n | awk 'NR == 3 { print; exit }'
}

compare_metric() {
  local name="$1"
  local metric="$2"
  local observed="$3"
  local threshold="$4"
  if awk -v observed="$observed" -v threshold="$threshold" 'BEGIN { exit !(observed > threshold) }'; then
    echo "$name exceeded $metric threshold: median $observed > $threshold" >&2
    return 1
  fi
  echo "$name $metric median: $observed (limit $threshold)"
}

format_version="$(toml_root_value format_version)"
samples="$(toml_root_value samples)"
expected_toolchain="$(toml_root_value go_toolchain)"
runner_class="$(toml_root_value runner_class)"
if [[ "$format_version" != "2" || "$samples" != "5" || -z "$runner_class" ]]; then
  echo "threshold manifest must use format_version=2, samples=5, and a reviewed runner_class" >&2
  exit 1
fi
actual_toolchain="$(go env GOVERSION)"
if [[ "$actual_toolchain" != "$expected_toolchain"* ]]; then
  echo "benchmark toolchain mismatch: got $actual_toolchain, manifest requires $expected_toolchain" >&2
  exit 1
fi
echo "Benchmark policy: toolchain=$actual_toolchain runner_class=$runner_class samples=$samples"

benchmarks=(
  BenchmarkProtocolCanonicalSolver
  BenchmarkProtocolRecursiveTabulation
  BenchmarkProtocolAliasJoin
  BenchmarkProtocolRefinementEvidence
  BenchmarkProtocolReferenceInterpreter
  BenchmarkProtocolGeneratedAnalyzer
  BenchmarkConstructorSuccessfulReturnClassification
  BenchmarkProtocolPackageProcedureInventory
)
benchmark_pattern="^($(IFS='|'; echo "${benchmarks[*]}"))$"
bench_output="$({
  cd "$ROOT_DIR/tools/goplint"
  go test -run '^$' -bench "$benchmark_pattern" -benchmem -count="$samples" ./goplint
})"
echo "$bench_output"

status=0
for benchmark in "${benchmarks[@]}"; do
  section="benchmarks.$benchmark"
  max_ns="$(toml_section_value "$section" max_ns_per_op)"
  max_bytes="$(toml_section_value "$section" max_bytes_per_op)"
  max_allocs="$(toml_section_value "$section" max_allocs_per_op)"
  if [[ -z "$max_ns" || -z "$max_bytes" || -z "$max_allocs" ]]; then
    echo "missing time/bytes/allocations threshold for $benchmark" >&2
    status=1
    continue
  fi
  mapfile -t rows < <(
    awk -v benchmark="$benchmark" '
      $1 ~ ("^" benchmark "-") {
        ns=""; bytes=""; allocs=""
        for (i=2; i<=NF; i++) {
          if ($i == "ns/op") ns=$(i-1)
          if ($i == "B/op") bytes=$(i-1)
          if ($i == "allocs/op") allocs=$(i-1)
        }
        print ns, bytes, allocs
      }
    ' <<<"$bench_output"
  )
  if [[ ${#rows[@]} -ne 5 ]]; then
    echo "$benchmark produced ${#rows[@]} samples, want 5" >&2
    status=1
    continue
  fi
  ns_samples=()
  byte_samples=()
  alloc_samples=()
  for row in "${rows[@]}"; do
    read -r ns bytes allocs <<<"$row"
    ns_samples+=("$ns")
    byte_samples+=("$bytes")
    alloc_samples+=("$allocs")
  done
  compare_metric "$benchmark" ns_per_op "$(median_five "${ns_samples[@]}")" "$max_ns" || status=1
  compare_metric "$benchmark" bytes_per_op "$(median_five "${byte_samples[@]}")" "$max_bytes" || status=1
  compare_metric "$benchmark" allocs_per_op "$(median_five "${alloc_samples[@]}")" "$max_allocs" || status=1

  if [[ "$benchmark" == "BenchmarkProtocolRecursiveTabulation" ]]; then
    max_path_edges="$(toml_section_value "$section" max_path_edges_per_op)"
    min_summary_reuses="$(toml_section_value "$section" min_summary_reuses_per_op)"
    mapfile -t path_edge_samples < <(awk -v benchmark="$benchmark" '$1 ~ ("^" benchmark "-") { for (i=2; i<=NF; i++) if ($i == "path-edges/op") print $(i-1) }' <<<"$bench_output")
    mapfile -t summary_reuse_samples < <(awk -v benchmark="$benchmark" '$1 ~ ("^" benchmark "-") { for (i=2; i<=NF; i++) if ($i == "summary-reuses/op") print $(i-1) }' <<<"$bench_output")
    if [[ ${#path_edge_samples[@]} -ne 5 || ${#summary_reuse_samples[@]} -ne 5 || -z "$max_path_edges" || -z "$min_summary_reuses" ]]; then
      echo "$benchmark missing reviewed state-count or summary-reuse evidence" >&2
      status=1
    else
      compare_metric "$benchmark" path_edges_per_op "$(median_five "${path_edge_samples[@]}")" "$max_path_edges" || status=1
      median_reuses="$(median_five "${summary_reuse_samples[@]}")"
      if awk -v observed="$median_reuses" -v threshold="$min_summary_reuses" 'BEGIN { exit !(observed < threshold) }'; then
        echo "$benchmark summary reuse median $median_reuses below required $min_summary_reuses" >&2
        status=1
      else
        echo "$benchmark summary_reuses_per_op median: $median_reuses (minimum $min_summary_reuses)"
      fi
    fi
  fi
done

if [[ ! -x /usr/bin/time ]]; then
  echo "/usr/bin/time is required for repository full-scan thresholds" >&2
  exit 1
fi
make -s -C "$ROOT_DIR" build-goplint
scan_wall_samples=()
scan_rss_samples=()
for _ in 1 2 3 4 5; do
  time_file="$(mktemp)"
  set +e
  (cd "$ROOT_DIR" && /usr/bin/time -f 'goplint-bench-time %e %M' -o "$time_file" \
    ./bin/goplint -test=false -check-all -check-enum-sync \
    -baseline=tools/goplint/baseline.toml \
    -config=tools/goplint/exceptions.toml \
    ./cmd/... ./internal/... ./pkg/... >/dev/null 2>&1)
  scan_status=$?
  set -e
  if [[ $scan_status -ne 0 && $scan_status -ne 3 ]]; then
    rm -f "$time_file"
    echo "canonical repository benchmark scan failed" >&2
    exit 1
  fi
  time_metrics="$(awk '$1 == "goplint-bench-time" { print $2, $3 }' "$time_file" | tail -n 1)"
  read -r elapsed_seconds peak_kib <<<"$time_metrics"
  if [[ ! "$elapsed_seconds" =~ ^[0-9]+([.][0-9]+)?$ || ! "$peak_kib" =~ ^[0-9]+$ ]]; then
    time_output="$(tr '\n' ' ' <"$time_file")"
    rm -f "$time_file"
    echo "could not parse repository benchmark metrics: $time_output" >&2
    exit 1
  fi
  rm -f "$time_file"
  scan_wall_samples+=("$(awk -v seconds="$elapsed_seconds" 'BEGIN { printf "%.0f", seconds * 1000 }')")
  scan_rss_samples+=("$((peak_kib * 1024))")
done

max_scan_wall="$(toml_section_value repository_full_scan max_wall_ms)"
max_scan_bytes="$(toml_section_value repository_full_scan max_peak_bytes)"
compare_metric repository_full_scan wall_ms "$(median_five "${scan_wall_samples[@]}")" "$max_scan_wall" || status=1
compare_metric repository_full_scan peak_bytes "$(median_five "${scan_rss_samples[@]}")" "$max_scan_bytes" || status=1

if [[ $status -eq 0 ]]; then
  (
    cd "$ROOT_DIR/tools/goplint"
    go test -count=1 ./goplint -run '^TestEmitProtocolBenchmarkEvidence$'
  )
fi

exit "$status"
