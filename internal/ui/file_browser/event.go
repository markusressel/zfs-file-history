package file_browser

import "github.com/rivo/tview"

type Event interface {
	isFileBrowserEvent()
}

type PathChangedEvent struct {
	NewPath string
}

func (pathChangedEvent PathChangedEvent) isFileBrowserEvent() {}

type CreateSnapshotEvent struct {
	SnapshotName string
}

func (CreateSnapshotEvent) isFileBrowserEvent() {}

type RequestFocusEvent struct {
	Layout tview.Primitive
}

func (RequestFocusEvent) isFileBrowserEvent() {}
