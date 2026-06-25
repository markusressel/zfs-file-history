package util

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// LoadingContainer is a tview.Pages wrapper that handles switching between a loading view and content.
type LoadingContainer struct {
	*tview.Pages
	loadingView *LoadingView
	content     tview.Primitive
	isLoading   bool
}

func NewLoadingContainer(app *tview.Application, content tview.Primitive, title string, message string) *LoadingContainer {
	loadingView := NewLoadingView(app, title, message)
	pages := tview.NewPages().
		AddPage(LoadingContainerContentPage, content, true, true).
		AddPage(LoadingContainerLoadingPage, loadingView, true, false)

	return &LoadingContainer{
		Pages:       pages,
		loadingView: loadingView,
		content:     content,
	}
}

func (c *LoadingContainer) SetIsLoading(isLoading bool) {
	if c.isLoading == isLoading {
		return
	}
	c.isLoading = isLoading
	if isLoading {
		c.loadingView.Start()
		// Only hide the content, do not touch dynamic dialogs
		c.HidePage(LoadingContainerContentPage)
		c.ShowPage(LoadingContainerLoadingPage)
	} else {
		c.loadingView.Stop()
		// Only hide the loading screen, do not touch dynamic dialogs
		c.HidePage(LoadingContainerLoadingPage)
		c.ShowPage(LoadingContainerContentPage)
	}
}

func (c *LoadingContainer) GetFrontPage() (string, tview.Primitive) {
	return c.Pages.GetFrontPage()
}

func (c *LoadingContainer) SetBorderColor(color tcell.Color) {
	if c.loadingView != nil {
		c.loadingView.SetBorderColor(color)
	}
	if c.content != nil {
		if box, ok := c.content.(interface{ SetBorderColor(tcell.Color) *tview.Box }); ok {
			box.SetBorderColor(color)
		} else if flex, ok := c.content.(*tview.Flex); ok {
			flex.SetBorderColor(color)
		}
	}
}
