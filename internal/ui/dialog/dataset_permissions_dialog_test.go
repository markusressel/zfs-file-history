package dialog

import (
	"fmt"
	"strings"
	"testing"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func newTestDatasetPermissions() *zfs.DatasetPermissions {
	own := zfs.Delegation{Dataset: "pool/data", Scope: zfs.ScopeLocalAndDescendent, WhoType: zfs.WhoUser, Who: "alice", Permissions: []string{"@backup", "snapshot"}}
	parent := zfs.Delegation{Dataset: "pool", Scope: zfs.ScopeDescendent, WhoType: zfs.WhoGroup, Who: "staff", Permissions: []string{"mount"}}
	return &zfs.DatasetPermissions{
		Dataset: "pool/data",
		User:    "alice",
		Delegations: &zfs.Delegations{
			Dataset: "pool/data",
			Entries: []zfs.Delegation{own, parent},
			Sets:    []zfs.PermissionSet{{Dataset: "pool", Name: "@backup", Permissions: []string{"hold"}}},
		},
		Grants: map[zfs.Permission][]zfs.Grant{
			zfs.PermissionSnapshot: {{Delegation: own}},
			zfs.PermissionHold:     {{Delegation: own, Set: "@backup"}},
			zfs.PermissionMount:    {{Delegation: parent}, {Delegation: own}},
		},
	}
}

// tableRows returns the cell texts of the dialog's table, joined by "|" per row.
func tableRows(d *DatasetPermissionsDialog) []string {
	var rows []string
	for row := 0; row < d.table.GetRowCount(); row++ {
		var cells []string
		for column := 0; column < d.table.GetColumnCount(); column++ {
			cells = append(cells, d.table.GetCell(row, column).Text)
		}
		rows = append(rows, strings.TrimRight(strings.Join(cells, "|"), "|"))
	}
	return rows
}

func TestDatasetPermissionsDialog_Content(t *testing.T) {
	d := NewDatasetPermissionsDialog(tview.NewApplication(), newTestDatasetPermissions())

	assert.Equal(t, "DatasetPermissionsDialog", d.GetName())
	assert.Equal(t, []string{
		"Permission||You|Granted by",
		"snapshot|s|✓|user alice (this dataset)",
		"destroy|d|✗",
		"mount|m|✓|group staff (from pool) +1",
		"hold|h|✓|user alice via @backup (this dataset)",
		"release|r|✗",
		"clone|c|✗",
		"create|n|✗",
		"rollback|b|✗",
		"diff|f|✗",
	}, tableRows(d))
	assert.Equal(t, "Delegations\n"+
		"on this dataset:\n"+
		"  user alice: @backup,snapshot (local+descendent)\n"+
		"on pool:\n"+
		"  group staff: mount (descendent)\n"+
		"set @backup: hold (defined on pool)", d.delegations.GetText(true))
}

func TestDatasetPermissionsDialog_Details(t *testing.T) {
	d := NewDatasetPermissionsDialog(tview.NewApplication(), newTestDatasetPermissions())

	// the first permission is selected
	assert.Equal(t, "snapshot: Create snapshots (also needs mount)\nGranted to user alice (this dataset), local+descendent", d.details.GetText(true))

	d.table.Select(3, 0) // mount
	assert.Equal(t, "mount: Mount and unmount datasets, needed by most other permissions\n"+
		"Granted to group staff (from pool), descendent\n"+
		"Granted to user alice (this dataset), local+descendent", d.details.GetText(true))

	// the header row has no details
	d.updateDetails(0)
	assert.Equal(t, "", d.details.GetText(true))
}

func TestDatasetPermissionsDialog_Root(t *testing.T) {
	d := NewDatasetPermissionsDialog(tview.NewApplication(), &zfs.DatasetPermissions{
		Dataset:     "pool/data",
		User:        "root",
		IsRoot:      true,
		Delegations: &zfs.Delegations{Dataset: "pool/data"},
	})

	assert.Equal(t, "destroy|d|✓|root", tableRows(d)[2])
	assert.Equal(t, "Delegations\nnone on this dataset or its parents", d.delegations.GetText(true))
}

func TestDatasetPermissionsDialog_EscapesNames(t *testing.T) {
	permissions := newTestDatasetPermissions()
	permissions.Delegations.Entries[0].Who = "[red]alice"
	d := NewDatasetPermissionsDialog(tview.NewApplication(), permissions)
	// escaped, so it is displayed as "[red]alice" instead of coloring the text
	assert.Contains(t, d.delegations.GetText(true), "  user [red]alice: @backup,snapshot (local+descendent)")
}

func TestDatasetPermissionsDialog_ScrollDelegations(t *testing.T) {
	permissions := newTestDatasetPermissions()
	for i := 0; i < 20; i++ {
		permissions.Delegations.Entries = append(permissions.Delegations.Entries,
			zfs.Delegation{Dataset: "pool", Scope: zfs.ScopeLocal, WhoType: zfs.WhoUser, Who: fmt.Sprintf("user%d", i), Permissions: []string{"hold"}})
	}
	d := NewDatasetPermissionsDialog(tview.NewApplication(), permissions)
	d.delegations.SetRect(0, 0, 40, 5)

	press := func(key tcell.Key) {
		assert.Nil(t, d.layout.GetInputCapture()(tcell.NewEventKey(key, 0, tcell.ModNone)))
	}
	offset := func() int {
		row, _ := d.delegations.GetScrollOffset()
		return row
	}

	_, _, _, height := d.delegations.GetInnerRect()
	lines := 26 // heading, 2 datasets, 22 delegations, 1 set

	press(tcell.KeyPgDn)
	assert.Equal(t, height-1, offset(), "by a page, keeping one line for context")
	for i := 0; i < 20; i++ {
		press(tcell.KeyPgDn)
	}
	assert.Equal(t, lines-height, offset(), "stops at the last line")
	press(tcell.KeyPgUp)
	assert.Equal(t, lines-height-(height-1), offset())
	for i := 0; i < 20; i++ {
		press(tcell.KeyPgUp)
	}
	assert.Zero(t, offset())
}
