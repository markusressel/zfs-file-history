package ui

import "github.com/rivo/tview"

type HelpPage struct {
	layout *tview.Flex
}

func NewHelpPage() *HelpPage {
	helpPage := &HelpPage{}

	helpPage.createLayout()

	return helpPage
}

func (p HelpPage) createLayout() {
	someText := tview.NewTextView().SetText("ABC")

	pageContent := tview.NewFlex().
		AddItem(someText, 40, 0, false)
	p.layout = pageContent
	// createModal("Help", pageContent, 80, 80)
}
