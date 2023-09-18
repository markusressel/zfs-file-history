package dataset_info

import (
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"zfs-file-history/internal/logging"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"
)

type DatasetInfoComponent struct {
	application *tview.Application
	dataset     *zfs.Dataset
	layout      *tview.Table
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
		logging.Error(err.Error())
		datasetInfo.SetDataset(nil)
	}
}

func (datasetInfo *DatasetInfoComponent) SetDataset(dataset *zfs.Dataset) {
	if datasetInfo.dataset == dataset || datasetInfo.dataset != nil && dataset != nil && datasetInfo.dataset.ZfsData.Name == dataset.ZfsData.Name {
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
	layout := tview.NewTable()
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

	properties := []*DatasetInfoTableEntry{}
	if dataset.ZfsData != nil {
		properties = []*DatasetInfoTableEntry{
			{
				Name:  "Type",
				Value: dataset.ZfsData.Type,
			},
			{
				Name:  "Name",
				Value: dataset.ZfsData.Name,
			},
			{
				Name:  "Mountpoint",
				Value: dataset.ZfsData.Mountpoint,
			},
			{
				Name:  "Volsize",
				Value: humanize.IBytes(dataset.ZfsData.Volsize),
			},
			{
				Name:  "Avail",
				Value: humanize.IBytes(dataset.ZfsData.Avail),
			},
			{
				Name:  "Used",
				Value: humanize.IBytes(dataset.ZfsData.Used),
			},
			{
				Name:  "Compression",
				Value: dataset.ZfsData.Compression,
			},
			{
				Name:  "Origin",
				Value: dataset.ZfsData.Origin,
			},
		}
	}

	datasetInfo.layout.Clear()
	columns, rows := 2, len(properties)
	for row := 0; row < rows; row++ {
		entry := properties[row]

		for col := 0; col < columns; col++ {
			var text string
			var cellAlignment int
			var cellColor = tcell.ColorWhite
			if col == 0 {
				text = fmt.Sprintf("%s:", entry.Name)
				cellAlignment = tview.AlignRight
				cellColor = tcell.ColorSteelBlue
			} else {
				text = entry.Value
				cellAlignment = tview.AlignLeft
			}
			datasetInfo.layout.SetCell(
				row, col,
				tview.NewTableCell(text).SetAlign(cellAlignment).SetTextColor(cellColor),
			)
		}
	}
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
