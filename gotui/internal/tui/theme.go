package tui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/jasperan/orahermes-agent/gotui/internal/skin"
)

// Width guards.
//
// A terminal can be resized to anything, including a handful of columns, and
// bubbletea delivers whatever the kernel reports. Every renderer in this package
// goes through these constants so no arithmetic on the window width can go
// negative — lipgloss.Padding / strings.Repeat with a negative count is a panic,
// and that is the failure this package must never have (the same defect was
// already fixed once in this workspace, in prompt-enhance's TUI).
const (
	// MinRenderWidth is the narrowest window this UI still draws a frame for.
	// Below it the UI draws a one-line notice instead of a layout.
	MinRenderWidth = 16
	// MinPaneWidth is the narrowest a bordered pane may be drawn. A lipgloss
	// rounded border needs 2 columns of furniture, and padding below adds 4.
	MinPaneWidth = 12
	// MinHeight keeps the view from collapsing to zero lines.
	MinHeight = 3
)

// Theme holds the rendered styles for one skin.
//
// It is built once per run from the announced skin rather than per frame, and
// nothing in it is a colour literal: every value traces back to
// internal/skin, which traces back to hermes_cli/skin_engine.py.
type Theme struct {
	Skin skin.Skin

	primary lipgloss.Style
	accent  lipgloss.Style
	text    lipgloss.Style
	dim     lipgloss.Style
	label   lipgloss.Style
	ok      lipgloss.Style
	warn    lipgloss.Style
	danger  lipgloss.Style

	border      lipgloss.Style
	borderFocus lipgloss.Style
}

// NewTheme builds the style set for a skin.
func NewTheme(s skin.Skin) *Theme {
	colour := func(hex string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(hex))
	}
	return &Theme{
		Skin:    s,
		primary: colour(s.Primary()),
		accent:  colour(s.Accent()),
		text:    colour(s.Text()),
		dim:     colour(s.Dim()),
		label:   colour(s.Label()),
		ok:      colour(s.OK()),
		warn:    colour(s.Warn()),
		danger:  colour(s.Error()),

		border:      lipgloss.NewStyle().Foreground(lipgloss.Color(s.Border())),
		borderFocus: lipgloss.NewStyle().Foreground(lipgloss.Color(s.Accent())),
	}
}

// Pane renders a titled panel: rounded border, padding, accent title.
//
// At or below MinRenderWidth the border and padding are dropped and the body is
// returned bare, because the furniture would consume the entire window and
// leave the content outside it.
func (t *Theme) Pane(title, body string, width int) string {
	if width < MinRenderWidth {
		return t.dim.Render(Truncate(strings.ReplaceAll(body, "\n", " "), max(width, 1)))
	}

	inner := clampWidth(width, MinPaneWidth)
	bordered := t.border.Width(inner).Border(lipgloss.RoundedBorder()).Padding(0, 1)

	heading := ""
	if strings.TrimSpace(title) != "" {
		heading = t.primary.Bold(true).Render(title) + "\n"
	}
	return bordered.Render(heading + t.text.Render(body))
}

// PaneError renders the failure pane.
//
// A failed RPC or a stopped gateway is a normal state in this front-end, not a
// crash: the user gets the cause and a way onward.
func (t *Theme) PaneError(title string, err error, width int) string {
	message := "unknown error"
	if err != nil {
		message = err.Error()
	}
	return t.Pane(title, t.danger.Render(message), width)
}

// Header is the persistent identity bar: agent name left, context right.
func (t *Theme) Header(left, right string, width int) string {
	if width < MinRenderWidth {
		return t.accent.Bold(true).Render(Truncate(left, max(width, 1)))
	}
	title := t.accent.Bold(true).Render(left)
	tail := ""
	if strings.TrimSpace(right) != "" {
		tail = t.dim.Render(right)
	}
	gap := width - lipgloss.Width(title) - lipgloss.Width(tail) - 2
	if gap < 1 {
		// Not enough room for both: the identity wins over the context.
		return Truncate(left, max(width, 1))
	}
	return title + strings.Repeat(" ", gap) + tail
}

// Footer is the key-hint bar.
func (t *Theme) Footer(hint string, width int) string {
	if width < 1 {
		return ""
	}
	return t.dim.Render(Truncate(hint, width))
}

// Stat renders one "label: value" line.
func (t *Theme) Stat(label, value string) string {
	return t.dim.Render(label+": ") + t.text.Render(value)
}

// StatOK renders a "label: value" line in the success colour.
func (t *Theme) StatOK(label, value string) string {
	return t.dim.Render(label+": ") + t.ok.Render(value)
}

// Label renders a section label.
func (t *Theme) Label(body string) string { return t.label.Render(body) }

// Dim renders low-emphasis text.
func (t *Theme) Dim(body string) string { return t.dim.Render(body) }

// Text renders body text.
func (t *Theme) Text(body string) string { return t.text.Render(body) }

// Accent renders emphasised text.
func (t *Theme) Accent(body string) string { return t.accent.Render(body) }

// Danger renders error text.
func (t *Theme) Danger(body string) string { return t.danger.Render(body) }

// Wrap wraps text to width columns on word boundaries.
//
// It is rune-safe and never returns a line wider than width, which is what the
// transcript renderer needs: a single long token (a URL, a hash) must be
// hard-split rather than overflowing the pane.
func Wrap(text string, width int) string {
	if width < 1 {
		return text
	}

	var lines []string
	for _, logical := range strings.Split(text, "\n") {
		if logical == "" {
			lines = append(lines, "")
			continue
		}
		remaining := logical
		for len([]rune(remaining)) > width {
			runes := []rune(remaining)
			// Prefer the last space at or before the limit.
			cut := -1
			for i := width; i > 0; i-- {
				if runes[i-1] == ' ' || runes[i-1] == '\t' {
					cut = i - 1
					break
				}
			}
			if cut <= 0 {
				// No breakable space: hard-split so the line cannot overflow.
				cut = width
			}
			lines = append(lines, strings.TrimRight(string(runes[:cut]), " \t"))
			remaining = strings.TrimLeft(string(runes[cut:]), " \t")
		}
		lines = append(lines, remaining)
	}
	return strings.Join(lines, "\n")
}

// Truncate shortens s to at most n runes, marking a cut with an ellipsis.
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n == 1 {
		return string(runes[:1])
	}
	return string(runes[:n-1]) + "…"
}

// Pad right-pads s to n runes.
func Pad(s string, n int) string {
	length := len([]rune(s))
	if length >= n {
		return s
	}
	return s + strings.Repeat(" ", n-length)
}

// clampWidth returns width, but never below min and never above a sane ceiling.
func clampWidth(width, min int) int {
	if width < min {
		return min
	}
	return width
}

// max returns the larger of two ints.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// FormatCount renders a count with a singular/plural noun.
func FormatCount(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
