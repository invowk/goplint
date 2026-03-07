// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"testing"
)

func TestIsKnownInterfaceContractMethod(t *testing.T) {
	t.Parallel()

	src := `package testpkg
import "fmt"

type Good struct{}
type Bad struct{}

func (Good) Write(p []byte) (int, error) { return len(p), nil }
func (Good) WriteString(s string) (int, error) { return len(s), nil }
func (Good) Format(_ fmt.State, _ rune) {}
func (Good) ReadRune() (rune, int, error) { return 'x', 1, nil }

func (Bad) Write(s string) (int, error) { return len(s), nil }
func (Bad) Format(_ string, _ rune) {}

func Write(p []byte) (int, error) { return len(p), nil }
`

	pass, file := buildTypedPassFromSource(t, src)

	goodWrite := findMethodDecl(t, file, "Good", "Write")
	if !isKnownInterfaceContractMethod(pass, goodWrite) {
		t.Fatal("expected Good.Write to match contract")
	}
	goodWriteString := findMethodDecl(t, file, "Good", "WriteString")
	if !isKnownInterfaceContractMethod(pass, goodWriteString) {
		t.Fatal("expected Good.WriteString to match contract")
	}
	goodFormat := findMethodDecl(t, file, "Good", "Format")
	if !isKnownInterfaceContractMethod(pass, goodFormat) {
		t.Fatal("expected Good.Format to match contract")
	}
	goodReadRune := findMethodDecl(t, file, "Good", "ReadRune")
	if !isKnownInterfaceContractMethod(pass, goodReadRune) {
		t.Fatal("expected Good.ReadRune to match contract")
	}

	badWrite := findMethodDecl(t, file, "Bad", "Write")
	if isKnownInterfaceContractMethod(pass, badWrite) {
		t.Fatal("expected Bad.Write to not match contract")
	}
	badFormat := findMethodDecl(t, file, "Bad", "Format")
	if isKnownInterfaceContractMethod(pass, badFormat) {
		t.Fatal("expected Bad.Format to not match contract")
	}

	plainWrite := findFreeFuncDecl(t, file, "Write")
	if isKnownInterfaceContractMethod(pass, plainWrite) {
		t.Fatal("expected top-level Write function to not match method contract")
	}
}

func findMethodDecl(t *testing.T, file *ast.File, recvName, methodName string) *ast.FuncDecl {
	t.Helper()

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		if fn.Name.Name != methodName {
			continue
		}
		if receiverTypeName(fn.Recv.List[0].Type) == recvName {
			return fn
		}
	}
	t.Fatalf("method %s.%s not found", recvName, methodName)
	return nil
}

func findFreeFuncDecl(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		if fn.Name.Name == name {
			return fn
		}
	}
	t.Fatalf("function %s not found", name)
	return nil
}
