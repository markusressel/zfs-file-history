package dataset_info

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
)

type DatasetInfoComponent struct {
	application *tview.Application
	dataset     *zfs.Dataset
	textView    *tview.TextView
	container   *uiutil.LoadingContainer
	loader      *uiutil.DataLoader[*zfs.Dataset]
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

	datasetInfo.loader = uiutil.NewDataLoader[*zfs.Dataset](application).
		OnStart(func() {
			datasetInfo.container.SetIsLoading(true)
		}).
		OnLoad(func(ds *zfs.Dataset) {
			datasetInfo.dataset = ds
			datasetInfo.container.SetIsLoading(false)
			datasetInfo.updateUi()
		}).
		OnError(func(err error) {
			datasetInfo.container.SetIsLoading(false)
			// Handle error if needed, for now just clear
			datasetInfo.dataset = nil
			datasetInfo.updateUi()
		})

	return datasetInfo
}

func (datasetInfo *DatasetInfoComponent) SetPath(path string) {
	if datasetInfo.dataset != nil && datasetInfo.dataset.Path == path {
		return
	}

	loadFunc := func(ctx context.Context) (*zfs.Dataset, error) {
		return zfs.FindHostDataset(path)
	}

	if datasetInfo.dataset != nil {
		datasetInfo.loader.LoadQuietly(loadFunc)
	} else {
		datasetInfo.loader.Load(loadFunc)
	}
}

func (datasetInfo *DatasetInfoComponent) SetDataset(dataset *zfs.Dataset) {
	datasetInfo.dataset = dataset
	datasetInfo.container.SetIsLoading(false)
	datasetInfo.updateUi()
}

type DatasetInfoTableEntry struct {
	Name  string
	Value string
}

func (datasetInfo *DatasetInfoComponent) updateUi() {
	dataset := datasetInfo.dataset

	titleText := "Dataset"
	if dataset == nil {
		datasetInfo.textView.Clear()
		uiutil.SetupWindow(datasetInfo.textView, titleText)
		return
	}

	titleText = fmt.Sprintf("%s: %s", titleText, dataset.Path)
	uiutil.SetupWindow(datasetInfo.textView, titleText)

	properties := []*DatasetInfoTableEntry{
		{Name: "Type", Value: dataset.GetType()},
		{Name: "Creation", Value: dataset.GetCreationString().Format(theme.Style.Format.DateTime)},
		{Name: "Mount Point", Value: dataset.GetMountPoint()},
		{Name: "Mounted", Value: dataset.GetMounted()},
		{Name: "Readonly", Value: dataset.GetReadonly()},
		{Name: "Compression", Value: dataset.GetCompression()},
		{Name: "Compress Ratio", Value: dataset.GetCompressRatio()},
		{Name: "Available", Value: uiutil.StableLengthHumanizedBytes(dataset.GetAvailable())},
		{Name: "Used", Value: uiutil.StableLengthHumanizedBytes(dataset.GetUsed())},
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

	if dataset.GetOrigin() != "" {
		properties = append(properties, &DatasetInfoTableEntry{Name: "Origin", Value: dataset.GetOrigin()})
	}

	if dataset.GetSnapshotLimit() > 0 {
		properties = append(properties, &DatasetInfoTableEntry{
			Name:  "Snapshots",
			Value: fmt.Sprintf("%d / %d", dataset.GetSnapshotCount(), dataset.GetSnapshotLimit()),
		})
	}

	datasetInfo.textView.Clear()

	// Calculate alignment padding dynamically based on longest key name
	maxKeyLen := 0
	for _, prop := range properties {
		if len(prop.Name) > maxKeyLen {
			maxKeyLen = len(prop.Name)
		}
	}

	// Sort properties by Name for consistent display
	sort.Slice(properties, func(i, j int) bool {
		return properties[i].Name < properties[j].Name
	})

	var out strings.Builder
	for _, prop := range properties {
		out.WriteString(fmt.Sprintf(" [gray]%*s:[-]  %s\n",
			maxKeyLen,
			prop.Name,
			prop.Value,
		))
	}

	datasetInfo.textView.SetText(out.String())
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

func (datasetInfo *DatasetInfoComponent) CreateSnapshot(name string) error {
	if datasetInfo.dataset == nil {
		return fmt.Errorf("no dataset selected")
	}
	return datasetInfo.dataset.CreateSnapshot(name)
}
