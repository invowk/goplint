// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"testing"

	"golang.org/x/tools/go/ssa"
)

func TestProtocolAliasSnapshotJoinAndStoreKill(t *testing.T) {
	t.Parallel()

	const (
		alias  protocolIdentity = 1
		left   protocolIdentity = 2
		right  protocolIdentity = 3
		shared protocolIdentity = 4
	)
	leftPath := newProtocolAliasSnapshot()
	leftPath.bind(alias, left)
	leftPath.bind(shared, left)
	rightPath := newProtocolAliasSnapshot()
	rightPath.bind(alias, right)
	rightPath.bind(shared, left)

	joined := leftPath.join(rightPath)
	if _, resolution := joined.resolve(alias); resolution != protocolAliasAmbiguous {
		t.Fatalf("conflicting alias join resolution = %d, want ambiguous", resolution)
	}
	if object, resolution := joined.resolve(shared); resolution != protocolAliasMust || object != left {
		t.Fatalf("shared alias join = (%d, %d), want (%d, must)", object, resolution, left)
	}

	leftPath.killObject(left)
	for _, identity := range []protocolIdentity{alias, shared} {
		if _, resolution := leftPath.resolve(identity); resolution != protocolAliasAmbiguous {
			t.Fatalf("store kill identity %d resolution = %d, want ambiguous", identity, resolution)
		}
	}
}

func TestProtocolAliasAnalysisPhiAndStoreSemantics(t *testing.T) {
	t.Parallel()

	const source = `package probe

type Item struct{ value int }

func Same(left *Item, choose bool) *Item {
	var selected *Item
	if choose {
		selected = left
	} else {
		selected = left
	}
	return selected
}

func Different(left, right *Item, choose bool) *Item {
	var selected *Item
	if choose {
		selected = left
	} else {
		selected = right
	}
	return selected
}

func Store(slot **Item, value *Item) {
	*slot = value
	*slot = value
}
`
	pass, _ := buildTypedPassFromSource(t, source)
	ssaRes := buildSSAForPass(pass)
	if ssaRes == nil || ssaRes.Pkg == nil {
		t.Fatal("buildSSAForPass() returned no package")
	}

	tests := []struct {
		name           string
		wantResolution protocolAliasResolution
	}{
		{name: "Same", wantResolution: protocolAliasMust},
		{name: "Different", wantResolution: protocolAliasAmbiguous},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fn := findMemberFunc(ssaRes.Pkg, tt.name)
			if fn == nil {
				t.Fatalf("SSA function %s is missing", tt.name)
			}
			phi := findFirstProtocolPhi(t, fn)
			ret := findFirstProtocolReturn(t, fn)
			interner := newProtocolIdentityInterner()
			analysis := analyzeProtocolAliases(fn, interner)
			_, resolution := analysis.resolveBefore(ret, phi)
			if resolution != tt.wantResolution {
				t.Fatalf("phi resolution = %d, want %d", resolution, tt.wantResolution)
			}
			if tt.wantResolution == protocolAliasAmbiguous {
				if got := analysis.uncertaintyBefore(ret, phi); got != protocolUncertaintyAmbiguousIdentity {
					t.Fatalf("phi uncertainty = %d, want ambiguous-identity", got)
				}
			}
		})
	}

	storeFn := findMemberFunc(ssaRes.Pkg, "Store")
	if storeFn == nil {
		t.Fatal("SSA function Store is missing")
	}
	stores := findProtocolStores(storeFn)
	if len(stores) < 2 {
		t.Fatalf("stores = %d, want at least two", len(stores))
	}
	interner := newProtocolIdentityInterner()
	analysis := analyzeProtocolAliases(storeFn, interner)
	snapshot := analysis.before[stores[1]]
	addressObject, addressResolution := snapshot.resolve(interner.internValue(stores[1].Addr))
	if addressResolution != protocolAliasMust {
		t.Fatalf("address resolution after prior store = %d, want must", addressResolution)
	}
	valueObject, valueResolution := snapshot.resolve(interner.internValue(stores[0].Val))
	storeState := "updated-with-stored-identity"
	if valueResolution != protocolAliasMust || snapshot.memoryMust[addressObject] != valueObject {
		storeState = "missing-stored-identity"
	}
	requireMutationGuardObservation(
		t,
		"alias-memory/store-transfer",
		mutationGuardState("pointed-memory-cell", "updated-with-stored-identity"),
		mutationGuardState("pointed-memory-cell", storeState),
	)
	if valueResolution != protocolAliasMust || snapshot.memoryMust[addressObject] != valueObject {
		t.Fatalf("stored object = %d, want %d with must resolution", snapshot.memoryMust[addressObject], valueObject)
	}
}

func TestProtocolAliasAnalysisEscapeAndClosureSemantics(t *testing.T) {
	t.Parallel()

	const source = `package probe

type Item struct{ value int }

var global *Item
var closureSink func()

func Send(value, unrelated *Item, sink chan *Item) *Item {
	sink <- value
	_ = unrelated
	return value
}

func SendValue(value Item, sink chan Item) Item {
	sink <- value
	return value
}

func Publish(value *Item) *Item {
	global = value
	return value
}

func MapUpdate(value *Item, items map[string]*Item) *Item {
	items["value"] = value
	return value
}

func EscapingClosure(value, unrelated *Item) *Item {
	closureSink = func() { _ = value }
	_ = unrelated
	return value
}

func ImmediateClosure(value *Item) *Item {
	func() { _ = value }()
	return value
}

func DeferredClosure(value *Item) *Item {
	defer func() { _ = value }()
	return value
}

func ConcurrentClosure(value *Item) *Item {
	go func() { _ = value }()
	return value
}
`
	pass, _ := buildTypedPassFromSource(t, source)
	ssaRes := buildSSAForPass(pass)
	if ssaRes == nil || ssaRes.Pkg == nil {
		t.Fatal("buildSSAForPass() returned no package")
	}

	tests := []struct {
		name           string
		wantResolution protocolAliasResolution
		checkUnrelated bool
	}{
		{name: "Send", wantResolution: protocolAliasAmbiguous, checkUnrelated: true},
		{name: "SendValue", wantResolution: protocolAliasMust},
		{name: "Publish", wantResolution: protocolAliasAmbiguous},
		{name: "MapUpdate", wantResolution: protocolAliasAmbiguous},
		{name: "EscapingClosure", wantResolution: protocolAliasAmbiguous, checkUnrelated: true},
		{name: "ImmediateClosure", wantResolution: protocolAliasMust},
		{name: "DeferredClosure", wantResolution: protocolAliasMust},
		{name: "ConcurrentClosure", wantResolution: protocolAliasAmbiguous},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fn := findMemberFunc(ssaRes.Pkg, tt.name)
			if fn == nil || len(fn.Params) == 0 {
				t.Fatalf("SSA function %s or its parameter is missing", tt.name)
			}
			ret := findFirstProtocolReturn(t, fn)
			if len(ret.Results) == 0 {
				t.Fatalf("SSA function %s has no return result", tt.name)
			}
			interner := newProtocolIdentityInterner()
			analysis := analyzeProtocolAliases(fn, interner)
			if _, resolution := analysis.resolveBefore(ret, ret.Results[0]); resolution != tt.wantResolution {
				t.Fatalf("escaped parameter resolution = %d, want %d", resolution, tt.wantResolution)
			}
			if tt.checkUnrelated {
				if _, resolution := analysis.resolveBefore(ret, fn.Params[1]); resolution != protocolAliasMust {
					t.Fatalf("unrelated parameter resolution = %d, want must", resolution)
				}
			}
		})
	}
}

func findFirstProtocolPhi(t *testing.T, fn *ssa.Function) *ssa.Phi {
	t.Helper()
	for _, block := range fn.Blocks {
		for _, instruction := range block.Instrs {
			if phi, ok := instruction.(*ssa.Phi); ok {
				return phi
			}
		}
	}
	t.Fatalf("SSA function %s has no phi", fn.Name())
	return nil
}

func findFirstProtocolReturn(t *testing.T, fn *ssa.Function) *ssa.Return {
	t.Helper()
	for _, block := range fn.Blocks {
		for _, instruction := range block.Instrs {
			if ret, ok := instruction.(*ssa.Return); ok {
				return ret
			}
		}
	}
	t.Fatalf("SSA function %s has no return", fn.Name())
	return nil
}

func findProtocolStores(fn *ssa.Function) []*ssa.Store {
	stores := make([]*ssa.Store, 0)
	for _, block := range fn.Blocks {
		for _, instruction := range block.Instrs {
			if store, ok := instruction.(*ssa.Store); ok {
				stores = append(stores, store)
			}
		}
	}
	return stores
}
