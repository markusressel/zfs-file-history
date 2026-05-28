package theme

import (
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	Primary   = tcell.ColorIsRGB | tcell.ColorValid | 0xFFA333
	Secondary = tcell.ColorIsRGB | tcell.ColorValid | 0x48525C
	// Secondary = tcell.ColorGray
	Accent = tcell.ColorDarkOrange

	OnPrimary   = tcell.ColorBlack
	OnSecondary = tcell.ColorWhite

	OnBackground = tcell.ColorWhite
)

var (
	Colors = Color{
		Header: HeaderColors{
			NameBackground: tcell.ColorBlack,
			Name:           Accent,

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
					Unknown:      tcell.ColorGray,
					Modified:     tcell.ColorYellow,
					LocalOnly:    tcell.ColorRed,
					SnapshotOnly: tcell.ColorGreen,
					Equal:        tcell.ColorGray,
				},
			},
		},
		Layout: LayoutColors{
			Title:  Accent,
			Border: Secondary,
			Table: LayoutTableColors{
				Header: Accent,

				SelectedForeground: tcell.ColorBlack,
				SelectedBackground: tcell.ColorWhite,

				MultiSelectionBackground: Secondary,
				MultiSelectionForeground: OnSecondary,
			},
		},
		ShortcutMap: ShortcutMapColors{
			KeyCombo: Accent,
			Name:     tcell.ColorLightGray,
		},
	}

	Style = StyleStruct{
		Layout: LayoutStyle{
			TitleAlign:       tview.AlignCenter,
			DialogTitleAlign: tview.AlignCenter,
		},
		Format: FormatStyle{
			DateTime: time.DateTime,
		},
	}
)
