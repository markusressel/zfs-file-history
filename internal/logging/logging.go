package logging

import (
	"github.com/pterm/pterm"
	"os"
)

func SetDebugEnabled(enabled bool) {
	pterm.PrintDebugMessages = enabled
}

func Printf(format string, a ...interface{}) {
	pterm.Printf(format, a...)
}

func Printfln(format string, a ...interface{}) {
	pterm.Printfln(format, a...)
}

func Debug(format string, a ...interface{}) {
	pterm.Debug.Printfln(format, a...)
}

func Success(format string, a ...interface{}) {
	pterm.Success.Printfln(format, a...)
}

func Info(format string, a ...interface{}) {
	pterm.Info.Printfln(format, a...)
}

func Warning(format string, a ...interface{}) {
	pterm.Warning.Printfln(format, a...)
}

func WarningAndNotify(title string, format string, a ...interface{}) {
	Error(format, a...)
}

func Error(format string, a ...interface{}) {
	pterm.Error.Printfln(format, a...)
}

func ErrorAndNotify(title string, format string, a ...interface{}) {
	Error(format, a...)
}

func FatalWithoutStacktrace(format string, a ...interface{}) {
	pterm.Fatal.WithFatal(false).Printfln(format, a...)
	os.Exit(1)
}

func Fatal(format string, a ...interface{}) {
	pterm.Fatal.Printfln(format, a...)
}
