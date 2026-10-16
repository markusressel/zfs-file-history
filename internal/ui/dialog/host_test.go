package dialog

import (
	"testing"

	"zfs-file-history/internal/testutil"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestHost_ShowDialog(t *testing.T) {
	app, _ := testutil.NewApp()
	own := tview.NewPages().AddPage("own", tview.NewBox(), true, true)
	application := tview.NewPages().AddPage("application", tview.NewBox(), true, true)
	testutil.StartApp(t, app, tview.NewFlex().AddItem(own, 0, 1, false).AddItem(application, 0, 1, false))

	var withoutPages, withOwnPages Host
	testutil.OnUiThread(t, app, func() {
		withoutPages = NewHost(app, nil)
		withOwnPages = NewHost(app, own)
	})
	pageCounts := func() (ownCount, applicationCount int) {
		testutil.OnUiThread(t, app, func() { ownCount, applicationCount = own.GetPageCount(), application.GetPageCount() })
		return ownCount, applicationCount
	}
	show := func(host *Host) {
		testutil.OnUiThread(t, app, func() { host.ShowDialog(NewErrorDialog(app, "Error", assert.AnError), nil) })
	}

	// without pages: nothing, no panic
	show(&withoutPages)
	ownCount, applicationCount := pageCounts()
	assert.Equal(t, 1, ownCount)
	assert.Equal(t, 1, applicationCount)

	// on the own pages until the page sets its pages
	show(&withOwnPages)
	ownCount, applicationCount = pageCounts()
	assert.Equal(t, 2, ownCount)
	assert.Equal(t, 1, applicationCount)

	// then on the pages of the application (dialogs of the same kind share a page, so it is replaced there)
	testutil.OnUiThread(t, app, func() {
		withoutPages.SetDialogPages(application)
		withOwnPages.SetDialogPages(application)
	})
	show(&withoutPages)
	ownCount, applicationCount = pageCounts()
	assert.Equal(t, 2, ownCount)
	assert.Equal(t, 2, applicationCount)
	show(&withOwnPages)
	ownCount, applicationCount = pageCounts()
	assert.Equal(t, 2, ownCount, "not on the own pages anymore")
	assert.Equal(t, 2, applicationCount)
}
