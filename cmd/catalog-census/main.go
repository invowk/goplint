// SPDX-License-Identifier: MPL-2.0

// Command catalog-census emits the deterministic goplint semantic coverage
// census and fails when any registered category lacks executable evidence.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/invowk/invowk/tools/goplint/goplint"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

func main() {
	catalog := flag.String("catalog", "spec/semantic-rules.v1.json", "semantic rules catalog")
	registryPath := flag.String("registry", "spec/semantic-evidence.v2.json", "executable evidence registry")
	flag.Parse()
	ctx := context.Background()
	if err := goplint.ValidateSemanticCatalogInconclusivePolicy(ctx, *catalog); err != nil {
		fmt.Fprintln(os.Stderr, "goplint catalog census:", err)
		os.Exit(1)
	}
	registry, err := soundnessevidence.LoadRegistry(ctx, *registryPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "goplint catalog census:", err)
		os.Exit(1)
	}
	if err := goplint.ValidateSemanticEvidenceRegistry(registry); err != nil {
		fmt.Fprintln(os.Stderr, "goplint catalog census:", err)
		os.Exit(1)
	}
	categories := make(map[string]bool)
	observations := make([]soundnessgate.ObservedMember, 0, 2*len(registry.Registrations))
	for _, registration := range registry.Registrations {
		categories[registration.Category] = true
		observations = append(observations, soundnessgate.ObservedMember{
			PopulationID: "semantic-registrations",
			MemberID:     registration.ID,
		})
	}
	for category := range categories {
		observations = append(observations, soundnessgate.ObservedMember{
			PopulationID: "protocol-categories",
			MemberID:     category,
		})
	}
	populations, err := soundnessgate.PopulationsFromObservedMembers(observations)
	if err != nil {
		fmt.Fprintln(os.Stderr, "goplint catalog census:", err)
		os.Exit(1)
	}
	if _, err := soundnessgate.EmitReportFromEnvironment(ctx, populations); err != nil {
		fmt.Fprintln(os.Stderr, "goplint catalog census:", err)
		os.Exit(1)
	}
	summary := struct {
		FormatVersion         int `json:"format_version"`
		ProtocolCategories    int `json:"protocol_categories"`
		SemanticRegistrations int `json:"semantic_registrations"`
	}{
		FormatVersion:         soundnessevidence.RegistryFormatVersion,
		ProtocolCategories:    len(categories),
		SemanticRegistrations: len(registry.Registrations),
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		fmt.Fprintln(os.Stderr, "goplint catalog census: encode summary:", err)
		os.Exit(1)
	}
}
