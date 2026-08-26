#!/usr/bin/env bash
# SPDX-License-Identifier: MPL-2.0
#
# Enforces a 1000-line limit on all Go source files (production and test).
# Warns at 950 lines. Exempts testdata/ and vendor/ directories.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

LIMIT=1000
WARN_AT=900
errors=0
warnings=0

while IFS= read -r -d '' file; do
    lines=$(wc -l < "$file")
    if [[ "$lines" -gt "$LIMIT" ]]; then
        echo "ERROR: $file has $lines lines (limit: $LIMIT)"
        errors=$((errors + 1))
    elif [[ "$lines" -gt "$WARN_AT" ]]; then
        echo "WARN:  $file has $lines lines (approaching limit: $LIMIT)"
        warnings=$((warnings + 1))
    fi
done < <(find . \
    -name "*.go" \
    -not -path "*/testdata/*" \
    -not -path "*/vendor/*" \
    -not -path "*/.git/*" \
    -print0)

if [[ "$warnings" -gt 0 ]]; then
    echo ""
    echo "$warnings file(s) approaching the $LIMIT-line limit"
fi
if [[ "$errors" -gt 0 ]]; then
    echo "$errors file(s) exceed the $LIMIT-line limit"
    exit 1
fi
echo "All Go files are within the $LIMIT-line limit"
