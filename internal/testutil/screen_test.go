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

// Tests that read the screen of a running app on the UI thread draw it first (or use ScreenText or FindText): a change
// made on the UI thread is only shown once something draws. Such a test passed only while a background load happened
// to redraw the screen in time, and failed on a slower machine (e.g. in CI).
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
					case "ScreenLines", "FindInScreen", "GetContents":
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

// Tests read the screen of a running app (of NewApp or UseSimulationScreen) on the UI thread, with ScreenText, FindText
// or inside OnUiThread: the app may redraw at the same time (e.g. after a background load), and a test that read it
// meanwhile saw it half drawn, which failed in CI but not on a faster machine. Screens a test draws on itself
// (NewScreen, DrawToScreen) are not checked.
func TestScreenOfRunningAppIsReadOnTheUiThread(t *testing.T) {
	checked := 0
	inspectTests(t, func(files *token.FileSet, file *ast.File) {
		if strings.Contains(files.Position(file.Pos()).Filename, "/internal/testutil/") {
			return
		}
		// Fields are set in the constructor of a test and read in its methods, so they are tracked in the whole
		// file; local variables only in their function.
		fields := appScreens(file, true)
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			locals := appScreens(function, false)
			var stack []ast.Node
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if node == nil {
					stack = stack[:len(stack)-1]
					return true
				}
				stack = append(stack, node)
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				screen := readScreen(call)
				if screen == nil || !isAppScreen(screen, fields, locals) {
					return true
				}
				checked++
				assert.True(t, isOnUiThread(stack), "%s reads the screen of a running app outside the UI thread, "+
					"use testutil.ScreenText, FindText or OnUiThread", files.Position(call.Pos()))
				return true
			})
		}
	})
	assert.Positive(t, checked, "the check found the screen readers")
}

// appScreens returns the names of the screens of running apps assigned in node: the fields (dt.screen) if fields,
// otherwise the local variables.
func appScreens(node ast.Node, fields bool) map[string]bool {
	names := map[string]bool{}
	add := func(expression ast.Expr) {
		switch expression := expression.(type) {
		case *ast.Ident:
			if !fields && expression.Name != "_" {
				names[expression.Name] = true
			}
		case *ast.SelectorExpr:
			if fields {
				names[expression.Sel.Name] = true
			}
		}
	}
	ast.Inspect(node, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			if len(node.Rhs) != 1 {
				return true
			}
			call, ok := node.Rhs[0].(*ast.CallExpr)
			if !ok {
				return true
			}
			switch {
			case calledName(call) == "NewApp" && len(node.Lhs) == 2:
				add(node.Lhs[1])
			case calledName(call) == "UseSimulationScreen" && len(node.Lhs) == 1:
				add(node.Lhs[0])
			}
		case *ast.KeyValueExpr:
			call, ok := node.Value.(*ast.CallExpr)
			key, isIdent := node.Key.(*ast.Ident)
			if ok && isIdent && fields && calledName(call) == "UseSimulationScreen" {
				names[key.Name] = true
			}
		}
		return true
	})
	return names
}

// readScreen returns the screen call reads from (FindInScreen, ScreenLines, screen.Get, ...), nil if it does not.
func readScreen(call *ast.CallExpr) ast.Expr {
	switch calledName(call) {
	case "FindInScreen", "ScreenLines":
		if len(call.Args) > 0 {
			return call.Args[0]
		}
	case "Get", "GetContents", "GetCursor":
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
			return selector.X
		}
	}
	return nil
}

// isAppScreen returns whether screen is one of the screens of running apps of appScreens.
func isAppScreen(screen ast.Expr, fields map[string]bool, locals map[string]bool) bool {
	switch screen := screen.(type) {
	case *ast.Ident:
		return locals[screen.Name]
	case *ast.SelectorExpr:
		return fields[screen.Sel.Name]
	}
	return false
}

// isOnUiThread returns whether the innermost node of stack is inside the function passed to OnUiThread.
func isOnUiThread(stack []ast.Node) bool {
	for i := len(stack) - 1; i > 0; i-- {
		if _, ok := stack[i].(*ast.FuncLit); !ok {
			continue
		}
		if call, ok := stack[i-1].(*ast.CallExpr); ok && calledName(call) == "OnUiThread" {
			return true
		}
	}
	return false
}

// Tests read the text of the screen with the helpers of testutil (ScreenText, ScreenLines, FindText, ...) and create
// screens with NewApp or NewScreen: tests that did it themselves skipped the cells nothing was drawn in (so the
// columns did not line up), read it without showing it first, or set the size before initializing the screen, which
// resets it. A cell is read with screen.Get.
func TestScreenIsReadWithTestutil(t *testing.T) {
	inspectTests(t, func(files *token.FileSet, file *ast.File) {
		if strings.Contains(files.Position(file.Pos()).Filename, "/internal/testutil/") {
			return
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch calledName(call) {
			case "GetContents":
				assert.Fail(t, "reads the screen itself", "%s: use testutil.ScreenLines (or screen.Get for a cell)",
					files.Position(call.Pos()))
			case "NewSimulationScreen":
				assert.Fail(t, "creates a screen itself", "%s: use testutil.NewApp or testutil.NewScreen",
					files.Position(call.Pos()))
			}
			return true
		})
	})
}

// Tests click and drag with testutil.Click and Drag, which wait until the app handled the events: a test that checked
// the result right after injecting them read it before they were handled on a slow machine. Moving the mouse (hover)
// and the wheel are injected directly.
func TestClicksUseTestutil(t *testing.T) {
	inspectTests(t, func(files *token.FileSet, file *ast.File) {
		if strings.Contains(files.Position(file.Pos()).Filename, "/internal/testutil/") {
			return
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || calledName(call) != "InjectMouse" || len(call.Args) < 3 {
				return true
			}
			if button, ok := call.Args[2].(*ast.SelectorExpr); ok && button.Sel.Name == "Button1" {
				assert.Fail(t, "clicks without testutil", "%s: use testutil.Click (or ClickText, Drag)",
					files.Position(call.Pos()))
			}
			return true
		})
	})
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
