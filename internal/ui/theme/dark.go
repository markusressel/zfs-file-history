package theme

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	PrimaryColor   = tcell.ColorTeal
	SecondaryColor = tcell.ColorDarkOliveGreen

	OnPrimaryColor = tcell.ColorWhite
	OnSecondary    = tcell.ColorBlack
)

var (
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
