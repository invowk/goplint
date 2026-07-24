// SPDX-License-Identifier: MPL-2.0

package racerepeat

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
)

// ParseCensus parses canonical `go test -list '^(Test|Fuzz|Example)'` output.
func ParseCensus(output []byte) ([]CensusEntry, error) {
	entries := make([]CensusEntry, 0)
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "ok ") || strings.HasPrefix(line, "?") {
			continue
		}
		kind, err := kindForName(line)
		if err != nil {
			return nil, err
		}
		entries = append(entries, CensusEntry{ID: line, Kind: kind})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan race/repeat census: %w", err)
	}
	return canonicalCensus(entries)
}
