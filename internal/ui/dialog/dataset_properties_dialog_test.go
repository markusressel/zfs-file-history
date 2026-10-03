package dialog

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestProperties() []*zfs.Property {
	return []*zfs.Property{
		{Name: "org.example:note", Value: "hello", Source: "local"},
		{Name: "used", Value: "271G", Source: "-"},
		{Name: "compression", Value: "zstd", Source: "inherited from rpool"},
		{Name: "canmount", Value: "on", Source: "local"},
		{Name: "atime", Value: "on", Source: "default"},
	}
}

// propertiesTest shows the properties dialog in a running application. zfs and sudo are never run.
type propertiesTest struct {
	t      *testing.T
	app    *tview.Application
	screen tcell.SimulationScreen
	pages  *tview.Pages
	dialog *DatasetPropertiesDialog

	mu         sync.Mutex
	set        []string
	inherited  []string
	sudo       [][]string
	setErr     error
	sudoErr    error
	reloads    int
	onChanged  int
	properties []*zfs.Property
}

func newPropertiesTest(t *testing.T, isRoot bool) *propertiesTest {
	pt := &propertiesTest{t: t, properties: newTestProperties()}
	originalList, originalSet, originalInherit, originalGrant := listProperties, setProperty, inheritProperty, grantPermissions
	t.Cleanup(func() {
		listProperties, setProperty, inheritProperty, grantPermissions = originalList, originalSet, originalInherit, originalGrant
	})
	setProperty = func(dataset string, name string, value string) error {
		pt.mu.Lock()
		defer pt.mu.Unlock()
		pt.set = append(pt.set, fmt.Sprintf("%s %s=%s", dataset, name, value))
		if pt.setErr == nil {
			pt.apply(name, value, "local")
		}
		return pt.setErr
	}
	inheritProperty = func(dataset string, name string) error {
		pt.mu.Lock()
		defer pt.mu.Unlock()
		pt.inherited = append(pt.inherited, dataset+" "+name)
		pt.apply(name, "off", "default")
		return nil
	}
	grantPermissions = func(application *tview.Application, explanation string, commands [][]string) error {
		pt.mu.Lock()
		defer pt.mu.Unlock()
		pt.sudo = append(pt.sudo, commands...)
		if pt.sudoErr == nil {
			pt.apply("compression", "lz4", "local")
		}
		return pt.sudoErr
	}
	listProperties = func(dataset string) ([]*zfs.Property, error) {
		pt.mu.Lock()
		defer pt.mu.Unlock()
		pt.reloads++
		var copied []*zfs.Property
		for _, property := range pt.properties {
			copy := *property
			copied = append(copied, &copy)
		}
		return copied, nil
	}

	pt.app = tview.NewApplication()
	pt.screen = tcell.NewSimulationScreen("UTF-8")
	pt.app.SetScreen(pt.screen)
	pt.pages = tview.NewPages().AddPage("background", tview.NewBox(), true, true)
	pt.app.SetRoot(pt.pages, true)
	go func() { _ = pt.app.Run() }()
	t.Cleanup(pt.app.Stop)

	testutil.OnUiThread(t, pt.app, func() {
		pt.dialog = NewDatasetPropertiesDialog(pt.app, "pool/data", newTestProperties(), isRoot, func() {
			pt.mu.Lock()
			defer pt.mu.Unlock()
			pt.onChanged++
		})
		ShowDialogOnPages(pt.app, pt.pages, pt.dialog, nil)
		pt.app.ForceDraw()
	})
	return pt
}

// apply changes (or creates) the property that listProperties returns. Must be called with mu held.
func (pt *propertiesTest) apply(name string, value string, source string) {
	for _, property := range pt.properties {
		if property.Name == name {
			property.Value, property.Source = value, source
			return
		}
	}
	pt.properties = append(pt.properties, &zfs.Property{Name: name, Value: value, Source: source})
}

func (pt *propertiesTest) press(key tcell.Key, r rune) {
	pt.screen.InjectKey(key, r, tcell.ModNone)
}

func (pt *propertiesTest) hasPage(name string) bool {
	shown := false
	testutil.OnUiThread(pt.t, pt.app, func() { shown = pt.pages.HasPage(name) })
	return shown
}

func (pt *propertiesTest) waitFor(message string, condition func() bool) {
	require.Eventually(pt.t, condition, 3*time.Second, 10*time.Millisecond, message)
}

// selectProperty selects the property with the given name in the table.
func (pt *propertiesTest) selectProperty(name string) {
	testutil.OnUiThread(pt.t, pt.app, func() {
		for _, property := range pt.dialog.table.GetEntries() {
			if property.Name == name {
				pt.dialog.table.Select(property)
			}
		}
	})
}

func (pt *propertiesTest) value(name string) string {
	value := ""
	testutil.OnUiThread(pt.t, pt.app, func() {
		for _, property := range pt.dialog.table.GetAllEntries() {
			if property.Name == name {
				value = property.Value + " (" + property.Source + ")"
			}
		}
	})
	return value
}

func (pt *propertiesTest) details() string {
	text := ""
	testutil.OnUiThread(pt.t, pt.app, func() { text = pt.dialog.details.GetText(true) })
	return text
}

func TestDatasetPropertiesDialog_Content(t *testing.T) {
	d := NewDatasetPropertiesDialog(tview.NewApplication(), "pool/data", newTestProperties(), false, nil)

	var names []string
	for _, property := range d.table.GetEntries() {
		names = append(names, property.Name)
	}
	assert.Equal(t, []string{"atime", "canmount", "compression", "org.example:note", "used"}, names, "sorted by name")
	assert.Equal(t, "5 properties", d.table.GetFooter())
	assert.Equal(t, "atime = on (default)\nEnter: change", d.details.GetText(true))

	colorOf := func(property *zfs.Property) tcell.Color {
		foreground, _, _ := toPropertyCells(0, propertyColumns, property)[1].Style.Decompose()
		return foreground
	}
	assert.Equal(t, theme.Colors.Properties.ReadOnly, colorOf(&zfs.Property{Source: "-"}))
	assert.Equal(t, theme.Colors.Properties.Local, colorOf(&zfs.Property{Source: "local"}))
	assert.Equal(t, theme.Colors.Properties.Value, colorOf(&zfs.Property{Source: "inherited from rpool"}))

	// names and values are escaped
	cells := toPropertyCells(0, propertyColumns, &zfs.Property{Name: "a:b", Value: "[red]x", Source: "local"})
	assert.Equal(t, tview.Escape("[red]x"), cells[1].Text)
}

func TestDatasetPropertiesDialog_Details(t *testing.T) {
	d := NewDatasetPropertiesDialog(tview.NewApplication(), "pool/data", newTestProperties(), false, nil)
	selectByName := func(name string) {
		for _, property := range d.table.GetEntries() {
			if property.Name == name {
				d.table.Select(property)
			}
		}
		d.updateDetails()
	}

	selectByName("used")
	assert.Equal(t, "used = 271G (-)\nread-only (a statistic, or fixed when the dataset was created)", d.details.GetText(true))
	selectByName("canmount")
	assert.Equal(t, "canmount = on (local)\nEnter: change, i: reset to the inherited/default value", d.details.GetText(true))
	selectByName("org.example:note")
	assert.Equal(t, "org.example:note = hello (local)\nEnter: change, i: remove", d.details.GetText(true))
}

func TestDatasetPropertiesDialog_ShortcutsFit(t *testing.T) {
	shortcuts := shortcut_helper.NewShortcutMap(tview.NewApplication())
	shortcuts.SetEntries(datasetPropertiesShortcuts())
	assert.LessOrEqual(t, shortcuts.CalculateHeightForWidth(76+6-2), datasetPropertiesShortcutLines)
}

func TestDatasetPropertiesDialog_Edit(t *testing.T) {
	pt := newPropertiesTest(t, false)
	pt.selectProperty("canmount")

	pt.press(tcell.KeyEnter, 0)
	pt.waitFor("edit dialog", func() bool { return pt.hasPage("EditPropertyDialog") })
	pt.press(tcell.KeyCtrlU, 0)
	for _, r := range "noauto" {
		pt.press(tcell.KeyRune, r)
	}
	pt.press(tcell.KeyEnter, 0)

	pt.waitFor("set and reloaded", func() bool { return pt.value("canmount") == "noauto (local)" })
	pt.mu.Lock()
	defer pt.mu.Unlock()
	assert.Equal(t, []string{"pool/data canmount=noauto"}, pt.set)
	assert.Empty(t, pt.sudo, "set as the current user")
	assert.Equal(t, 1, pt.onChanged)
}

func TestDatasetPropertiesDialog_EditUnchangedDoesNothing(t *testing.T) {
	pt := newPropertiesTest(t, false)
	pt.selectProperty("canmount")

	pt.press(tcell.KeyEnter, 0)
	pt.waitFor("edit dialog", func() bool { return pt.hasPage("EditPropertyDialog") })
	pt.press(tcell.KeyEnter, 0)
	pt.waitFor("closed", func() bool { return !pt.hasPage("EditPropertyDialog") })
	time.Sleep(50 * time.Millisecond)

	pt.mu.Lock()
	defer pt.mu.Unlock()
	assert.Empty(t, pt.set)
}

func TestDatasetPropertiesDialog_ReadOnlyCannotBeEdited(t *testing.T) {
	pt := newPropertiesTest(t, false)
	pt.selectProperty("used")

	pt.press(tcell.KeyEnter, 0)
	pt.press(tcell.KeyRune, 'i')
	time.Sleep(100 * time.Millisecond)
	assert.False(t, pt.hasPage("EditPropertyDialog"))
	assert.False(t, pt.hasPage("InheritPropertyDialog"))
}

func TestDatasetPropertiesDialog_EditWithSudo(t *testing.T) {
	pt := newPropertiesTest(t, false)
	pt.setErr = fmt.Errorf("%w: cannot set property", zfs.ErrPermissionDenied)
	pt.selectProperty("compression")

	pt.press(tcell.KeyEnter, 0)
	pt.waitFor("edit dialog", func() bool { return pt.hasPage("EditPropertyDialog") })
	pt.press(tcell.KeyCtrlU, 0)
	for _, r := range "lz4" {
		pt.press(tcell.KeyRune, r)
	}
	pt.press(tcell.KeyEnter, 0)

	pt.waitFor("set with sudo", func() bool { return pt.value("compression") == "lz4 (local)" })
	pt.mu.Lock()
	defer pt.mu.Unlock()
	assert.Equal(t, [][]string{{"zfs", "set", "compression=lz4", "pool/data"}}, pt.sudo)
}

func TestDatasetPropertiesDialog_EditFails(t *testing.T) {
	pt := newPropertiesTest(t, false)
	pt.setErr = errors.New("cannot set property for 'pool/data': 'compression' must be one of 'on | off | lz4 | ...'")
	pt.selectProperty("compression")

	pt.press(tcell.KeyEnter, 0)
	pt.waitFor("edit dialog", func() bool { return pt.hasPage("EditPropertyDialog") })
	pt.press(tcell.KeyCtrlU, 0)
	for _, r := range "fast" {
		pt.press(tcell.KeyRune, r)
	}
	pt.press(tcell.KeyEnter, 0)

	pt.waitFor("error", func() bool { return pt.hasPage("ErrorDialog") })
	assert.Equal(t, "zstd (inherited from rpool)", pt.value("compression"))
	assert.True(t, strings.HasPrefix(pt.details(), "compression = zstd"), "the details are shown again")
	pt.mu.Lock()
	defer pt.mu.Unlock()
	assert.Empty(t, pt.sudo, "not a permission error")
	assert.Zero(t, pt.onChanged)
}

func TestDatasetPropertiesDialog_Inherit(t *testing.T) {
	pt := newPropertiesTest(t, false)

	// only local values can be reset
	pt.selectProperty("compression")
	pt.press(tcell.KeyRune, 'i')
	time.Sleep(50 * time.Millisecond)
	assert.False(t, pt.hasPage("InheritPropertyDialog"))

	pt.selectProperty("canmount")
	pt.press(tcell.KeyRune, 'i')
	pt.waitFor("confirmation", func() bool { return pt.hasPage("InheritPropertyDialog") })
	pt.press(tcell.KeyRune, '1')
	pt.press(tcell.KeyEnter, 0)

	pt.waitFor("inherited and reloaded", func() bool { return pt.value("canmount") == "off (default)" })
	pt.mu.Lock()
	defer pt.mu.Unlock()
	assert.Equal(t, []string{"pool/data canmount"}, pt.inherited)
	assert.Equal(t, 1, pt.onChanged)
}

func TestDatasetPropertiesDialog_EscClosesUnlessFiltering(t *testing.T) {
	pt := newPropertiesTest(t, false)

	pt.press(tcell.KeyCtrlF, 0)
	pt.press(tcell.KeyRune, 'c')
	pt.waitFor("filtered", func() bool {
		count := 0
		testutil.OnUiThread(t, pt.app, func() { count = len(pt.dialog.table.GetEntries()) })
		return count == 2 // canmount, compression
	})
	pt.press(tcell.KeyEscape, 0)
	time.Sleep(50 * time.Millisecond)
	assert.True(t, pt.hasPage("DatasetPropertiesDialog"), "Esc ends typing the filter first")

	pt.press(tcell.KeyEscape, 0)
	pt.waitFor("closed", func() bool { return !pt.hasPage("DatasetPropertiesDialog") })
}

func (pt *propertiesTest) typeText(text string) {
	for _, r := range text {
		pt.press(tcell.KeyRune, r)
	}
}

func (pt *propertiesTest) screenText() string {
	var text strings.Builder
	testutil.OnUiThread(pt.t, pt.app, func() {
		pt.app.ForceDraw()
		cells, width, height := pt.screen.GetContents()
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				if runes := cells[y*width+x].Runes; len(runes) > 0 {
					text.WriteRune(runes[0])
				}
			}
			text.WriteRune('\n')
		}
	})
	return text.String()
}

func (pt *propertiesTest) selectedName() string {
	name := ""
	testutil.OnUiThread(pt.t, pt.app, func() {
		if selected := pt.dialog.table.GetSelectedEntry(); selected != nil {
			name = selected.Name
		}
	})
	return name
}

func TestDatasetPropertiesDialog_AddUserProperty(t *testing.T) {
	pt := newPropertiesTest(t, false)

	pt.press(tcell.KeyRune, 'a')
	pt.waitFor("name dialog", func() bool { return pt.hasPage("AddPropertyNameDialog") })

	// invalid names keep the dialog open, with the reason
	pt.typeText("note")
	pt.press(tcell.KeyEnter, 0)
	pt.waitFor("colon error", func() bool { return strings.Contains(pt.screenText(), "need a colon") })
	pt.press(tcell.KeyCtrlU, 0)
	pt.typeText("compression")
	pt.press(tcell.KeyEnter, 0)
	pt.waitFor("native error", func() bool { return strings.Contains(pt.screenText(), "native property") })
	assert.True(t, pt.hasPage("AddPropertyNameDialog"))

	pt.press(tcell.KeyCtrlU, 0)
	pt.typeText("org.example:owner")
	pt.press(tcell.KeyEnter, 0)
	pt.waitFor("value dialog", func() bool { return pt.hasPage("AddPropertyValueDialog") })
	pt.waitFor("command shown", func() bool {
		// the description is word-wrapped
		return strings.Contains(pt.screenText(), "org.example:owner=<value>")
	})
	pt.typeText("markus")
	pt.press(tcell.KeyEnter, 0)

	pt.waitFor("added, reloaded and selected", func() bool {
		return pt.value("org.example:owner") == "markus (local)" && pt.selectedName() == "org.example:owner"
	})
	pt.mu.Lock()
	defer pt.mu.Unlock()
	assert.Equal(t, []string{"pool/data org.example:owner=markus"}, pt.set)
	assert.Equal(t, 1, pt.onChanged)
}

func TestDatasetPropertiesDialog_AddExistingUserPropertyEditsIt(t *testing.T) {
	pt := newPropertiesTest(t, false)

	pt.press(tcell.KeyRune, 'a')
	pt.waitFor("name dialog", func() bool { return pt.hasPage("AddPropertyNameDialog") })
	pt.typeText("org.example:note")
	pt.press(tcell.KeyEnter, 0)

	pt.waitFor("edit dialog", func() bool { return pt.hasPage("EditPropertyDialog") })
	assert.False(t, pt.hasPage("AddPropertyValueDialog"))
}

func TestDatasetPropertiesDialog_RemoveUserProperty(t *testing.T) {
	pt := newPropertiesTest(t, false)
	pt.selectProperty("org.example:note")

	pt.press(tcell.KeyRune, 'i')
	pt.waitFor("confirmation", func() bool { return pt.hasPage("InheritPropertyDialog") })
	pt.waitFor("remove wording", func() bool {
		return strings.Contains(pt.screenText(), "Remove the user property org.example:note")
	})
}
