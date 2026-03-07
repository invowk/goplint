#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
FIXTURE_SRC="${ROOT_DIR}/tools/goplint/goplint/testdata/src/cfa_ssa_alias/cfa_ssa_alias.go"
BIN_PATH="${ROOT_DIR}/bin/goplint"
TMP_DIR="$(mktemp -d)"

cleanup() {
	rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

if [[ ! -f "${FIXTURE_SRC}" ]]; then
	echo "alias fixture not found: ${FIXTURE_SRC}" >&2
	exit 1
fi

if [[ ! -x "${BIN_PATH}" ]]; then
	echo "goplint binary not found at ${BIN_PATH}; run make build-goplint or make check-cfg-alias" >&2
	exit 1
fi

echo "Running goplint Phase D alias gate..."

mkdir -p "${TMP_DIR}/fixture"
cp "${FIXTURE_SRC}" "${TMP_DIR}/fixture/"

cat >"${TMP_DIR}/go.mod" <<'EOF'
module example.com/goplintaliasgate

go 1.26
EOF

fixture_file="${TMP_DIR}/fixture/cfa_ssa_alias.go"
off_output="${TMP_DIR}/alias-mode-off.txt"
ssa_output="${TMP_DIR}/alias-mode-ssa.txt"

line_in_function() {
	local function_name="$1"
	local line_fragment="$2"
	local line

	line="$(
		awk -v function_name="${function_name}" -v line_fragment="${line_fragment}" '
			$0 ~ "^func " function_name "\\(" { in_function = 1 }
			in_function && index($0, line_fragment) { print NR; exit }
			in_function && $0 ~ "^}" { in_function = 0 }
		' "${fixture_file}"
	)"

	if [[ -z "${line}" ]]; then
		echo "failed to locate ${line_fragment} inside ${function_name} in ${fixture_file}" >&2
		exit 1
	fi

	printf '%s\n' "${line}"
}

run_alias_mode() {
	local alias_mode="$1"
	local output_path="$2"
	local status=0

	(
		cd "${TMP_DIR}"
		set +e
		GOCACHE="${GOCACHE:-/tmp/go-build}" "${BIN_PATH}" \
			-check-cast-validation \
			-cfg-interproc-engine=legacy \
			-cfg-feasibility-engine=off \
			-cfg-refinement-mode=off \
			-cfg-alias-mode="${alias_mode}" \
			./... >"${output_path}" 2>&1
		status=$?
		set -e
		printf '%s\n' "${status}" >"${output_path}.status"
	)
}

require_nonzero_status() {
	local output_path="$1"
	local label="$2"
	local status

	status="$(<"${output_path}.status")"
	if [[ "${status}" -eq 0 ]]; then
		echo "expected ${label} run to report findings, but it exited cleanly" >&2
		cat "${output_path}" >&2
		exit 1
	fi
}

require_diagnostic_line() {
	local output_path="$1"
	local line="$2"
	local label="$3"

	if ! grep -Fq "${fixture_file}:${line}:" "${output_path}"; then
		echo "expected ${label} diagnostic at ${fixture_file}:${line}, but it was missing" >&2
		cat "${output_path}" >&2
		exit 1
	fi
}

reject_diagnostic_line() {
	local output_path="$1"
	local line="$2"
	local label="$3"

	if grep -Fq "${fixture_file}:${line}:" "${output_path}"; then
		echo "expected ${label} diagnostic at ${fixture_file}:${line} to be absent" >&2
		cat "${output_path}" >&2
		exit 1
	fi
}

copy_alias_line="$(line_in_function "CopyAliasValidated" "x := AliasTarget(raw)")"
multi_hop_line="$(line_in_function "MultiHopAlias" "x := AliasTarget(raw)")"
no_alias_line="$(line_in_function "NoAlias" "x := AliasTarget(raw1)")"
reassignment_line="$(line_in_function "ReassignmentBreaksAlias" "x := AliasTarget(raw1)")"
direct_validation_line="$(line_in_function "DirectValidation" "x := AliasTarget(raw)")"
partial_branch_line="$(line_in_function "PartialBranchAlias" "x := AliasTarget(raw)")"

run_alias_mode "off" "${off_output}"
run_alias_mode "ssa" "${ssa_output}"

require_nonzero_status "${off_output}" "alias-mode=off"
require_nonzero_status "${ssa_output}" "alias-mode=ssa"

# Off mode must preserve the pre-Phase-D behavior on alias-only validations.
require_diagnostic_line "${off_output}" "${copy_alias_line}" "CopyAliasValidated"
require_diagnostic_line "${off_output}" "${multi_hop_line}" "MultiHopAlias"

# SSA mode must improve the curated alias fixtures.
reject_diagnostic_line "${ssa_output}" "${copy_alias_line}" "CopyAliasValidated"
reject_diagnostic_line "${ssa_output}" "${multi_hop_line}" "MultiHopAlias"

# Negative controls must continue reporting in both modes.
require_diagnostic_line "${off_output}" "${no_alias_line}" "NoAlias"
require_diagnostic_line "${ssa_output}" "${no_alias_line}" "NoAlias"
require_diagnostic_line "${off_output}" "${reassignment_line}" "ReassignmentBreaksAlias"
require_diagnostic_line "${ssa_output}" "${reassignment_line}" "ReassignmentBreaksAlias"
require_diagnostic_line "${off_output}" "${partial_branch_line}" "PartialBranchAlias"
require_diagnostic_line "${ssa_output}" "${partial_branch_line}" "PartialBranchAlias"

# Direct validation should remain safe regardless of alias mode.
reject_diagnostic_line "${off_output}" "${direct_validation_line}" "DirectValidation"
reject_diagnostic_line "${ssa_output}" "${direct_validation_line}" "DirectValidation"

echo "Phase D alias gate passed: alias mode stays opt-in and improves curated alias fixtures."
