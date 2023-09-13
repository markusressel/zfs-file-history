package ui

import (
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"zfs-file-history/internal/logging"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"
)

type DatasetInfo struct {
	application *tview.Application
	dataset     *zfs.Dataset
	layout      *tview.Table
}

func NewDatasetInfo(application *tview.Application) *DatasetInfo {
	datasetInfo := &DatasetInfo{
		application: application,
	}

	datasetInfo.createLayout()

	return datasetInfo
}

func (datasetInfo *DatasetInfo) SetPath(path string) {
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

func (datasetInfo *DatasetInfo) SetDataset(dataset *zfs.Dataset) {
	datasetInfo.dataset = dataset
	datasetInfo.updateUi()
}

type DatasetInfoTableEntry struct {
	Name  string
	Value string
}

func (datasetInfo *DatasetInfo) createLayout() {
	layout := tview.NewTable()
	layout.SetBorder(true)
	uiutil.SetupWindowTitle(layout, "Dataset")

	datasetInfo.layout = layout
	datasetInfo.updateUi()
}

func (datasetInfo *DatasetInfo) updateUi() {
	dataset := datasetInfo.dataset

	titleText := "Dataset"
	if dataset == nil {
		datasetInfo.layout.Clear()
		uiutil.SetupWindowTitle(datasetInfo.layout, titleText)
		return
	}

	titleText = fmt.Sprintf("%s: %s", titleText, dataset.Path)
	uiutil.SetupWindowTitle(datasetInfo.layout, titleText)

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

func (datasetInfo *DatasetInfo) HasFocus() bool {
	return datasetInfo.layout.HasFocus()
}

func (datasetInfo *DatasetInfo) Focus() {
	datasetInfo.application.SetFocus(datasetInfo.layout)
}
