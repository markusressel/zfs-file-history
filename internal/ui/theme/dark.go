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
	// Hover is the background of clickable things below the mouse
	Hover = tcell.ColorIsRGB | tcell.ColorValid | 0x3A434D

	OnPrimary   = tcell.ColorBlack
	OnSecondary = tcell.ColorWhite

	OnBackground = tcell.ColorWhite

	// PanelBackground is the background of the panels below the header (activities and alerts), which sets them apart
	// from the views of the page
	PanelBackground = tcell.ColorIsRGB | tcell.ColorValid | 0x16191C
	// Ok, Degraded and Faulted are the colors of states: all well, something that still works with less redundancy
	// (or needs attention), and something that does not work or lost data. Fixed RGB values, not the green, yellow
	// and red of the terminal: those were drawn in the colors of its palette in tables, but as their RGB values in
	// texts (style tags, see txwidgets.ColorTag), two different shades. Similar in lightness and saturation, so they
	// look like one palette next to the orange accent.
	Ok       = tcell.ColorIsRGB | tcell.ColorValid | 0x46C95B
	Degraded = tcell.ColorIsRGB | tcell.ColorValid | 0xF0C93C
	Faulted  = tcell.ColorIsRGB | tcell.ColorValid | 0xF2514A
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

			PageNumberBackground: Secondary,
			PageNumber:           OnSecondary,
			// a lighter Secondary, so the hovered number stands out from the others
			PageNumberHoverBackground: tcell.ColorIsRGB | tcell.ColorValid | 0x6B7785,
		},
		Dialog: DialogColors{
			Border:       Secondary,
			Error:        Faulted,
			Danger:       Faulted,
			DangerBorder: Faulted,
			Highlight:    Accent,
			// a lighter Secondary, readable on the background
			SectionTitle: tcell.ColorIsRGB | tcell.ColorValid | 0x8C99A6,
			Unavailable:  tcell.ColorGray,
		},
		FileBrowser: FileBrowserColors{
			Table: FileBrowserTableColors{
				State: FileBrowserTableStatusColors{
					Unknown:  tcell.ColorGray,
					Modified: Degraded,
					Added:    Ok,
					Deleted:  Faulted,
					Equal:    tcell.ColorGray,
				},
			},
		},
		SnapshotBrowser: SnapshotBrowserColors{
			Table: SnapshotBrowserTableColors{
				State: SnapshotBrowserTableStatusColors{
					Unknown:      tcell.ColorGray,
					Modified:     Degraded,
					LocalOnly:    Faulted,
					SnapshotOnly: Ok,
					Equal:        tcell.ColorGray,
				},
				Changed:   tcell.ColorWhite,
				Unchanged: tcell.ColorGray,
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
			Title:                 Accent,
			Border:                Secondary,
			FooterHoverBackground: Hover,
			// unlike the selected row of a table (Accent), so both can be told apart
			SelectionForeground: tcell.ColorWhite,
			SelectionBackground: tcell.ColorIsRGB | tcell.ColorValid | 0x264F78,
			Loading: LoadingColors{
				Spinner: Accent,
				Message: tcell.ColorWhite,
				Detail:  tcell.ColorGray,
			},
			Table: LayoutTableColors{
				Accent: Accent,

				HeaderForeground: tcell.ColorWhite,
				// Secondary at 60% brightness: a dark slate that matches the window borders, without drawing attention.
				// Keep it distinct from the "unchanged" text color (tcell.ColorGray)
				// and from the multi-selection background (Secondary).
				HeaderBackground: tcell.ColorIsRGB | tcell.ColorValid | 0x2B3137,

				// A dark blue bar with light text, not a light bar with dark text: a light row glared on the dark
				// background, and its text was harder to read than that of the other rows. Cells keep their own text
				// color on it (see SelectedStyle); bluer than the multi-selection (Secondary) and the header, less
				// saturated than the text selection (Layout.SelectionBackground).
				SelectedForeground: tcell.ColorWhite,
				SelectedBackground: tcell.ColorIsRGB | tcell.ColorValid | 0x2D4560,

				SortColumnSelectedForeground: OnPrimary,
				SortColumnSelectedBackground: Primary,

				MultiSelectionBackground: Secondary,
				MultiSelectionForeground: OnSecondary,

				// a lighter variant of Secondary, readable on the dark background
				TreeLines:              tcell.ColorIsRGB | tcell.ColorValid | 0x7D8A96,
				TreeCollapsedIndicator: Primary,
				ZeroSize:               tcell.ColorDarkGray,
				EmptyText:              tcell.ColorGray,
				GroupTitle:             Accent,
				GroupCount:             tcell.ColorGray,
			},
		},
		ShortcutMap: ShortcutMapColors{
			KeyCombo:           Accent,
			ViewKeyCombo:       tcell.ColorIsRGB | tcell.ColorValid | 0x6CB6E8,
			NavigationKeyCombo: tcell.ColorIsRGB | tcell.ColorValid | 0x8CC98C,
			GlobalKeyCombo:     tcell.ColorIsRGB | tcell.ColorValid | 0x9AA5B1,
			Name:               tcell.ColorLightGray,
			Separator:          Secondary,
			HoverBackground:    Hover,
		},
		Sparkline: SparklineColors{
			Graph:              Accent,
			Selected:           Primary,
			SelectedBackground: Secondary,
		},
		// calm for small values, warm for large ones
		Magnitude: []GradientStop{
			{Position: 0, Color: tcell.ColorIsRGB | tcell.ColorValid | 0x7D8A96},
			{Position: 0.5, Color: Accent},
			{Position: 1, Color: Faulted},
		},
		Permissions: PermissionColors{
			Granted: Ok,
			Missing: tcell.ColorGray,
			Unknown: tcell.ColorDarkGray,
			Pending: Degraded,
		},
		Properties: PropertyColors{
			Value:    tcell.ColorLightGray,
			Local:    Accent,
			ReadOnly: tcell.ColorGray,
			Hint:     tcell.ColorGray,
		},
		Pool: PoolColors{
			Online:   Ok,
			Degraded: Degraded,
			Faulted:  Faulted,
			Offline:  tcell.ColorGray,
			Group:    Accent,
			Errors:   Faulted,
			NoErrors: tcell.ColorGray,
			// from cool blue over green to red, hot when it reaches the warning temperature of the disk
			Temperature: []GradientStop{
				{Position: 0, Color: tcell.ColorIsRGB | tcell.ColorValid | 0x6CB6E8},
				{Position: 0.4, Color: tcell.ColorIsRGB | tcell.ColorValid | 0x8CC98C},
				{Position: 0.75, Color: tcell.ColorIsRGB | tcell.ColorValid | 0xFFB84D},
				{Position: 1, Color: Faulted},
			},
			TemperatureIdle:    tcell.ColorGray,
			TemperatureUnknown: tcell.ColorDarkGray,
			ReadTemperature:    Accent,
			HoverBackground:    Hover,
			Hint:               tcell.ColorGray,
		},
		Activity: ActivityColors{
			Border: PrimaryMuted,
			// darker than BarTrack, so the empty part of the bars stays visible
			Background: PanelBackground,
			Kind:       Accent,
			Pool:       tcell.ColorWhite,
			Remaining:  tcell.ColorLightGray,
			// slightly lighter than Background, tinted blue and violet
			RemainingBackground:      tcell.ColorIsRGB | tcell.ColorValid | 0x1E2A36,
			EtaBackground:            tcell.ColorIsRGB | tcell.ColorValid | 0x2C2436,
			RemainingHoverBackground: tcell.ColorIsRGB | tcell.ColorValid | 0x2F4256,
			EtaHoverBackground:       tcell.ColorIsRGB | tcell.ColorValid | 0x44385A,
			// from a deep to a light orange, all light enough for BarDarkText
			BarFilled: []GradientStop{
				{Position: 0, Color: tcell.ColorIsRGB | tcell.ColorValid | 0xCC5500},
				{Position: 0.6, Color: Accent},
				{Position: 1, Color: tcell.ColorIsRGB | tcell.ColorValid | 0xFFB84D},
			},
			BarDarkText: OnPrimary,
			// darker than Secondary (e.g. the version in the header) and Hover, so it does not look like either
			BarTrack:        tcell.ColorIsRGB | tcell.ColorValid | 0x262B31,
			BarLightText:    OnSecondary,
			HoverBackground: Hover,
		},
		Info: InfoColors{
			Label:   tcell.ColorGray,
			Section: Accent,
			Value:   tcell.ColorWhite,
			Muted:   tcell.ColorGray,
			Ok:      Ok,
			Warning: Degraded,
			Error:   Faulted,
			// a darker gray than the labels, so the bar stays visible
			BarEmpty: tcell.ColorIsRGB | tcell.ColorValid | 0x3A4048,
			// green while there is room, yellow when it gets full, red when (almost) full
			BarFilled: []GradientStop{
				{Position: 0, Color: tcell.ColorIsRGB | tcell.ColorValid | 0x8CC98C},
				{Position: 0.75, Color: tcell.ColorIsRGB | tcell.ColorValid | 0x8CC98C},
				{Position: 0.85, Color: tcell.ColorIsRGB | tcell.ColorValid | 0xFFB84D},
				{Position: 1, Color: Faulted},
			},
			// blue, unlike the states (green, yellow, red); the snapshots lighter, the children darker
			SpaceData:      tcell.ColorIsRGB | tcell.ColorValid | 0x4A9EE0,
			SpaceSnapshots: tcell.ColorIsRGB | tcell.ColorValid | 0xA5D2F7,
			SpaceChildren:  tcell.ColorIsRGB | tcell.ColorValid | 0x2C6A9E,
			// lighter than BarEmpty, so used and free space can be told apart
			SpaceOthers: tcell.ColorIsRGB | tcell.ColorValid | 0x7A838C,
		},
		Alert: AlertColors{
			Background:      PanelBackground,
			Error:           Faulted,
			Warning:         Degraded,
			Pool:            tcell.ColorWhite,
			Message:         tcell.ColorLightGray,
			Hint:            tcell.ColorLightGray,
			Unknown:         tcell.ColorGray,
			HoverBackground: Hover,
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
