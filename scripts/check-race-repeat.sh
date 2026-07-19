#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${MODULE_DIR}"
module_path="$(go list -m)"
goplint_package="${module_path}/goplint"

# The aggregate gate binds this script to one report path and evidence
# directory. The package tests include independent evidence producers, so
# parallel race/repeat processes must not publish into this subgate's output.
# Only the final subgate-report command below owns those aggregate outputs.
run_test_without_soundness_output() {
  (
    unset GOPLINT_SOUNDNESS_EVIDENCE_DIR
    unset GOPLINT_SOUNDNESS_SUBGATE_REPORT_PATH
    GOCACHE="${GOCACHE:-/tmp/go-build}" go test "$@"
  )
}

other_packages=()
while IFS= read -r package; do
  if [[ "${package}" != "${goplint_package}" ]]; then
    other_packages+=("${package}")
  fi
done < <(go list ./...)
if (( ${#other_packages[@]} == 0 )); then
  echo "goplint race/repeat: package census unexpectedly contains only ${goplint_package}" >&2
  exit 1
fi

echo "Running non-analyzer goplint packages with the race detector..."
run_test_without_soundness_output -race -count=1 -timeout=20m "${other_packages[@]}"

# The analyzer package's production-backed evidence corpus exceeds one
# race-instrumented or three-count package-binary budget. Build exact, disjoint
# patterns from the complete top-level Test/Fuzz/Example census and distribute
# adjacent heavy families across sixteen shards, with at most four running
# concurrently.
analyzer_shard_patterns=(
  "" "" "" ""
  "" "" "" ""
  "" "" "" ""
  "" "" "" ""
)
analyzer_max_parallel=4
repeat_count=3
analyzer_test_names=()
declare -A seen_analyzer_tests=()
while IFS= read -r test_name; do
  case "${test_name}" in
    Test*|Fuzz*|Example*)
      if [[ ! "${test_name}" =~ ^(Test|Fuzz|Example)[[:alnum:]_]*$ ]]; then
        echo "goplint race/repeat: unsafe analyzer test name ${test_name}" >&2
        exit 1
      fi
      if [[ -n "${seen_analyzer_tests[${test_name}]+present}" ]]; then
        echo "goplint race/repeat: duplicate analyzer test name ${test_name}" >&2
        exit 1
      fi
      seen_analyzer_tests["${test_name}"]=1
      analyzer_test_names+=("${test_name}")
      ;;
  esac
done < <(go test -list '^(Test|Fuzz|Example)' ./goplint)
if (( ${#analyzer_test_names[@]} == 0 )); then
  echo "goplint race/repeat: analyzer test census is empty" >&2
  exit 1
fi

for index in "${!analyzer_test_names[@]}"; do
  shard_index=$((index % ${#analyzer_shard_patterns[@]}))
  if [[ -n "${analyzer_shard_patterns[shard_index]}" ]]; then
    analyzer_shard_patterns[shard_index]+="|"
  fi
  analyzer_shard_patterns[shard_index]+="${analyzer_test_names[index]}"
done
for index in "${!analyzer_shard_patterns[@]}"; do
  if [[ -z "${analyzer_shard_patterns[index]}" ]]; then
    echo "goplint race/repeat: analyzer shard ${index} is empty" >&2
    exit 1
  fi
  analyzer_shard_patterns[index]="^(${analyzer_shard_patterns[index]})$"
done

run_analyzer_shards() {
  local phase="$1"
  shift
  local -a test_args=("$@")
  local -a pids=()
  local failed=0

  echo "Running ${#analyzer_test_names[@]} analyzer tests across ${#analyzer_shard_patterns[@]} ${phase} shards..."
  for index in "${!analyzer_shard_patterns[@]}"; do
    (
      echo "Running analyzer ${phase} shard ${index}..."
      run_test_without_soundness_output "${test_args[@]}" -timeout=20m \
        -run "${analyzer_shard_patterns[index]}" ./goplint
    ) &
    pids+=("$!")
    if (( ${#pids[@]} != analyzer_max_parallel )); then
      continue
    fi
    for pid in "${pids[@]}"; do
      if ! wait "${pid}"; then
        failed=1
      fi
    done
    pids=()
    if (( failed != 0 )); then
      exit 1
    fi
  done
  for pid in "${pids[@]}"; do
    if ! wait "${pid}"; then
      failed=1
    fi
  done
  if (( failed != 0 )); then
    exit 1
  fi
}

run_analyzer_shards race -race -count=1

echo "Repeating non-analyzer goplint packages..."
run_test_without_soundness_output -count="${repeat_count}" -timeout=20m "${other_packages[@]}"
run_analyzer_shards repeat -count="${repeat_count}"
report_observations=(-observation race-runs=complete-race-profile)
for ((iteration = 1; iteration <= repeat_count; iteration++)); do
  report_observations+=(-observation "repeat-runs=complete-repeat-${iteration}")
done
GOCACHE="${GOCACHE:-/tmp/go-build}" go run ./cmd/subgate-report "${report_observations[@]}"
