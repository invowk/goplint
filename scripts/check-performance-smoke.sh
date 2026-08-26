#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
# The repository smoke-scan target is parameterizable so a standalone goplint
# checkout can smoke against an external consumer corpus. Defaults preserve
# this repository's canonical invowk scan.
SCAN_ROOT="${GOPLINT_SCAN_ROOT:-$ROOT_DIR}"
SCAN_BASELINE="${GOPLINT_SCAN_BASELINE:-baseline.toml}"
SCAN_EXCEPTIONS="${GOPLINT_SCAN_EXCEPTIONS:-exceptions.toml}"
read -r -a SCAN_PACKAGES <<<"${GOPLINT_SCAN_PACKAGES:-./...}"
POLICY_FILE="${1:-${GOPLINT_BENCH_SMOKE_POLICY:-$ROOT_DIR/bench/consumer-smoke.github-ubuntu-x64-4cpu.toml}}"
if [[ "$POLICY_FILE" != /* ]]; then
  POLICY_FILE="$ROOT_DIR/$POLICY_FILE"
fi

if [[ ! -f "$POLICY_FILE" ]]; then
  echo "smoke policy file not found: $POLICY_FILE" >&2
  exit 1
fi

# The smoke policy is reviewed repository content. Unlike the scan target, it
# may never be redirected out of the tree: an out-of-tree manifest would let an
# inherited environment variable weaken the policy this gate smokes against
# without appearing in any tree digest.
ROOT_DIR_REAL="$(cd "$ROOT_DIR" && pwd -P)"
POLICY_FILE="$(cd "$(dirname "$POLICY_FILE")" && pwd -P)/$(basename "$POLICY_FILE")"
if [[ "$POLICY_FILE" != "$ROOT_DIR_REAL/"* ]]; then
  echo "smoke policy file must be inside the repository: $POLICY_FILE (root: $ROOT_DIR_REAL)" >&2
  exit 2
fi

(
  cd "$ROOT_DIR"
  go run ./cmd/benchmark-policy -manifest "$POLICY_FILE" -policy consumer-smoke
)

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
  ' "$POLICY_FILE"
}

compare_upper() {
  local name="$1"
  local metric="$2"
  local observed="$3"
  local limit="$4"
  if awk -v observed="$observed" -v limit="$limit" 'BEGIN { exit !(observed > limit) }'; then
    echo "$name catastrophic smoke regression: $metric $observed > $limit" >&2
    return 1
  fi
  echo "$name smoke $metric: $observed (catastrophic limit $limit)"
}

benchmarks=(BenchmarkProtocolCanonicalSolver BenchmarkProtocolRecursiveTabulation)
benchmark_pattern="^($(IFS='|'; echo "${benchmarks[*]}"))$"
bench_output="$({
  cd "$ROOT_DIR"
  "${SCRIPT_DIR}/soundness-go-test.sh" -run '^$' -bench "$benchmark_pattern" -benchmem -benchtime=1x -count=1 ./goplint
})"
echo "$bench_output"

for benchmark in "${benchmarks[@]}"; do
  # GOMAXPROCS=1 reservations omit the -N benchmark name suffix entirely.
  row="$(awk -v benchmark="$benchmark" '$1 == benchmark || $1 ~ ("^" benchmark "-") { print; exit }' <<<"$bench_output")"
  if [[ -z "$row" ]]; then
    echo "$benchmark produced no consumer smoke sample" >&2
    exit 1
  fi
  ns="$(awk '{ for (i=2; i<=NF; i++) if ($i == "ns/op") print $(i-1) }' <<<"$row")"
  bytes="$(awk '{ for (i=2; i<=NF; i++) if ($i == "B/op") print $(i-1) }' <<<"$row")"
  allocs="$(awk '{ for (i=2; i<=NF; i++) if ($i == "allocs/op") print $(i-1) }' <<<"$row")"
  section="benchmarks.$benchmark"
  compare_upper "$benchmark" ns_per_op "$ns" "$(toml_section_value "$section" max_ns_per_op)"
  compare_upper "$benchmark" bytes_per_op "$bytes" "$(toml_section_value "$section" max_bytes_per_op)"
  compare_upper "$benchmark" allocs_per_op "$allocs" "$(toml_section_value "$section" max_allocs_per_op)"
  if [[ "$benchmark" == "BenchmarkProtocolRecursiveTabulation" ]]; then
    path_edges="$(awk '{ for (i=2; i<=NF; i++) if ($i == "path-edges/op") print $(i-1) }' <<<"$row")"
    summary_reuses="$(awk '{ for (i=2; i<=NF; i++) if ($i == "summary-reuses/op") print $(i-1) }' <<<"$row")"
    compare_upper "$benchmark" path_edges_per_op "$path_edges" "$(toml_section_value "$section" max_path_edges_per_op)"
    minimum_reuses="$(toml_section_value "$section" min_summary_reuses_per_op)"
    if awk -v observed="$summary_reuses" -v minimum="$minimum_reuses" 'BEGIN { exit !(observed < minimum) }'; then
      echo "$benchmark summary reuse smoke $summary_reuses below $minimum_reuses" >&2
      exit 1
    fi
  fi
done

if [[ -n "${GOPLINT_REPOSITORY_AUDIT_PATH:-}" ]]; then
  if [[ ! -f "$GOPLINT_REPOSITORY_AUDIT_PATH" ]]; then
    echo "bound repository audit is missing: $GOPLINT_REPOSITORY_AUDIT_PATH" >&2
    exit 1
  fi
  # The consumer binding digests bin/goplint; rebuild it so a distributed
  # worker on a fresh checkout reproduces the producer's analyzer digest.
  make -s -C "$SCAN_ROOT" build-goplint
  (
    cd "$ROOT_DIR"
    go run ./cmd/repository-audit -mode full-scan
  )
  wall_ns="$(jq -er '.scan.wall_duration_nanoseconds' "$GOPLINT_REPOSITORY_AUDIT_PATH")"
  peak_bytes="$(jq -er '.scan.peak_rss_bytes' "$GOPLINT_REPOSITORY_AUDIT_PATH")"
  wall_ms="$((wall_ns / 1000000))"
else
  if [[ ! -x /usr/bin/time ]]; then
    echo "/usr/bin/time is required for repository smoke measurement" >&2
    exit 1
  fi
  make -s -C "$SCAN_ROOT" build-goplint
  time_file="$(mktemp)"
  trap 'rm -f "$time_file"' EXIT
  set +e
  (cd "$SCAN_ROOT" && /usr/bin/time -f 'goplint-smoke-time %e %M' -o "$time_file" \
    ./bin/goplint -test=false -check-all -check-enum-sync \
    -baseline="$SCAN_BASELINE" \
    -config="$SCAN_EXCEPTIONS" \
    "${SCAN_PACKAGES[@]}" >/dev/null 2>&1)
  scan_status=$?
  set -e
  if [[ $scan_status -ne 0 && $scan_status -ne 3 ]]; then
    echo "canonical repository smoke scan failed" >&2
    exit 1
  fi
  read -r elapsed_seconds peak_kib < <(awk '$1 == "goplint-smoke-time" { print $2, $3 }' "$time_file")
  wall_ms="$(awk -v seconds="$elapsed_seconds" 'BEGIN { printf "%.0f", seconds * 1000 }')"
  peak_bytes="$((peak_kib * 1024))"
fi
compare_upper repository_full_scan wall_ms "$wall_ms" "$(toml_section_value repository_full_scan max_wall_ms)"
compare_upper repository_full_scan peak_bytes "$peak_bytes" "$(toml_section_value repository_full_scan max_peak_bytes)"

cd "$ROOT_DIR"
go run ./cmd/subgate-report -observation consumer-performance-smoke=single-sample
