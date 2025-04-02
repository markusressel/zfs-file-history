package logging

import (
	"github.com/pterm/pterm"
	"log"
	"os"
)

const (
	LoggingRootPath = "/tmp/zfs-file-history"
	LoggingFileName = "zfs-file-history.log"
	LogFilePath     = LoggingRootPath + "/" + LoggingFileName
)

func SetDebugEnabled(enabled bool) {
	pterm.PrintDebugMessages = enabled
}

func Printf(format string, a ...interface{}) {
	writeToLogFile(format, a...)
	// pterm.Printf(format, a...)
}

func Printfln(format string, a ...interface{}) {
	writeToLogFile(format, a...)
	// pterm.Printfln(format, a...)
}

func Debug(format string, a ...interface{}) {
	writeToLogFile(format, a...)
	// pterm.Debug.Printfln(format, a...)
}

func Success(format string, a ...interface{}) {
	writeToLogFile(format, a...)
	// pterm.Success.Printfln(format, a...)
}

func Info(format string, a ...interface{}) {
	writeToLogFile(format, a...)
	// pterm.Info.Printfln(format, a...)
}

func Warning(format string, a ...interface{}) {
	writeToLogFile(format, a...)
	// pterm.Warning.Printfln(format, a...)
}

func Error(format string, a ...interface{}) {
	writeToLogFile(format, a...)
	// pterm.Error.Printfln(format, a...)
}

func FatalWithoutStacktrace(format string, a ...interface{}) {
	writeToLogFile(format, a...)
	pterm.Fatal.WithFatal(false).Printfln(format, a...)
	os.Exit(1)
}

func Fatal(format string, a ...interface{}) {
	writeToLogFile(format, a...)
	pterm.Fatal.Printfln(format, a...)
}

func writeToLogFile(format string, a ...interface{}) {
	if len(format) <= 0 {
		return
	}
	file := openLogFile()
	defer func(file *os.File) {
		_ = file.Close()
	}(file)
	log.SetOutput(file)
	log.Printf(format, a...)
}

func openLogFile() *os.File {
	err := os.MkdirAll(LoggingRootPath, 0777)
	if err != nil {
		log.Println(err)
	}
	err = os.Chmod(LoggingRootPath, 0777)
	if err != nil {
		log.Println(err)
	}
	file, err := os.OpenFile(LogFilePath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Println(err)
	}
	err = os.Chmod(LogFilePath, 0666)
	if err != nil {
		log.Println(err)
	}
	return file
}
