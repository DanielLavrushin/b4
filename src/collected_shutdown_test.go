package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func isCallStatementOf(s ast.Stmt, recv, name string) bool {
	es, ok := s.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.X.(*ast.CallExpr)
	return ok && isSelectorOf(call.Fun, recv, name)
}

func runB4CollectedShutdownOrder(path string) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return err
	}
	var body *ast.BlockStmt
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "runB4" {
			body = fn.Body
		}
	}
	if body == nil {
		return fmt.Errorf("%s has no runB4", path)
	}
	pool, mgr := "", ""
	poolAt := token.NoPos
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		lhs, isIdent := as.Lhs[0].(*ast.Ident)
		call, isCall := as.Rhs[0].(*ast.CallExpr)
		if !isIdent || !isCall {
			return true
		}
		if pool == "" && isSelectorOf(call.Fun, "nfq", "NewPool") {
			pool = lhs.Name
			poolAt = as.Pos()
		}
		if mgr == "" && isSelectorOf(call.Fun, "tproxy", "NewManager") {
			mgr = lhs.Name
		}
		return true
	})
	if pool == "" || mgr == "" {
		return fmt.Errorf("runB4 in %s builds no pool with nfq.NewPool or no manager with tproxy.NewManager", path)
	}

	var problems []string
	stops := 0
	ast.Inspect(body, func(n ast.Node) bool {
		block, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		holdsStop := false
		for _, s := range block.List {
			holdsStop = holdsStop || isCallStatementOf(s, pool, "StopCollected")
		}
		stopped := false
		for _, s := range block.List {
			if isCallStatementOf(s, pool, "StopCollected") {
				stopped = true
			}
			isClear := isCallStatementOf(s, "tables", "RoutingClearAll") || isCallStatementOf(s, "tables", "ClearAppliedRules")
			if isClear && holdsStop && !stopped && s.Pos() > poolAt {
				problems = append(problems, fmt.Sprintf("the firewall clear at line %d runs before %s.StopCollected() in its block, so a collected install still in flight can put rules back after the clear", fset.Position(s.Pos()).Line, pool))
			}
			if isCallStatementOf(s, mgr, "Stop") {
				stops++
				if !stopped {
					problems = append(problems, fmt.Sprintf("%s.Stop() at line %d runs before %s.StopCollected() in its block, so a collected install still in flight can sync the stopped manager", mgr, fset.Position(s.Pos()).Line, pool))
				}
			}
		}
		return true
	})
	if stops < 2 {
		problems = append(problems, fmt.Sprintf("runB4 stops %s in %d place(s), want the failed-engine branch and the shutdown", mgr, stops))
	}

	cleanups := 0
	ast.Inspect(body, func(n ast.Node) bool {
		def, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		lit, ok := def.Call.Fun.(*ast.FuncLit)
		if !ok {
			return true
		}
		stopAt, firstTables := token.NoPos, token.NoPos
		clears := false
		ast.Inspect(lit.Body, func(m ast.Node) bool {
			call, ok := m.(*ast.CallExpr)
			if !ok {
				return true
			}
			if !stopAt.IsValid() && isSelectorOf(call.Fun, pool, "StopCollected") {
				stopAt = call.Pos()
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && isIdentNamed(sel.X, "tables") {
				if !firstTables.IsValid() {
					firstTables = call.Pos()
				}
				clears = clears || sel.Sel.Name == "ClearAppliedRules" || sel.Sel.Name == "RoutingClearAll"
			}
			return true
		})
		if !clears {
			return true
		}
		cleanups++
		switch {
		case !stopAt.IsValid():
			problems = append(problems, fmt.Sprintf("the deferred cleanup at line %d clears the firewall without %s.StopCollected()", fset.Position(def.Pos()).Line, pool))
		case firstTables.IsValid() && firstTables < stopAt:
			problems = append(problems, fmt.Sprintf("the deferred cleanup at line %d calls tables at line %d before %s.StopCollected() at line %d, so a collected install still in flight can put rules back after the clear", fset.Position(def.Pos()).Line, fset.Position(firstTables).Line, pool, fset.Position(stopAt).Line))
		}
		return false
	})
	if cleanups == 0 {
		problems = append(problems, "runB4 has no deferred cleanup that clears the firewall")
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s: %s", path, strings.Join(problems, "; "))
	}
	return nil
}

func TestRunB4StopsCollectedInstallsBeforeTheProxyAndTheFirewall(t *testing.T) {
	if err := runB4CollectedShutdownOrder("main.go"); err != nil {
		t.Fatal(err)
	}
}
