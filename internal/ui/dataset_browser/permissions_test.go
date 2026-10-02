package dataset_browser

import (
	"errors"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain makes sure no test reads permissions from ZFS: tests that need them stub them with stubPermissions.
func TestMain(m *testing.M) {
	loadDatasetPermissions = func(dataset string) (*zfs.DatasetPermissions, error) {
		return nil, errors.New("permissions are not available in tests")
	}
	isCurrentUserRoot = func() bool { return false }
	os.Exit(m.Run())
}

// permissionStub records which datasets the permissions were read for. Each dataset grants the permissions in
// granted (by name), others fail.
type permissionStub struct {
	mu      sync.Mutex
	read    []string
	granted map[string][]zfs.Permission
	// block, if set, delays every read until it is closed
	block chan struct{}
}

func stubPermissions(t *testing.T, granted map[string][]zfs.Permission) *permissionStub {
	stub := &permissionStub{granted: granted}
	original := loadDatasetPermissions
	t.Cleanup(func() { loadDatasetPermissions = original })
	loadDatasetPermissions = func(dataset string) (*zfs.DatasetPermissions, error) {
		stub.mu.Lock()
		stub.read = append(stub.read, dataset)
		block := stub.block
		permissions, ok := stub.granted[dataset]
		stub.mu.Unlock()
		if block != nil {
			<-block
		}
		if !ok {
			return nil, errors.New("cannot read delegations")
		}
		grants := map[zfs.Permission][]zfs.Grant{}
		for _, permission := range permissions {
			grants[permission] = []zfs.Grant{{Delegation: zfs.Delegation{Dataset: dataset, Scope: zfs.ScopeLocalAndDescendent, WhoType: zfs.WhoUser, Who: "alice"}}}
		}
		return &zfs.DatasetPermissions{Dataset: dataset, User: "alice", Grants: grants, Delegations: &zfs.Delegations{Dataset: dataset}}, nil
	}
	return stub
}

func (stub *permissionStub) readDatasets() []string {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	result := append([]string{}, stub.read...)
	sort.Strings(result)
	return result
}

func TestFormatPermissionMask(t *testing.T) {
	permissions := &zfs.DatasetPermissions{Grants: map[zfs.Permission][]zfs.Grant{
		zfs.PermissionSnapshot: {{}},
		zfs.PermissionMount:    {{}},
		zfs.PermissionHold:     {{}},
	}}
	assert.Equal(t, "s-mh-----", formatPermissionMask(permissionState{permissions: permissions}, true))
	assert.Equal(t, "sdmhrcnbf", formatPermissionMask(permissionState{permissions: &zfs.DatasetPermissions{IsRoot: true}}, true))
	assert.Equal(t, "?", formatPermissionMask(permissionState{err: errors.New("failed")}, true))
	assert.Equal(t, "", formatPermissionMask(permissionState{}, false), "not loaded yet")
}

// permissionCell returns the text of the permissions column of the dataset.
func permissionCell(t *testing.T, browser *DatasetBrowserComponent, name string) string {
	var text string
	onUiThread(t, browser.application, func() {
		entry := findByName(browser.tableContainer.GetEntries(), name)
		if entry == nil {
			return
		}
		cells := browser.toTableCells(0, []*table.Column{columnPermissions}, entry)
		text = cells[0].Text
	})
	return text
}

func TestDatasetBrowser_LoadsPermissionsOfDisplayedDatasets(t *testing.T) {
	stub := stubPermissions(t, map[string][]zfs.Permission{
		"rpool/a": {zfs.PermissionSnapshot, zfs.PermissionMount},
	})
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		return []*zfs.DatasetListEntry{
			{Name: "rpool/a", MountPath: "/a"},
			{Name: "rpool/b", MountPath: "/b"},
			{Name: "rpool/hidden", Mountpoint: "legacy"},
		}, nil
	})
	app, browser, _ := newBrowserApp(t)

	onUiThread(t, app, func() { browser.Refresh(false) })

	require.Eventually(t, func() bool { return permissionCell(t, browser, "rpool/a") == "s-m------" }, 2*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool { return permissionCell(t, browser, "rpool/b") == "?" }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"rpool/a", "rpool/b"}, stub.readDatasets(), "hidden (unmounted) datasets are not read")

	// showing the unmounted datasets reads only the new ones
	onUiThread(t, app, func() { browser.ToggleHideUnmounted() })
	require.Eventually(t, func() bool { return len(stub.readDatasets()) == 3 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"rpool/a", "rpool/b", "rpool/hidden"}, stub.readDatasets())

	// a reload reads them again, but keeps showing the previous values meanwhile
	stub.mu.Lock()
	stub.block = make(chan struct{})
	block := stub.block
	stub.mu.Unlock()
	onUiThread(t, app, func() { browser.Refresh(false) })
	require.Eventually(t, func() bool { return len(stub.readDatasets()) > 3 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, "s-m------", permissionCell(t, browser, "rpool/a"))
	close(block)
	require.Eventually(t, func() bool { return len(stub.readDatasets()) == 6 }, 2*time.Second, 10*time.Millisecond)
}

func TestDatasetBrowser_PermissionsAreOnlyLoadedForTheColumn(t *testing.T) {
	stub := stubPermissions(t, map[string][]zfs.Permission{"rpool/a": {zfs.PermissionSnapshot}})
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		return []*zfs.DatasetListEntry{{Name: "rpool/a", MountPath: "/a"}}, nil
	})
	app, browser, _ := newBrowserApp(t)

	withoutPermissions := []*table.Column{columnName, columnUsed}
	onUiThread(t, app, func() {
		browser.tableContainer.SetActiveColumns(withoutPermissions)
		browser.Refresh(false)
	})
	require.Eventually(t, func() bool {
		count := 0
		onUiThread(t, app, func() { count = len(browser.tableContainer.GetEntries()) })
		return count == 1
	}, 2*time.Second, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, stub.readDatasets())

	// adding the column (F2) loads them
	onUiThread(t, app, func() { browser.tableContainer.SetActiveColumns(append(withoutPermissions, columnPermissions)) })
	require.Eventually(t, func() bool { return permissionCell(t, browser, "rpool/a") == "s--------" }, 2*time.Second, 10*time.Millisecond)
}

func TestDatasetBrowser_RootReadsNoPermissions(t *testing.T) {
	stub := stubPermissions(t, nil)
	originalRoot := isCurrentUserRoot
	t.Cleanup(func() { isCurrentUserRoot = originalRoot })
	isCurrentUserRoot = func() bool { return true }
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		return []*zfs.DatasetListEntry{{Name: "rpool/a", MountPath: "/a"}}, nil
	})
	app, browser, _ := newBrowserApp(t)

	onUiThread(t, app, func() { browser.Refresh(false) })

	require.Eventually(t, func() bool { return permissionCell(t, browser, "rpool/a") == "sdmhrcnbf" }, 2*time.Second, 10*time.Millisecond)
	assert.Empty(t, stub.readDatasets())
}

func TestDatasetBrowser_PermissionsDialog(t *testing.T) {
	stubPermissions(t, map[string][]zfs.Permission{"rpool/a": {zfs.PermissionHold}})
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		return []*zfs.DatasetListEntry{{Name: "rpool/a", MountPath: "/a"}}, nil
	})
	app, browser, screen := newBrowserApp(t)
	onUiThread(t, app, func() {
		browser.Refresh(false)
		browser.Focus()
	})
	require.Eventually(t, func() bool { return permissionCell(t, browser, "rpool/a") != "" }, 2*time.Second, 10*time.Millisecond)

	screen.InjectKey(tcell.KeyRune, 'p', tcell.ModNone)

	require.Eventually(t, func() bool {
		shown := false
		onUiThread(t, app, func() { shown = browser.layout.HasPage("DatasetPermissionsDialog") })
		return shown
	}, 2*time.Second, 10*time.Millisecond)

	screen.InjectKey(tcell.KeyEscape, 0, tcell.ModNone)
	require.Eventually(t, func() bool {
		shown := true
		onUiThread(t, app, func() { shown = browser.layout.HasPage("DatasetPermissionsDialog") })
		return !shown
	}, 2*time.Second, 10*time.Millisecond)
}

func TestDatasetBrowser_PermissionsDialogError(t *testing.T) {
	stubPermissions(t, nil)
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		return []*zfs.DatasetListEntry{{Name: "rpool/a", MountPath: "/a"}}, nil
	})
	app, browser, screen := newBrowserApp(t)
	onUiThread(t, app, func() {
		browser.Refresh(false)
		browser.Focus()
	})
	require.Eventually(t, func() bool { return permissionCell(t, browser, "rpool/a") == "?" }, 2*time.Second, 10*time.Millisecond)

	screen.InjectKey(tcell.KeyRune, 'p', tcell.ModNone)

	require.Eventually(t, func() bool {
		shown := false
		onUiThread(t, app, func() { shown = browser.layout.HasPage("ErrorDialog") })
		return shown
	}, 2*time.Second, 10*time.Millisecond)
}
