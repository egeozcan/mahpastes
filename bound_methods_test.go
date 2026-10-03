package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"reflect"
	"strings"
	"testing"
)

// boundServiceTypes lists every struct passed to Wails' Bind option in
// main.go. TestBoundServiceListMatchesMain keeps it in step with main.go.
var boundServiceTypes = []interface{}{
	&App{},
	&PluginService{},
	&ClipboardService{},
	&TransferService{},
	&ServeService{},
	&APIService{},
	&ShareService{},
	&LinkService{},
	&MarkdownService{},
	&FileProviderService{},
	&SelfTestService{},
}

var errorType = reflect.TypeOf((*error)(nil)).Elem()

// Wails v2's BoundMethod.Call only understands 0, 1 or 2 return values, and a
// 2-value method must return (T, error). Anything else resolves the JS promise
// with null, so the frontend silently sees nothing (ShowRestoreBackupDialog
// returned three values and restore did nothing).
func TestBoundMethodsHaveWailsCompatibleReturns(t *testing.T) {
	for _, svc := range boundServiceTypes {
		typ := reflect.TypeOf(svc)
		for i := 0; i < typ.NumMethod(); i++ {
			m := typ.Method(i)
			out := m.Type.NumOut()
			name := typ.Elem().Name() + "." + m.Name
			switch {
			case out > 2:
				t.Errorf("%s returns %d values; Wails v2 binds at most 2 — wrap them in a struct", name, out)
			case out == 2 && m.Type.Out(1) != errorType:
				t.Errorf("%s returns (%s, %s); a 2-value bound method must end in error", name, m.Type.Out(0), m.Type.Out(1))
			}
		}
	}
}

// TestBoundServiceListMatchesMain fails when main.go binds a service that
// boundServiceTypes does not list (or vice versa), so the return-shape check
// above cannot quietly miss a new service. It compares type names, not
// counts: swapping one service for another must fail too.
//
// main.go binds variables, so each element of `binds := []interface{}{...}`
// (and of any `binds = append(binds, ...)`) is resolved through its
// declaration: `x := NewFoo(...)` takes NewFoo's result type, `x := &Foo{}`
// and `var x *Foo` name it directly.
func TestBoundServiceListMatchesMain(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	pkg, ok := pkgs["main"]
	if !ok {
		t.Fatal("package main not found")
	}

	// Constructor name -> the struct type it returns.
	ctorResult := map[string]string{}
	for _, f := range pkg.Files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Type.Results == nil || len(fn.Type.Results.List) == 0 {
				continue
			}
			if name := typeName(fn.Type.Results.List[0].Type); name != "" {
				ctorResult[fn.Name.Name] = name
			}
		}
	}

	mainFile := pkg.Files["main.go"]
	if mainFile == nil {
		t.Fatal("main.go not found")
	}
	// Variable name -> the struct type it holds.
	varType := map[string]string{}
	var bound []ast.Expr
	foundLiteral := false
	ast.Inspect(mainFile, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if len(n.Lhs) != 1 || len(n.Rhs) != 1 {
				return true
			}
			id, ok := n.Lhs[0].(*ast.Ident)
			if !ok {
				return true
			}
			if id.Name == "binds" {
				switch rhs := n.Rhs[0].(type) {
				case *ast.CompositeLit:
					foundLiteral = true
					bound = append(bound, rhs.Elts...)
				case *ast.CallExpr:
					if fn, ok := rhs.Fun.(*ast.Ident); ok && fn.Name == "append" && len(rhs.Args) > 1 {
						bound = append(bound, rhs.Args[1:]...)
					}
				}
				return true
			}
			if name := exprType(n.Rhs[0], ctorResult); name != "" {
				varType[id.Name] = name
			}
		case *ast.ValueSpec:
			if n.Type == nil {
				return true
			}
			if name := typeName(n.Type); name != "" {
				for _, id := range n.Names {
					varType[id.Name] = name
				}
			}
		}
		return true
	})
	if !foundLiteral {
		t.Fatal("could not find `binds := []interface{}{...}` in main.go")
	}

	inMain := map[string]bool{}
	for _, e := range bound {
		name := exprType(e, ctorResult)
		if id, ok := e.(*ast.Ident); ok {
			name = varType[id.Name]
		}
		if name == "" {
			t.Fatalf("cannot resolve the type of bound value %s in main.go", types.ExprString(e))
		}
		inMain[name] = true
	}
	inTest := map[string]bool{}
	for _, svc := range boundServiceTypes {
		inTest[reflect.TypeOf(svc).Elem().Name()] = true
	}
	for name := range inMain {
		if !inTest[name] {
			t.Errorf("main.go binds %s, which boundServiceTypes does not list", name)
		}
	}
	for name := range inTest {
		if !inMain[name] {
			t.Errorf("boundServiceTypes lists %s, which main.go does not bind", name)
		}
	}
}

// typeName returns Foo for Foo or *Foo, and "" for anything else.
func typeName(e ast.Expr) string {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// exprType returns the struct type a `&Foo{}` or `NewFoo(...)` expression
// yields, or "" if it is neither.
func exprType(e ast.Expr, ctorResult map[string]string) string {
	switch e := e.(type) {
	case *ast.UnaryExpr:
		if lit, ok := e.X.(*ast.CompositeLit); ok && e.Op == token.AND {
			return typeName(lit.Type)
		}
	case *ast.CallExpr:
		if fn, ok := e.Fun.(*ast.Ident); ok {
			return ctorResult[fn.Name]
		}
	}
	return ""
}
