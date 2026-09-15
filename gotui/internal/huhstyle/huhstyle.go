// Package huhstyle adapts charmbracelet/huh forms to the orahermes-agent skin.
//
// The colours are not declared here. They are read from the skin the gateway
// announced on gateway.ready (see internal/skin), which is the same source
// hermes_cli/skin_engine.py gives the Python CLI and the Ink TUI. A user who
// switches skins gets the same palette in this front-end, and there is exactly
// one place to change a colour for the whole project.
package huhstyle

import (
	"os"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"golang.org/x/term"

	"github.com/jasperan/orahermes-agent/gotui/internal/skin"
)

// Accessible reports whether the user requested screen-reader mode.
//
// Wire it into every form: huh then swaps the TUI for plain prompts, which is
// the only sane rendering when a screen reader is driving the terminal.
func Accessible() bool { return os.Getenv("ACCESSIBLE") != "" }

// Interactive reports whether stdin is a terminal, i.e. whether a form can
// actually be driven by a human.
//
// Callers must route around a prompt when this is false rather than into it: a
// huh form attached to a pipe or a cron job can never be answered.
//
// This must use term.IsTerminal, NOT an os.ModeCharDevice probe: /dev/null IS a
// character device, so a char-device check reports "interactive" for exactly
// the case it is supposed to reject, and a form would then hang forever on
// /dev/null.
func Interactive() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// NOTE ON ACCESSIBILITY, measured against huh v2.0.3:
//
// `WithAccessible` is only consulted inside Form.RunWithContext (form.go:676).
// Form.Init/Update/View never read the flag, so an *embedded* form (driven as a
// Bubble Tea component) renders identically with and without it — screen-reader
// support is real only for a form run standalone via Form.Run().
//
// Pass WithAccessible(huhstyle.Accessible()) anyway (it is the correct intent
// and costs nothing), but never claim accessible support for an embedded form.
// Flows that must be accessible are routed to the standalone Run path in
// main.go instead.

// Styles builds skin-compliant form styles.
//
// Design rules applied (the same ones the CLI's skin engine enforces):
//  1. rounded borders everywhere
//  2. focus is a border *colour* change, never a thickness change
//  3. selection is inverted (accent background, background-coloured text, bold)
//  4. semantic colours only where the content is genuinely semantic
//  5. titles are bold and title-coloured
func Styles(s skin.Skin) *huh.Styles {
	t := huh.ThemeBase(true)

	colBG := lipgloss.Color(s.StatusBG())
	colElevated := lipgloss.Color(s.Border())
	colText := lipgloss.Color(s.Text())
	colSubtext := lipgloss.Color(s.Dim())
	colMuted := lipgloss.Color(s.Dim())
	colDim := lipgloss.Color(s.Border())
	colAccent := lipgloss.Color(s.Accent())
	colTitle := lipgloss.Color(s.Primary())
	colError := lipgloss.Color(s.Error())
	colInfo := lipgloss.Color(s.Primary())

	// Focused field: rounded box, accent border (rules 1 + 2).
	t.Focused.Base = lipgloss.NewStyle().
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent)
	t.Focused.Card = t.Focused.Base

	// Titles are the focal hierarchy (rule 5).
	t.Focused.Title = t.Focused.Title.Bold(true).Foreground(colTitle)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Bold(true).Foreground(colTitle)
	t.Focused.Description = t.Focused.Description.Foreground(colSubtext)
	t.Focused.Directory = t.Focused.Directory.Foreground(colInfo)
	t.Focused.File = t.Focused.File.Foreground(colText)

	// Errors are the one place a semantic colour is mandatory (rule 4).
	t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(colError)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(colError)

	// Select: the cursor row is an inverted selection (rule 3).
	t.Focused.SelectSelector = t.Focused.SelectSelector.
		Foreground(colBG).
		Background(colAccent).
		Bold(true)
	t.Focused.Option = t.Focused.Option.Foreground(colText)
	t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(colAccent)
	t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(colAccent)

	// Multi-select: chosen items are accent-coloured, not success-coloured
	// (selection is not a success event).
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(colAccent)
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(colAccent).Bold(true)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(colAccent).Bold(true)
	t.Focused.UnselectedPrefix = t.Focused.UnselectedPrefix.Foreground(colMuted)
	t.Focused.UnselectedOption = t.Focused.UnselectedOption.Foreground(colText)

	// Confirm buttons: inverted accent for the focused choice (rule 3). A
	// blurred confirm must NOT keep the accent fill, or two fields would both
	// read as focused and rule 2 (focus is a colour change) would be broken.
	t.Focused.FocusedButton = t.Focused.FocusedButton.
		Foreground(colBG).
		Background(colAccent).
		Bold(true)
	t.Focused.BlurredButton = t.Focused.BlurredButton.
		Foreground(colText).
		Background(colElevated)

	// Inputs.
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(colInfo)
	t.Focused.TextInput.CursorText = t.Focused.TextInput.CursorText.
		Foreground(colBG).
		Background(colInfo)
	t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(colMuted)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(colAccent)
	t.Focused.TextInput.Text = t.Focused.TextInput.Text.Foreground(colText)

	// Blurred fields keep their structure but lose the accent (rule 2).
	t.Blurred = t.Focused
	t.Blurred.Base = t.Blurred.Base.BorderForeground(colDim)
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.FocusedButton = t.Blurred.BlurredButton
	t.Blurred.MultiSelectSelector = lipgloss.NewStyle().SetString("  ")
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()

	// Help footer: keys readable, descriptions muted.
	t.Help.Ellipsis = t.Help.Ellipsis.Foreground(colMuted)
	t.Help.ShortKey = t.Help.ShortKey.Foreground(colSubtext)
	t.Help.ShortDesc = t.Help.ShortDesc.Foreground(colMuted)
	t.Help.ShortSeparator = t.Help.ShortSeparator.Foreground(colDim)
	t.Help.FullKey = t.Help.FullKey.Foreground(colSubtext)
	t.Help.FullDesc = t.Help.FullDesc.Foreground(colMuted)
	t.Help.FullSeparator = t.Help.FullSeparator.Foreground(colDim)

	t.Group.Title = t.Focused.Title
	t.Group.Description = t.Focused.Description

	return t
}

// ThemeFunc adapts Styles to huh's Theme interface so it can be passed to
// Form.WithTheme. The isDark argument is ignored: the skin decides, and the
// skin already knows whether it is a light or dark palette.
func ThemeFunc(s skin.Skin) huh.ThemeFunc {
	return func(bool) *huh.Styles { return Styles(s) }
}
