#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
EVENT="${GOPLINT_SOUNDNESS_EVENT:-pre_commit}"
BASE="${GOPLINT_SOUNDNESS_BASE:-}"
HEAD="${GOPLINT_SOUNDNESS_HEAD:-HEAD}"
route_args=(
  -root .
  -manifest spec/soundness-ownership.v2.json
  -event "$EVENT"
  -format profile
)
if [[ "$EVENT" == "pre_commit" ]]; then
  route_args+=(-staged)
elif [[ -n "$BASE" ]]; then
  route_args+=(-base "$BASE" -head "$HEAD")
fi

cd "$MODULE_DIR"
profile="$(go run ./cmd/soundness-profile "${route_args[@]}")"
echo "Selected goplint soundness profile: $profile"

# The local pre-commit surface is capped below the semantic tier: harness and
# analyzer-semantics diffs run the consumer tier here, and continuous
# integration remains the authoritative executor for the routed tier.
if [[ "$EVENT" == "pre_commit" ]]; then
  case "$profile" in
    harness | semantic | complete)
      echo "Pre-commit caps local execution at the consumer tier;" \
        "continuous integration runs the authoritative '$profile' profile for this diff."
      echo "Run 'make check-goplint-soundness-semantic' or" \
        "'make check-goplint-soundness-complete' to execute the heavier tier locally."
      profile="consumer"
      ;;
  esac
fi

# One-release migration escape hatch: forces the semantic profile regardless
# of routing or the pre-commit cap. Remove after one release ships with no
# misrouting diagnosis needing it (see docs/goplint/soundness-gate-execution.md).
if [[ "${GOPLINT_FORCE_SEMANTIC:-}" == "1" && "$profile" != "complete" ]]; then
  echo "GOPLINT_FORCE_SEMANTIC=1 escalates the routed profile to semantic"
  profile="semantic"
fi

if [[ "$profile" == "documentation" ]]; then
  go run ./cmd/docs-guard -root .
  exit 0
fi

go run ./cmd/soundness-gate \
  -root . \
  -manifest spec/soundness-gate.v1.json \
  -profile "$profile"
