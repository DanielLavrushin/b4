package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"path/filepath"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/nfq"
)

func learnedKeepConfig(t *testing.T) *config.Config {
	t.Helper()
	held := config.NewSetConfig()
	held.Id, held.Name, held.Enabled = "ggc", "ggc", true
	held.Targets.IpsToMatch = []string{"198.51.100.0/24"}
	held.Targets.IPPriority = true
	learner := config.NewSetConfig()
	learner.Id, learner.Name, learner.Enabled = "video", "video", true
	learner.Targets.DomainsToMatch = []string{"video.example"}
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.Queue.IsDiscovery = true
	cfg.Sets = []*config.SetConfig{&held, &learner}
	return &cfg
}

func TestTheLearnedAddressFilterKeepsEverythingUntilThePoolHasAMatcher(t *testing.T) {
	cfg := learnedKeepConfig(t)
	held := net.ParseIP("198.51.100.20")
	for name, pool := range map[string]*nfq.Pool{
		"no pool":                  nil,
		"a pool without workers":   {},
		"a worker without matcher": {Workers: []*nfq.Worker{nfq.NewWorkerWithQueue(cfg, 0)}},
	} {
		if !learnedAddressKeeper(pool)(cfg, cfg.Sets[1], held) {
			t.Errorf("%s: a learned address was dropped with no matcher to decide it", name)
		}
	}
}

func TestTheLearnedAddressFilterAsksThePoolsMatcherBeforeThePoolStarts(t *testing.T) {
	cfg := learnedKeepConfig(t)
	pool := nfq.NewPool(cfg)
	t.Cleanup(pool.Stop)
	keep := learnedAddressKeeper(pool)
	if keep(cfg, cfg.Sets[1], net.ParseIP("198.51.100.20")) {
		t.Error("set video kept an address that set ggc above holds with IP priority")
	}
	if !keep(cfg, cfg.Sets[1], net.ParseIP("203.0.113.5")) {
		t.Error("set video lost an address no set holds")
	}
}

func isIdentNamed(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

func isSelectorOf(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == name && isIdentNamed(sel.X, pkg)
}

func runB4LearnedFilterWiring(path string) error {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
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
	pool := ""
	wired, engine := token.NoPos, token.NoPos
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if len(n.Lhs) != 1 || len(n.Rhs) != 1 {
				return true
			}
			call, ok := n.Rhs[0].(*ast.CallExpr)
			if !ok {
				return true
			}
			if lhs, ok := n.Lhs[0].(*ast.Ident); ok && pool == "" && isSelectorOf(call.Fun, "nfq", "NewPool") {
				pool = lhs.Name
			}
			if !wired.IsValid() && pool != "" && isSelectorOf(n.Lhs[0], "tables", "LearnedAddressKeepFunc") &&
				isIdentNamed(call.Fun, "learnedAddressKeeper") && len(call.Args) == 1 && isIdentNamed(call.Args[0], pool) {
				wired = n.Pos()
			}
		case *ast.CallExpr:
			if !engine.IsValid() && (isIdentNamed(n.Fun, "startNFQueueEngine") || isIdentNamed(n.Fun, "startTUNEngine")) {
				engine = n.Pos()
			}
		}
		return true
	})
	switch {
	case pool == "":
		return fmt.Errorf("runB4 in %s builds no pool with nfq.NewPool", path)
	case !wired.IsValid():
		return fmt.Errorf("runB4 in %s never sets tables.LearnedAddressKeepFunc to learnedAddressKeeper(%s), so the shipped binary routes and stamps every learned address a set with IP priority holds", path, pool)
	case !engine.IsValid():
		return fmt.Errorf("runB4 in %s starts no engine with startNFQueueEngine or startTUNEngine", path)
	case engine < wired:
		return fmt.Errorf("runB4 in %s starts the engine before it sets tables.LearnedAddressKeepFunc, so learned writes race with the assignment", path)
	}
	return nil
}

func TestRunB4HandsThePoolsMatcherToTheLearnedAddressFilterBeforeTheEngineStarts(t *testing.T) {
	if err := runB4LearnedFilterWiring("main.go"); err != nil {
		t.Fatal(err)
	}
}
