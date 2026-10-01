package util

import "github.com/rivo/tview"

// activeTextInputs contains primitives that currently capture text input without being a tview.InputField,
// e.g. a table while its filter is being typed. Only accessed on the UI thread.
var activeTextInputs = map[tview.Primitive]bool{}

// SetTextInputActive marks the given primitive as capturing text input (or not).
// Global key bindings on printable keys (like '?') must not trigger while the focused primitive captures text input.
// Must be called on the UI thread.
func SetTextInputActive(primitive tview.Primitive, active bool) {
	if active {
		activeTextInputs[primitive] = true
	} else {
		delete(activeTextInputs, primitive)
	}
}

// IsTextInputActive returns whether the given (usually the focused) primitive captures text input.
// Must be called on the UI thread.
func IsTextInputActive(primitive tview.Primitive) bool {
	if primitive == nil {
		return false
	}
	if _, ok := primitive.(*tview.InputField); ok {
		return true
	}
	return activeTextInputs[primitive]
}
