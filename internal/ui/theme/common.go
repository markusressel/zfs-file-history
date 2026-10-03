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
}

func CreateTitleText(text string) string {
	titleText := fmt.Sprintf(" %s ", text)
	return titleText
}
