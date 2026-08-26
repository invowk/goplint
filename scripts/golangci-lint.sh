#!/usr/bin/env bash
# SPDX-License-Identifier: MPL-2.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

GO_CMD="${GO_CMD:-go}"
GOLANGCI_LINT_TOOL="golangci-lint"
GOLANGCI_LINT_MODULE="github.com/golangci/golangci-lint/v2"
GOLANGCI_LINT_VERSION="v2.13.1"

GOLANGCI_LINT_BIN=""

usage() {
	cat <<'EOF'
Usage: scripts/golangci-lint.sh <command> [args...]

Commands:
  run            Run golangci-lint for the module.
  fmt            Check formatter diffs for the module.
  config-verify  Verify the golangci-lint config file.
  linters        Print effective linter JSON for the module.
  version        Print the normalized golangci-lint version.
  help           Show this help.

Environment:
  GO_CMD         Go command used to resolve the pinned tool (default: go).
EOF
}

die() {
	printf 'ERROR: %s\n' "$*" >&2
	exit 1
}

golangci_lint_binary() {
	(cd "$REPO_ROOT" && "$GO_CMD" tool -n "$GOLANGCI_LINT_TOOL")
}

verify_golangci_lint_version() {
	local binary
	local resolved_version

	if [[ -n "$GOLANGCI_LINT_BIN" ]]; then
		return 0
	fi

	binary="$(golangci_lint_binary)" ||
		die "failed to resolve $GOLANGCI_LINT_TOOL from the go.mod tool directive"
	[[ -x "$binary" ]] || die "resolved $GOLANGCI_LINT_TOOL is not executable: $binary"

	resolved_version="$("$GO_CMD" version -m "$binary" | awk -v module="$GOLANGCI_LINT_MODULE" '$1 == "mod" && $2 == module {print $3; found=1} END {if (!found) exit 1}')" ||
		die "failed to read $GOLANGCI_LINT_MODULE version from $binary"

	if [[ "$resolved_version" != "$GOLANGCI_LINT_VERSION" ]]; then
		die "expected $GOLANGCI_LINT_MODULE $GOLANGCI_LINT_VERSION, got $resolved_version"
	fi

	GOLANGCI_LINT_BIN="$binary"
}

run_lint() {
	verify_golangci_lint_version
	printf 'Running golangci-lint...\n'
	(cd "$REPO_ROOT" && "$GOLANGCI_LINT_BIN" run --config=.golangci.toml "$@" ./...)
}

run_format_check() {
	verify_golangci_lint_version
	printf 'Checking golangci-lint formatters...\n'
	(cd "$REPO_ROOT" && "$GOLANGCI_LINT_BIN" fmt --config=.golangci.toml --diff "$@")
}

run_config_verify() {
	verify_golangci_lint_version
	printf 'Verifying golangci-lint config...\n'
	(cd "$REPO_ROOT" && "$GOLANGCI_LINT_BIN" config verify --config=.golangci.toml "$@")
}

print_linters() {
	verify_golangci_lint_version
	printf 'Effective golangci-lint linters:\n' >&2
	(cd "$REPO_ROOT" && "$GOLANGCI_LINT_BIN" linters --config=.golangci.toml --json "$@")
}

print_version() {
	verify_golangci_lint_version
	"$GOLANGCI_LINT_BIN" version
}

main() {
	local command="${1:-help}"

	if [[ $# -gt 0 ]]; then
		shift
	fi

	case "$command" in
		run)
			run_lint "$@"
			;;
		fmt)
			run_format_check "$@"
			;;
		config-verify)
			run_config_verify "$@"
			;;
		linters)
			print_linters "$@"
			;;
		version)
			print_version
			;;
		help|--help|-h)
			usage
			;;
		*)
			usage >&2
			die "unknown golangci-lint command: $command"
			;;
	esac
}

if [[ "${GOPLINT_GOLANGCI_LINT_TESTING:-0}" != "1" ]]; then
	main "$@"
fi
