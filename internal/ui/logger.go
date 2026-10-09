package ui

import "github.com/fatih/color"

var apiLogColor = color.New(color.FgHiBlack)

// APILog formats and prints a log message from the API server.
func APILog(format string, a ...interface{}) {
	prefix := apiLogColor.Sprint("[API]")
	Systemln(prefix+" "+format, a...)
}
