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
}

type ShortcutMapColors struct {
	KeyCombo tcell.Color
	Name     tcell.Color
}

func CreateTitleText(text string) string {
	titleText := fmt.Sprintf(" %s ", text)
	return titleText
}
