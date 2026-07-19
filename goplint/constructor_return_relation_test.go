// SPDX-License-Identifier: MPL-2.0

package goplint

import "testing"

func TestConstructorReturnRelationUsesExactSSAEdge(t *testing.T) {
	t.Parallel()

	const source = `package probe
import "errors"

type Value struct{}
func (*Value) Validate() error { return nil }

func ElseSuccess(err error) (*Value, error) {
	value := &Value{}
	if err != nil {
		return nil, err
	} else {
		return value, err
	}
}

func InvertedSuccess(err error) (*Value, error) {
	value := &Value{}
	if err == nil {
		return value, err
	}
	return nil, err
}

func NestedSuccess(err error, ready bool) (*Value, error) {
	value := &Value{}
	if ready {
		if err != nil {
			return nil, err
		}
		return value, err
	}
	return nil, errors.New("not ready")
}

func SwitchSuccess(err error) (*Value, error) {
	value := &Value{}
	switch {
	case err != nil:
		return nil, err
	default:
		return value, err
	}
}

func FailureOnly(err error) (*Value, error) {
	value := &Value{}
	if err != nil {
		return value, err
	}
	return nil, nil
}

func NamedSuccess(err error) (value *Value, resultErr error) {
	value = &Value{}
	resultErr = err
	if resultErr != nil {
		return nil, resultErr
	}
	return
}

func ErrorFirst(err error) (error, *Value) {
	value := &Value{}
	if err != nil {
		return err, nil
	}
	return err, value
}

func Overwritten(err error) (*Value, error) {
	value := &Value{}
	if err != nil {
		err = nil
	}
	return value, err
}

func Mismatched(conditionErr, returnedErr error) (*Value, error) {
	value := &Value{}
	if conditionErr != nil {
		return value, returnedErr
	}
	return nil, nil
}

func PhiResult(fail bool) (*Value, error) {
	value := &Value{}
	var err error
	if fail {
		err = errors.New("failed")
	}
	return value, err
}
`
	pass, file := buildTypedPassFromSource(t, source)
	ssaResult := buildSSAForPass(pass)
	tests := []struct {
		name       string
		wantClass  interprocOutcomeClass
		wantReason pathOutcomeReason
	}{
		{name: "ElseSuccess", wantClass: interprocOutcomeUnsafe},
		{name: "InvertedSuccess", wantClass: interprocOutcomeUnsafe},
		{name: "NestedSuccess", wantClass: interprocOutcomeUnsafe},
		{name: "SwitchSuccess", wantClass: interprocOutcomeUnsafe},
		{name: "FailureOnly", wantClass: interprocOutcomeSafe},
		{name: "NamedSuccess", wantClass: interprocOutcomeUnsafe},
		{name: "ErrorFirst", wantClass: interprocOutcomeUnsafe},
		{name: "Overwritten", wantClass: interprocOutcomeInconclusive, wantReason: pathOutcomeReasonUnresolvedTarget},
		{name: "Mismatched", wantClass: interprocOutcomeInconclusive, wantReason: pathOutcomeReasonUnresolvedTarget},
		{name: "PhiResult", wantClass: interprocOutcomeInconclusive, wantReason: pathOutcomeReasonUnresolvedTarget},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			declaration := findFuncDecl(t, file, tt.name)
			returnInfo := resolveReturnTypeValidateInfo(pass, declaration)
			identityModel, _ := buildConstructorSSAIdentityModel(
				pass,
				ssaResult,
				declaration,
				returnInfo.ResultSlot,
			)
			result := newInterprocSolverWithSSA(pass, ssaResult).EvaluateConstructorPath(
				interprocConstructorPathInput{
					Decl:            declaration,
					ReturnTypeKey:   returnInfo.TypeKey,
					ResultSlot:      returnInfo.ResultSlot,
					Constructor:     "probe." + tt.name,
					MaxStates:       defaultCFGMaxStates,
					SSAAvailability: protocolSSAAvailabilityForDecl(pass, ssaResult, declaration),
				},
			)
			if result.Class != tt.wantClass || result.Reason != tt.wantReason {
				t.Fatalf(
					"constructor result = %s (%s), want %s (%s); errors=%+v returns=%+v witness=%+v",
					result.Class,
					result.Reason,
					tt.wantClass,
					tt.wantReason,
					identityModel.returnErrors,
					identityModel.returnsByPosition,
					result.WitnessEdges,
				)
			}
		})
	}
}
