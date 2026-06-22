package dialog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSelectionDialog_SelectCloseOption_EmitsCloseAction(t *testing.T) {
	d := &SelectionDialog{actionChannel: make(chan DialogActionId, 1)}

	d.selectAction(&DialogOption{Id: DialogCloseActionId, Name: "Close"})

	assertDialogActionEmitted(t, d.actionChannel, DialogCloseActionId)
}

func TestSelectionDialog_SelectOtherOption_EmitsCloseAndAction(t *testing.T) {
	d := &SelectionDialog{actionChannel: make(chan DialogActionId, 2)}

	d.selectAction(&DialogOption{Id: DialogActionId(42), Name: "Action"})

	assertDialogActionEmitted(t, d.actionChannel, DialogCloseActionId)
	assertDialogActionEmitted(t, d.actionChannel, DialogActionId(42))
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
