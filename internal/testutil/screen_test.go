package testutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests that read the screen of a running app on the UI thread draw it first (or use ScreenText): a change made on
// the UI thread is only shown once something draws. Such a test passed only while a background load happened to
// redraw the screen in time, and failed on a slower machine (e.g. in CI).
func TestScreenIsDrawnBeforeRead(t *testing.T) {
	checked := 0
	inspectTests(t, func(files *token.FileSet, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || calledName(call) != "OnUiThread" || len(call.Args) == 0 {
				return true
			}
			body, ok := call.Args[len(call.Args)-1].(*ast.FuncLit)
			if !ok {
				return true
			}
			reads, draws := false, false
			ast.Inspect(body, func(node ast.Node) bool {
				if inner, ok := node.(*ast.CallExpr); ok {
					switch calledName(inner) {
					case "GetContents":
						reads = true
					case "ForceDraw", "Draw":
						draws = true
					}
				}
				return true
			})
			if reads {
				checked++
				assert.True(t, draws, "%s reads the screen without drawing it first, use testutil.ScreenText",
					files.Position(call.Pos()))
			}
			return true
		})
	})
	assert.Positive(t, checked, "the check found the screen readers")
}

// Tests run their apps with RunApp, which waits until the event loop returned when the test ends: an app stopped with
// tview.Application.Stop alone could still handle an event while the cleanups of the test changed what it reads, a
// data race that failed the next test with -race.
func TestAppsAreRunWithRunApp(t *testing.T) {
	inspectTests(t, func(files *token.FileSet, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			statement, ok := node.(*ast.GoStmt)
			if !ok {
				return true
			}
			ast.Inspect(statement, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok && calledName(call) == "Run" && len(call.Args) == 0 {
					assert.Fail(t, "runs an app without RunApp", "%s: use testutil.RunApp",
						files.Position(call.Pos()))
				}
				return true
			})
			return true
		})
	})
}

// Test applications are created with NewApp (or get their screen with UseSimulationScreen), so they are set up like
// the application: tests that created their own forgot to enable the mouse or to disable double clicks, and their
// clicks did nothing.
func TestAppsAreCreatedWithNewApp(t *testing.T) {
	inspectTests(t, func(files *token.FileSet, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "tview" && selector.Sel.Name == "NewApplication" {
				assert.Fail(t, "creates an app without NewApp", "%s: use testutil.NewApp (or testutil.App)",
					files.Position(call.Pos()))
			}
			if selector.Sel.Name == "SetScreen" {
				assert.Fail(t, "sets a screen without UseSimulationScreen", "%s: use testutil.NewApp or "+
					"testutil.UseSimulationScreen", files.Position(call.Pos()))
			}
			return true
		})
	})
}

// inspectTests calls inspect with each test file of the module.
func inspectTests(t *testing.T, inspect func(files *token.FileSet, file *ast.File)) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "go.mod"))
	require.NoError(t, err, "the root of the module")

	files := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "bin") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(files, path, nil, 0)
		if err != nil {
			return err
		}
		inspect(files, file)
		return nil
	})
	require.NoError(t, err)
}

// calledName returns the name of the called function or method, "" if it is not called by name.
func calledName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}
	return ""
}
