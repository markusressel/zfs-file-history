package theme

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	PrimaryColor   = tcell.ColorTeal
	SecondaryColor = tcell.ColorDarkOliveGreen

	OnPrimaryColor = tcell.ColorWhite
	OnSecondary    = tcell.ColorBlack

	Colors = Color{
		Header: HeaderColors{
			NameBackground: PrimaryColor,
			Name:           OnPrimaryColor,

			VersionBackground: SecondaryColor,
			Version:           OnSecondary,
		},
		Dialog: DialogColors{
			Border: SecondaryColor,
		},
		FileBrowser: FileBrowserColors{
			Table: FileBrowserTableColors{
				State: FileBrowserTableStatusColors{
					Unknown:  tcell.ColorGray,
					Modified: tcell.ColorYellow,
					Added:    tcell.ColorGreen,
					Deleted:  tcell.ColorRed,
					Equal:    tcell.ColorGray,
				},
			},
		},
		SnapshotBrowser: SnapshotBrowserColors{
			Table: SnapshotBrowserTableColors{
				State: SnapshotBrowserTableStatusColors{
					Unknown:  tcell.ColorGray,
					Modified: tcell.ColorYellow,
					Added:    tcell.ColorRed,
					Deleted:  tcell.ColorGreen,
					Equal:    tcell.ColorGray,
				},
			},
		},
		Layout: LayoutColors{
			Title:  PrimaryColor,
			Border: SecondaryColor,
			Table: LayoutTableColors{
				Header: PrimaryColor,
			},
		},
	}

	Style = StyleStruct{
		Layout: LayoutStyle{
			TitleAlign:       tview.AlignCenter,
			DialogTitleAlign: tview.AlignCenter,
		},
	}
)

type HeaderColors struct {
	Name           tcell.Color
	NameBackground tcell.Color

	PageIndicator           tcell.Color
	PageIndicatorBackground tcell.Color

	UpdateInterval           tcell.Color
	UpdateIntervalBackground tcell.Color

	Version           tcell.Color
	VersionBackground tcell.Color
}

type FileBrowserColors struct {
	Table FileBrowserTableColors
}

type FileBrowserTableColors struct {
	State FileBrowserTableStatusColors
}

type FileBrowserTableStatusColors struct {
	Unknown  tcell.Color
	Modified tcell.Color
	Added    tcell.Color
	Deleted  tcell.Color
	Equal    tcell.Color
}

type SnapshotBrowserColors struct {
	Table SnapshotBrowserTableColors
}

type SnapshotBrowserTableColors struct {
	State SnapshotBrowserTableStatusColors
}

type SnapshotBrowserTableStatusColors struct {
	Unknown  tcell.Color
	Modified tcell.Color
	Added    tcell.Color
	Deleted  tcell.Color
	Equal    tcell.Color
}

type DialogColors struct {
	Border tcell.Color
}

type StyleStruct struct {
	Layout LayoutStyle
}

type LayoutStyle struct {
	TitleAlign       int
	DialogTitleAlign int
}

type Color struct {
	Header          HeaderColors
	Dialog          DialogColors
	FileBrowser     FileBrowserColors
	SnapshotBrowser SnapshotBrowserColors
	Layout          LayoutColors
}

type LayoutColors struct {
	Border tcell.Color
	Title  tcell.Color
	Table  LayoutTableColors
}

type LayoutTableColors struct {
	Header tcell.Color
}

func CreateTitleText(text string) string {
	titleText := fmt.Sprintf(" %s ", text)
	return titleText
}
