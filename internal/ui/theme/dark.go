package theme

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	Primary   = tcell.ColorTeal
	Secondary = tcell.ColorDarkOliveGreen

	OnPrimary   = tcell.ColorWhite
	OnSecondary = tcell.ColorBlack
)

var (
	Colors = Color{
		Header: HeaderColors{
			NameBackground: Primary,
			Name:           OnPrimary,

			VersionBackground: Secondary,
			Version:           OnSecondary,
		},
		Dialog: DialogColors{
			Border: Secondary,
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
			Title:  Primary,
			Border: Secondary,
			Table: LayoutTableColors{
				Header: Primary,

				SelectedForeground: tcell.ColorBlack,
				SelectedBackground: tcell.ColorWhite,

				MultiSelectionBackground: Secondary,
				MultiSelectionForeground: OnSecondary,
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
