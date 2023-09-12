package ui

import (
	"github.com/rivo/tview"
	"zfs-file-history/internal/zfs"
)

type DatasetInfo struct {
	application         *tview.Application
	dataset             *zfs.Dataset
	layout              *tview.Flex
	datasetPathTextView *tview.TextView
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

func (datasetInfo *DatasetInfo) createLayout() *tview.Flex {
	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	layout.SetBorder(true)
	layout.SetTitle(" Dataset ")

	datasetInfo.layout = layout

	datasetPath := tview.NewTextView()
	datasetInfo.datasetPathTextView = datasetPath
	datasetInfo.updateUi()

	layout.AddItem(datasetPath, 0, 1, false)

	return layout
}

func (datasetInfo *DatasetInfo) updateUi() {
	dataset := datasetInfo.dataset

	var datasetPathText = ""
	if dataset != nil {
		datasetPathText = dataset.Path
	}
	datasetInfo.datasetPathTextView.SetText(datasetPathText)
}
