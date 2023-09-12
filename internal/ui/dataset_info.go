package ui

import (
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"zfs-file-history/internal/zfs"
)

type DatasetInfo struct {
	application *tview.Application
	dataset     *zfs.Dataset
	layout      *tview.Table
}

func NewDatasetInfo(application *tview.Application, dataset *zfs.Dataset) *DatasetInfo {
	datasetInfo := &DatasetInfo{
		application: application,
		dataset:     dataset,
	}

	datasetInfo.createLayout()

	return datasetInfo
}

func (datasetInfo *DatasetInfo) SetDataset(dataset *zfs.Dataset) {
	datasetInfo.dataset = dataset
}

type DatasetInfoTableEntry struct {
	Name  string
	Value string
}

func (datasetInfo *DatasetInfo) createLayout() {
	layout := tview.NewTable()
	layout.SetBorder(true)
	layout.SetTitle(" Dataset ")

	datasetInfo.layout = layout
	datasetInfo.updateUi()
}

func (datasetInfo *DatasetInfo) updateUi() {
	dataset := datasetInfo.dataset

	if dataset == nil {
		datasetInfo.layout.Clear()
		return
	}

	properties := []*DatasetInfoTableEntry{
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
