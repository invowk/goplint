#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
PHASE="all"
GROUP=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --phase)
      PHASE="${2:-}"
      shift 2
      ;;
    --group)
      GROUP="${2:-}"
      shift 2
      ;;
    *)
      echo "usage: $0 [--phase all|supporting|analyzer] [--group index/count]" >&2
      exit 2
      ;;
  esac
done
if [[ ( "$PHASE" != "all" && "$PHASE" != "supporting" && "$PHASE" != "analyzer" ) ]]; then
  echo "usage: $0 [--phase all|supporting|analyzer] [--group index/count]" >&2
  exit 2
fi
if [[ -n "$GROUP" && "$PHASE" != "analyzer" ]]; then
  echo "--group requires --phase analyzer" >&2
  exit 2
fi

cd "${MODULE_DIR}"

# The aggregate gate binds this script to one report path and evidence
# directory. The package tests include independent evidence producers, so
# parallel race/repeat processes must not publish into this subgate's output.
# Only the final subgate-report command below owns those aggregate outputs.
run_test_without_soundness_output() {
  (
    unset GOPLINT_SOUNDNESS_EVIDENCE_DIR
    unset GOPLINT_SOUNDNESS_SUBGATE_REPORT_PATH
    GOCACHE="${GOCACHE:-/tmp/go-build}" "${SCRIPT_DIR}/soundness-go-test.sh" "$@"
  )
}

repeat_count=3
if [[ "$PHASE" == "all" || "$PHASE" == "supporting" ]]; then
  module_path="$(go list -m)"
  goplint_package="${module_path}/goplint"
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
  echo "Repeating non-analyzer goplint packages..."
  run_test_without_soundness_output -count="${repeat_count}" -timeout=20m "${other_packages[@]}"

  supporting_observations=(-observation supporting-race-runs=complete-supporting-race-profile)
  for ((iteration = 1; iteration <= repeat_count; iteration++)); do
    supporting_observations+=(-observation "supporting-repeat-runs=complete-supporting-repeat-${iteration}")
  done
  GOCACHE="${GOCACHE:-/tmp/go-build}" go run ./cmd/subgate-report "${supporting_observations[@]}"
fi

# The analyzer package is compiled once normally and once with the race
# detector. Its exact live census is then allocated with reviewed timing
# weights and every work result is validated from structured test2json events.
if [[ "$PHASE" == "all" || "$PHASE" == "analyzer" ]]; then
  echo "Running balanced build-once analyzer race/repeat work..."
  # Work-group executions keep two CPUs per shard so the heaviest
  # race-detector shard stays well inside its weight-derived timeout on
  # constrained four-CPU runners.
  default_workers=4
  if [[ -n "$GROUP" ]]; then
    default_workers=2
  fi
  race_repeat_workers="${GOPLINT_RACE_REPEAT_WORKERS:-$default_workers}"
  if [[ -z "${GOPLINT_RACE_REPEAT_WORKERS:-}" && "${GOMAXPROCS:-}" =~ ^[1-9][0-9]*$ && "$GOMAXPROCS" -lt "$race_repeat_workers" ]]; then
    race_repeat_workers="$GOMAXPROCS"
  fi
  group_args=()
  if [[ -n "$GROUP" ]]; then
    group_args+=(-work-group "$GROUP")
  fi
  (
    unset GOPLINT_SOUNDNESS_EVIDENCE_DIR
    unset GOPLINT_SOUNDNESS_SUBGATE_REPORT_PATH
    GOCACHE="${GOCACHE:-/tmp/go-build}" go run ./cmd/race-repeat \
      -timings spec/goplint-test-timings.v1.json \
      -repeat "${repeat_count}" \
      -max-workers "$race_repeat_workers" \
      "${group_args[@]}"
  )

  analyzer_observations=(-observation race-runs=complete-race-profile)
  for ((iteration = 1; iteration <= repeat_count; iteration++)); do
    analyzer_observations+=(-observation "repeat-runs=complete-repeat-${iteration}")
  done
  GOCACHE="${GOCACHE:-/tmp/go-build}" go run ./cmd/subgate-report "${analyzer_observations[@]}"
fi
