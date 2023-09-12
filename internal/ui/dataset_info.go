package ui

import (
	"fmt"
	"github.com/dustin/go-humanize"
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
			Name:  "Name",
			Value: dataset.Properties.Name,
		},
		{
			Name:  "Type",
			Value: dataset.Properties.Type,
		},
		{
			Name:  "Mountpoint",
			Value: dataset.Properties.Mountpoint,
		},
		{
			Name:  "Compression",
			Value: dataset.Properties.Compression,
		},
		{
			Name:  "Avail",
			Value: humanize.IBytes(dataset.Properties.Avail),
		},
		{
			Name:  "Used",
			Value: humanize.IBytes(dataset.Properties.Used),
		},
		{
			Name:  "Origin",
			Value: dataset.Properties.Origin,
		},
		{
			Name:  "Volsize",
			Value: humanize.IBytes(dataset.Properties.Volsize),
		},
	}

	datasetInfo.layout.Clear()
	columns, rows := 2, len(properties)
	for row := 0; row < rows; row++ {
		entry := properties[row]

		for col := 0; col < columns; col++ {
			var text string
			var cellAlignment int
			if col == 0 {
				text = fmt.Sprintf("%s:", entry.Name)
				cellAlignment = tview.AlignRight
			} else {
				text = entry.Value
				cellAlignment = tview.AlignLeft
			}
			datasetInfo.layout.SetCell(
				row, col,
				tview.NewTableCell(text).SetAlign(cellAlignment),
			)
		}
	}
}
