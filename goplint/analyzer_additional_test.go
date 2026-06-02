// SPDX-License-Identifier: MPL-2.0

package goplint

import "testing"

func TestShouldReportOverdueReviewFinding(t *testing.T) {
	t.Parallel()

	if !shouldReportOverdueReviewFinding(nil, "finding-1") {
		t.Fatal("nil state should allow reporting")
	}

	state := &flagState{}
	if !shouldReportOverdueReviewFinding(state, "finding-1") {
		t.Fatal("first finding occurrence should report")
	}
	if shouldReportOverdueReviewFinding(state, "finding-1") {
		t.Fatal("duplicate finding occurrence should be suppressed")
	}
	if !shouldReportOverdueReviewFinding(state, "finding-2") {
		t.Fatal("different finding ID should report")
	}
	if state.overdueReviewSeen == nil {
		t.Fatal("overdueReviewSeen map should be initialized")
	}
}

func TestFlagState_CalleeSummaryCacheIsAnalyzerScoped(t *testing.T) {
	t.Parallel()

	stateA := &flagState{}
	stateB := &flagState{}
	resetFlagStateDefaults(stateA)
	resetFlagStateDefaults(stateB)

	stateA.calleeSummaryCache.Store("callee|arg:0", calleeSummaryEntry{ok: true})
	if _, ok := stateB.calleeSummaryCache.Load("callee|arg:0"); ok {
		t.Fatal("callee summary cache leaked between analyzer states")
	}

	resetFlagStateDefaults(stateA)
	if _, ok := stateA.calleeSummaryCache.Load("callee|arg:0"); ok {
		t.Fatal("resetFlagStateDefaults() did not clear callee summary cache")
	}
}

func TestSameStructTypeIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		structKey string
		returnKey string
		want      bool
	}{
		{name: "exact", structKey: "pkg.Box", returnKey: "pkg.Box", want: true},
		{name: "generic struct base", structKey: "pkg.Box[T]", returnKey: "pkg.Box", want: true},
		{name: "generic return base", structKey: "pkg.Box", returnKey: "pkg.Box[int]", want: true},
		{name: "different type", structKey: "pkg.Box", returnKey: "pkg.Other", want: false},
		{name: "empty struct key", structKey: "", returnKey: "pkg.Box", want: false},
		{name: "empty return key", structKey: "pkg.Box", returnKey: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := sameStructTypeIdentity(tt.structKey, tt.returnKey); got != tt.want {
				t.Fatalf("sameStructTypeIdentity(%q, %q) = %v, want %v", tt.structKey, tt.returnKey, got, tt.want)
			}
		})
	}
}

func TestFindConstructorForStruct(t *testing.T) {
	t.Parallel()

	t.Run("generic type identity matches when return name differs", func(t *testing.T) {
		t.Parallel()

		structInfo := exportedStructInfo{name: "Box", typeKey: "pkg.Box[T]"}
		ctor := &constructorFuncInfo{returnTypeName: "InstantiatedBox", returnTypeKey: "pkg.Box"}
		got := findConstructorForStruct(structInfo, map[string]*constructorFuncInfo{
			"NewBox": ctor,
		})
		if got != ctor {
			t.Fatalf("findConstructorForStruct() = %p, want %p", got, ctor)
		}
	})

	t.Run("exact constructor wins over lexicographic prefix", func(t *testing.T) {
		t.Parallel()

		structInfo := exportedStructInfo{name: "Config", typeKey: "pkg.Config"}
		exact := &constructorFuncInfo{returnTypeName: "Config", returnTypeKey: "pkg.Config"}
		prefix := &constructorFuncInfo{returnTypeName: "Config", returnTypeKey: "pkg.Config"}
		got := findConstructorForStruct(structInfo, map[string]*constructorFuncInfo{
			"NewConfigAlpha": prefix,
			"NewConfig":      exact,
		})
		if got != exact {
			t.Fatalf("findConstructorForStruct() = %p, want exact constructor %p", got, exact)
		}
	})

	t.Run("interface return satisfies constructor", func(t *testing.T) {
		t.Parallel()

		structInfo := exportedStructInfo{name: "Client", typeKey: "pkg.Client"}
		ctor := &constructorFuncInfo{returnTypeName: "Service", returnTypeKey: "pkg.Service", returnsInterface: true}
		got := findConstructorForStruct(structInfo, map[string]*constructorFuncInfo{
			"NewClient": ctor,
		})
		if got != ctor {
			t.Fatalf("findConstructorForStruct() = %p, want interface-return constructor %p", got, ctor)
		}
	})
}
