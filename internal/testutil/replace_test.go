package testutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var replaced = 1

func TestReplace(t *testing.T) {
	t.Run("replaced until the end of the test", func(t *testing.T) {
		Replace(t, &replaced, 2)
		assert.Equal(t, 2, replaced)
	})
	assert.Equal(t, 1, replaced, "restored")

	t.Run("restored in the order of the replacements", func(t *testing.T) {
		Replace(t, &replaced, 2)
		Replace(t, &replaced, 3)
		assert.Equal(t, 3, replaced)
	})
	assert.Equal(t, 1, replaced)

	t.Run("not in parallel tests", func(t *testing.T) {
		t.Parallel()
		assert.Panics(t, func() { Replace(t, &replaced, 2) })
	})
}

// Tests change package variables (the functions that run processes or call libzfs, the state, the emitters, the zfs
// backend) only with Replace, which restores them when the test ends and fails a parallel test: an assignment restored by hand was
// forgotten, restored before the app of the test stopped, or raced with a parallel test, and later tests saw the
// value of an earlier one. Assignments in TestMain and init set up the whole package and are not restored.
func TestPackageVariablesAreReplacedWithReplace(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "go.mod"))
	require.NoError(t, err, "the root of the module")

	files := token.NewFileSet()
	// the files of each package, by directory and package name (a directory has a package and its external tests)
	packages := map[string][]*ast.File{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "bin" || entry.Name() == "scripts") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(files, path, nil, 0)
		if err != nil {
			return err
		}
		key := filepath.Dir(path) + ":" + file.Name.Name
		packages[key] = append(packages[key], file)
		return nil
	})
	require.NoError(t, err)

	for _, packageFiles := range packages {
		variables := packageVariables(packageFiles)
		for _, file := range packageFiles {
			if !strings.HasSuffix(files.Position(file.Pos()).Filename, "_test.go") {
				continue
			}
			imports := importNames(file)
			topLevel := topLevelSpecs(file)
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Body == nil || (function.Recv == nil && (function.Name.Name == "TestMain" || function.Name.Name == "init")) {
					continue
				}
				ast.Inspect(function.Body, func(node ast.Node) bool {
					if call, ok := node.(*ast.CallExpr); ok && calledName(call) == "SetBackend" {
						t.Errorf("%s: SetBackend is called, use testutil.Replace on the backend", files.Position(call.Pos()))
					}
					assignment, ok := node.(*ast.AssignStmt)
					if !ok || assignment.Tok != token.ASSIGN {
						return true
					}
					for _, target := range assignment.Lhs {
						if name, ok := assignedPackageVariable(target, variables, imports, topLevel); ok {
							t.Errorf("%s: %s is assigned, use testutil.Replace", files.Position(target.Pos()), name)
						}
					}
					return true
				})
			}
		}
	}
}

// topLevelSpecs returns the variable declarations at the top level of the file.
func topLevelSpecs(file *ast.File) map[*ast.ValueSpec]bool {
	specs := map[*ast.ValueSpec]bool{}
	for _, declaration := range file.Decls {
		if general, ok := declaration.(*ast.GenDecl); ok && general.Tok == token.VAR {
			for _, spec := range general.Specs {
				specs[spec.(*ast.ValueSpec)] = true
			}
		}
	}
	return specs
}

// packageVariables returns the names of the variables declared at the top level of the files of a package.
func packageVariables(files []*ast.File) map[string]bool {
	variables := map[string]bool{}
	for _, file := range files {
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.VAR {
				continue
			}
			for _, spec := range general.Specs {
				for _, name := range spec.(*ast.ValueSpec).Names {
					variables[name.Name] = true
				}
			}
		}
	}
	return variables
}

// importNames returns the names the file refers to its imported packages by.
func importNames(file *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, spec := range file.Imports {
		if spec.Name != nil {
			names[spec.Name.Name] = true
			continue
		}
		importPath, _ := strconv.Unquote(spec.Path.Value)
		name := path.Base(importPath)
		if strings.HasPrefix(name, "v") && strings.Trim(name[1:], "0123456789") == "" {
			// a major version suffix, e.g. github.com/gdamore/tcell/v2
			name = path.Base(path.Dir(importPath))
		}
		names[name] = true
	}
	return names
}

// assignedPackageVariable returns the package variable that an assignment to target changes (also a field or an
// element of it), and whether it is one: a variable of the package itself that is not shadowed by a local one, or of
// an imported package.
func assignedPackageVariable(target ast.Expr, variables map[string]bool, imports map[string]bool,
	topLevel map[*ast.ValueSpec]bool) (string, bool) {
	for {
		switch expression := target.(type) {
		case *ast.ParenExpr:
			target = expression.X
		case *ast.IndexExpr:
			target = expression.X
		case *ast.StarExpr:
			target = expression.X
		case *ast.SelectorExpr:
			if root, ok := expression.X.(*ast.Ident); ok && root.Obj == nil && imports[root.Name] {
				return root.Name + "." + expression.Sel.Name, true
			}
			target = expression.X
		case *ast.Ident:
			// Obj is set for the declarations of the same file: a local variable, or a package variable of this file
			if expression.Obj != nil {
				spec, ok := expression.Obj.Decl.(*ast.ValueSpec)
				return expression.Name, ok && expression.Obj.Kind == ast.Var && topLevel[spec]
			}
			return expression.Name, expression.Name != "_" && variables[expression.Name]
		default:
			return "", false
		}
	}
}

// An app started after Replace (e.g. a restart of the UI in a test) is stopped before the value is restored, so it
// never sees the change.
func TestReplaceStopsTheAppsFirst(t *testing.T) {
	var atStop int
	t.Run("test", func(t *testing.T) {
		Replace(t, &replaced, 2)
		runningApps.add(t, func() { atStop = replaced })
	})
	assert.Equal(t, 2, atStop, "stopped before the value was restored")
	assert.Equal(t, 1, replaced)
}
