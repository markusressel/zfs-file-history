package util

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestLoadingContainer(t *testing.T) {
	app := tview.NewApplication()
	simScreen := tcell.NewSimulationScreen("")
	app.SetScreen(simScreen)
	go app.Run()
	defer app.Stop()

	content := tview.NewBox()
	container := NewLoadingContainer(app, content, "Test Container", "Please wait...")

	// Verify initial state
	assert.False(t, container.isLoading)
	pageName, frontPage := container.GetFrontPage()
	assert.Equal(t, LoadingContainerContentPage, pageName)
	assert.Equal(t, content, frontPage)

	// Set to loading
	container.SetIsLoading(true)
	assert.True(t, container.isLoading)
	pageName, frontPage = container.GetFrontPage()
	assert.Equal(t, LoadingContainerLoadingPage, pageName)

	// Wait a bit to let the loading view ticker run and render frames
	time.Sleep(150 * time.Millisecond)

	// Calling SetIsLoading again with same value should do nothing
	container.SetIsLoading(true)
	assert.True(t, container.isLoading)

	// Set back to not loading
	container.SetIsLoading(false)
	assert.False(t, container.isLoading)
	pageName, frontPage = container.GetFrontPage()
	assert.Equal(t, LoadingContainerContentPage, pageName)
	assert.Equal(t, content, frontPage)
}

func TestLoadingView_EdgeCases(t *testing.T) {
	app := tview.NewApplication()
	simScreen := tcell.NewSimulationScreen("")
	app.SetScreen(simScreen)
	go app.Run()
	defer app.Stop()

	loadingView := NewLoadingView(app, "Title", "Message")

	// Start once
	loadingView.Start()
	assert.NotNil(t, loadingView.cancel)

	// Start again (should return early since cancel != nil)
	loadingView.Start()

	// Stop
	loadingView.Stop()
	assert.Nil(t, loadingView.cancel)

	// Stop again (should do nothing since cancel == nil)
	loadingView.Stop()

	// Test Start and ticker fire with nil app
	loadingViewNilApp := NewLoadingView(nil, "Title", "Message")
	loadingViewNilApp.Start()
	time.Sleep(150 * time.Millisecond)
	loadingViewNilApp.Stop()

	// Test SetMessage
	loadingView.SetMessage("New message")
	assert.Equal(t, "New message", loadingView.message)
}

func TestLoadingContainer_SetBorderColorAndMessage(t *testing.T) {
	app := tview.NewApplication()

	// Content is a Box
	boxContent := tview.NewBox()
	containerBox := NewLoadingContainer(app, boxContent, "Title", "Msg")
	containerBox.SetBorderColor(tcell.ColorBlue)
	assert.Equal(t, tcell.ColorBlue, boxContent.GetBorderColor())

	// Content is a Flex
	flexContent := tview.NewFlex()
	containerFlex := NewLoadingContainer(app, flexContent, "Title", "Msg")
	containerFlex.SetBorderColor(tcell.ColorRed)
	assert.Equal(t, tcell.ColorRed, flexContent.GetBorderColor())

	// Test SetMessage
	containerBox.SetMessage("Another message")
	assert.Equal(t, "Another message", containerBox.loadingView.message)
}
