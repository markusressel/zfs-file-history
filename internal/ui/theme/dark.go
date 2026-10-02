package theme

import (
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	Primary = tcell.ColorIsRGB | tcell.ColorValid | 0xFFA333
	// PrimaryMuted is a less bright variant of Primary (same hue, ~70% of its brightness) for larger areas,
	// with black text (contrast 7.6:1). Darker variants look brown.
	PrimaryMuted = tcell.ColorIsRGB | tcell.ColorValid | 0xE8820C
	Secondary    = tcell.ColorIsRGB | tcell.ColorValid | 0x48525C
	Accent       = tcell.ColorDarkOrange

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

			PageIndicatorBackground: PrimaryMuted,
			PageIndicator:           OnPrimary,
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
		List: ListColors{
			Scrollbar: ScrollbarColors{
				Background:        tcell.ColorBlack,
				Bar:               tcell.ColorDarkGray,
				IndicatorActive:   Accent,
				IndicatorInactive: tcell.ColorGray,
			},
		},
		Layout: LayoutColors{
			Title:  Accent,
			Border: Secondary,
			Table: LayoutTableColors{
				Accent: Accent,

				HeaderForeground: tcell.ColorWhite,
				// Secondary at 60% brightness: a dark slate that matches the window borders, without drawing attention.
				// Keep it distinct from the "unchanged" text color (tcell.ColorGray)
				// and from the multi-selection background (Secondary).
				HeaderBackground: tcell.ColorIsRGB | tcell.ColorValid | 0x2B3137,

				SelectedForeground: tcell.ColorBlack,
				SelectedBackground: tcell.ColorWhite,

				SortColumnSelectedForeground: OnPrimary,
				SortColumnSelectedBackground: Primary,

				MultiSelectionBackground: Secondary,
				MultiSelectionForeground: OnSecondary,

				// a lighter variant of Secondary, readable on the dark background
				TreeLines:              tcell.ColorIsRGB | tcell.ColorValid | 0x7D8A96,
				TreeCollapsedIndicator: Primary,
			},
		},
		ShortcutMap: ShortcutMapColors{
			KeyCombo: Accent,
			Name:     tcell.ColorLightGray,
		},
		Permissions: PermissionColors{
			Granted: tcell.ColorGreen,
			Missing: tcell.ColorGray,
			Unknown: tcell.ColorDarkGray,
			Pending: tcell.ColorYellow,
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
