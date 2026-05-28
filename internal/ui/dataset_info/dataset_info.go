package dataset_info

import (
	"fmt"
	"strings"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"

	"github.com/dustin/go-humanize"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type DatasetInfoComponent struct {
	application *tview.Application
	dataset     *zfs.Dataset
	layout      *tview.TextView
}

func NewDatasetInfo(application *tview.Application) *DatasetInfoComponent {
	datasetInfo := &DatasetInfoComponent{
		application: application,
	}

	datasetInfo.createLayout()

	return datasetInfo
}

func (datasetInfo *DatasetInfoComponent) SetPath(path string) {
	if path == "" {
		datasetInfo.SetDataset(nil)
		return
	}
	dataset, err := zfs.FindHostDataset(path)
	if err == nil {
		datasetInfo.SetDataset(dataset)
	} else {
		logging.Error("Could not find dataset for path %s: %s", path, err.Error())
		datasetInfo.SetDataset(nil)
	}
}

func (datasetInfo *DatasetInfoComponent) SetDataset(dataset *zfs.Dataset) {
	if datasetInfo.dataset == dataset || datasetInfo.dataset != nil && dataset != nil && datasetInfo.dataset.GetName() == dataset.GetName() {
		return
	}
	datasetInfo.dataset = dataset
	datasetInfo.updateUi()
}

type DatasetInfoTableEntry struct {
	Name  string
	Value string
}

func (datasetInfo *DatasetInfoComponent) createLayout() {
	layout := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false).
		SetScrollable(true)
	layout.SetBorder(true)
	uiutil.SetupWindow(layout, "Dataset")

	datasetInfo.layout = layout
	datasetInfo.updateUi()
}

func (datasetInfo *DatasetInfoComponent) updateUi() {
	dataset := datasetInfo.dataset

	titleText := "Dataset"
	if dataset == nil {
		datasetInfo.layout.Clear()
		uiutil.SetupWindow(datasetInfo.layout, titleText)
		return
	}

	titleText = fmt.Sprintf("%s: %s", titleText, dataset.Path)
	uiutil.SetupWindow(datasetInfo.layout, titleText)

	properties := []*DatasetInfoTableEntry{
		{Name: "Type", Value: dataset.GetType()},
		{Name: "Name", Value: dataset.GetName()},
		{Name: "Mountpoint", Value: dataset.GetMountPoint()},
		{Name: "Mounted", Value: dataset.GetMounted()},
		{Name: "Volsize", Value: humanize.IBytes(dataset.GetVolSize())},
		{Name: "Avail", Value: humanize.IBytes(dataset.GetAvailable())},
		{Name: "Used", Value: humanize.IBytes(dataset.GetUsed())},
		{Name: "Compression", Value: dataset.GetCompression()},
	}

	if !util.IsBlank(dataset.GetOrigin()) {
		properties = append(properties, &DatasetInfoTableEntry{
			Name: "Origin", Value: dataset.GetOrigin(),
		})
	}

	if dataset.GetSnapshotLimit() > 0 {
		properties = append(properties, &DatasetInfoTableEntry{
			Name: "Snapshot Limit", Value: fmt.Sprintf("%d/%d", dataset.GetSnapshotCount(), dataset.GetSnapshotLimit()),
		})
	}

	datasetInfo.layout.Clear()

	// Calculate alignment padding dynamically based on longest key name
	maxKeyLen := 0
	for _, entry := range properties {
		if len(entry.Name) > maxKeyLen {
			maxKeyLen = len(entry.Name)
		}
	}

	keyColorTag := colorTag(theme.Colors.Layout.Table.Header)
	var out strings.Builder

	for _, entry := range properties {
		valueColor := resolveValueColor(entry.Name, entry.Value)
		valueColorTag := colorTag(valueColor)

		// Format key with trailing colon, maintaining clean alignment padding
		labelText := fmt.Sprintf("%s:", entry.Name)

		out.WriteString(fmt.Sprintf("%s%*s[-] %s%s[-]\n",
			keyColorTag,
			maxKeyLen+1, // +1 maps to the colon addition
			tview.Escape(labelText),
			valueColorTag,
			tview.Escape(entry.Value),
		))
	}

	datasetInfo.layout.SetText(out.String())
}

func (datasetInfo *DatasetInfoComponent) HasFocus() bool {
	return datasetInfo.layout.HasFocus()
}

func (datasetInfo *DatasetInfoComponent) Focus() {
	datasetInfo.application.SetFocus(datasetInfo.layout)
}

func (datasetInfo *DatasetInfoComponent) GetLayout() tview.Primitive {
	return datasetInfo.layout
}

func (datasetInfo *DatasetInfoComponent) CreateSnapshot(name string) error {
	if datasetInfo.dataset == nil {
		return fmt.Errorf("no dataset for current file selection")
	}
	return datasetInfo.dataset.CreateSnapshot(name)
}

func (datasetInfo *DatasetInfoComponent) GetShortcutMap() []shortcut_helper.ShortcutEntry {
	return []shortcut_helper.ShortcutEntry{}
}

// Helper formatting utilities matching ConfigInfo Component patterns

func colorTag(color tcell.Color) string {
	r, g, b := color.RGB()
	return fmt.Sprintf("[#%02x%02x%02x]", uint8(r), uint8(g), uint8(b))
}

func resolveValueColor(name, value string) tcell.Color {
	if value == "" || value == "-" || strings.EqualFold(value, "none") {
		return tcell.ColorGray
	}

	// Paths / Mountpoints
	if strings.HasPrefix(value, "/") || strings.EqualFold(name, "mountpoint") || strings.EqualFold(name, "origin") {
		return tcell.ColorLightBlue
	}

	// Booleans (yes/no)
	if strings.EqualFold(value, "yes") || strings.EqualFold(value, "true") {
		return tcell.ColorGreen
	}
	if strings.EqualFold(value, "no") || strings.EqualFold(value, "false") {
		return tcell.ColorRed
	}

	// File Sizes (Volsize, Avail, Used)
	lowerName := strings.ToLower(name)
	if lowerName == "volsize" || lowerName == "avail" || lowerName == "used" {
		return tcell.ColorYellow
	}

	// Fallback Default String Color
	return tcell.ColorWhite
}
