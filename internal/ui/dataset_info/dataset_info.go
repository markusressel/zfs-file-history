package dataset_info

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"zfs-file-history/internal/ui/clickable"
	"zfs-file-history/internal/ui/scrollbar"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/dustin/go-humanize"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type DatasetInfoComponent struct {
	application *tview.Application
	textView    *scrollbar.TextView
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
	// shown is the result displayed, nil: none
	shown *datasetInfoResult
	// renderedWidth is the width the text was wrapped at (0: not wrapped), see render
	renderedWidth int
	// contentWidth is the width of the widest line of the text, not wrapped, see ContentSize
	contentWidth int
	// poolLink is where the name of the pool is shown, which can be clicked (see infoView)
	poolLink poolLink
	// regions are the parts of the text that can be clicked: the name of the pool
	regions clickable.Regions
	// onShowPool shows a pool on the pools page (nil: the name of the pool cannot be clicked), see SetOnShowPool
	onShowPool func(pool string)
	// enabled returns whether the name of the pool can be clicked, e.g. not while a dialog is in front
	enabled func() bool
}

// infoView is the text view of the dataset info, which also draws the parts that can be clicked.
type infoView struct {
	*scrollbar.TextView
	datasetInfo *DatasetInfoComponent
}

// SetOnShowPool sets the function that shows a pool on the pools page, called when the name of the pool is clicked.
// Must be called on the UI thread.
func (datasetInfo *DatasetInfoComponent) SetOnShowPool(onShowPool func(pool string)) {
	datasetInfo.onShowPool = onShowPool
}

// SetEnabledFunc sets a function that returns whether the name of the pool can be clicked, e.g. not while a dialog is
// in front. Must be called on the UI thread.
func (datasetInfo *DatasetInfoComponent) SetEnabledFunc(enabled func() bool) {
	datasetInfo.enabled = enabled
}

// Draw draws the text, wrapped again if the width changed, and adds the name of the pool, which can be clicked, where
// it is shown.
func (view *infoView) Draw(screen tcell.Screen) {
	datasetInfo := view.datasetInfo
	if _, _, width, _ := view.GetInnerRect(); datasetInfo.shown != nil && width != datasetInfo.renderedWidth {
		datasetInfo.render(width)
	}
	view.TextView.Draw(screen)

	datasetInfo.regions.Reset()
	defer datasetInfo.regions.DrawHover(screen)
	link := datasetInfo.poolLink
	if link.width == 0 || datasetInfo.onShowPool == nil {
		return
	}
	x, y, width, height := view.GetInnerRect()
	row, column := view.GetScrollOffset()
	linkY := y + link.line - row
	if linkY < y || linkY >= y+height {
		return
	}
	start := max(x+link.column-column, x)
	end := min(x+link.column-column+link.width, x+width)
	onShowPool := datasetInfo.onShowPool
	datasetInfo.regions.Add(clickable.Region{
		X: start, Y: linkY, Width: end - start, Key: link.pool,
		HoverBackground: theme.Colors.Pool.HoverBackground,
		OnClick:         func() { onShowPool(link.pool) },
	})
}

// MouseHandler shows the pool when its name is clicked (see clickable.Regions), and otherwise scrolls and focuses like
// a text view.
func (view *infoView) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	textViewHandler := view.TextView.MouseHandler()
	return func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
		datasetInfo := view.datasetInfo
		consumed, captures := datasetInfo.regions.HandleMouse(action, event, datasetInfo.enabled() && view.InRect(event.Position()))
		if !consumed {
			consumed, capture = textViewHandler(action, event, setFocus)
		}
		if capture == nil && captures {
			// to learn when the mouse leaves
			capture = view
		}
		return consumed, capture
	}
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
		enabled:     func() bool { return true },
	}

	// capped by the page on small terminals, see ContentSize
	datasetInfo.textView = scrollbar.NewTextView(application)
	datasetInfo.textView.SetDynamicColors(true).
		SetWrap(false).
		SetScrollable(true)
	datasetInfo.textView.SetBorder(true)
	datasetInfo.textView.SetBorderPadding(0, 0, 1, 1)
	uiutil.SetupWindow(datasetInfo.textView, "Dataset")

	datasetInfo.container = uiutil.NewLoadingContainer(application, &infoView{TextView: datasetInfo.textView, datasetInfo: datasetInfo},
		"Dataset", "Loading dataset info...")

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
	// isPoolRoot is set for the root dataset of the pool, which holds all others
	isPoolRoot bool
	// poolUsed and poolAvailable are the used and available space of the root dataset of the pool, for the space of the
	// whole pool, if poolSpaceKnown
	poolUsed, poolAvailable uint64
	poolSpaceKnown          bool
	// poolOffset is the space before the dataset in the bar of the pool (see datasetOffset), if offsetKnown
	poolOffset  uint64
	offsetKnown bool
}

// loadDatasetPermissions, loadPoolStatus and loadDatasetsAlong are replaceable in tests.
var (
	loadDatasetPermissions = zfs.LoadDatasetPermissions
	loadPoolStatus         = zfs.GetPoolStatus
	loadDatasetsAlong      = zfs.ListDatasetsAlong
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
	facts.isPoolRoot = facts.poolName == dataset.GetName()
	if facts.poolName != "" {
		if status, err := loadPoolStatus(facts.poolName); err == nil {
			facts.pool = status
		}
	}
	if facts.poolName != "" {
		readPoolSpace(facts, dataset.GetName())
	}
	return &datasetInfoResult{
		dataset: dataset,
		title:   fmt.Sprintf("Dataset: %s", titleName),
		facts:   facts,
	}
}

// readPoolSpace reads the space of the pool and the place of the dataset in it, for the bar of the pool (see
// formatPoolBar). This runs zfs list, so it must only be called in the background.
func readPoolSpace(facts *datasetFacts, name string) {
	if facts.isPoolRoot {
		facts.poolUsed, facts.poolAvailable, facts.poolSpaceKnown = facts.used, facts.available, true
		facts.poolOffset, facts.offsetKnown = 0, true
		return
	}
	entries, err := loadDatasetsAlong(name)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.Name == facts.poolName {
			facts.poolUsed, facts.poolAvailable, facts.poolSpaceKnown = entry.Used, entry.Available, true
		}
	}
	facts.poolOffset, facts.offsetKnown = datasetOffset(entries, name)
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
	// fullWarning and fullError are how full a pool (or a quota) is from which it is a warning or an error: ZFS slows
	// down when a pool gets full, and nothing can be written once it (or the quota) is
	fullWarning = 0.85
	fullError   = 0.95
	// spaceBarWidth is the width of the bars of the used space
	spaceBarWidth = 20
)

// poolSpace returns the used and the total space of the pool of the dataset. If the root dataset of the pool could
// not be read, of the dataset alone (used and available), without the other datasets.
func poolSpace(facts *datasetFacts) (used uint64, total uint64) {
	if facts.poolSpaceKnown {
		return facts.poolUsed, facts.poolUsed + facts.poolAvailable
	}
	return facts.used, facts.used + facts.available
}

// quotaSpace returns the used and the total space of the dataset if a quota (of it or a parent) leaves it less than
// the pool has: then its available space is what is left of the quota. false if there is none, or it is not known.
func quotaSpace(facts *datasetFacts) (used uint64, total uint64, limited bool) {
	if !facts.poolSpaceKnown || facts.isPoolRoot || facts.available >= facts.poolAvailable {
		return 0, 0, false
	}
	return facts.used, facts.used + facts.available, true
}

// fraction returns used of total, 0 if total is.
func fraction(used uint64, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total)
}

// fullState returns whether a pool or quota filled to the fraction is a warning or an error.
func fullState(fraction float64) uiutil.InfoState {
	switch {
	case fraction >= fullError:
		return uiutil.InfoError
	case fraction >= fullWarning:
		return uiutil.InfoWarning
	default:
		return uiutil.InfoNone
	}
}

// formatPercent returns the fraction in percent, in the color of how full it is (see fullState).
func formatPercent(fraction float64) string {
	return txwidgets.Span(fullState(fraction).Color(), "%.0f%%", fraction*100)
}

// formatPoolBar returns the bar of the pool: what the dataset uses (its data, snapshots and children, in shades of
// one color) at its place among the other datasets of the pool (see datasetOffset), and what is free. The dataset
// alone would hide how full the pool is, e.g. a dataset of a few MiB on a pool that is almost full.
func formatPoolBar(facts *datasetFacts) string {
	used, total := poolSpace(facts)
	if total == 0 {
		return ""
	}
	colors := theme.Colors.Info
	own := min(facts.used, used)
	snapshots := min(facts.usedBySnapshots, own)
	children := min(facts.usedByChildren, own-snapshots)
	// the rest, also what a refreservation keeps
	data := own - snapshots - children
	// the other datasets before it in the tree (see datasetOffset), so it is shown where it lies within its parents
	before := uint64(0)
	if facts.poolSpaceKnown && facts.offsetKnown {
		before = min(facts.poolOffset, used-own)
	}
	segments := []uiutil.BarSegment{
		{Fraction: fraction(before, total), Color: colors.SpaceOthers},
		{Fraction: fraction(data, total), Color: colors.SpaceData},
		{Fraction: fraction(snapshots, total), Color: colors.SpaceSnapshots},
		{Fraction: fraction(children, total), Color: colors.SpaceChildren},
		{Fraction: fraction(used-own-before, total), Color: colors.SpaceOthers},
	}
	full := fraction(used, total)
	return uiutil.InfoStackedBar(segments, spaceBarWidth) + " " + formatPercent(full) +
		uiutil.InfoMuted(fmt.Sprintf(" of %s · %s free", uiutil.HumanizedBytes(total), uiutil.HumanizedBytes(total-used)))
}

// formatQuotaBar returns the bar of the quota that limits the dataset, "" if there is none (see quotaSpace).
func formatQuotaBar(facts *datasetFacts) string {
	used, total, limited := quotaSpace(facts)
	if !limited || total == 0 {
		return ""
	}
	full := fraction(used, total)
	return uiutil.InfoBar(full, spaceBarWidth) + " " + formatPercent(full) +
		uiutil.InfoMuted(fmt.Sprintf(" of %s quota · %s free", uiutil.HumanizedBytes(total), uiutil.HumanizedBytes(total-used)))
}

// swatch returns a square in the color, which tells the part of the bar a value is.
func swatch(color tcell.Color) string {
	return txwidgets.Span(color, "■") + " "
}

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
// space, then what else is known about it. Defaults that say nothing (e.g. not read-only) are left out. Lines wider
// than width are wrapped (see uiutil.InfoText.WrappedLines), 0: none.
func formatInfo(facts *datasetFacts, width int) string {
	text, _ := formatInfoWithPoolLink(facts, width)
	return text
}

// poolLink is where the name of the pool is in the text of the dataset info (line and column, without scrolling), which
// shows the pool on the pools page when clicked. Its width is 0 if there is none.
type poolLink struct {
	pool         string
	line, column int
	width        int
}

// formatInfoWithPoolLink returns the text of formatInfo, and where the name of the pool is in it.
func formatInfoWithPoolLink(facts *datasetFacts, width int) (string, poolLink) {
	var info uiutil.InfoText
	var link poolLink
	poolField := -1
	info.Verdict(verdict(facts)...)
	if bar := formatPoolBar(facts); bar != "" {
		info.Line(bar)
	}
	if bar := formatQuotaBar(facts); bar != "" {
		info.Line(bar)
	}

	colors := theme.Colors.Info
	info.Section("Space")
	if facts.kind == "volume" {
		info.Field("Size", uiutil.InfoValue(uiutil.HumanizedBytes(facts.volSize)))
	}
	info.Field("Data", swatch(colors.SpaceData)+formatSize(facts.usedByDataset))
	snapshots := swatch(colors.SpaceSnapshots) + formatSize(facts.usedBySnapshots)
	if facts.snapshotLimit > 0 {
		snapshots += uiutil.InfoMuted(fmt.Sprintf(" · %d of %d snapshots", facts.snapshotCount, facts.snapshotLimit))
	}
	info.Field("Snapshots", snapshots)
	if facts.usedByChildren > 0 {
		info.Field("Children", swatch(colors.SpaceChildren)+formatSize(facts.usedByChildren))
	}
	if facts.poolSpaceKnown && !facts.isPoolRoot {
		info.Field("Other datasets", swatch(colors.SpaceOthers)+formatSize(facts.poolUsed-min(facts.used, facts.poolUsed)))
	}
	info.Field("Compression", formatCompression(facts))

	info.Section("Dataset")
	if facts.poolName != "" {
		pool := uiutil.InfoValue(facts.poolName) + uiutil.InfoMuted(" · unknown")
		if facts.pool != nil {
			pool = uiutil.InfoValue(facts.poolName) + " " + txwidgets.Span(poolState(facts.pool.State).Color(), "%s", facts.pool.State) +
				uiutil.InfoMuted(" · "+formatPoolScan(facts.pool))
		}
		poolField = info.LineCount()
		link = poolLink{pool: facts.poolName, width: tview.TaggedStringWidth(tview.Escape(facts.poolName))}
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
	lines, positions := info.Wrap(width)
	if poolField >= 0 {
		// a wrapped line above moves it down, a narrow pane below its label
		link.line, link.column = positions[poolField].Line, positions[poolField].Column
		// the name is not wrapped: it comes first, and is short
		if width > 0 {
			link.width = min(link.width, width-link.column)
		}
	}
	return strings.Join(lines, "\n"), link
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
	if used, total := poolSpace(facts); total > 0 {
		if full := fraction(used, total); fullState(full) != uiutil.InfoNone {
			text := fmt.Sprintf("%.0f%% full", full*100)
			if facts.poolSpaceKnown {
				text = "pool " + text
			}
			parts = append(parts, uiutil.InfoPart{Text: text, State: fullState(full)})
		}
	}
	if used, total, limited := quotaSpace(facts); limited && total > 0 {
		if full := fraction(used, total); fullState(full) != uiutil.InfoNone {
			parts = append(parts, uiutil.InfoPart{Text: fmt.Sprintf("quota %.0f%% full", full*100), State: fullState(full)})
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
		datasetInfo.shown = nil
		datasetInfo.textView.Clear()
		uiutil.SetupWindow(datasetInfo.textView, "Dataset")
		datasetInfo.poolLink = poolLink{}
		return
	}

	datasetInfo.shown = result
	uiutil.SetupWindow(datasetInfo.textView, result.title)
	unwrapped, _ := formatInfoWithPoolLink(result.facts, 0)
	datasetInfo.contentWidth, _ = uiutil.TextSize(unwrapped)
	datasetInfo.render(datasetInfo.renderedWidth)
}

// render shows the text of the shown dataset, its lines wrapped at width, 0: not wrapped (before the view was drawn).
// The page sizes the pane by its content (see ContentSize), but only up to a share of the screen: the lines that do
// not fit then are wrapped instead of cut off. Must be called on the UI thread.
func (datasetInfo *DatasetInfoComponent) render(width int) {
	datasetInfo.renderedWidth = width
	text, link := formatInfoWithPoolLink(datasetInfo.shown.facts, width)
	datasetInfo.textView.SetText(text)
	datasetInfo.poolLink = link
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

// ContentSize returns the size needed to show all of the text, including the border and its padding: the width of
// the text not wrapped, so the page gives it all the width it needs, and the height of the text as wrapped at the
// width it got. Must be called on the UI thread.
func (datasetInfo *DatasetInfoComponent) ContentSize() (width int, height int) {
	_, height = uiutil.TextViewContentSize(datasetInfo.textView.TextView)
	if height == 0 {
		return 0, 0
	}
	// and the border
	width = datasetInfo.contentWidth + 2
	// the padding, see NewDatasetInfo, and the scrollbar while not all lines fit
	return width + 2 + datasetInfo.textView.BarWidth(), height
}
