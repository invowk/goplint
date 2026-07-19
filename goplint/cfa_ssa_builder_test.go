// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"
)

func TestTypedSSAAvailabilityDistinguishesFailureClasses(t *testing.T) {
	t.Parallel()

	incomplete := buildSSAForPass(nil)
	if incomplete == nil || incomplete.Availability.Status != ssaAvailabilityIncompleteDependencies {
		t.Fatalf("buildSSAForPass(nil) = %#v, want incomplete dependencies", incomplete)
	}

	buildFailure := &ssaResult{Availability: ssaAvailability{Status: ssaAvailabilityBuildFailure}}
	if got := resolveSSAFunction(buildFailure, nil).Availability.Status; got != ssaAvailabilityBuildFailure {
		t.Fatalf("build failure resolution status = %q", got)
	}

	program := ssa.NewProgram(token.NewFileSet(), ssa.GlobalDebug)
	missingFunction := &ssaResult{
		Pkg:          program.CreatePackage(types.NewPackage("testpkg", "testpkg"), nil, nil, true),
		Availability: ssaAvailability{Status: ssaAvailabilityReady},
	}
	gotMissingFunction := resolveSSAFunction(missingFunction, nil).Availability.Status
	requireMutationGuardObservation(
		t,
		"missing-ssa/nil-function-status",
		mutationGuardState("nil-ssa-function-availability", string(ssaAvailabilityMissingFunction)),
		mutationGuardState("nil-ssa-function-availability", string(gotMissingFunction)),
	)
	if got := gotMissingFunction; got != ssaAvailabilityMissingFunction {
		t.Fatalf("missing function resolution status = %q", got)
	}
	if got := resolveSSAClosure(missingFunction, token.NoPos).Availability.Status; got != ssaAvailabilityMissingClosure {
		t.Fatalf("missing closure resolution status = %q", got)
	}
	if got := unsupportedSSAInstruction(nil); got.Status != ssaAvailabilityUnsupportedInstruction || got.Detail != "<nil>" {
		t.Fatalf("unsupported instruction availability = %#v", got)
	}
}

func TestTypedSSAAvailabilityResolvesFunction(t *testing.T) {
	t.Parallel()

	src := `package testpkg
func target(value string) string { return value }
`
	pass, file := buildTypedPassFromSource(t, src)
	result := buildSSAForPass(pass)
	object, ok := pass.TypesInfo.Defs[findFuncDecl(t, file, "target").Name].(*types.Func)
	if !ok {
		t.Fatal("target definition is not a function")
	}
	resolution := resolveSSAFunction(result, object)
	if !resolution.Availability.ready() || resolution.Function == nil {
		t.Fatalf("resolveSSAFunction() = %#v, want ready function", resolution)
	}
}

func TestProtocolPackageFunctionsIncludesLocalMethodsAndClosures(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type request struct{}
func (request) run() {
	callback := func() {}
	callback()
}
`
	pass, file := buildTypedPassFromSource(t, src)
	result := buildSSAForPass(pass)
	methodDeclaration := findFuncDecl(t, file, "run")
	methodObject, ok := pass.TypesInfo.Defs[methodDeclaration.Name].(*types.Func)
	if !ok {
		t.Fatal("run definition is not a function")
	}
	methodResolution := resolveSSAFunction(result, methodObject)
	if !methodResolution.Availability.ready() || methodResolution.Function == nil {
		t.Fatalf("resolveSSAFunction(run) = %#v, want ready function", methodResolution)
	}

	var literal *ast.FuncLit
	ast.Inspect(methodDeclaration.Body, func(node ast.Node) bool {
		if candidate, isLiteral := node.(*ast.FuncLit); isLiteral && literal == nil {
			literal = candidate
		}
		return true
	})
	if literal == nil {
		t.Fatal("fixture closure not found")
	}
	closureResolution := resolveSSAClosure(result, literal.Pos())
	if !closureResolution.Availability.ready() || closureResolution.Function == nil {
		t.Fatalf("resolveSSAClosure() = %#v, want ready closure", closureResolution)
	}

	functions := protocolPackageFunctions(result)
	if !containsSSAFunction(functions, methodResolution.Function) {
		t.Fatal("local-function census omitted method")
	}
	if !containsSSAFunction(functions, closureResolution.Function) {
		t.Fatal("local-function census omitted method closure")
	}
	for _, function := range functions {
		if function.Pkg != result.Pkg {
			t.Fatalf("local-function census included foreign function %q", function.String())
		}
	}
}

func containsSSAFunction(functions []*ssa.Function, target *ssa.Function) bool {
	return slices.Contains(functions, target)
}

func TestUnavailableSSAPathResultPreservesFailureClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status ssaAvailabilityStatus
		reason pathOutcomeReason
	}{
		{name: "build failure", status: ssaAvailabilityBuildFailure, reason: pathOutcomeReasonMissingSSA},
		{name: "incomplete dependencies", status: ssaAvailabilityIncompleteDependencies, reason: pathOutcomeReasonMissingSSA},
		{name: "missing function", status: ssaAvailabilityMissingFunction, reason: pathOutcomeReasonMissingSSA},
		{name: "missing closure", status: ssaAvailabilityMissingClosure, reason: pathOutcomeReasonMissingSSA},
		{name: "unsupported instruction", status: ssaAvailabilityUnsupportedInstruction, reason: pathOutcomeReasonUnsupportedInstr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			availability := ssaAvailability{Status: tt.status, Detail: "fixture-detail"}
			result := unavailableSSAPathResult(availability, ifdsFactFamilyCastNeedsValidate, "fixture-fact", []string{"fixture.Call"})
			if got := result.toPathOutcome(); got != pathOutcomeInconclusive {
				t.Fatalf("path outcome = %v, want inconclusive", got)
			}
			if result.Reason != tt.reason {
				t.Fatalf("path reason = %q, want %q", result.Reason, tt.reason)
			}
			if result.SSAAvailability != availability {
				t.Fatalf("SSA availability = %#v, want %#v", result.SSAAvailability, availability)
			}
			meta := appendProtocolRefinementMeta(nil, result)
			if got := meta["ssa_availability_status"]; got != string(tt.status) {
				t.Fatalf("SSA status metadata = %q, want %q", got, tt.status)
			}
			if got := meta["ssa_availability_detail"]; got != availability.Detail {
				t.Fatalf("SSA detail metadata = %q, want %q", got, availability.Detail)
			}
		})
	}
}
