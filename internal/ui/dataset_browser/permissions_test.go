package dataset_browser

import (
	"errors"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain makes sure no test reads permissions or properties from ZFS: tests that need them stub them with stubPermissions.
func TestMain(m *testing.M) {
	loadDatasetPermissions = func(dataset string) (*zfs.DatasetPermissions, error) {
		return nil, errors.New("permissions are not available in tests")
	}
	isCurrentUserRoot = func() bool { return false }
	listDatasetProperties = func(dataset string) ([]*zfs.Property, error) {
		return nil, errors.New("properties are not available in tests")
	}
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
	testutil.OnUiThread(t, browser.application, func() {
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

	testutil.OnUiThread(t, app, func() { browser.Refresh(false) })

	require.Eventually(t, func() bool { return permissionCell(t, browser, "rpool/a") == "s-m------" }, 2*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool { return permissionCell(t, browser, "rpool/b") == "?" }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"rpool/a", "rpool/b"}, stub.readDatasets(), "hidden (unmounted) datasets are not read")

	// showing the unmounted datasets reads only the new ones
	testutil.OnUiThread(t, app, func() { browser.ToggleHideUnmounted() })
	require.Eventually(t, func() bool { return len(stub.readDatasets()) == 3 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"rpool/a", "rpool/b", "rpool/hidden"}, stub.readDatasets())

	// a reload reads them again, but keeps showing the previous values meanwhile
	stub.mu.Lock()
	stub.block = make(chan struct{})
	block := stub.block
	stub.mu.Unlock()
	testutil.OnUiThread(t, app, func() { browser.Refresh(false) })
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
	testutil.OnUiThread(t, app, func() {
		browser.tableContainer.SetActiveColumns(withoutPermissions)
		browser.Refresh(false)
	})
	require.Eventually(t, func() bool {
		count := 0
		testutil.OnUiThread(t, app, func() { count = len(browser.tableContainer.GetEntries()) })
		return count == 1
	}, 2*time.Second, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, stub.readDatasets())

	// adding the column (F2) loads them
	testutil.OnUiThread(t, app, func() { browser.tableContainer.SetActiveColumns(append(withoutPermissions, columnPermissions)) })
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

	testutil.OnUiThread(t, app, func() { browser.Refresh(false) })

	require.Eventually(t, func() bool { return permissionCell(t, browser, "rpool/a") == "sdmhrcnbf" }, 2*time.Second, 10*time.Millisecond)
	assert.Empty(t, stub.readDatasets())
}

func TestDatasetBrowser_PermissionsDialog(t *testing.T) {
	stubPermissions(t, map[string][]zfs.Permission{"rpool/a": {zfs.PermissionHold}})
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		return []*zfs.DatasetListEntry{{Name: "rpool/a", MountPath: "/a"}}, nil
	})
	app, browser, screen := newBrowserApp(t)
	testutil.OnUiThread(t, app, func() {
		browser.Refresh(false)
		browser.Focus()
	})
	require.Eventually(t, func() bool { return permissionCell(t, browser, "rpool/a") != "" }, 2*time.Second, 10*time.Millisecond)

	screen.InjectKey(tcell.KeyRune, 'p', tcell.ModNone)

	require.Eventually(t, func() bool {
		shown := false
		testutil.OnUiThread(t, app, func() { shown = browser.layout.HasPage("DatasetPermissionsDialog") })
		return shown
	}, 2*time.Second, 10*time.Millisecond)

	screen.InjectKey(tcell.KeyEscape, 0, tcell.ModNone)
	require.Eventually(t, func() bool {
		shown := true
		testutil.OnUiThread(t, app, func() { shown = browser.layout.HasPage("DatasetPermissionsDialog") })
		return !shown
	}, 2*time.Second, 10*time.Millisecond)
}

func TestDatasetBrowser_PermissionsDialogError(t *testing.T) {
	stubPermissions(t, nil)
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		return []*zfs.DatasetListEntry{{Name: "rpool/a", MountPath: "/a"}}, nil
	})
	app, browser, screen := newBrowserApp(t)
	testutil.OnUiThread(t, app, func() {
		browser.Refresh(false)
		browser.Focus()
	})
	require.Eventually(t, func() bool { return permissionCell(t, browser, "rpool/a") == "?" }, 2*time.Second, 10*time.Millisecond)

	screen.InjectKey(tcell.KeyRune, 'p', tcell.ModNone)

	require.Eventually(t, func() bool {
		shown := false
		testutil.OnUiThread(t, app, func() { shown = browser.layout.HasPage("ErrorDialog") })
		return shown
	}, 2*time.Second, 10*time.Millisecond)
}

func TestDatasetBrowser_PropertiesDialog(t *testing.T) {
	original := listDatasetProperties
	t.Cleanup(func() { listDatasetProperties = original })
	var read []string
	var mu sync.Mutex
	listDatasetProperties = func(dataset string) ([]*zfs.Property, error) {
		mu.Lock()
		defer mu.Unlock()
		read = append(read, dataset)
		if dataset == "rpool/broken" {
			return nil, errors.New("cannot open 'rpool/broken'")
		}
		return []*zfs.Property{{Name: "compression", Value: "zstd", Source: "local"}}, nil
	}
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		return []*zfs.DatasetListEntry{{Name: "rpool/a", MountPath: "/a"}, {Name: "rpool/broken", MountPath: "/b"}}, nil
	})
	app, browser, screen := newBrowserApp(t)
	testutil.OnUiThread(t, app, func() {
		browser.Refresh(false)
		browser.Focus()
	})
	require.Eventually(t, func() bool {
		count := 0
		testutil.OnUiThread(t, app, func() { count = len(browser.tableContainer.GetEntries()) })
		return count == 2
	}, 2*time.Second, 10*time.Millisecond)
	hasPage := func(name string) func() bool {
		return func() bool {
			shown := false
			testutil.OnUiThread(t, app, func() { shown = browser.layout.HasPage(name) })
			return shown
		}
	}

	testutil.OnUiThread(t, app, func() { browser.tableContainer.Select(findByName(browser.tableContainer.GetEntries(), "rpool/a")) })
	screen.InjectKey(tcell.KeyRune, 'e', tcell.ModNone)
	require.Eventually(t, hasPage("DatasetPropertiesDialog"), 2*time.Second, 10*time.Millisecond)
	screen.InjectKey(tcell.KeyEscape, 0, tcell.ModNone)
	require.Eventually(t, func() bool { return !hasPage("DatasetPropertiesDialog")() }, 2*time.Second, 10*time.Millisecond)

	testutil.OnUiThread(t, app, func() { browser.tableContainer.Select(findByName(browser.tableContainer.GetEntries(), "rpool/broken")) })
	screen.InjectKey(tcell.KeyRune, 'e', tcell.ModNone)
	require.Eventually(t, hasPage("ErrorDialog"), 2*time.Second, 10*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"rpool/a", "rpool/broken"}, read)
}

// After permissions were granted (e.g. in the missing permissions dialog), the column is read again without a reload
// of the dataset list; the previous values are shown meanwhile.
func TestDatasetBrowser_ReloadPermissions(t *testing.T) {
	granted := map[string][]zfs.Permission{"rpool/a": {zfs.PermissionSnapshot}}
	stub := stubPermissions(t, granted)
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		return []*zfs.DatasetListEntry{{Name: "rpool/a", MountPath: "/a"}}, nil
	})
	app, browser, _ := newBrowserApp(t)
	testutil.OnUiThread(t, app, func() { browser.Refresh(false) })
	require.Eventually(t, func() bool { return permissionCell(t, browser, "rpool/a") == "s--------" }, 2*time.Second, 10*time.Millisecond)

	// granted: hold
	stub.mu.Lock()
	granted["rpool/a"] = []zfs.Permission{zfs.PermissionSnapshot, zfs.PermissionHold}
	stub.block = make(chan struct{})
	block := stub.block
	stub.mu.Unlock()
	testutil.OnUiThread(t, app, func() { browser.ReloadPermissions() })
	require.Eventually(t, func() bool { return len(stub.readDatasets()) == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, "s--------", permissionCell(t, browser, "rpool/a"), "the previous value while reading")

	close(block)
	require.Eventually(t, func() bool { return permissionCell(t, browser, "rpool/a") == "s--h-----" }, 2*time.Second, 10*time.Millisecond)
}
