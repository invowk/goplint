// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ObservedMember identifies one unique member actually exercised by a
// population producer during its current invocation.
type ObservedMember struct {
	PopulationID string
	MemberID     string
}

// PopulationsFromObservedMembers derives canonical nonzero populations from
// exact current-run members. It rejects marker-only zero populations and
// duplicate credit for the same member.
func PopulationsFromObservedMembers(observations []ObservedMember) ([]Population, error) {
	if len(observations) == 0 {
		return nil, errors.New("population census has no observed members")
	}
	members := make(map[string]map[string]bool)
	for index, observation := range observations {
		if err := validateIdentifier(fmt.Sprintf("observed members[%d].population", index), observation.PopulationID); err != nil {
			return nil, err
		}
		if err := validateIdentifier(fmt.Sprintf("observed members[%d].member", index), observation.MemberID); err != nil {
			return nil, err
		}
		populationMembers := members[observation.PopulationID]
		if populationMembers == nil {
			populationMembers = make(map[string]bool)
			members[observation.PopulationID] = populationMembers
		}
		if populationMembers[observation.MemberID] {
			return nil, fmt.Errorf(
				"population %q contains duplicate observed member %q",
				observation.PopulationID,
				observation.MemberID,
			)
		}
		populationMembers[observation.MemberID] = true
	}
	populations := make([]Population, 0, len(members))
	for populationID, populationMembers := range members {
		if len(populationMembers) == 0 {
			return nil, fmt.Errorf("population %q has zero observed members", populationID)
		}
		populations = append(populations, Population{ID: populationID, Count: len(populationMembers)})
	}
	slices.SortFunc(populations, func(left, right Population) int {
		return strings.Compare(left.ID, right.ID)
	})
	return populations, nil
}
