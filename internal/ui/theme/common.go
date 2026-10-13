package theme

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
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

type ListColors struct {
	Scrollbar ScrollbarColors
}

type ScrollbarColors struct {
	Background        tcell.Color
	Bar               tcell.Color
	IndicatorActive   tcell.Color
	IndicatorInactive tcell.Color
}

type SnapshotBrowserColors struct {
	Table SnapshotBrowserTableColors
}

type SnapshotBrowserTableColors struct {
	State SnapshotBrowserTableStatusColors
	// Changed is the text of snapshots in which something changed (e.g. a new version of the selected file starts),
	// Unchanged the text of snapshots that hold the same as the previous one
	Changed   tcell.Color
	Unchanged tcell.Color
}

type SnapshotBrowserTableStatusColors struct {
	Unknown      tcell.Color
	Modified     tcell.Color
	LocalOnly    tcell.Color
	SnapshotOnly tcell.Color
	Equal        tcell.Color
}

type DialogColors struct {
	Border tcell.Color
	// Error is the color of validation errors in input dialogs
	Error tcell.Color
	// SectionTitle is the color of the titles of the sections of lists, e.g. of the command menu
	SectionTitle tcell.Color
}

type StyleStruct struct {
	Layout LayoutStyle
	Format FormatStyle
}

type LayoutStyle struct {
	TitleAlign       int
	DialogTitleAlign int
}

type FormatStyle struct {
	DateTime string
}

type Color struct {
	Header          HeaderColors
	Dialog          DialogColors
	FileBrowser     FileBrowserColors
	SnapshotBrowser SnapshotBrowserColors
	List            ListColors
	Layout          LayoutColors
	ShortcutMap     ShortcutMapColors
	Sparkline       SparklineColors
	// Magnitude colors values by how big they are (see GradientStop), from the lowest to the highest stop: the
	// sparklines and the size columns of the snapshot list, so warm means big everywhere
	Magnitude   []GradientStop
	Permissions PermissionColors
	Properties  PropertyColors
	Pool        PoolColors
	Activity    ActivityColors
	Alert       AlertColors
}

// AlertColors are used by the panel below the header that shows the problems of the pools, e.g. data errors.
type AlertColors struct {
	// Background sets the panel apart from the views of the page
	Background tcell.Color
	// Error and Warning are the severities of the problems, also the border of the panel while it shows one of them
	Error   tcell.Color
	Warning tcell.Color
	// Pool is the name of the pool with the problem
	Pool tcell.Color
	// Message describes the problem
	Message tcell.Color
	// Hint explains the problems, e.g. in the dialog of the damaged files
	Hint tcell.Color
	// Unknown is a damaged object whose location is not known, e.g. metadata
	Unknown tcell.Color
	// HoverBackground is the background of the line below the mouse, which can be clicked
	HoverBackground tcell.Color
}

// ActivityColors are used by the bar below the header that shows the operations running on the pools, e.g. a scrub.
type ActivityColors struct {
	// Border and Background set the panel apart from the views of the page
	Border     tcell.Color
	Background tcell.Color
	// Kind is what runs, e.g. "Scrub"
	Kind tcell.Color
	// Pool is the name of the pool it runs on
	Pool tcell.Color
	// Remaining is the estimated time left, behind the progress bar
	Remaining tcell.Color
	// BarFilled is the background of the done part of a progress bar, by the column (0: the start of the bar, 1: its
	// end), so the colors show up as the progress grows. BarTrack is the background of the remaining part.
	BarFilled []GradientStop
	BarTrack  tcell.Color
	// BarDarkText and BarLightText are the colors of the text written on the bar: the dark one on light backgrounds,
	// the light one on dark backgrounds
	BarDarkText  tcell.Color
	BarLightText tcell.Color
	// HoverBackground is the background of the line below the mouse, which can be clicked
	HoverBackground tcell.Color
}

// PoolColors are used to show the health of pools and their devices.
type PoolColors struct {
	// Online is a healthy pool or device (also an available spare)
	Online tcell.Color
	// Degraded is a pool or device that still works, but with less redundancy (also a spare in use)
	Degraded tcell.Color
	// Faulted is a pool or device that does not work (faulted, unavailable, suspended)
	Faulted tcell.Color
	// Offline is a device taken offline or removed on purpose
	Offline tcell.Color
	// Group is the name of a group of devices, e.g. "logs"
	Group tcell.Color
	// Errors are read, write or checksum errors, NoErrors counters of 0
	Errors   tcell.Color
	NoErrors tcell.Color
}

// PropertyColors are used in the table of ZFS properties.
type PropertyColors struct {
	// Value is a property with a default or inherited value
	Value tcell.Color
	// Local is a property set on the dataset itself
	Local tcell.Color
	// ReadOnly is a property that cannot be set (statistics, values fixed at creation)
	ReadOnly tcell.Color
	// Hint explains the selected property
	Hint tcell.Color
}

// PermissionColors are used to show which ZFS permissions the user has.
type PermissionColors struct {
	Granted tcell.Color
	Missing tcell.Color
	// Unknown is used while the permissions are loading or if they cannot be read
	Unknown tcell.Color
	// Pending marks changes of delegations that are not applied yet
	Pending tcell.Color
}

type LayoutColors struct {
	Border tcell.Color
	Title  tcell.Color
	Table  LayoutTableColors
}

type LayoutTableColors struct {
	// Accent highlights important values, e.g. directory names and the keys of key-value listings
	Accent tcell.Color

	HeaderForeground tcell.Color
	HeaderBackground tcell.Color

	SelectedForeground tcell.Color
	SelectedBackground tcell.Color

	// SortColumnSelected* highlight the header cell of the sort column while the header row is selected
	SortColumnSelectedForeground tcell.Color
	SortColumnSelectedBackground tcell.Color

	MultiSelectionBackground tcell.Color
	MultiSelectionForeground tcell.Color

	// TreeLines are the lines of tree views (e.g. "├─ ")
	TreeLines tcell.Color
	// TreeCollapsedIndicator is the "▸ +N" behind collapsed entries of tree views
	TreeCollapsedIndicator tcell.Color
	// ZeroSize is the color of sizes of 0 in size columns (e.g. "Written" of snapshots in which nothing changed),
	// dimmed so the others stand out; the others are colored with Colors.Magnitude
	ZeroSize tcell.Color
}

// SparklineColors are the colors of the sparklines (see uiutil.DrawSparkline), e.g. in the path overview and the
// histories.
type SparklineColors struct {
	// Graph is the color of the sparklines without Color.Magnitude stops
	Graph tcell.Color
	// Selected and SelectedBackground mark the selected value, e.g. the snapshot selected in a list
	Selected           tcell.Color
	SelectedBackground tcell.Color
}

// GradientStop is a color of a gradient: values at Position (0: zero, 1: the highest value of the graph) have this
// color, values between two stops a mix of both.
type GradientStop struct {
	Position float64
	Color    tcell.Color
}

type ShortcutMapColors struct {
	// KeyCombo is the color of the keys of actions (shortcut_helper.GroupAction), the ones looked for most
	KeyCombo tcell.Color
	// ViewKeyCombo is the color of the keys that change what is shown (columns, filter, sorting, modes)
	ViewKeyCombo tcell.Color
	// NavigationKeyCombo is the color of the keys that move the selection or the focus
	NavigationKeyCombo tcell.Color
	// GlobalKeyCombo is the color of the keys that work everywhere (switch page, refresh, quit)
	GlobalKeyCombo tcell.Color
	Name           tcell.Color
	// Separator is the color of the line between groups of shortcuts
	Separator tcell.Color
	// HoverBackground is the background of the entry (or key) below the mouse, which can be clicked
	HoverBackground tcell.Color
}

func CreateTitleText(text string) string {
	titleText := fmt.Sprintf(" %s ", text)
	return titleText
}
