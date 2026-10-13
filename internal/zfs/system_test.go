package zfs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// systemImports are the packages that run processes or call libzfs, which only System may use.
var systemImports = []string{"os/exec", "github.com/kraudcloud/go-libzfs", "github.com/mistifyio/go-zfs/v4"}

// systemHelpers are the functions of System that run processes or read the system, see systemImports.
var systemHelpers = []string{"runZfs", "runZpool", "runCommand", "runPoolCommand", "runSudoCommand", "readZfsMounts",
	"listSnapshotNames", "currentIdentity", "readDelegations", "listSudoRules", "isRoot"}

// notYetMovedToSystem are the files that still use libzfs directly, until the datasets and snapshots are loaded
// through the Backend as well.
var notYetMovedToSystem = map[string]bool{"common.go": true, "dataset.go": true, "permissions.go": true, "snapshot.go": true}

// TestOnlySystemRunsCommands makes sure that nothing but System touches the pools of the system, so a virtual
// Backend never does.
func TestOnlySystemRunsCommands(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || strings.HasPrefix(file, "system") || notYetMovedToSystem[file] {
			continue
		}
		source, err := os.ReadFile(file)
		require.NoError(t, err)
		parsed, err := parser.ParseFile(token.NewFileSet(), file, source, 0)
		require.NoError(t, err)
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			require.NoError(t, err)
			assert.NotContains(t, systemImports, path, "%s imports %s, which only System may use", file, path)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			if ident, ok := node.(*ast.Ident); ok {
				assert.NotContains(t, systemHelpers, ident.Name, "%s uses %s, which only System may use", file, ident.Name)
			}
			return true
		})
	}
}
