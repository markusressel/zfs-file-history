package dialog

import (
	"testing"
	"time"
	"zfs-file-history/internal/data"

	"github.com/stretchr/testify/assert"
)

func TestSnapshotActionDialog_SelectCloseOption_EmitsCloseAction(t *testing.T) {
	d := &SnapshotActionDialog{actionChannel: make(chan DialogActionId, 1)}

	d.selectAction(&DialogOption{Id: DialogCloseActionId, Name: "Close"})

	assertDialogActionEmitted(t, d.actionChannel, DialogCloseActionId)
}

func TestDeleteFileDialog_SelectCancelOption_EmitsCloseAction(t *testing.T) {
	d := &DeleteFileDialog{actionChannel: make(chan DialogActionId, 1)}

	d.selectAction(&DialogOption{Id: DialogCloseActionId, Name: "Cancel"})

	assertDialogActionEmitted(t, d.actionChannel, DialogCloseActionId)
}

func TestDeleteSnapshotDialog_SelectCancelOption_EmitsCloseAction(t *testing.T) {
	d := &DeleteSnapshotDialog{actionChannel: make(chan DialogActionId, 1)}

	d.selectAction(&DialogOption{Id: DialogCloseActionId, Name: "Cancel"})

	assertDialogActionEmitted(t, d.actionChannel, DialogCloseActionId)
}

func TestMultiSnapshotActionDialog_SelectCloseOption_EmitsCloseAction(t *testing.T) {
	d := &MultiSnapshotActionDialog{actionChannel: make(chan DialogActionId, 1)}

	d.selectAction(&DialogOption{Id: DialogCloseActionId, Name: "Close"})

	assertDialogActionEmitted(t, d.actionChannel, DialogCloseActionId)
}

func TestSnapshotActionDialog_SelectCloseOption_DoesNotEmitOtherAction(t *testing.T) {
	d := &SnapshotActionDialog{
		actionChannel: make(chan DialogActionId, 2),
		snapshot:      &data.SnapshotBrowserEntry{},
	}

	d.selectAction(&DialogOption{Id: DialogCloseActionId, Name: "Close"})

	assertDialogActionEmitted(t, d.actionChannel, DialogCloseActionId)
	assertNoMoreDialogActions(t, d.actionChannel)
}

func assertDialogActionEmitted(t *testing.T, ch <-chan DialogActionId, expected DialogActionId) {
	t.Helper()
	select {
	case action := <-ch:
		assert.Equal(t, expected, action)
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("expected action %v to be emitted", expected)
	}
}

func assertNoMoreDialogActions(t *testing.T, ch <-chan DialogActionId) {
	t.Helper()
	select {
	case action := <-ch:
		t.Fatalf("did not expect extra dialog action %v", action)
	case <-time.After(30 * time.Millisecond):
		// expected no more actions
	}
}
