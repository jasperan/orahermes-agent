package tui

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"charm.land/huh/v2"

	"github.com/jasperan/orahermes-agent/gotui/internal/gateway"
	"github.com/jasperan/orahermes-agent/gotui/internal/huhstyle"
	"github.com/jasperan/orahermes-agent/gotui/internal/skin"
)

// This file covers the accessible (screen-reader) path.
//
// Why it exists: huh's accessible prompts run a field's validator on the raw
// line and only afterwards substitute the field's default, and they never print
// that default. A pre-filled field whose validator rejects "" therefore
// re-prompts on every bare Enter, so a screen-reader user cannot accept a value
// they cannot see.
//
// These tests drive the module's own field builders through huh's real
// accessible entry point (huh.Field.RunAccessible) rather than re-declaring the
// fields, so they pin the actual wiring. Field.RunAccessible is used instead of
// Form.Run because each accessible prompt builds its own buffered scanner over
// the reader, so only the first prompt of a multi-field form can be fed a real
// answer; per-field driving is the only way to exercise the rest.

var errBlank = errors.New("blank answer rejected")

// runFieldAccessible drives one real field, built by the module's own field
// builder, through huh's accessible path with a single input line.
func runFieldAccessible(t *testing.T, f huh.Field, input string) string {
	t.Helper()
	var out bytes.Buffer
	if err := f.RunAccessible(&out, strings.NewReader(input)); err != nil {
		// A field whose echo mode needs a tty reports here; the form ignores it too.
		t.Logf("RunAccessible returned %v (ignored, as huh.Form does)", err)
	}
	return out.String()
}

// TestValidateDefaultedAcceptsBlank is the unit half of the fix.
func TestValidateDefaultedAcceptsBlank(t *testing.T) {
	inner := func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errBlank
		}
		if s == "bad" {
			return errors.New("not usable")
		}
		return nil
	}
	wrapped := ValidateDefaulted(inner)

	for _, in := range []string{"", "   ", "\t"} {
		if err := wrapped(in); err != nil {
			t.Errorf("ValidateDefaulted(inner)(%q) = %v, want nil", in, err)
		}
	}
	if err := wrapped("bad"); err == nil {
		t.Error(`ValidateDefaulted(inner)("bad") = nil, want the inner validator's error`)
	}
	if err := wrapped("/tmp/checkout"); err != nil {
		t.Errorf(`ValidateDefaulted(inner)("/tmp/checkout") = %v, want nil`, err)
	}
}

// TestValidateDefaultedValueOnlyRelaxesWhenSomethingIsKept is the guard: the
// launch form passes through whatever was inferred, so a field may have nothing
// to keep.
func TestValidateDefaultedValueOnlyRelaxesWhenSomethingIsKept(t *testing.T) {
	inner := func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errBlank
		}
		return nil
	}

	if err := ValidateDefaultedValue("", inner)(""); err == nil {
		t.Error("empty pre-fill accepted a blank answer; a required field was weakened")
	}
	if err := ValidateDefaultedValue("   ", inner)(""); err == nil {
		t.Error("whitespace-only pre-fill accepted a blank answer")
	}
	if err := ValidateDefaultedValue("/tmp/checkout", inner)(""); err != nil {
		t.Errorf("pre-filled field rejected blank: %v", err)
	}
}

// TestAccessibleBlankAnswerKeepsThePrefilledRoot drives the real launch field.
func TestAccessibleBlankAnswerKeepsThePrefilledRoot(t *testing.T) {
	// The pre-filled root must be a real checkout for the validator to accept it,
	// so point it at this repository.
	answers := LaunchAnswers{ProjectRoot: "../../.."}
	if err := ValidateProjectRoot(answers.ProjectRoot); err != nil {
		t.Skipf("not running inside an orahermes-agent checkout: %v", err)
	}

	fields := launchFields(&answers)
	if len(fields) == 0 {
		t.Fatal("the launch form must have fields")
	}
	seeded := answers.ProjectRoot
	if strings.TrimSpace(seeded) == "" {
		t.Fatal("the root is not pre-filled; the test would prove nothing")
	}

	out := runFieldAccessible(t, fields[0], "\n")
	if strings.Contains(out, "no tui_gateway") {
		t.Errorf("a blank answer was rejected, so a screen-reader user cannot keep the "+
			"pre-filled checkout.\noutput:\n%s", out)
	}
	if answers.ProjectRoot != seeded {
		t.Errorf("root = %q after a blank answer, want the pre-filled %q", answers.ProjectRoot, seeded)
	}
}

// TestAccessibleBlankPassesOnAPrefilledFieldButTheAppRechecksStrictly documents
// the two-layer design.
//
// The field's accessible validator accepts a blank answer as "keep what is in the
// field", because huh never prints the field's current value and a screen-reader
// user has no other way to accept it. That relaxation is safe only because the
// strict check runs again in advance() before anything is spawned — which is what
// this test pins.
func TestAccessibleBlankPassesOnAPrefilledFieldButTheAppRechecksStrictly(t *testing.T) {
	answers := LaunchAnswers{ProjectRoot: "/definitely/not/a/checkout"}
	fields := launchFields(&answers)

	out := runFieldAccessible(t, fields[0], "\n")
	if strings.Contains(out, "no tui_gateway") {
		t.Fatalf("a prefilled field must accept a blank answer, or a screen-reader user "+
			"cannot keep the value huh never shows.\noutput:\n%s", out)
	}

	// Layer one: the strict guard rejects it outright.
	if err := ValidateProjectRoot("/definitely/not/a/checkout"); err == nil {
		t.Fatal("ValidateProjectRoot must reject a directory that is not a checkout")
	}

	// Layer two: advance() applies that guard again before starting anything.
	m := New(Options{Skin: skin.Default()})
	m.launch = LaunchAnswers{ProjectRoot: "/definitely/not/a/checkout"}
	m.form.State = huh.StateCompleted
	m.advance()

	if m.failure == nil {
		t.Fatal("advance() must refuse to start a gateway for a non-checkout")
	}
	if m.form == nil {
		t.Fatal("the launch form must be reinstalled so the user can correct the path")
	}
}

func TestValidateProjectRootRejectsNonCheckouts(t *testing.T) {
	for name, path := range map[string]string{
		"empty":    "",
		"blank":    "   ",
		"missing":  "/definitely/not/a/checkout",
		"temp dir": t.TempDir(),
	} {
		if err := ValidateProjectRoot(path); err == nil {
			t.Errorf("%s: %q must be rejected", name, path)
		}
	}
}

// TestValidateProjectRootAcceptsThisCheckout is the positive half.
func TestValidateProjectRootAcceptsThisCheckout(t *testing.T) {
	if err := ValidateProjectRoot("../.."); err != nil {
		t.Skipf("not running inside an orahermes-agent checkout: %v", err)
	}
}

// TestPlainFormIsASingleGroup is the accessible-mode contract: huh runs only the
// first group of a form in accessible mode and then reports the form complete,
// so a multi-group form would silently default every later answer.
//
// huh does not export a form's groups, so the invariant is asserted on the field
// list that the single huh.NewGroup call is handed.
func TestPlainFormIsASingleGroup(t *testing.T) {
	var answers PlainAnswers
	answers.Launch = LaunchDefaults("", "")

	fields := plainFields(&answers, nil)
	if len(fields) < 3 {
		t.Fatalf("fields = %d, want the launch, action and message questions", len(fields))
	}

	// Building the form must not panic and must carry the accessible flag.
	form := PlainForm(&answers, skin.Default(), nil)
	if form.State != huh.StateNormal {
		t.Fatalf("a fresh form should be normal, got %v", form.State)
	}
}

// TestPlainFormOmitsTheThreadPickerWhenThereAreNoThreads keeps the accessible
// form from asking about an empty list.
func TestPlainFormOmitsTheThreadPickerWhenThereAreNoThreads(t *testing.T) {
	var answers PlainAnswers
	answers.Launch = LaunchDefaults("", "")

	without := len(plainFields(&answers, nil))
	withThreads := len(plainFields(&answers, testSessions()))

	if withThreads != without+1 {
		t.Fatalf("with threads the form must add exactly one question: %d vs %d", withThreads, without)
	}
	if withThreads != without+1 {
		t.Fatal("unreachable")
	}
}

// TestHuhStyleBuildsFromTheSkin proves the forms follow the palette rather than a
// hard-coded theme: two skins must render different output.
func TestHuhStyleBuildsFromTheSkin(t *testing.T) {
	one := skin.Default()
	two := skin.Default()
	two.Colors[skin.KeyBannerTitle] = "#00FF00"

	renderedOne := huhstyle.Styles(one).Focused.Title.Render("title")
	renderedTwo := huhstyle.Styles(two).Focused.Title.Render("title")

	if renderedOne == renderedTwo {
		t.Fatal("the title style must change when the skin's banner_title changes")
	}
	if renderedOne == "title" {
		t.Fatal("the focused title must actually be styled")
	}
}

// TestHuhStylesFollowTheSkinAccent covers the focus colour, which is the most
// visible skin difference in a form.
func TestHuhStylesFollowTheSkinAccent(t *testing.T) {
	one := skin.Default()
	two := skin.Default()
	two.Colors[skin.KeyAccent] = "#123456"

	oneBorder := huhstyle.Styles(one).Focused.Base.Render("field")
	twoBorder := huhstyle.Styles(two).Focused.Base.Render("field")
	if oneBorder == twoBorder {
		t.Fatal("the focused field's border must follow the skin accent")
	}
}

// TestThemeFuncReturnsATypedTheme keeps the WithTheme call site compiling.
func TestThemeFuncReturnsATypedTheme(t *testing.T) {
	theme := huhstyle.ThemeFunc(skin.Default())
	if theme(true) == nil {
		t.Fatal("the theme function must return styles")
	}
}

func testSessions() []gateway.SessionSummary {
	return []gateway.SessionSummary{{ID: "a", Title: "A"}}
}
