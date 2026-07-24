// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"time"
)

// ExceptionReviewFinding is one malformed or overdue configuration-only
// governance result.
type ExceptionReviewFinding struct {
	Subject     string `json:"subject"`
	ReviewAfter string `json:"review_after"`
	BlockedBy   string `json:"blocked_by,omitempty"`
	Message     string `json:"message"`
}

// AuditExceptionReviewDates parses and validates exception review policy
// without loading or analyzing any Go packages.
func AuditExceptionReviewDates(path string, now time.Time) ([]ExceptionReviewFinding, error) {
	config, err := LoadExceptionConfig(path)
	if err != nil {
		return nil, err
	}
	findings := make([]ExceptionReviewFinding, 0)
	appendFinding := func(subject, reviewAfter, blockedBy string, settingsReview bool) {
		if reviewAfter == "" {
			return
		}
		reviewDate, parseErr := time.Parse("2006-01-02", reviewAfter)
		message := ""
		if parseErr != nil {
			message = reviewDateInvalidMessage(subject, reviewAfter, parseErr, settingsReview)
		} else if now.After(reviewDate) {
			message = reviewDateOverdueMessage(subject, reviewAfter, settingsReview)
			if blockedBy != "" {
				message += fmt.Sprintf(" (blocked by: %s)", blockedBy)
			}
		}
		if message != "" {
			findings = append(findings, ExceptionReviewFinding{
				Subject: subject, ReviewAfter: reviewAfter, BlockedBy: blockedBy, Message: message,
			})
		}
	}
	appendFinding(
		"settings.exception_review_after",
		config.Settings.ExceptionReviewAfter,
		config.Settings.ExceptionReviewBlockedBy,
		true,
	)
	for _, exception := range config.Exceptions {
		appendFinding(exception.Pattern, exception.ReviewAfter, exception.BlockedBy, false)
	}
	return findings, nil
}
