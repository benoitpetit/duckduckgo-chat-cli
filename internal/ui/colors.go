package ui

import (
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/fatih/color"
)

type colorRole uint8

const (
	roleUser colorRole = iota
	roleAssistant
	roleAccent
	roleInfo
	roleMuted
	roleSuccess
	roleWarning
	roleError
	roleForeground
)

type themedColor struct {
	role colorRole
}

func (c themedColor) color() *color.Color {
	theme := CurrentTheme()
	value := theme.Colors.Foreground
	switch c.role {
	case roleUser:
		value = theme.Colors.User
	case roleAssistant:
		value = theme.Colors.Assistant
	case roleAccent:
		value = theme.Colors.Accent
	case roleInfo:
		value = theme.Colors.Info
	case roleMuted:
		value = theme.Colors.Muted
	case roleSuccess:
		value = theme.Colors.Success
	case roleWarning:
		value = theme.Colors.Warning
	case roleError:
		value = theme.Colors.Error
	}
	r, g, b := parseHexColor(value)
	styled := color.RGB(r, g, b)
	if c.role == roleWarning || c.role == roleError || (theme.ID == ThemeMono && c.role == roleAccent) {
		styled.Add(color.Bold)
	}
	return styled
}

func parseHexColor(value string) (int, int, int) {
	value = strings.TrimPrefix(value, "#")
	parsed, err := strconv.ParseUint(value, 16, 24)
	if err != nil || len(value) != 6 {
		return 255, 255, 255
	}
	return int(parsed >> 16), int((parsed >> 8) & 0xFF), int(parsed & 0xFF)
}

func (c themedColor) Printf(format string, a ...interface{}) { c.color().Printf(format, a...) }
func (c themedColor) Sprint(a ...interface{}) string         { return c.color().Sprint(a...) }

// Semantic colors resolve their RGB values at each write so /config changes
// are visible immediately, including in messages emitted by background work.
var (
	UserColor        = themedColor{role: roleUser}
	AIColor          = themedColor{role: roleAssistant}
	SystemColor      = themedColor{role: roleInfo}
	WarningColor     = themedColor{role: roleWarning}
	ErrorColor       = themedColor{role: roleError}
	WhiteColor       = themedColor{role: roleForeground}
	PromptColor      = themedColor{role: roleAccent}
	MutedColor       = themedColor{role: roleMuted}
	AccentColor      = themedColor{role: roleAccent}
	SuccessColor     = themedColor{role: roleSuccess}
	issueHelpEnabled atomic.Bool
)

// SetIssueHelpEnabled controls whether interactive CLI errors include a hint
// for reporting the problem through /issue.
func SetIssueHelpEnabled(enabled bool) { issueHelpEnabled.Store(enabled) }

// Formatted print functions (without newlines)
func Userf(format string, a ...interface{})    { UserColor.Printf(format, a...) }
func AIf(format string, a ...interface{})      { AIColor.Printf(format, a...) }
func Systemf(format string, a ...interface{})  { SystemColor.Printf(format, a...) }
func Warningf(format string, a ...interface{}) { WarningColor.Printf(format, a...) }
func Errorf(format string, a ...interface{})   { ErrorColor.Printf(format, a...) }
func Whitef(format string, a ...interface{})   { WhiteColor.Printf(format, a...) }
func Promptf(format string, a ...interface{})  { PromptColor.Printf(format, a...) }
func Mutedf(format string, a ...interface{})   { MutedColor.Printf(format, a...) }

// Formatted print functions (with newlines)
func Userln(format string, a ...interface{})    { UserColor.Printf(format+"\n", a...) }
func AIln(format string, a ...interface{})      { AIColor.Printf(format+"\n", a...) }
func Systemln(format string, a ...interface{})  { SystemColor.Printf(format+"\n", a...) }
func Warningln(format string, a ...interface{}) { WarningColor.Printf(format+"\n", a...) }
func Errorln(format string, a ...interface{}) {
	ErrorColor.Printf(format+"\n", a...)
	if issueHelpEnabled.Load() {
		Mutedln("Tip: use /issue to report this problem.")
	}
}
func Whiteln(format string, a ...interface{}) { WhiteColor.Printf(format+"\n", a...) }
func Mutedln(format string, a ...interface{}) { MutedColor.Printf(format+"\n", a...) }
