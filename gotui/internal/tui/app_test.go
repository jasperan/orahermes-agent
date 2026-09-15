package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"charm.land/bubbletea/v2"

	"github.com/jasperan/orahermes-agent/gotui/internal/gateway"
	"github.com/jasperan/orahermes-agent/gotui/internal/skin"
)

func newTestModel(t *testing.T) *Model {
	t.Helper()
	return New(Options{Skin: skin.Default()})
}

// newChatModel builds a model that is already in the conversation, the way
// toChat() leaves it: on the chat screen with no form installed.
//
// The form matters: handleKey routes every key to an installed form first, so a
// model left holding the launch form would swallow the composer's keys. That is
// the real transition, not a test convenience — advance() and toChat() both call
// clearForm().
func newChatModel(t *testing.T) *Model {
	t.Helper()
	m := New(Options{Skin: skin.Default(), StartSession: "abc123"})
	m.scr = screenChat
	m.clearForm()
	m.width, m.height = 100, 30
	m.resize()
	// New() only records the requested thread; the live session id is assigned
	// when the gateway answers session.create/resume. The chat screen's key
	// handling refuses to submit without one, so set it here.
	m.sessionID = "abc123"
	return m
}

// TestNewStartsWithTheLaunchForm is the default entry: a plain launch asks where
// the checkout is before anything is spawned.
func TestNewStartsWithTheLaunchForm(t *testing.T) {
	m := newTestModel(t)
	if m.scr != screenLaunch {
		t.Fatalf("screen = %v, want screenLaunch", m.scr)
	}
	if m.form == nil {
		t.Fatal("the launch form must be installed")
	}
	if m.form.Init() == nil {
		// Init may legitimately return nil, so this is informational only.
		t.Log("the launch form's Init returned no command")
	}
}

// TestFlagDrivenLaunchSkipsTheForm covers the scripted entry points, which have
// already decided where to attach.
func TestFlagDrivenLaunchSkipsTheForm(t *testing.T) {
	m := New(Options{Skin: skin.Default(), StartSession: "thread-1"})
	if m.scr != screenChat {
		t.Fatalf("screen = %v, want screenChat", m.scr)
	}
	if m.form != nil {
		t.Fatal("a flag-driven launch must not show the launch form")
	}
}

// TestInitStartsTheGatewayWhenThereIsNoForm pins Init's two branches.
func TestInitStartsTheGatewayWhenThereIsNoForm(t *testing.T) {
	m := New(Options{Skin: skin.Default(), StartSession: "thread-1"})
	if m.Init() == nil {
		t.Fatal("Init must return the gateway-start command when no form is shown")
	}
}

// TestWindowSizeNeverPanicsAtAnySize is the resize guard. A terminal can be
// resized to 1x1 and every downstream renderer must cope.
func TestWindowSizeNeverPanicsAtAnySize(t *testing.T) {
	m := newTestModel(t)

	for width := -3; width <= 30; width++ {
		for height := -3; height <= 12; height++ {
			m.Update(tea.WindowSizeMsg{Width: width, Height: height})
			// Rendering immediately after a resize is the case that used to
			// panic in a sibling project's TUI.
			_ = m.View()
		}
	}
}

// TestViewSurvivesEveryScreenAtNarrowWidths exercises the chat transcript maths
// with a populated buffer, which is where the height budget is computed.
func TestViewSurvivesEveryScreenAtNarrowWidths(t *testing.T) {
	screens := []screen{screenLaunch, screenMenu, screenSessions, screenChat, screenStatus, screenTools, screenApproval, screenClarify, screenSecret, screenReadout}

	for _, scr := range screens {
		for width := 1; width <= 25; width++ {
			m := newTestModel(t)
			m.scr = scr
			m.width = width
			m.height = 6
			m.sessionID = "abc123"
			m.append(RoleYou, "a question")
			m.append(RoleAssistant, "a considerably longer answer that must wrap")
			m.streaming = "partial output still arriving"
			m.busy = "Thinking"
			m.transcript = append(m.transcript, Entry{Role: RoleStatus, Text: "a status note"})
			m.readout = "database: up\ntools: 12"
			m.toolsets = []gateway.Toolset{{Name: "core", Enabled: true, Description: "the core toolset"}}
			_ = m.View()
		}
	}
}

// TestKeyReleasesDoNotFireBindings is the regression guard for the bug recorded
// in this workspace: bubbletea v2 delivers key PRESSES and RELEASES, and acting
// on both fired every binding twice.
func TestKeyReleasesDoNotFireBindings(t *testing.T) {
	m := newChatModel(t)
	m.composer.SetValue("hello")
	m.composer.Focus()

	before := len(m.transcript)

	// A release for the submit key must do nothing at all. If releases were
	// routed like presses, this would append the message.
	if _, cmd := m.Update(tea.KeyReleaseMsg(tea.Key{Code: tea.KeyEnter})); cmd != nil {
		t.Fatal("a key release must not produce a command")
	}
	if got := len(m.transcript); got != before {
		t.Fatalf("a key release appended %d transcript entries", got-before)
	}
	if m.composer.Value() != "hello" {
		t.Fatalf("a key release mutated the composer: %q", m.composer.Value())
	}
}

// TestEnterSubmitsAndClearsTheComposer covers the composer contract.
func TestEnterSubmitsAndClearsTheComposer(t *testing.T) {
	m := newChatModel(t)
	m.composer.SetValue("  what is the plan?  ")
	m.composer.Focus()

	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if cmd == nil {
		t.Fatal("enter with a non-empty composer must issue a command")
	}
	if m.composer.Value() != "" {
		t.Fatalf("the composer must be cleared, got %q", m.composer.Value())
	}
	if len(m.transcript) != 1 || m.transcript[0].Role != RoleYou {
		t.Fatalf("the message must be echoed once, got %+v", m.transcript)
	}
	if m.transcript[0].Text != "what is the plan?" {
		t.Fatalf("the message must be trimmed, got %q", m.transcript[0].Text)
	}
}

// TestEnterOnAnEmptyComposerDoesNothing avoids sending blank turns.
func TestEnterOnAnEmptyComposerDoesNothing(t *testing.T) {
	m := newChatModel(t)
	m.composer.SetValue("   ")
	m.composer.Focus()

	if _, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})); cmd != nil {
		t.Fatal("enter on a blank composer must not issue a command")
	}
	if len(m.transcript) != 0 {
		t.Fatalf("nothing should be appended, got %+v", m.transcript)
	}
}

// TestEnterDuringAnActiveTurnIsRefused keeps two turns from overlapping, which
// the gateway would reject anyway with "session busy".
func TestEnterDuringAnActiveTurnIsRefused(t *testing.T) {
	m := newChatModel(t)
	m.busy = "Thinking"
	m.composer.SetValue("a second question")
	m.composer.Focus()

	if _, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})); cmd != nil {
		t.Fatal("a second turn must not be submitted while one is running")
	}
	if m.notice == "" {
		t.Fatal("the user must be told why nothing happened")
	}
	if len(m.transcript) != 0 {
		t.Fatalf("nothing should be appended, got %+v", m.transcript)
	}
}

// TestCtrlCStopsTheTurnBeforeQuitting mirrors the CLI: mid-turn, the first
// ctrl+c interrupts rather than exits.
func TestCtrlCStopsTheTurnBeforeQuitting(t *testing.T) {
	m := newChatModel(t)
	m.busy = "Thinking"

	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if cmd == nil {
		t.Fatal("ctrl+c mid-turn must issue the interrupt command")
	}
	if m.quitRequested {
		t.Fatal("the first ctrl+c mid-turn must not quit")
	}
	if m.busy != "Thinking" {
		t.Fatal("the turn state must not be cleared locally before the gateway confirms")
	}
}

func TestCtrlCQuitsWhenIdle(t *testing.T) {
	m := newChatModel(t)

	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if cmd == nil {
		t.Fatal("ctrl+c when idle must quit")
	}
	if !m.quitRequested {
		t.Fatal("quit must be recorded so the event pump stops re-arming")
	}
}

// TestEventRouting drives the full streaming sequence and asserts the
// transcript outcome.
func TestEventRouting(t *testing.T) {
	m := newTestModel(t)

	m.handleEvent(gateway.Event{Type: gateway.EventMessageStart})
	if m.busy == "" {
		t.Fatal("message.start must mark the model busy")
	}

	for _, piece := range []string{"Hel", "lo ", "there"} {
		m.handleEvent(payloadEvent(t, gateway.EventMessageDelta, map[string]any{"text": piece}))
	}
	if m.streaming != "Hello there" {
		t.Fatalf("streaming = %q", m.streaming)
	}

	m.handleEvent(payloadEvent(t, gateway.EventMessageDone, map[string]any{
		"text": "Hello there", "status": "complete",
	}))
	if m.streaming != "" {
		t.Fatal("the streaming buffer must be cleared when the turn completes")
	}
	if m.busy != "" {
		t.Fatal("the model must no longer be busy")
	}
	if len(m.transcript) != 1 || m.transcript[0].Role != RoleAssistant {
		t.Fatalf("unexpected transcript: %+v", m.transcript)
	}
	if m.transcript[0].Text != "Hello there" {
		t.Fatalf("text = %q", m.transcript[0].Text)
	}
}

// TestMessageCompleteFallsBackToTheStreamedText covers a completion whose text
// field is empty, where the assembled deltas are all the UI has.
func TestMessageCompleteFallsBackToTheStreamedText(t *testing.T) {
	m := newTestModel(t)
	m.handleEvent(payloadEvent(t, gateway.EventMessageDelta, map[string]any{"text": "only deltas"}))
	m.handleEvent(payloadEvent(t, gateway.EventMessageDone, map[string]any{"text": "", "status": "complete"}))

	if len(m.transcript) != 1 || m.transcript[0].Text != "only deltas" {
		t.Fatalf("unexpected transcript: %+v", m.transcript)
	}
}

// TestMessageCompleteWarningIsSurfaced keeps a budget warning visible.
func TestMessageCompleteWarningIsSurfaced(t *testing.T) {
	m := newTestModel(t)
	m.handleEvent(payloadEvent(t, gateway.EventMessageDone, map[string]any{
		"text": "answer", "status": "complete", "warning": "budget low",
	}))

	found := false
	for _, entry := range m.transcript {
		if strings.Contains(entry.Text, "budget low") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the warning must be shown: %+v", m.transcript)
	}
}

// TestGatewayReadyAdoptsTheAnnouncedSkin is the point of the skin package: the
// palette follows the gateway, not this binary.
func TestGatewayReadyAdoptsTheAnnouncedSkin(t *testing.T) {
	m := newTestModel(t)
	if m.skin.Name != "default" {
		t.Fatalf("the starting skin should be the default, got %q", m.skin.Name)
	}

	m.handleEvent(payloadEvent(t, gateway.EventGatewayReady, map[string]any{
		"skin": map[string]any{
			"name":   "ares",
			"colors": map[string]string{"ui_accent": "#DD4A3A"},
		},
	}))

	if m.skin.Name != "ares" {
		t.Fatalf("skin = %q, want ares", m.skin.Name)
	}
	if got := m.skin.Accent(); got != "#DD4A3A" {
		t.Fatalf("accent = %q", got)
	}
}

// TestSkinChangedRebuildsTheTheme covers a mid-session skin switch.
func TestSkinChangedRebuildsTheTheme(t *testing.T) {
	m := newTestModel(t)
	before := m.theme

	m.handleEvent(payloadEvent(t, gateway.EventSkinChanged, map[string]any{
		"name":   "daylight",
		"colors": map[string]string{"ui_accent": "#2563EB"},
	}))

	if m.skin.Name != "daylight" {
		t.Fatalf("skin = %q", m.skin.Name)
	}
	if m.theme == before {
		t.Fatal("the theme must be rebuilt so the new palette takes effect")
	}
	if got := m.skin.Accent(); got != "#2563EB" {
		t.Fatalf("accent = %q", got)
	}
}

// TestUnknownEventTypesAreIgnored keeps the pump alive when the gateway grows a
// new event kind.
func TestUnknownEventTypesAreIgnored(t *testing.T) {
	m := newTestModel(t)
	m.scr = screenChat
	before := len(m.transcript)

	// Must not panic and must not be reported as an error.
	m.handleEvent(gateway.Event{Type: "subagent.spawn_requested", Payload: []byte(`{"task_id":"t"}`)})
	m.handleEvent(gateway.Event{Type: "totally.unknown"})

	if len(m.transcript) != before {
		t.Fatalf("an unknown event must not write to the transcript: %+v", m.transcript)
	}
	if m.failure != nil {
		t.Fatalf("an unknown event must not set a failure: %v", m.failure)
	}
}

// TestErrorEventIsShownAsATranscriptEntry keeps agent errors visible.
func TestErrorEventIsShownAsATranscriptEntry(t *testing.T) {
	m := newTestModel(t)
	m.busy = "Thinking"
	m.handleEvent(payloadEvent(t, gateway.EventError, map[string]any{"message": "agent init failed"}))

	if m.busy != "" {
		t.Fatal("an error must end the busy state")
	}
	found := false
	for _, entry := range m.transcript {
		if entry.Role == RoleError && strings.Contains(entry.Text, "agent init failed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the error must appear in the transcript: %+v", m.transcript)
	}
}

// TestStatusUpdateBecomesActivity keeps the "what is it doing" line fed.
func TestStatusUpdateBecomesActivity(t *testing.T) {
	m := newTestModel(t)
	m.handleEvent(payloadEvent(t, gateway.EventStatusUpdate, map[string]any{"kind": "process", "text": "Running tests"}))

	if m.activity != "Running tests" {
		t.Fatalf("activity = %q", m.activity)
	}
}

// TestApprovalRequestOpensThePrompt is the highest-stakes flow: a tool waiting
// on consent must reach the user.
func TestApprovalRequestOpensThePrompt(t *testing.T) {
	m := newChatModel(t)

	m.handleEvent(payloadEvent(t, gateway.EventApprovalReq, map[string]any{
		"command":     "rm -rf /tmp/x",
		"description": "delete scratch files",
	}))

	if m.scr != screenApproval {
		t.Fatalf("screen = %v, want screenApproval", m.scr)
	}
	if m.form == nil {
		t.Fatal("the approval form must be installed")
	}
	if m.approvalReq.Command != "rm -rf /tmp/x" {
		t.Fatalf("command = %q", m.approvalReq.Command)
	}
}

// TestApprovalFormDefaultsToDeny is the safety property: the zero value of the
// answers struct is not a valid choice, and an invalid choice is refused rather
// than sent.
func TestApprovalFormDefaultsToDeny(t *testing.T) {
	if ValidApprovalChoice("") {
		t.Fatal("an empty choice must not be valid")
	}
	if ValidApprovalChoice("yes") {
		t.Fatal("only the four backend choices are valid")
	}
	for _, choice := range []string{gateway.ApprovalOnce, gateway.ApprovalSession, gateway.ApprovalAlways, gateway.ApprovalDeny} {
		if !ValidApprovalChoice(choice) {
			t.Fatalf("%q must be accepted", choice)
		}
	}
}

// TestClarifyRequestWithChoicesAndWithout covers both payload shapes.
func TestClarifyRequestWithChoicesAndWithout(t *testing.T) {
	m := newChatModel(t)

	m.handleEvent(payloadEvent(t, gateway.EventClarifyReq, map[string]any{
		"question": "which environment?", "choices": []string{"dev", "prod"}, "request_id": "r1",
	}))
	if m.scr != screenClarify || m.form == nil {
		t.Fatalf("screen = %v, form nil = %t", m.scr, m.form == nil)
	}
	if m.clarifyReq.RequestID != "r1" {
		t.Fatalf("request id = %q", m.clarifyReq.RequestID)
	}

	m2 := newChatModel(t)
	m2.handleEvent(payloadEvent(t, gateway.EventClarifyReq, map[string]any{
		"question": "explain", "choices": nil, "request_id": "r2",
	}))
	if m2.scr != screenClarify || m2.form == nil {
		t.Fatalf("free-form clarify must also open a prompt: screen = %v", m2.scr)
	}
}

// TestSecretRequestOpensAMaskedPrompt covers the credential flow.
func TestSecretRequestOpensAMaskedPrompt(t *testing.T) {
	m := newChatModel(t)

	m.handleEvent(payloadEvent(t, gateway.EventSecretReq, map[string]any{
		"env_var": "OPENAI_API_KEY", "prompt": "Paste your key", "request_id": "s1",
	}))

	if m.scr != screenSecret || m.form == nil {
		t.Fatalf("screen = %v, form nil = %t", m.scr, m.form == nil)
	}
	if m.secretReq.EnvVar != "OPENAI_API_KEY" {
		t.Fatalf("env_var = %q", m.secretReq.EnvVar)
	}
}

// TestSudoRequestIsDistinguishedFromASecret keeps the sudo response shape
// (password) separate from the generic secret shape (value).
func TestSudoRequestIsDistinguishedFromASecret(t *testing.T) {
	m := newChatModel(t)

	m.handleEvent(payloadEvent(t, gateway.EventSudoReq, map[string]any{"request_id": "p1"}))

	if m.scr != screenSecret || m.form == nil {
		t.Fatalf("screen = %v, form nil = %t", m.scr, m.form == nil)
	}
	if m.secretReq.RequestID != "p1" {
		t.Fatalf("request id = %q", m.secretReq.RequestID)
	}
	if m.secretReq.EnvVar != "" {
		t.Fatalf("a sudo request has no env var, got %q", m.secretReq.EnvVar)
	}
}

// TestUnreadablePromptPayloadsAreReportedNotFatal keeps a malformed frame from
// killing the session.
func TestUnreadablePromptPayloadsAreReportedNotFatal(t *testing.T) {
	for name, eventType := range map[string]string{
		"approval": gateway.EventApprovalReq,
		"clarify":  gateway.EventClarifyReq,
		"sudo":     gateway.EventSudoReq,
		"secret":   gateway.EventSecretReq,
	} {
		m := newChatModel(t)
		m.handleEvent(gateway.Event{Type: eventType, Payload: []byte(`"not an object"`)})

		if m.form != nil {
			t.Errorf("%s: a malformed payload must not open a form", name)
		}
		if m.scr != screenChat {
			t.Errorf("%s: screen changed to %v", name, m.scr)
		}
	}
}

// TestTranscriptIsBounded keeps the render buffer from growing without limit.
func TestTranscriptIsBounded(t *testing.T) {
	m := newTestModel(t)
	for i := 0; i < maxTranscriptEntries+50; i++ {
		m.append(RoleAssistant, "line")
	}
	if len(m.transcript) != maxTranscriptEntries {
		t.Fatalf("transcript length = %d, want %d", len(m.transcript), maxTranscriptEntries)
	}
}

func TestAppendIgnoresBlankText(t *testing.T) {
	m := newTestModel(t)
	m.append(RoleAssistant, "   \n  ")
	if len(m.transcript) != 0 {
		t.Fatalf("blank text must not become an entry: %+v", m.transcript)
	}
}

// TestMenuContainsTheThreeImplementedSurfaces documents the deliberate scope.
func TestMenuContainsTheThreeImplementedSurfaces(t *testing.T) {
	actions := map[string]bool{}
	for _, option := range MenuOptions(false) {
		actions[option.Value] = true
	}
	for _, want := range []string{ActionResume, ActionStatus, ActionTools, ActionQuit} {
		if !actions[want] {
			t.Errorf("menu is missing %q", want)
		}
	}
	if actions[ActionBack] {
		t.Error("the back entry is only offered when a conversation is open")
	}

	withSession := map[string]bool{}
	for _, option := range MenuOptions(true) {
		withSession[option.Value] = true
	}
	if !withSession[ActionBack] {
		t.Error("an open conversation must offer a way back to it")
	}
}

// TestSessionLabelFallsBackThroughTitlePreviewAndPlaceholder keeps rows readable
// for a thread that has no title yet.
func TestSessionLabelFallsBackThroughTitlePreviewAndPlaceholder(t *testing.T) {
	cases := map[string]struct {
		row  gateway.SessionSummary
		want string
	}{
		"title":       {gateway.SessionSummary{Title: "A title", Preview: "ignored"}, "A title"},
		"preview":     {gateway.SessionSummary{Preview: "a preview"}, "a preview"},
		"placeholder": {gateway.SessionSummary{}, "(untitled)"},
	}
	for name, tc := range cases {
		got := SessionLabel(tc.row)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: label = %q, want it to contain %q", name, got, tc.want)
		}
	}
}

// TestSessionOptionsAlwaysHasAtLeastOneRow matters because huh silently ignores
// an empty Options call, which would leave the previous thread list on screen.
func TestSessionOptionsAlwaysHasAtLeastOneRow(t *testing.T) {
	if got := len(SessionOptions(nil)); got == 0 {
		t.Fatal("an empty thread list must still yield a placeholder option")
	}
	rows := SessionOptions([]gateway.SessionSummary{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}})
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
}

// TestEscClosesAnOpenForm is the regression guard for a bug that has shipped
// more than once in this workspace.
//
// charm.land/huh/v2 binds a form's Quit key to ctrl+c ONLY (huh/v2 keymap.go:
// Quit = key.WithKeys("ctrl+c")), and the only path that sets StateAborted is
// key.Matches(msg, keymap.Quit). Escape therefore never aborts a huh form: a
// shell that delegates esc to the form leaves it installed, and the now-invisible
// form silently swallows every later keystroke.
//
// The menu is where escaping is meaningful: it is opened from the conversation
// and the form itself offers "Back to the conversation", so esc must agree with
// that option rather than doing nothing.
func TestEscClosesAnOpenForm(t *testing.T) {
	m := newChatModel(t)

	m.toMenu()
	if m.scr != screenMenu {
		t.Fatalf("screen = %v, want screenMenu", m.scr)
	}
	if m.form == nil {
		t.Fatal("the menu form must be installed")
	}

	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))

	if m.form != nil {
		t.Fatal("esc left the form installed; it would keep swallowing keystrokes")
	}
	if m.scr != screenChat {
		t.Errorf("screen = %v, want screenChat: esc must back out of the menu", m.scr)
	}

	// The cancellation must be visible, using this repo's status-entry convention.
	if n := len(m.transcript); n == 0 || m.transcript[n-1].Role != RoleStatus {
		t.Errorf("esc left no status note; transcript = %+v", m.transcript)
	} else if !strings.Contains(strings.ToLower(m.transcript[n-1].Text), "clos") {
		t.Errorf("the cancellation note reads %q, want it to say what was closed", m.transcript[n-1].Text)
	}

	// The real damage of the bug is that later keys go nowhere. Prove the shell
	// is routing them again by typing into the composer.
	m.composer.Focus()
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'x', Text: "x"}))
	if got := m.composer.Value(); got != "x" {
		t.Errorf("composer = %q after esc, want %q: keys are still being swallowed", got, "x")
	}
}

// TestEscDoesNotDropABlockingRequestForm pins the other half of the contract.
//
// The approval, clarify and secret screens exist because the gateway is blocked
// waiting for an answer. Clearing the form there would strand the request: the
// screen would stay put with nothing left to answer it, and no way back. esc is
// deliberately ignored so the only ways out are answering (enter) or ctrl+c,
// which is exactly what those screens advertise.
func TestEscDoesNotDropABlockingRequestForm(t *testing.T) {
	for _, tc := range []struct {
		name    string
		event   string
		payload map[string]any
		want    screen
	}{
		{"approval", gateway.EventApprovalReq,
			map[string]any{"command": "rm -rf /tmp/x", "description": "delete scratch files"}, screenApproval},
		{"clarify", gateway.EventClarifyReq,
			map[string]any{"question": "which environment?", "choices": []string{"dev", "prod"}, "request_id": "r1"}, screenClarify},
		{"secret", gateway.EventSecretReq,
			map[string]any{"env_var": "OPENAI_API_KEY", "prompt": "Paste your key", "request_id": "s1"}, screenSecret},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newChatModel(t)
			m.handleEvent(payloadEvent(t, tc.event, tc.payload))
			if m.scr != tc.want {
				t.Fatalf("screen = %v, want %v", m.scr, tc.want)
			}
			if m.form == nil {
				t.Fatal("the request form must be installed")
			}

			m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))

			if m.form == nil {
				t.Fatal("esc dropped a form the gateway is still waiting on")
			}
			if m.scr != tc.want {
				t.Errorf("screen = %v, want %v: esc must not move a blocked request", m.scr, tc.want)
			}
		})
	}
}

// payloadEvent builds an event whose payload is the given map.
func payloadEvent(t *testing.T, eventType string, payload map[string]any) gateway.Event {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return gateway.Event{Type: eventType, Payload: encoded}
}
