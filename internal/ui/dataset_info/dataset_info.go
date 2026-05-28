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
		{Name: "Creation", Value: dataset.GetCreationString().Format(theme.Style.Format.DateTime)},
		{Name: "Mountpoint", Value: dataset.GetMountPoint()},
		{Name: "Mounted", Value: dataset.GetMounted()},
		{Name: "Readonly", Value: dataset.GetReadonly()}, // "on" or "off"
		{Name: "Volsize", Value: humanize.IBytes(dataset.GetVolSize())},
		{Name: "Avail", Value: humanize.IBytes(dataset.GetAvailable())},
		{Name: "Used", Value: humanize.IBytes(dataset.GetUsed())},
		{Name: "Compression", Value: fmt.Sprintf("%s (%s)", dataset.GetCompression(), dataset.GetCompressRatio())}, // Combine for compact view
		{Name: "Snapdir", Value: dataset.GetSnapdir()},                                                             // "visible" or "hidden"
		{Name: "Case", Value: dataset.GetCaseSensitivity()},                                                        // "sensitive" / "insensitive"
	}

	// If encryption is utilized on the host pool
	if dataset.IsEncrypted() {
		properties = append(properties, &DatasetInfoTableEntry{
			Name: "Encryption", Value: fmt.Sprintf("%s [%s]", dataset.GetEncryption(), dataset.GetKeyStatus()),
		})
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

	lowerName := strings.ToLower(name)
	lowerValue := strings.ToLower(value)

	// Warning / Restrictive States
	if lowerName == "readonly" && lowerValue == "on" {
		return tcell.ColorOrange // Visual cue that writes/restores are blocked
	}
	if strings.Contains(lowerValue, "unavailable") {
		return tcell.ColorRed // Key is missing/locked
	}

	// Paths / Mountpoints
	if strings.HasPrefix(value, "/") || lowerName == "mountpoint" || lowerName == "origin" {
		return tcell.ColorLightBlue
	}

	// Booleans / Positive Flags
	if lowerValue == "yes" || lowerValue == "true" || lowerValue == "on" || lowerValue == "visible" {
		return tcell.ColorGreen
	}
	if lowerValue == "no" || lowerValue == "false" || lowerValue == "off" || lowerValue == "hidden" {
		return tcell.ColorRed
	}

	// File Sizes & Ratios
	if lowerName == "volsize" || lowerName == "avail" || lowerName == "used" || strings.Contains(lowerValue, "x") {
		return tcell.ColorYellow
	}

	// Fallback Default String Color
	return tcell.ColorWhite
}
