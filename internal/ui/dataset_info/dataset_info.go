package dataset_info

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

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
	dataset    *zfs.Dataset
	title      string
	properties []*DatasetInfoTableEntry
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

type DatasetInfoTableEntry struct {
	Name  string
	Value string
}

// loadDatasetPermissions is replaceable in tests.
var loadDatasetPermissions = zfs.LoadDatasetPermissions

// newDatasetInfoResult reads all displayed properties of the dataset.
// This calls into ZFS (and may spawn processes), so it must only be called in the background.
func newDatasetInfoResult(dataset *zfs.Dataset, titleName string) *datasetInfoResult {
	properties := collectProperties(dataset)
	properties = append(properties, &DatasetInfoTableEntry{Name: "Permissions", Value: formatPermissions(loadDatasetPermissions(dataset.GetName()))})
	sortProperties(properties)
	return &datasetInfoResult{
		dataset:    dataset,
		title:      fmt.Sprintf("Dataset: %s", titleName),
		properties: properties,
	}
}

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
	names := make([]string, 0, len(granted))
	for _, permission := range granted {
		names = append(names, string(permission))
	}
	return strings.Join(names, ", ")
}

func collectProperties(dataset *zfs.Dataset) []*DatasetInfoTableEntry {
	mounted := dataset.GetMounted()
	properties := []*DatasetInfoTableEntry{
		{Name: "Type", Value: dataset.GetType()},
		{Name: "Creation", Value: dataset.GetCreationString().Format(theme.Style.Format.DateTime)},
		{Name: "Mount Point", Value: dataset.GetMountPoint()},
		{Name: "Mounted", Value: mounted},
		{Name: "Readonly", Value: dataset.GetReadonly()},
		{Name: "Compression", Value: dataset.GetCompression()},
		{Name: "Compress Ratio", Value: dataset.GetCompressRatio()},
		{Name: "Available", Value: uiutil.StableLengthHumanizedBytes(dataset.GetAvailable())},
		{Name: "Used", Value: uiutil.StableLengthHumanizedBytes(dataset.GetUsed())},
	}

	if mounted != "yes" {
		// usually explains why the dataset is not mounted
		properties = append(properties, &DatasetInfoTableEntry{Name: "Can Mount", Value: dataset.GetCanMount()})
	}

	if dataset.GetType() == "volume" {
		properties = append(properties, &DatasetInfoTableEntry{Name: "Vol Size", Value: uiutil.StableLengthHumanizedBytes(dataset.GetVolSize())})
	}

	if dataset.IsEncrypted() {
		properties = append(properties, []*DatasetInfoTableEntry{
			{Name: "Encryption", Value: dataset.GetEncryption()},
			{Name: "Key Status", Value: dataset.GetKeyStatus()},
		}...)
	}

	if origin := dataset.GetOrigin(); origin != "" {
		properties = append(properties, &DatasetInfoTableEntry{Name: "Origin", Value: origin})
	}

	if snapshotLimit := dataset.GetSnapshotLimit(); snapshotLimit > 0 {
		properties = append(properties, &DatasetInfoTableEntry{
			Name:  "Snapshots",
			Value: fmt.Sprintf("%d / %d", dataset.GetSnapshotCount(), snapshotLimit),
		})
	}

	sortProperties(properties)
	return properties
}

// sortProperties sorts the properties by name, for a consistent display.
func sortProperties(properties []*DatasetInfoTableEntry) {
	sort.Slice(properties, func(i, j int) bool {
		return properties[i].Name < properties[j].Name
	})
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
	datasetInfo.textView.SetText(formatProperties(result.properties))
}

func formatProperties(properties []*DatasetInfoTableEntry) string {
	// Calculate alignment padding dynamically based on longest key name
	maxKeyLen := 0
	for _, prop := range properties {
		if len(prop.Name) > maxKeyLen {
			maxKeyLen = len(prop.Name)
		}
	}

	keyColorTag := txwidgets.ColorTag(theme.Colors.Layout.Table.Accent)
	var out strings.Builder
	for _, prop := range properties {
		valueColor := resolveValueColor(prop.Name, prop.Value)
		valueColorTag := txwidgets.ColorTag(valueColor)

		out.WriteString(fmt.Sprintf(" %s%*s:[-]  %s%s[-]\n",
			keyColorTag,
			maxKeyLen,
			tview.Escape(prop.Name),
			valueColorTag,
			tview.Escape(prop.Value),
		))
	}
	return out.String()
}

func resolveValueColor(name, value string) tcell.Color {
	if value == "" || value == "-" || strings.EqualFold(value, "none") {
		return tcell.ColorGray
	}

	lowerName := strings.ToLower(name)
	lowerValue := strings.ToLower(value)

	if lowerName == "permissions" {
		if lowerValue == "unknown" {
			return theme.Colors.Permissions.Unknown
		}
		return theme.Colors.Permissions.Granted
	}

	// Warning / Restrictive States
	if lowerName == "readonly" && lowerValue == "on" {
		return tcell.ColorOrange // Visual cue that writes/restores are blocked
	}
	if strings.Contains(lowerValue, "unavailable") {
		return tcell.ColorRed // Key is missing/locked
	}

	// Paths / Mountpoints
	if strings.HasPrefix(value, "/") || lowerName == "mount point" || lowerName == "origin" {
		return tcell.ColorLightBlue
	}

	// Booleans / Positive Flags
	if lowerValue == "yes" || lowerValue == "true" || lowerValue == "on" || lowerValue == "visible" || lowerValue == "mounted" || (lowerName == "compression" && lowerValue != "off") {
		return tcell.ColorGreen
	}
	if lowerValue == "no" || lowerValue == "false" || lowerValue == "off" || lowerValue == "hidden" {
		return tcell.ColorRed
	}

	// File Sizes & Ratios
	if lowerName == "vol size" || lowerName == "available" || lowerName == "used" || lowerName == "snapshots" || strings.Contains(lowerValue, "x") {
		return tcell.ColorYellow
	}

	// Fallback Default String Color
	return tcell.ColorWhite
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
