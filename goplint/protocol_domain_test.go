// SPDX-License-Identifier: MPL-2.0

package goplint

import "testing"

func TestProtocolDomainOrderingLaws(t *testing.T) {
	t.Parallel()

	states := protocolLawStates()
	for _, a := range states {
		if !a.lessEqual(a) {
			t.Fatalf("state %+v is not <= itself", a)
		}
		for _, b := range states {
			if a.lessEqual(b) && b.lessEqual(a) && a != b {
				t.Fatalf("antisymmetry failed for %+v and %+v", a, b)
			}
			for _, c := range states {
				if a.lessEqual(b) && b.lessEqual(c) && !a.lessEqual(c) {
					t.Fatalf("transitivity failed for %+v <= %+v <= %+v", a, b, c)
				}
			}
		}
	}
}

func TestProtocolDomainJoinLaws(t *testing.T) {
	t.Parallel()

	states := protocolLawStates()
	for _, a := range states {
		if got := a.join(a); got != a {
			t.Fatalf("idempotence failed: join(%+v, itself) = %+v", a, got)
		}
		for _, b := range states {
			got, want := a.join(b), b.join(a)
			joinState := "commutative"
			if got != want {
				joinState = "non-commutative"
			}
			requireMutationGuardObservation(
				t,
				"production-domain/joined-uncertainty",
				mutationGuardState("protocol-domain-join", "commutative"),
				mutationGuardState("protocol-domain-join", joinState),
			)
			if got != want {
				t.Fatalf("commutativity failed: join(%+v, %+v) = %+v, reverse = %+v", a, b, got, want)
			}
			joined := a.join(b)
			if !a.lessEqual(joined) || !b.lessEqual(joined) {
				t.Fatalf("join(%+v, %+v) = %+v is not an upper bound", a, b, joined)
			}
			for _, c := range states {
				if got, want := a.join(b).join(c), a.join(b.join(c)); got != want {
					t.Fatalf("associativity failed for %+v, %+v, %+v: %+v != %+v", a, b, c, got, want)
				}
			}
		}
	}
}

func TestProtocolTransfersAreMonotone(t *testing.T) {
	t.Parallel()

	states := protocolLawStates()
	effects := []protocolConditionalEffect{
		{Kind: protocolEffectValidate},
		{Kind: protocolEffectEscape},
		{Kind: protocolEffectConsume},
	}
	results := []protocolErrorResult{
		protocolErrorResultUnknown,
		protocolErrorResultNil,
		protocolErrorResultNonNil,
	}
	for _, effect := range effects {
		for _, result := range results {
			for _, a := range states {
				for _, b := range states {
					if !a.lessEqual(b) {
						continue
					}
					gotA := a.apply(effect, result, protocolUncertaintyUnresolvedCall)
					gotB := b.apply(effect, result, protocolUncertaintyUnresolvedCall)
					if !gotA.lessEqual(gotB) {
						t.Fatalf("transfer is not monotone: effect=%d result=%d, %+v <= %+v but %+v !<= %+v", effect.Kind, result, a, b, gotA, gotB)
					}
				}
			}
		}
	}
}

func TestProtocolConditionalValidationEffect(t *testing.T) {
	t.Parallel()

	relation := protocolValidationRelation{ReceiverIdentity: 1, ErrorIdentity: 2}
	effect := protocolConditionalEffect{Kind: protocolEffectValidate, Relation: relation}
	initial := newProtocolRequiredState()

	if got := initial.apply(effect, protocolErrorResultNil, 0); got.Validation != protocolValidationProven {
		t.Fatalf("nil result validation = %d, want proven", got.Validation)
	}
	if got := initial.apply(effect, protocolErrorResultNonNil, 0); !got.validationRequired() ||
		got.Result != protocolErrorResultNonNil {
		t.Fatalf("non-nil result state = %+v, want required obligation with non-nil edge result", got)
	}
	unknown := initial.apply(effect, protocolErrorResultUnknown, protocolUncertaintyUnresolvedCall)
	if unknown.Validation != protocolValidationRequired {
		t.Fatalf("unknown result validation = %d, want required", unknown.Validation)
	}
	if unknown.Uncertainty != protocolUncertaintyUnresolvedCall {
		t.Fatalf("unknown result uncertainty = %d, want unresolved-call", unknown.Uncertainty)
	}
	if effect.Relation != relation {
		t.Fatalf("validation relation = %+v, want %+v", effect.Relation, relation)
	}
}

func TestProtocolUnknownAndOutcomeAggregation(t *testing.T) {
	t.Parallel()

	unknownState := newProtocolRequiredState()
	unknownState.Uncertainty = protocolUncertaintyUnresolvedCall
	if got := unknownState.pathStatus(); got != protocolPathUnresolved {
		t.Fatalf("unknown state status = %d, want unresolved", got)
	}

	tests := []struct {
		name           string
		statuses       []protocolPathStatus
		wantKind       protocolEvidenceKind
		wantDiagnostic bool
		wantDischarged int
	}{
		{name: "all satisfied", statuses: []protocolPathStatus{protocolPathSatisfied}},
		{
			name:           "unknown becomes inconclusive",
			statuses:       []protocolPathStatus{protocolPathSatisfied, protocolPathUnresolved},
			wantKind:       protocolEvidenceInconclusive,
			wantDiagnostic: true,
		},
		{
			name:           "violation outranks uncertainty",
			statuses:       []protocolPathStatus{protocolPathUnresolved, protocolPathViolation},
			wantKind:       protocolEvidenceViolation,
			wantDiagnostic: true,
		},
		{
			name:           "discharge stays trace only",
			statuses:       []protocolPathStatus{protocolPathDischargedInfeasible, protocolPathSatisfied},
			wantDischarged: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := aggregateProtocolPaths(tt.statuses)
			if got.Kind != tt.wantKind || got.EmitDiagnostic != tt.wantDiagnostic || got.DischargedWitnesses != tt.wantDischarged {
				t.Fatalf("aggregateProtocolPaths() = %+v, want kind=%q diagnostic=%t discharged=%d", got, tt.wantKind, tt.wantDiagnostic, tt.wantDischarged)
			}
		})
	}
}

func protocolLawStates() []protocolAbstractState {
	validations := []protocolValidationState{protocolValidationProven, protocolValidationRequired}
	hazards := []protocolHazardSet{0, protocolHazardEscaped, protocolHazardConsumed, protocolHazardEscaped | protocolHazardConsumed}
	uncertainties := []protocolUncertaintySet{0, protocolUncertaintyUnresolvedCall, protocolUncertaintyMissingSSA, protocolUncertaintyUnresolvedCall | protocolUncertaintyMissingSSA}
	states := make([]protocolAbstractState, 0, len(validations)*len(hazards)*len(uncertainties))
	for _, validation := range validations {
		for _, hazard := range hazards {
			for _, uncertainty := range uncertainties {
				states = append(states, protocolAbstractState{
					Validation:  validation,
					Hazards:     hazard,
					Uncertainty: uncertainty,
				})
			}
		}
	}
	return states
}
