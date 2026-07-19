// SPDX-License-Identifier: MPL-2.0

// Command subgate-report publishes a fresh aggregate-bound subgate report.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

type observationFlags []string

func main() {
	var observations observationFlags
	flag.Var(&observations, "observation", "observed population member as population-id=member-id (repeatable)")
	flag.Parse()
	populations, err := parseObservations(observations)
	if err != nil {
		fmt.Fprintln(os.Stderr, "goplint subgate report:", err)
		os.Exit(2)
	}
	emitted, err := soundnessgate.EmitReportFromEnvironment(context.Background(), populations)
	if err != nil {
		fmt.Fprintln(os.Stderr, "goplint subgate report:", err)
		os.Exit(1)
	}
	if emitted {
		fmt.Printf("recorded %d soundness populations\n", len(populations))
	}
}

func (flags *observationFlags) String() string {
	return strings.Join(*flags, ",")
}

func (flags *observationFlags) Set(value string) error {
	*flags = append(*flags, value)
	return nil
}

func parseObservations(values []string) ([]soundnessgate.Population, error) {
	if len(values) == 0 {
		return nil, errors.New("at least one -observation is required")
	}
	observations := make([]soundnessgate.ObservedMember, 0, len(values))
	for _, value := range values {
		populationID, memberID, found := strings.Cut(value, "=")
		if !found || strings.TrimSpace(populationID) == "" || strings.TrimSpace(memberID) == "" {
			return nil, fmt.Errorf("observation %q must use nonempty population-id=member-id form", value)
		}
		observations = append(observations, soundnessgate.ObservedMember{
			PopulationID: populationID,
			MemberID:     memberID,
		})
	}
	populations, err := soundnessgate.PopulationsFromObservedMembers(observations)
	if err != nil {
		return nil, fmt.Errorf("derive populations from observed members: %w", err)
	}
	return populations, nil
}
