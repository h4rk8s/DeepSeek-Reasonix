package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestProductionCodeDoesNotUsePanicConfigLoaders(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	panicLoaders := map[string]bool{
		"LoadForEdit":                   true,
		"LoadForEditWithoutCredentials": true,
	}
	fset := token.NewFileSet()
	var violations []string
	err = filepath.WalkDir(repoRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != repoRoot && (entry.Name() == ".git" || entry.Name() == ".worktree" || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				name = fun.Name
			case *ast.SelectorExpr:
				name = fun.Sel.Name
			}
			if panicLoaders[name] {
				position := fset.Position(call.Pos())
				rel, _ := filepath.Rel(repoRoot, position.Filename)
				violations = append(violations, rel+":"+strconv.Itoa(position.Line))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("production code must use error-returning strict config loaders; panic loader calls: %v", violations)
	}
}
