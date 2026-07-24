// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditExceptionReviewDatesDoesNotRequirePackages(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "exceptions.toml")
	if err := os.WriteFile(path, []byte(`
[settings]
exception_review_after = "2026-01-01"
exception_review_blocked_by = "migration"

[[exceptions]]
pattern = "future.pattern"
reason = "reviewed"
review_after = "2099-01-01"

[[exceptions]]
pattern = "invalid.pattern"
reason = "reviewed"
review_after = "not-a-date"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	findings, err := AuditExceptionReviewDates(path, time.Date(2026, time.July, 19, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("AuditExceptionReviewDates() error = %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %+v, want overdue settings and invalid entry", findings)
	}
}
