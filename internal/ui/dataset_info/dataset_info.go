package dataset_info

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/dustin/go-humanize"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type DatasetInfoComponent struct {
	application *tview.Application
	textView    *tview.TextView
	container   *uiutil.LoadingContainer
	loader      *uiutil.DataLoader[*datasetInfoResult]

	// All fields below are only accessed on the UI thread.

	// path is the requested path, used if name is empty.
	path string
	// name and mountPath identify the requested dataset, if it was requested by name (see SetDatasetName).
	name      string
	mountPath string

	// dataset is the currently displayed dataset. It is only used on the UI thread after it was loaded.
	dataset *zfs.Dataset
}

// datasetInfoResult is the result of a background load.
// All values are fetched in the background, so displaying them never calls into ZFS on the UI thread.
type datasetInfoResult struct {
	dataset *zfs.Dataset
	title   string
	facts   *datasetFacts
}

func NewDatasetInfo(application *tview.Application) *DatasetInfoComponent {
	datasetInfo := &DatasetInfoComponent{
		application: application,
	}

	datasetInfo.textView = tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false).
		SetScrollable(true)
	datasetInfo.textView.SetBorder(true)
	datasetInfo.textView.SetBorderPadding(0, 0, 1, 1)
	uiutil.SetupWindow(datasetInfo.textView, "Dataset")

	datasetInfo.container = uiutil.NewLoadingContainer(application, datasetInfo.textView, "Dataset", "Loading dataset info...")

	datasetInfo.loader = uiutil.NewDataLoader[*datasetInfoResult](application).
		OnStart(func() {
			datasetInfo.container.SetIsLoading(true)
		}).
		OnLoad(func(result *datasetInfoResult) {
			datasetInfo.dataset = result.dataset
			datasetInfo.container.SetIsLoading(false)
			datasetInfo.updateUi(result)
		}).
		OnError(func(err error) {
			datasetInfo.container.SetIsLoading(false)
			// Handle error if needed, for now just clear
			datasetInfo.dataset = nil
			datasetInfo.updateUi(nil)
		})

	return datasetInfo
}

// SetPath shows the dataset containing the given path.
func (datasetInfo *DatasetInfoComponent) SetPath(path string) {
	datasetInfo.path = path
	datasetInfo.name = ""
	datasetInfo.mountPath = ""
	if datasetInfo.dataset != nil && datasetInfo.dataset.Path == path {
		return
	}

	datasetInfo.load(datasetInfo.dataset != nil)
}

// SetDatasetName shows the dataset with the given name, which does not need to be mounted.
// mountPath is the path the dataset is mounted at, or "" if it is not mounted.
func (datasetInfo *DatasetInfoComponent) SetDatasetName(name string, mountPath string) {
	if name == datasetInfo.name && mountPath == datasetInfo.mountPath {
		return
	}
	datasetInfo.path = mountPath
	datasetInfo.name = name
	datasetInfo.mountPath = mountPath

	datasetInfo.load(datasetInfo.dataset != nil)
}

// Refresh reloads the currently requested dataset, showing the loading indicator while loading.
func (datasetInfo *DatasetInfoComponent) Refresh() {
	datasetInfo.load(false)
}

// RefreshQuietly reloads the currently requested dataset, showing the old values while loading (e.g. after its
// permissions changed).
func (datasetInfo *DatasetInfoComponent) RefreshQuietly() {
	datasetInfo.load(true)
}

// load starts loading the currently requested dataset in the background.
// Must be called on the UI thread.
func (datasetInfo *DatasetInfoComponent) load(quietly bool) {
	// capture the request on the UI thread, the load function runs in the background
	request := datasetInfoRequest{
		path:      datasetInfo.path,
		name:      datasetInfo.name,
		mountPath: datasetInfo.mountPath,
	}

	loadFunc := func(ctx context.Context) (*datasetInfoResult, error) {
		return loadDatasetInfo(request)
	}

	if quietly {
		datasetInfo.loader.LoadQuietly(loadFunc)
	} else {
		datasetInfo.loader.Load(loadFunc)
	}
}

type datasetInfoRequest struct {
	path      string
	name      string
	mountPath string
}

// loadDatasetInfo opens the requested dataset and reads its properties.
// It runs in the background and is replaceable in tests.
var loadDatasetInfo = func(request datasetInfoRequest) (*datasetInfoResult, error) {
	if request.name != "" {
		dataset, err := zfs.OpenDatasetByName(request.name, request.mountPath)
		if err != nil {
			return nil, err
		}
		return newDatasetInfoResult(dataset, request.name), nil
	}

	dataset, err := zfs.FindHostDataset(request.path)
	if err != nil {
		return nil, err
	}
	return newDatasetInfoResult(dataset, dataset.Path), nil
}

// datasetFacts are the values the dataset info shows, all read in the background (see newDatasetInfoResult), so
// formatting them (formatInfo) never calls into ZFS.
type datasetFacts struct {
	kind, mountPoint, mounted, canMount, readonly string
	compression, compressRatio                    string
	encrypted                                     bool
	encryption, keyStatus, origin                 string
	created                                       time.Time
	used, available, volSize                      uint64
	usedByDataset, usedBySnapshots                uint64
	usedByChildren, logicalUsed                   uint64
	snapshotCount, snapshotLimit                  int
	// permissions are the ZFS permissions of the user, see formatPermissions
	permissions string
	// poolName is the name of the pool, pool its status (nil if it could not be loaded)
	poolName string
	pool     *zfs.PoolStatus
}

// loadDatasetPermissions and loadPoolStatus are replaceable in tests.
var (
	loadDatasetPermissions = zfs.LoadDatasetPermissions
	loadPoolStatus         = zfs.GetPoolStatus
)

// newDatasetInfoResult reads all displayed values of the dataset.
// This calls into ZFS (and may spawn processes), so it must only be called in the background.
func newDatasetInfoResult(dataset *zfs.Dataset, titleName string) *datasetInfoResult {
	facts := &datasetFacts{
		kind: dataset.GetType(), mountPoint: dataset.GetMountPoint(), mounted: dataset.GetMounted(),
		canMount: dataset.GetCanMount(), readonly: dataset.GetReadonly(),
		compression: dataset.GetCompression(), compressRatio: dataset.GetCompressRatio(),
		encrypted: dataset.IsEncrypted(), encryption: dataset.GetEncryption(), keyStatus: dataset.GetKeyStatus(),
		origin: dataset.GetOrigin(), created: dataset.GetCreationString(),
		used: dataset.GetUsed(), available: dataset.GetAvailable(), volSize: dataset.GetVolSize(),
		usedByDataset: dataset.GetUsedByDataset(), usedBySnapshots: dataset.GetUsedBySnapshots(),
		usedByChildren: dataset.GetUsedByChildren(), logicalUsed: dataset.GetLogicalUsed(),
		snapshotCount: dataset.GetSnapshotCount(), snapshotLimit: dataset.GetSnapshotLimit(),
		permissions: formatPermissions(loadDatasetPermissions(dataset.GetName())),
	}
	facts.poolName, _, _ = strings.Cut(dataset.GetName(), "/")
	if facts.poolName != "" {
		if status, err := loadPoolStatus(facts.poolName); err == nil {
			facts.pool = status
		}
	}
	return &datasetInfoResult{
		dataset: dataset,
		title:   fmt.Sprintf("Dataset: %s", titleName),
		facts:   facts,
	}
}

// permissionsPerLine is the number of permissions on a line of the dataset info, see formatPermissions.
const permissionsPerLine = 4

// formatPermissions lists the ZFS permissions of the current user, e.g. "snapshot, destroy, mount".
func formatPermissions(permissions *zfs.DatasetPermissions, err error) string {
	switch {
	case err != nil || permissions == nil:
		return "unknown"
	case permissions.IsRoot:
		return "all (root)"
	}
	granted := permissions.KnownGranted()
	if len(granted) == 0 {
		return "none"
	}
	// a few on each line, the pane is narrow
	var lines []string
	for chunk := range slices.Chunk(granted, permissionsPerLine) {
		names := make([]string, 0, len(chunk))
		for _, permission := range chunk {
			names = append(names, string(permission))
		}
		lines = append(lines, strings.Join(names, ", "))
	}
	return strings.Join(lines, ",\n")
}

// formatPoolScan returns the last or running scrub (or resilver) of the pool, short enough for the width of the
// dataset info: the date it finished or its progress, e.g. "scrub 2026-10-01" or "resilver 31.8%".
func formatPoolScan(status *zfs.PoolStatus) string {
	kind := status.ScanKind()
	if kind == "" {
		// e.g. an error scrub
		kind = "scan"
	}
	finished, isFinished := status.ScanFinished()
	switch {
	case status.IsScanning():
		return fmt.Sprintf("%s %.1f%%", kind, status.ScanProgress)
	case isFinished:
		return fmt.Sprintf("%s %s", kind, finished.Format(time.DateOnly))
	case status.Scan == "" || status.Scan == "none requested":
		return "never scrubbed"
	default:
		// e.g. "scrub canceled on Tue Oct 13 10:00:01 2026" or "scrub paused since ...", without the date
		scan, _, _ := strings.Cut(status.Scan, "\n")
		scan, _, _ = strings.Cut(scan, " on ")
		scan, _, _ = strings.Cut(scan, " since ")
		return scan
	}
}

const (
	// fullWarning and fullError are how full a dataset is (used of used and available) from which it is a warning or
	// an error: ZFS slows down when a pool gets full
	fullWarning = 0.85
	fullError   = 0.95
	// spaceBarWidth is the width of the bar of the used space
	spaceBarWidth = 20
)

// poolState returns the state of the pool for the verdict: fine while it is online.
func poolState(state string) uiutil.InfoState {
	switch state {
	case zfs.PoolStateOnline:
		return uiutil.InfoOk
	case zfs.PoolStateDegraded:
		return uiutil.InfoWarning
	default:
		return uiutil.InfoError
	}
}

// formatInfo returns the text of the dataset info: whether all is well (see verdict), how full it is, what uses the
// space, then what else is known about it. Defaults that say nothing (e.g. not read-only) are left out.
func formatInfo(facts *datasetFacts) string {
	var info uiutil.InfoText
	info.Verdict(verdict(facts)...)
	if total := facts.used + facts.available; total > 0 {
		fraction := float64(facts.used) / float64(total)
		info.Line(uiutil.InfoBar(fraction, spaceBarWidth) + " " + uiutil.InfoValue(fmt.Sprintf("%.0f%%", fraction*100)) +
			uiutil.InfoMuted(fmt.Sprintf(" of %s · %s free", uiutil.HumanizedBytes(total), uiutil.HumanizedBytes(facts.available))))
	}

	info.Section("Space")
	if facts.kind == "volume" {
		info.Field("Size", uiutil.InfoValue(uiutil.HumanizedBytes(facts.volSize)))
	}
	info.Field("Data", formatSize(facts.usedByDataset))
	snapshots := formatSize(facts.usedBySnapshots)
	if facts.snapshotLimit > 0 {
		snapshots += uiutil.InfoMuted(fmt.Sprintf(" · %d of %d snapshots", facts.snapshotCount, facts.snapshotLimit))
	}
	info.Field("Snapshots", snapshots)
	if facts.usedByChildren > 0 {
		info.Field("Children", formatSize(facts.usedByChildren))
	}
	info.Field("Compression", formatCompression(facts))

	info.Section("Dataset")
	if facts.poolName != "" {
		pool := uiutil.InfoValue(facts.poolName) + uiutil.InfoMuted(" · unknown")
		if facts.pool != nil {
			pool = uiutil.InfoValue(facts.poolName) + " " + txwidgets.Span(poolState(facts.pool.State).Color(), "%s", facts.pool.State) +
				uiutil.InfoMuted(" · "+formatPoolScan(facts.pool))
		}
		info.Field("Pool", pool)
	}
	if !facts.created.IsZero() {
		info.Field("Created", uiutil.InfoValue(humanize.Time(facts.created))+uiutil.InfoMuted(" · "+facts.created.Format(time.DateOnly)))
	}
	if facts.mounted != "yes" && facts.kind != "volume" {
		info.Field("Mount point", uiutil.InfoValue(facts.mountPoint))
		info.Field("Can mount", uiutil.InfoValue(facts.canMount))
	}
	if facts.origin != "" {
		info.Field("Clone of", uiutil.InfoValue(facts.origin))
	}
	if facts.encrypted {
		key := txwidgets.Span(uiutil.InfoOk.Color(), "key loaded")
		if facts.keyStatus != "available" {
			key = txwidgets.Span(uiutil.InfoError.Color(), "key not loaded")
		}
		info.Field("Encryption", uiutil.InfoValue(facts.encryption)+uiutil.InfoMuted(" · ")+key)
	}
	permissions := uiutil.InfoValue(facts.permissions)
	if facts.permissions == "unknown" || facts.permissions == "none" {
		permissions = uiutil.InfoMuted(facts.permissions)
	}
	info.Field("Permissions", permissions)
	return info.String()
}

// verdict returns the parts of the verdict line: the problems (the pool, a key that is not loaded, how full it is),
// then how it is used (mounted where, a volume, read-only, a clone).
func verdict(facts *datasetFacts) []uiutil.InfoPart {
	var parts []uiutil.InfoPart
	if facts.pool != nil && facts.pool.State != zfs.PoolStateOnline {
		parts = append(parts, uiutil.InfoPart{Text: "pool " + facts.pool.State, State: poolState(facts.pool.State)})
	}
	if facts.encrypted && facts.keyStatus != "available" {
		parts = append(parts, uiutil.InfoPart{Text: "key not loaded", State: uiutil.InfoError})
	}
	if total := facts.used + facts.available; total > 0 {
		fraction := float64(facts.used) / float64(total)
		switch {
		case fraction >= fullError:
			parts = append(parts, uiutil.InfoPart{Text: fmt.Sprintf("%.0f%% full", fraction*100), State: uiutil.InfoError})
		case fraction >= fullWarning:
			parts = append(parts, uiutil.InfoPart{Text: fmt.Sprintf("%.0f%% full", fraction*100), State: uiutil.InfoWarning})
		}
	}
	switch {
	case facts.kind == "volume":
		parts = append(parts, uiutil.InfoPart{Text: "volume"})
	case facts.mounted == "yes":
		parts = append(parts, uiutil.InfoPart{Text: "mounted at " + facts.mountPoint})
	case facts.canMount == "on" && facts.mountPoint != "none" && facts.mountPoint != "legacy":
		// it should be
		parts = append(parts, uiutil.InfoPart{Text: "not mounted", State: uiutil.InfoWarning})
	default:
		parts = append(parts, uiutil.InfoPart{Text: "not mounted"})
	}
	if facts.readonly == "on" {
		parts = append(parts, uiutil.InfoPart{Text: "read-only"})
	}
	if facts.origin != "" {
		parts = append(parts, uiutil.InfoPart{Text: "clone"})
	}
	return parts
}

// formatSize returns the size, dimmed if it is 0 (e.g. a dataset without snapshots).
func formatSize(size uint64) string {
	if size == 0 {
		return uiutil.InfoMuted(uiutil.HumanizedBytes(size))
	}
	return uiutil.InfoValue(uiutil.HumanizedBytes(size))
}

// formatCompression returns the compression of the dataset and how much it saves, e.g. "lz4 · saves 380 GiB (1.45×)".
func formatCompression(facts *datasetFacts) string {
	if facts.compression == "" || facts.compression == "off" {
		return uiutil.InfoMuted("off")
	}
	text := uiutil.InfoValue(facts.compression)
	if facts.logicalUsed > facts.used {
		text += uiutil.InfoMuted(" · saves ") + uiutil.InfoValue(uiutil.HumanizedBytes(facts.logicalUsed-facts.used)) +
			uiutil.InfoMuted(" ("+strings.TrimSuffix(facts.compressRatio, "x")+"×)")
	}
	return text
}

// updateUi displays the given result, or clears the view if it is nil.
// It only uses the pre-fetched values of the result and must not call into ZFS.
func (datasetInfo *DatasetInfoComponent) updateUi(result *datasetInfoResult) {
	if result == nil {
		datasetInfo.textView.Clear()
		uiutil.SetupWindow(datasetInfo.textView, "Dataset")
		return
	}

	uiutil.SetupWindow(datasetInfo.textView, result.title)
	datasetInfo.textView.SetText(formatInfo(result.facts))
}

func (datasetInfo *DatasetInfoComponent) HasFocus() bool {
	return datasetInfo.container.HasFocus()
}

func (datasetInfo *DatasetInfoComponent) Focus() {
	datasetInfo.application.SetFocus(datasetInfo.container)
}

func (datasetInfo *DatasetInfoComponent) GetLayout() *uiutil.LoadingContainer {
	return datasetInfo.container
}

func (datasetInfo *DatasetInfoComponent) SetBorderColor(color tcell.Color) {
	datasetInfo.textView.SetBorderColor(color)
	if datasetInfo.container != nil {
		datasetInfo.container.SetBorderColor(color)
	}
}

// ContentSize returns the size needed to show all of the text, including the border and its padding. Must be called
// on the UI thread.
func (datasetInfo *DatasetInfoComponent) ContentSize() (width int, height int) {
	width, height = uiutil.TextViewContentSize(datasetInfo.textView)
	if width == 0 {
		return 0, 0
	}
	// the padding, see NewDatasetInfo
	return width + 2, height
}
