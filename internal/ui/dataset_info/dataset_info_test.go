package dataset_info

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLoader struct {
	mu       sync.Mutex
	requests []datasetInfoRequest
	// block, if set for a dataset name, delays its load until the channel is closed
	block map[string]chan struct{}
	fail  map[string]bool
}

func (f *fakeLoader) load(request datasetInfoRequest) (*datasetInfoResult, error) {
	f.mu.Lock()
	f.requests = append(f.requests, request)
	block := f.block[request.name]
	fail := f.fail[request.name]
	f.mu.Unlock()

	if block != nil {
		<-block
	}
	if fail {
		return nil, errors.New("load failed")
	}

	key := request.name
	if key == "" {
		key = request.path
	}
	return &datasetInfoResult{
		dataset: &zfs.Dataset{Path: request.path},
		title:   "Dataset: " + key,
		facts:   &datasetFacts{kind: "filesystem", mounted: "yes", mountPoint: key},
	}, nil
}

func (f *fakeLoader) getRequests() []datasetInfoRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]datasetInfoRequest{}, f.requests...)
}

func setupTest(t *testing.T) (*tview.Application, *DatasetInfoComponent, *fakeLoader) {
	fake := &fakeLoader{block: map[string]chan struct{}{}, fail: map[string]bool{}}
	testutil.Replace(t, &loadDatasetInfo, fake.load)

	app, _ := testutil.NewApp()
	datasetInfo := NewDatasetInfo(app)
	// set the root before the event loop starts, like CreateUi does
	testutil.StartApp(t, app, datasetInfo.GetLayout())
	return app, datasetInfo, fake
}

// displayed returns the title and text currently displayed, read on the UI thread.
func displayed(t *testing.T, app *tview.Application, datasetInfo *DatasetInfoComponent) (title string, text string) {
	testutil.OnUiThread(t, app, func() {
		title = datasetInfo.textView.GetTitle()
		text = datasetInfo.textView.GetText(true)
	})
	return title, text
}

func waitForDisplayed(t *testing.T, app *tview.Application, datasetInfo *DatasetInfoComponent, key string) {
	assert.Eventually(t, func() bool {
		title, text := displayed(t, app, datasetInfo)
		return strings.Contains(title, key) && strings.Contains(text, key)
	}, 2*time.Second, 10*time.Millisecond, "expected %q to be displayed", key)
}

func TestDatasetInfo_SetDatasetName(t *testing.T) {
	app, datasetInfo, fake := setupTest(t)

	testutil.OnUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/legacy", "") })
	waitForDisplayed(t, app, datasetInfo, "rpool/legacy")
	assert.Equal(t, []datasetInfoRequest{{name: "rpool/legacy"}}, fake.getRequests())

	// same dataset again does not reload
	testutil.OnUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/legacy", "") })
	// a changed mount path does
	testutil.OnUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/legacy", "/mnt/legacy") })
	assert.Eventually(t, func() bool { return len(fake.getRequests()) == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, datasetInfoRequest{path: "/mnt/legacy", name: "rpool/legacy", mountPath: "/mnt/legacy"}, fake.getRequests()[1])
}

func TestDatasetInfo_SetPathAfterNameLoadsByPath(t *testing.T) {
	app, datasetInfo, fake := setupTest(t)

	testutil.OnUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/legacy", "") })
	waitForDisplayed(t, app, datasetInfo, "rpool/legacy")

	testutil.OnUiThread(t, app, func() { datasetInfo.SetPath("/home/user") })
	waitForDisplayed(t, app, datasetInfo, "/home/user")
	assert.Equal(t, datasetInfoRequest{path: "/home/user"}, fake.getRequests()[1])

	// the name request is reset, so selecting the same dataset by name again loads it again
	testutil.OnUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/legacy", "") })
	waitForDisplayed(t, app, datasetInfo, "rpool/legacy")
	assert.Len(t, fake.getRequests(), 3)
}

func TestDatasetInfo_SetPathOfDisplayedDatasetDoesNotReload(t *testing.T) {
	app, datasetInfo, fake := setupTest(t)

	testutil.OnUiThread(t, app, func() { datasetInfo.SetPath("/home") })
	waitForDisplayed(t, app, datasetInfo, "/home")
	testutil.OnUiThread(t, app, func() { datasetInfo.SetPath("/home") })

	time.Sleep(50 * time.Millisecond)
	assert.Len(t, fake.getRequests(), 1)
}

func frontPage(t *testing.T, app *tview.Application, datasetInfo *DatasetInfoComponent) (name string) {
	testutil.OnUiThread(t, app, func() { name, _ = datasetInfo.container.GetFrontPage() })
	return name
}

func TestDatasetInfo_RefreshReloadsCurrentRequest(t *testing.T) {
	app, datasetInfo, fake := setupTest(t)

	testutil.OnUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/legacy", "") })
	waitForDisplayed(t, app, datasetInfo, "rpool/legacy")
	assert.Equal(t, uiutil.LoadingContainerContentPage, frontPage(t, app, datasetInfo))

	release := make(chan struct{})
	fake.mu.Lock()
	fake.block["rpool/legacy"] = release
	fake.mu.Unlock()

	testutil.OnUiThread(t, app, func() { datasetInfo.Refresh() })
	assert.Eventually(t, func() bool { return len(fake.getRequests()) == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, datasetInfoRequest{name: "rpool/legacy"}, fake.getRequests()[1])

	// the loading indicator is shown while refreshing
	assert.Equal(t, uiutil.LoadingContainerLoadingPage, frontPage(t, app, datasetInfo))

	close(release)
	assert.Eventually(t, func() bool {
		return frontPage(t, app, datasetInfo) == uiutil.LoadingContainerContentPage
	}, 2*time.Second, 10*time.Millisecond)
	waitForDisplayed(t, app, datasetInfo, "rpool/legacy")
}

func TestDatasetInfo_ErrorClearsDisplay(t *testing.T) {
	app, datasetInfo, fake := setupTest(t)
	fake.fail["rpool/broken"] = true

	testutil.OnUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/ok", "") })
	waitForDisplayed(t, app, datasetInfo, "rpool/ok")

	testutil.OnUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/broken", "") })
	assert.Eventually(t, func() bool {
		title, text := displayed(t, app, datasetInfo)
		return strings.TrimSpace(text) == "" && !strings.Contains(title, "rpool")
	}, 2*time.Second, 10*time.Millisecond)

	testutil.OnUiThread(t, app, func() {
		assert.Nil(t, datasetInfo.dataset)
	})
}

func TestDatasetInfo_StaleResultIsDiscarded(t *testing.T) {
	app, datasetInfo, fake := setupTest(t)
	release := make(chan struct{})
	fake.block["rpool/slow"] = release

	testutil.OnUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/slow", "") })
	assert.Eventually(t, func() bool { return len(fake.getRequests()) == 1 }, 2*time.Second, 10*time.Millisecond)

	testutil.OnUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/fast", "") })
	waitForDisplayed(t, app, datasetInfo, "rpool/fast")

	close(release)
	time.Sleep(100 * time.Millisecond)
	title, _ := displayed(t, app, datasetInfo)
	assert.Contains(t, title, "rpool/fast")
}

func TestFormatPermissions(t *testing.T) {
	granted := &zfs.DatasetPermissions{Grants: map[zfs.Permission][]zfs.Grant{
		zfs.PermissionHold:     {{}},
		zfs.PermissionSnapshot: {{}},
		"send":                 {{}}, // not shown, not used by zfs-file-history
	}}
	assert.Equal(t, "snapshot, hold", formatPermissions(granted, nil), "in display order")
	all := &zfs.DatasetPermissions{Grants: map[zfs.Permission][]zfs.Grant{}}
	for _, permission := range []zfs.Permission{zfs.PermissionSnapshot, zfs.PermissionDestroy, zfs.PermissionMount, zfs.PermissionHold, zfs.PermissionRelease} {
		all.Grants[permission] = []zfs.Grant{{}}
	}
	assert.Equal(t, "snapshot, destroy, mount, hold,\nrelease", formatPermissions(all, nil), "a few on each line")
	assert.Equal(t, "none", formatPermissions(&zfs.DatasetPermissions{}, nil))
	assert.Equal(t, "all (root)", formatPermissions(&zfs.DatasetPermissions{IsRoot: true}, nil))
	assert.Equal(t, "unknown", formatPermissions(nil, errors.New("zfs not found")))
}

func TestFormatPoolScan(t *testing.T) {
	tests := []struct {
		status   zfs.PoolStatus
		expected string
	}{
		{zfs.PoolStatus{Name: "rpool", State: zfs.PoolStateOnline, ScanProgress: -1,
			Scan: "scrub repaired 0B in 00:06:46 with 0 errors on Thu Oct  1 12:50:53 2026"},
			"scrub 2026-10-01"},
		{zfs.PoolStatus{Name: "tank", State: zfs.PoolStateDegraded, ScanProgress: 31.77,
			Scan: "resilver in progress since Tue Oct 13 09:12:44 2026\n254G resilvered, 31.77% done"},
			"resilver 31.8%"},
		{zfs.PoolStatus{Name: "tank", State: zfs.PoolStateOnline, ScanProgress: -1,
			Scan: "resilvered 254G in 02:10:31 with 0 errors on Tue Oct 13 11:23:15 2026"},
			"resilver 2026-10-13"},
		{zfs.PoolStatus{Name: "new", State: zfs.PoolStateOnline, ScanProgress: -1, Scan: "none requested"},
			"never scrubbed"},
		{zfs.PoolStatus{Name: "new", State: zfs.PoolStateOnline, ScanProgress: -1},
			"never scrubbed"},
		{zfs.PoolStatus{Name: "tank", State: zfs.PoolStateOnline, ScanProgress: -1, Scan: "scrub canceled on Tue Oct 13 10:00:01 2026"},
			"scrub canceled"},
		{zfs.PoolStatus{Name: "tank", State: zfs.PoolStateOnline, ScanProgress: -1, Scan: "scrub paused since Tue Oct 13 10:00:01 2026\nmore"},
			"scrub paused"},
		// other kinds of scans
		{zfs.PoolStatus{Name: "tank", State: zfs.PoolStateOnline, ScanProgress: 12.5, Scan: "error scrub in progress since Tue Oct 13 10:00:01 2026"},
			"scan 12.5%"},
		{zfs.PoolStatus{Name: "tank", State: zfs.PoolStateOnline, ScanProgress: -1, Scan: "error scrub repaired 0B in 00:00:01 with 0 errors on Tue Oct 13 10:00:01 2026"},
			"scan 2026-10-13"},
	}
	for _, test := range tests {
		t.Run(test.expected, func(t *testing.T) {
			assert.Equal(t, test.expected, formatPoolScan(&test.status))
		})
	}
}

const gib = 1 << 30

// homeFacts are the facts of a mounted, compressed and encrypted dataset with snapshots and a child.
func homeFacts() *datasetFacts {
	return &datasetFacts{
		kind: "filesystem", mountPoint: "/home", mounted: "yes", canMount: "on", readonly: "off",
		compression: "zstd", compressRatio: "1.90x", encrypted: true, encryption: "aes-256-gcm", keyStatus: "available",
		created: time.Now().AddDate(-5, 0, 0), used: 336 * gib, available: 258 * gib,
		usedByDataset: 196 * gib, usedBySnapshots: 18 * gib, usedByChildren: 121 * gib, logicalUsed: 585 * gib,
		permissions: "snapshot, mount", poolName: "rpool",
		pool: &zfs.PoolStatus{Name: "rpool", State: zfs.PoolStateOnline, ScanProgress: -1, Scan: "none requested"},
		// 200 GiB of other datasets
		poolUsed: 536 * gib, poolAvailable: 258 * gib, poolSpaceKnown: true,
	}
}

func infoLines(facts *datasetFacts) []string {
	return strings.Split(testutil.StripTags(formatInfo(facts)), "\n")
}

func TestFormatInfo(t *testing.T) {
	facts := homeFacts()
	assert.Equal(t, []string{
		"● mounted at /home",
		// data, snapshots, children, other datasets, free
		"█████▍██▌████▌       68% of 794 GiB · 258 GiB free",
		"",
		"Space",
		"Data            ■ 196 GiB",
		"Snapshots       ■ 18 GiB",
		"Children        ■ 121 GiB",
		"Other datasets  ■ 200 GiB",
		"Compression     zstd · saves 249 GiB (1.90×)",
		"",
		"Dataset",
		"Pool            rpool ONLINE · never scrubbed",
		"Created         5 years ago · " + facts.created.Format(time.DateOnly),
		"Encryption      aes-256-gcm · key loaded",
		"Permissions     snapshot, mount",
	}, infoLines(facts), "defaults that say nothing are left out (read-only, can mount)")
	// the verdict is green: all is well
	assert.True(t, strings.HasPrefix(formatInfo(facts), txwidgets.ColorTag(uiutil.InfoOk.Color())+"●"))
}

func TestFormatInfo_Verdict(t *testing.T) {
	tests := []struct {
		name    string
		change  func(facts *datasetFacts)
		verdict string
		state   uiutil.InfoState
	}{
		{"almost full pool", func(facts *datasetFacts) { facts.available, facts.poolAvailable = 90*gib, 90*gib },
			"● pool 86% full · mounted at /home", uiutil.InfoWarning},
		{"full pool", func(facts *datasetFacts) { facts.available, facts.poolAvailable = 20*gib, 20*gib },
			"● pool 96% full · mounted at /home", uiutil.InfoError},
		{"almost full quota", func(facts *datasetFacts) { facts.available = 30 * gib }, "● quota 92% full · mounted at /home", uiutil.InfoWarning},
		{"full, pool unknown", func(facts *datasetFacts) { facts.available, facts.poolSpaceKnown = 10*gib, false },
			"● 97% full · mounted at /home", uiutil.InfoError},
		{"degraded pool", func(facts *datasetFacts) { facts.pool.State = zfs.PoolStateDegraded }, "● pool DEGRADED · mounted at /home", uiutil.InfoWarning},
		{"key not loaded", func(facts *datasetFacts) { facts.keyStatus = "unavailable"; facts.mounted = "no" },
			"● key not loaded · not mounted", uiutil.InfoError},
		{"not mounted, but should be", func(facts *datasetFacts) { facts.mounted = "no" }, "● not mounted", uiutil.InfoWarning},
		{"not mounted on purpose", func(facts *datasetFacts) { facts.mounted = "no"; facts.canMount = "noauto" }, "● not mounted", uiutil.InfoOk},
		{"read-only clone", func(facts *datasetFacts) { facts.readonly = "on"; facts.origin = "rpool/home@daily" },
			"● mounted at /home · read-only · clone", uiutil.InfoOk},
		{"volume", func(facts *datasetFacts) { facts.kind = "volume"; facts.mounted = "-" }, "● volume", uiutil.InfoOk},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts := homeFacts()
			test.change(facts)
			assert.Equal(t, test.verdict, infoLines(facts)[0])
			assert.True(t, strings.HasPrefix(formatInfo(facts), txwidgets.ColorTag(test.state.Color())+"●"), "the dot in the color of the worst part")
		})
	}
}

func TestFormatInfo_Quota(t *testing.T) {
	facts := homeFacts()
	// less than the pool has: a quota of 400 GiB
	facts.available = 64 * gib
	lines := infoLines(facts)
	assert.Equal(t, "█████▍██▌████▌       68% of 794 GiB · 258 GiB free", lines[1], "the pool")
	assert.Equal(t, "████████████████▊    84% of 400 GiB quota · 64 GiB free", lines[2], "and the quota")

	facts.available = facts.poolAvailable + 10*gib
	assert.NotContains(t, formatInfo(facts), "quota", "more than the pool has: a reservation, not a quota")
}

func TestFormatInfo_PoolRoot(t *testing.T) {
	facts := homeFacts()
	facts.isPoolRoot = true
	facts.poolUsed = facts.used
	lines := infoLines(facts)
	assert.Equal(t, "██████▋▎███▍         57% of 594 GiB · 258 GiB free", lines[1])
	assert.NotContains(t, lines, "Other datasets  ■ 0 B", "it holds all of them")
	assert.Equal(t, "Data         ■ 196 GiB", lines[4])
}

func TestFormatInfo_PoolSpaceUnknown(t *testing.T) {
	facts := homeFacts()
	facts.poolSpaceKnown = false
	lines := infoLines(facts)
	assert.Equal(t, "██████▋▎███▍         57% of 594 GiB · 258 GiB free", lines[1], "of the dataset alone")
	assert.Equal(t, "Data         ■ 196 GiB", lines[4], "no other datasets")
}

func TestFormatInfo_PoolBarColors(t *testing.T) {
	facts := homeFacts()
	text := formatInfo(facts)
	colors := theme.Colors.Info
	for _, color := range []tcell.Color{colors.SpaceData, colors.SpaceSnapshots, colors.SpaceChildren, colors.SpaceOthers} {
		assert.Contains(t, text, txwidgets.Span(color, "■"), "a swatch for each part of the bar")
		// as the text or the background of a cell (a part smaller than a cell)
		assert.Contains(t, strings.Split(text, "\n")[1], strings.Trim(txwidgets.ColorTag(color), "[]"), "and the part")
	}
}

func TestFormatInfo_NotMounted(t *testing.T) {
	facts := homeFacts()
	facts.mounted, facts.canMount = "no", "noauto"
	lines := infoLines(facts)
	// why it is not mounted
	assert.Contains(t, lines, "Mount point     /home")
	assert.Contains(t, lines, "Can mount       noauto")
}

func TestFormatInfo_Unknown(t *testing.T) {
	facts := homeFacts()
	facts.pool = nil
	facts.compression = "off"
	facts.usedBySnapshots = 0
	facts.permissions = "unknown"
	lines := infoLines(facts)
	assert.Contains(t, lines, "Pool            rpool · unknown")
	assert.Contains(t, lines, "Compression     off")
	assert.Contains(t, lines, "Snapshots       ■ 0 B")
	assert.Contains(t, lines, "Permissions     unknown")
	// dimmed: they say little
	text := formatInfo(facts)
	assert.Contains(t, text, uiutil.InfoMuted("off"))
	assert.Contains(t, text, uiutil.InfoMuted("0 B"))
	assert.Contains(t, text, uiutil.InfoMuted("unknown"))
}

// The link to the pool is where its name is in the text: after the label, on the line of the field.
func TestFormatInfo_PoolLink(t *testing.T) {
	text, link := formatInfoWithPoolLink(homeFacts())

	lines := strings.Split(testutil.StripTags(text), "\n")
	require.Less(t, link.line, len(lines))
	line := []rune(lines[link.line])
	require.LessOrEqual(t, link.column+link.width, len(line))
	assert.Equal(t, "rpool", string(line[link.column:link.column+link.width]))
	assert.Equal(t, "rpool", link.pool)
}

// A click on the name of the pool shows the pool (on the pools page, see CreateUi), the rest of the text does not.
func TestDatasetInfo_ClickOnPoolShowsIt(t *testing.T) {
	app, screen := testutil.NewApp()
	datasetInfo := NewDatasetInfo(app)
	testutil.StartApp(t, app, datasetInfo.GetLayout())
	shown := make(chan string, 10)
	testutil.OnUiThread(t, app, func() {
		datasetInfo.SetOnShowPool(func(pool string) { shown <- pool })
		datasetInfo.updateUi(&datasetInfoResult{dataset: &zfs.Dataset{Path: "/home"}, title: "Dataset: rpool/home", facts: homeFacts()})
	})

	// where the name of the pool is drawn, behind its label
	poolX, poolY := -1, -1
	for y, line := range strings.Split(testutil.ScreenText(t, app, screen), "\n") {
		if strings.Contains(line, "Pool  ") {
			poolX, poolY = len([]rune(line[:strings.Index(line, "rpool")])), y
		}
	}
	require.GreaterOrEqual(t, poolY, 0, "the pool is shown")
	testutil.Click(t, app, screen, poolX-3, poolY)
	assert.Empty(t, shown, "the label is not the link")
	testutil.Click(t, app, screen, poolX+2, poolY)
	select {
	case pool := <-shown:
		assert.Equal(t, "rpool", pool)
	case <-time.After(2 * time.Second):
		require.Fail(t, "the pool was not shown")
	}
}
