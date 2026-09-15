package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/jasperan/orahermes-agent/gotui/internal/skin"
)

func testTheme() *Theme { return NewTheme(skin.Default()) }

// TestRenderersSurviveEveryNarrowWidth is the regression guard for the defect
// this package must never have: negative width arithmetic reaching
// strings.Repeat or lipgloss padding, which panics. Terminals can be resized to
// anything, including 1 column.
func TestRenderersSurviveEveryNarrowWidth(t *testing.T) {
	theme := testTheme()
	body := "a line of text that is longer than any of these widths"

	for width := -5; width <= 40; width++ {
		// Each call must simply not panic. The return values are checked for
		// gross safety below.
		_ = theme.Pane("Title", body, width)
		_ = theme.PaneError("Problem", errSentinel{}, width)
		_ = theme.Header("Hermes Agent", "model · skin default", width)
		_ = theme.Footer("enter send · esc menu", width)
		_ = theme.Stat("label", "value")
	}
}

type errSentinel struct{}

func (errSentinel) Error() string { return "boom" }

// TestPaneIsBareBelowTheRenderThreshold pins the behaviour at tiny widths: the
// border and padding are dropped so the text is not pushed off screen.
func TestPaneIsBareBelowTheRenderThreshold(t *testing.T) {
	theme := testTheme()
	rendered := theme.Pane("Title", "body", MinRenderWidth-1)

	if strings.Contains(rendered, "Title") {
		t.Fatal("at tiny widths the pane must not draw its title chrome")
	}
	if strings.Contains(rendered, "─") || strings.ContainsAny(rendered, "╭╮╰╯") {
		t.Fatal("at tiny widths the pane must not draw a border")
	}
}

// TestPaneDrawsABorderAtNormalWidths is the positive half.
func TestPaneDrawsABorderAtNormalWidths(t *testing.T) {
	theme := testTheme()
	rendered := theme.Pane("Status", "database: up", 60)

	if !strings.Contains(rendered, "Status") {
		t.Fatal("the title must be rendered")
	}
	if !strings.Contains(rendered, "database: up") {
		t.Fatal("the body must be rendered")
	}
	if !strings.ContainsAny(rendered, "╭╮╰╯") {
		t.Fatal("a normal-width pane must have a rounded border")
	}
}

// TestWrapNeverExceedsTheWidth is the property the transcript depends on: a
// single long token (a URL, a hash) must be hard-split, not overflow the pane.
func TestWrapNeverExceedsTheWidth(t *testing.T) {
	long := strings.Repeat("x", 200)
	inputs := []string{
		"short",
		"two words",
		long,
		"prefix " + long + " suffix",
		"multi\nline\ntext",
		strings.Repeat("word ", 60),
		"",
		"\n\n",
	}

	for width := 1; width <= 40; width++ {
		for _, input := range inputs {
			wrapped := Wrap(input, width)
			for _, line := range strings.Split(wrapped, "\n") {
				if got := len([]rune(line)); got > width {
					t.Fatalf("Wrap(%q, %d) produced a %d-rune line: %q", input, width, got, line)
				}
			}
		}
	}
}

// TestWrapPreservesNewlines keeps explicit line breaks, which transcript entries
// rely on.
func TestWrapPreservesNewlines(t *testing.T) {
	got := Wrap("first\nsecond\nthird", 40)
	if strings.Count(got, "\n") != 2 {
		t.Fatalf("newlines were not preserved: %q", got)
	}
	if !strings.Contains(got, "first") || !strings.Contains(got, "third") {
		t.Fatalf("content lost: %q", got)
	}
}

// TestWrapWithNonPositiveWidthIsAPassthrough avoids an infinite loop when a
// caller passes a degenerate width.
func TestWrapWithNonPositiveWidthIsAPassthrough(t *testing.T) {
	for _, width := range []int{0, -1, -100} {
		got := Wrap("unchanged text", width)
		if got != "unchanged text" {
			t.Fatalf("Wrap at width %d = %q", width, got)
		}
	}
}

// TestTruncateIsRuneSafe is the guard against cutting a multi-byte character in
// half, which produces invalid UTF-8 in the terminal.
func TestTruncateIsRuneSafe(t *testing.T) {
	// The skin's own glyphs are multi-byte; so is the ellipsis Truncate adds.
	text := "⚕ Hermes — résumé ✓"

	for n := 1; n <= len([]rune(text))+2; n++ {
		got := Truncate(text, n)
		if len([]rune(got)) > n {
			t.Fatalf("Truncate(%q, %d) = %q, which is %d runes", text, n, got, len([]rune(got)))
		}
		if strings.ContainsRune(got, '\uFFFD') {
			t.Fatalf("Truncate(%q, %d) = %q, which contains a replacement character", text, n, got)
		}
	}
}

func TestTruncateEdgeCases(t *testing.T) {
	if got := Truncate("hello", 0); got != "" {
		t.Fatalf("Truncate(0) = %q, want empty", got)
	}
	if got := Truncate("hello", -1); got != "" {
		t.Fatalf("Truncate(-1) = %q, want empty", got)
	}
	if got := Truncate("hello", 10); got != "hello" {
		t.Fatalf("Truncate(10) = %q, want the input unchanged", got)
	}
	if got := Truncate("hello", 1); len([]rune(got)) != 1 {
		t.Fatalf("Truncate(1) = %q, want one rune", got)
	}
}

func TestPad(t *testing.T) {
	if got := Pad("ab", 5); got != "ab   " {
		t.Fatalf("Pad = %q", got)
	}
	if got := Pad("abcdef", 3); got != "abcdef" {
		t.Fatalf("Pad must not truncate, got %q", got)
	}
	// Rune-safe: a multi-byte glyph counts as one column.
	if got := Pad("⚕", 3); len([]rune(got)) != 3 {
		t.Fatalf("Pad = %q", got)
	}
}

// TestHeaderShowsIdentityWhenThereIsNoRoomForContext covers the narrow case
// where both halves cannot fit.
func TestHeaderShowsIdentityWhenThereIsNoRoomForContext(t *testing.T) {
	theme := testTheme()
	rendered := theme.Header("Hermes Agent", "a very long context string", MinRenderWidth-2)
	if !strings.Contains(rendered, "Hermes") {
		t.Fatalf("the identity must survive when the context cannot: %q", rendered)
	}
}

func TestHeaderAlignsRightAtNormalWidth(t *testing.T) {
	theme := testTheme()
	rendered := theme.Header("Hermes Agent", "skin default", 60)
	if !strings.Contains(rendered, "Hermes Agent") || !strings.Contains(rendered, "skin default") {
		t.Fatalf("both halves must be present: %q", rendered)
	}
	if len(strings.Split(rendered, "\n")) != 1 {
		t.Fatalf("the header must be a single line: %q", rendered)
	}
}

func TestFooterTruncatesRatherThanOverflowing(t *testing.T) {
	theme := testTheme()
	// The rendered string carries ANSI escapes, so the visible width is what
	// must fit — lipgloss.Width strips the styling before measuring.
	rendered := theme.Footer("a very long hint that cannot possibly fit", 10)
	if got := lipgloss.Width(rendered); got > 10 {
		t.Fatalf("footer is %d columns wide: %q", got, rendered)
	}
	if theme.Footer("hint", 0) != "" {
		t.Fatal("a zero-width footer must render nothing")
	}
}

func TestFormatCount(t *testing.T) {
	if got := FormatCount(1, "thread"); got != "1 thread" {
		t.Fatalf("got %q", got)
	}
	if got := FormatCount(0, "thread"); got != "0 threads" {
		t.Fatalf("got %q", got)
	}
	if got := FormatCount(3, "thread"); got != "3 threads" {
		t.Fatalf("got %q", got)
	}
}

// TestClampWidthNeverReturnsBelowTheMinimum is the arithmetic guard.
func TestClampWidthNeverReturnsBelowTheMinimum(t *testing.T) {
	for _, width := range []int{-100, 0, 1, 11, 12, 500} {
		if got := clampWidth(width, MinPaneWidth); got < MinPaneWidth {
			t.Fatalf("clampWidth(%d, %d) = %d", width, MinPaneWidth, got)
		}
	}
}
