// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"fmt"
	"slices"
)

func (manifest Manifest) validateDependencies() error {
	subgates := make(map[string]Subgate, len(manifest.Subgates))
	for _, subgate := range manifest.Subgates {
		subgates[subgate.ID] = subgate
	}
	for _, subgate := range manifest.Subgates {
		for _, dependencyID := range subgate.Dependencies {
			if dependencyID == subgate.ID {
				return fmt.Errorf("soundness subgate %q depends on itself", subgate.ID)
			}
			if _, exists := subgates[dependencyID]; !exists {
				return fmt.Errorf("soundness subgate %q depends on unknown subgate %q", subgate.ID, dependencyID)
			}
		}
	}
	visiting := make(map[string]bool, len(manifest.Subgates))
	visited := make(map[string]bool, len(manifest.Subgates))
	var visit func(string) error
	visit = func(subgateID string) error {
		if visiting[subgateID] {
			return fmt.Errorf("soundness manifest dependency cycle includes %q", subgateID)
		}
		if visited[subgateID] {
			return nil
		}
		visiting[subgateID] = true
		for _, dependencyID := range subgates[subgateID].Dependencies {
			if err := visit(dependencyID); err != nil {
				return err
			}
		}
		delete(visiting, subgateID)
		visited[subgateID] = true
		return nil
	}
	for _, subgate := range manifest.Subgates {
		if err := visit(subgate.ID); err != nil {
			return err
		}
	}
	return nil
}

func (manifest Manifest) validateSubgateProfileMembership() error {
	for _, subgate := range manifest.Subgates {
		expected := make([]ProfileID, 0, len(manifest.Profiles))
		for _, profile := range manifest.Profiles {
			if slices.Contains(profile.SubgateIDs, subgate.ID) {
				expected = append(expected, profile.ID)
			}
		}
		if !slices.Equal(subgate.ProfileIDs, expected) {
			return fmt.Errorf(
				"soundness subgate %q profile_ids = %q, want %q from profile selections",
				subgate.ID,
				subgate.ProfileIDs,
				expected,
			)
		}
	}
	return nil
}
