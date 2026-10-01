package dataset_info

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
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
		dataset:    &zfs.Dataset{Path: request.path},
		title:      "Dataset: " + key,
		properties: []*DatasetInfoTableEntry{{Name: "Key", Value: key}},
	}, nil
}

func (f *fakeLoader) getRequests() []datasetInfoRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]datasetInfoRequest{}, f.requests...)
}

func setupTest(t *testing.T) (*tview.Application, *DatasetInfoComponent, *fakeLoader) {
	fake := &fakeLoader{block: map[string]chan struct{}{}, fail: map[string]bool{}}
	original := loadDatasetInfo
	loadDatasetInfo = fake.load
	t.Cleanup(func() { loadDatasetInfo = original })

	app := tview.NewApplication()
	app.SetScreen(tcell.NewSimulationScreen("UTF-8"))
	datasetInfo := NewDatasetInfo(app)
	// set the root before the event loop starts, like CreateUi does
	app.SetRoot(datasetInfo.GetLayout(), true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)
	return app, datasetInfo, fake
}

// onUiThread runs f on the UI thread and waits for it, failing the test instead of hanging on a deadlock.
func onUiThread(t *testing.T, app *tview.Application, f func()) {
	done := make(chan struct{})
	go func() {
		app.QueueUpdate(f)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the UI thread (deadlock?)")
	}
}

// displayed returns the title and text currently displayed, read on the UI thread.
func displayed(t *testing.T, app *tview.Application, datasetInfo *DatasetInfoComponent) (title string, text string) {
	onUiThread(t, app, func() {
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

	onUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/legacy", "") })
	waitForDisplayed(t, app, datasetInfo, "rpool/legacy")
	assert.Equal(t, []datasetInfoRequest{{name: "rpool/legacy"}}, fake.getRequests())

	// same dataset again does not reload
	onUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/legacy", "") })
	// a changed mount path does
	onUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/legacy", "/mnt/legacy") })
	assert.Eventually(t, func() bool { return len(fake.getRequests()) == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, datasetInfoRequest{path: "/mnt/legacy", name: "rpool/legacy", mountPath: "/mnt/legacy"}, fake.getRequests()[1])
}

func TestDatasetInfo_SetPathAfterNameLoadsByPath(t *testing.T) {
	app, datasetInfo, fake := setupTest(t)

	onUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/legacy", "") })
	waitForDisplayed(t, app, datasetInfo, "rpool/legacy")

	onUiThread(t, app, func() { datasetInfo.SetPath("/home/user") })
	waitForDisplayed(t, app, datasetInfo, "/home/user")
	assert.Equal(t, datasetInfoRequest{path: "/home/user"}, fake.getRequests()[1])

	// the name request is reset, so selecting the same dataset by name again loads it again
	onUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/legacy", "") })
	waitForDisplayed(t, app, datasetInfo, "rpool/legacy")
	assert.Len(t, fake.getRequests(), 3)
}

func TestDatasetInfo_SetPathOfDisplayedDatasetDoesNotReload(t *testing.T) {
	app, datasetInfo, fake := setupTest(t)

	onUiThread(t, app, func() { datasetInfo.SetPath("/home") })
	waitForDisplayed(t, app, datasetInfo, "/home")
	onUiThread(t, app, func() { datasetInfo.SetPath("/home") })

	time.Sleep(50 * time.Millisecond)
	assert.Len(t, fake.getRequests(), 1)
}

func frontPage(t *testing.T, app *tview.Application, datasetInfo *DatasetInfoComponent) (name string) {
	onUiThread(t, app, func() { name, _ = datasetInfo.container.GetFrontPage() })
	return name
}

func TestDatasetInfo_RefreshReloadsCurrentRequest(t *testing.T) {
	app, datasetInfo, fake := setupTest(t)

	onUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/legacy", "") })
	waitForDisplayed(t, app, datasetInfo, "rpool/legacy")
	assert.Equal(t, uiutil.LoadingContainerContentPage, frontPage(t, app, datasetInfo))

	release := make(chan struct{})
	fake.mu.Lock()
	fake.block["rpool/legacy"] = release
	fake.mu.Unlock()

	onUiThread(t, app, func() { datasetInfo.Refresh() })
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

	onUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/ok", "") })
	waitForDisplayed(t, app, datasetInfo, "rpool/ok")

	onUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/broken", "") })
	assert.Eventually(t, func() bool {
		title, text := displayed(t, app, datasetInfo)
		return strings.TrimSpace(text) == "" && !strings.Contains(title, "rpool")
	}, 2*time.Second, 10*time.Millisecond)

	onUiThread(t, app, func() {
		assert.Nil(t, datasetInfo.dataset)
		// creating a snapshot must not target the previously displayed dataset
		assert.Error(t, datasetInfo.CreateSnapshot("test"))
	})
}

func TestDatasetInfo_StaleResultIsDiscarded(t *testing.T) {
	app, datasetInfo, fake := setupTest(t)
	release := make(chan struct{})
	fake.block["rpool/slow"] = release

	onUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/slow", "") })
	assert.Eventually(t, func() bool { return len(fake.getRequests()) == 1 }, 2*time.Second, 10*time.Millisecond)

	onUiThread(t, app, func() { datasetInfo.SetDatasetName("rpool/fast", "") })
	waitForDisplayed(t, app, datasetInfo, "rpool/fast")

	close(release)
	time.Sleep(100 * time.Millisecond)
	title, _ := displayed(t, app, datasetInfo)
	assert.Contains(t, title, "rpool/fast")
}

func TestFormatProperties(t *testing.T) {
	text := formatProperties([]*DatasetInfoTableEntry{
		{Name: "Mounted", Value: "no"},
		{Name: "Mount Point", Value: "/with[brackets]"},
	})
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	assert.Len(t, lines, 2)
	// keys are right-aligned to the longest key
	assert.Contains(t, lines[0], "    Mounted:")
	// values are escaped
	assert.Contains(t, lines[1], tview.Escape("/with[brackets]"))
}
