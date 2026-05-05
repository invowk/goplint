// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// PathDomainFact is exported for types annotated with
// //goplint:path-domain=<domain>. A path-domain marks a string-like path type
// whose semantics are not the host OS path semantics used by path/filepath.
type PathDomainFact struct {
	Domain string
}

type pathDomainProvenance struct {
	TypeName *types.TypeName
	Fact     PathDomainFact
}

// AFact implements the analysis.Fact interface marker method.
func (*PathDomainFact) AFact() {}

// String returns a human-readable representation for analysistest fact matching.
func (f *PathDomainFact) String() string {
	if f == nil || f.Domain == "" {
		return "path-domain"
	}
	return "path-domain=" + f.Domain
}

func exportPathDomainFacts(pass *analysis.Pass, gd *ast.GenDecl) {
	if gd == nil || pass == nil || pass.TypesInfo == nil || gd.Tok != token.TYPE {
		return
	}
	for _, spec := range gd.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || ts.Assign.IsValid() {
			continue
		}
		domain, ok := pathDomainDirective(gd.Doc, ts.Doc)
		if !ok || domain == "" {
			continue
		}
		obj := pass.TypesInfo.Defs[ts.Name]
		if obj == nil {
			continue
		}
		pass.ExportObjectFact(obj, &PathDomainFact{Domain: domain})
	}
}

func inspectPathDomainNativeFilepath(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	if fn == nil || fn.Body == nil || shouldSkipFunc(fn) {
		return
	}
	funcQualName := qualFuncName(pass, fn)
	if isPlatformCategoryExcepted(cfg, funcQualName+".path-domain-native-filepath", CategoryPathDomainNativeFilepath) {
		return
	}

	pathVars := make(map[*types.Var]pathDomainProvenance)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			recordPathDomainAssignments(pass, node, pathVars)
		case *ast.CallExpr:
			filepathFunc := pathDomainNativeFilepathFunc(pass, node)
			if filepathFunc == "" {
				return true
			}
			for _, arg := range node.Args {
				provenance := pathDomainExprType(pass, arg, pathVars)
				if provenance.TypeName == nil || provenance.Fact.Domain == "" {
					continue
				}
				reportPathDomainNativeFilepath(pass, node, funcQualName, filepathFunc, provenance.TypeName.Name(), provenance.Fact.Domain, bl)
				return true
			}
		}
		return true
	})
}

func recordPathDomainAssignments(pass *analysis.Pass, assign *ast.AssignStmt, pathVars map[*types.Var]pathDomainProvenance) {
	if pass == nil || pass.TypesInfo == nil || assign == nil || len(assign.Lhs) != len(assign.Rhs) {
		return
	}
	for idx, lhs := range assign.Lhs {
		v := varFromIdentExpr(pass, lhs)
		if v == nil {
			continue
		}
		provenance := pathDomainExprType(pass, assign.Rhs[idx], pathVars)
		if provenance.Fact.Domain != "" {
			pathVars[v] = provenance
			continue
		}
		delete(pathVars, v)
	}
}

func pathDomainExprType(
	pass *analysis.Pass,
	expr ast.Expr,
	pathVars map[*types.Var]pathDomainProvenance,
) pathDomainProvenance {
	if pass == nil || pass.TypesInfo == nil || expr == nil {
		return pathDomainProvenance{}
	}
	if v := varRefThroughStringConversion(pass, expr); v != nil {
		if provenance, ok := pathVars[v]; ok {
			return provenance
		}
	}
	expr = unwrapStringConversion(pass, expr)
	typeName := typeNameForType(pass, pass.TypesInfo.TypeOf(expr))
	if typeName == nil {
		return pathDomainProvenance{}
	}
	var fact PathDomainFact
	if !pass.ImportObjectFact(typeName, &fact) {
		return pathDomainProvenance{}
	}
	return pathDomainProvenance{TypeName: typeName, Fact: fact}
}

func typeNameForType(pass *analysis.Pass, t types.Type) *types.TypeName {
	if pass == nil || t == nil {
		return nil
	}
	t = types.Unalias(t)
	if ptr, ok := t.(*types.Pointer); ok {
		t = types.Unalias(ptr.Elem())
	}
	named, ok := t.(*types.Named)
	if !ok {
		return nil
	}
	return named.Obj()
}

func pathDomainNativeFilepathFunc(pass *analysis.Pass, call *ast.CallExpr) string {
	for _, name := range []string{"Join", "Clean", "Rel", "IsAbs"} {
		if isCallToPathFilepathFunc(pass, call, name) {
			return name
		}
	}
	return ""
}

func pathDomainDirective(genDoc *ast.CommentGroup, specDoc *ast.CommentGroup) (string, bool) {
	for _, cg := range []*ast.CommentGroup{genDoc, specDoc} {
		if domain, ok := directiveValue([]*ast.CommentGroup{cg}, "path-domain"); ok {
			domain = strings.TrimSpace(strings.ToLower(domain))
			if isPathDomainName(domain) {
				return domain, true
			}
		}
	}
	return "", false
}

func isPathDomainName(domain string) bool {
	if domain == "" {
		return false
	}
	for _, r := range domain {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func reportPathDomainNativeFilepath(
	pass *analysis.Pass,
	node ast.Node,
	funcQualName,
	filepathFunc,
	typeName,
	domain string,
	bl *BaselineConfig,
) {
	msg := fmt.Sprintf(
		"filepath.%s called on %s path-domain value %s in %s; use a domain-appropriate path package or slash-preserving handling instead of host-native filepath semantics",
		filepathFunc,
		domain,
		typeName,
		funcQualName,
	)
	findingID := PackageScopedFindingID(pass, CategoryPathDomainNativeFilepath, funcQualName, stablePosKey(pass, node.Pos()))
	reportFindingIfNotBaselined(pass, bl, node.Pos(), CategoryPathDomainNativeFilepath, findingID, msg)
}
