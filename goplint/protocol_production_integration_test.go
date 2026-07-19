// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/constant"
	"go/types"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestProtocolProductionDomainReachability(t *testing.T) {
	t.Parallel()

	pkg := loadProtocolProductionPackage(t)
	effects := productionConstantsOfType(t, pkg, "protocolEffectKind")
	uncertainties := productionConstantsOfType(t, pkg, "protocolUncertaintySet")
	reasons := productionConstantsOfType(t, pkg, "pathOutcomeReason")
	delete(reasons, "pathOutcomeReasonNone")

	assertProductionUses(t, pkg, effects, "protocol_domain.go")
	assertProductionUses(t, pkg, reasons, "cfa_outcome.go", "protocol_domain.go")
	assertProtocolUncertaintyCensus(t, uncertainties, reasons)
	assertProtocolTransferMethodsReachable(t, pkg)

	if pkg.Types.Scope().Lookup("protocolUncertaintyReason") != nil {
		t.Error("parallel protocolUncertaintyReason projection remains in production")
	}
	assertCanonicalProtocolStateCarriers(t, pkg.Types.Scope())
}

func loadProtocolProductionPackage(t *testing.T) *packages.Package {
	t.Helper()

	config := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo,
		Dir: ".",
	}
	loaded, err := packages.Load(config, ".")
	if err != nil {
		t.Fatalf("packages.Load() error: %v", err)
	}
	if packages.PrintErrors(loaded) != 0 || len(loaded) != 1 {
		t.Fatalf("load production package: got %d packages with errors", len(loaded))
	}
	return loaded[0]
}

func productionConstantsOfType(
	t *testing.T,
	pkg *packages.Package,
	typeName string,
) map[string]*types.Const {
	t.Helper()

	constants := make(map[string]*types.Const)
	for _, name := range pkg.Types.Scope().Names() {
		object, ok := pkg.Types.Scope().Lookup(name).(*types.Const)
		if !ok {
			continue
		}
		named, ok := object.Type().(*types.Named)
		if ok && named.Obj().Name() == typeName {
			constants[name] = object
		}
	}
	if len(constants) == 0 {
		t.Fatalf("production type %s has no declared constants", typeName)
	}
	return constants
}

func assertProductionUses(
	t *testing.T,
	pkg *packages.Package,
	constants map[string]*types.Const,
	excludedFiles ...string,
) {
	t.Helper()

	excluded := make(map[string]bool, len(excludedFiles))
	for _, filename := range excludedFiles {
		excluded[filename] = true
	}
	for name, object := range constants {
		used := false
		for identifier, usedObject := range pkg.TypesInfo.Uses {
			if usedObject != object || excluded[filepath.Base(pkg.Fset.Position(identifier.Pos()).Filename)] {
				continue
			}
			used = true
			break
		}
		if !used {
			t.Errorf("production constant %s has no real analyzer use outside %v", name, excludedFiles)
		}
	}
}

func assertProtocolUncertaintyCensus(
	t *testing.T,
	uncertainties map[string]*types.Const,
	reasons map[string]*types.Const,
) {
	t.Helper()

	routes := protocolPathReasonOrder()
	if len(routes) != len(uncertainties) || len(routes) != len(reasons) {
		t.Fatalf(
			"protocol uncertainty census: routes=%d uncertainty-bits=%d reasons=%d",
			len(routes),
			len(uncertainties),
			len(reasons),
		)
	}
	declaredBits := make(map[uint64]string, len(uncertainties))
	for name, object := range uncertainties {
		value, exact := constant.Uint64Val(object.Val())
		if !exact {
			t.Fatalf("uncertainty constant %s is not an exact uint64", name)
		}
		declaredBits[value] = name
	}
	declaredReasons := make(map[pathOutcomeReason]string, len(reasons))
	for name, object := range reasons {
		declaredReasons[pathOutcomeReason(constant.StringVal(object.Val()))] = name
	}
	seenBits := make(map[protocolUncertaintySet]bool, len(routes))
	seenReasons := make(map[pathOutcomeReason]bool, len(routes))
	for _, route := range routes {
		if route.bit == 0 || seenBits[route.bit] {
			t.Errorf("duplicate or zero uncertainty route bit %d", route.bit)
		}
		if route.reason == pathOutcomeReasonNone || seenReasons[route.reason] {
			t.Errorf("duplicate or empty uncertainty route reason %q", route.reason)
		}
		seenBits[route.bit] = true
		seenReasons[route.reason] = true
		if _, ok := declaredBits[uint64(route.bit)]; !ok {
			t.Errorf("uncertainty route bit %d is not declared", route.bit)
		}
		if _, ok := declaredReasons[route.reason]; !ok {
			t.Errorf("uncertainty route reason %q is not declared", route.reason)
		}
	}
}

func assertProtocolTransferMethodsReachable(t *testing.T, pkg *packages.Package) {
	t.Helper()

	stateType := pkg.Types.Scope().Lookup("protocolAbstractState")
	if stateType == nil {
		t.Fatal("production type protocolAbstractState is missing")
	}
	for _, methodName := range []string{"apply", "join", "withUncertainty"} {
		method, _, _ := types.LookupFieldOrMethod(stateType.Type(), false, pkg.Types, methodName)
		if method == nil {
			t.Errorf("protocolAbstractState.%s is missing", methodName)
			continue
		}
		used := false
		for identifier, usedObject := range pkg.TypesInfo.Uses {
			if usedObject == method && filepath.Base(pkg.Fset.Position(identifier.Pos()).Filename) != "protocol_domain.go" {
				used = true
				break
			}
		}
		if !used {
			t.Errorf("protocolAbstractState.%s has no real analyzer use", methodName)
		}
	}
}
