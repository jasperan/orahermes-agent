// Package tui is this front-end's terminal UI: a peer client of orahermes-agent's
// Python tui_gateway, built on charm.land/bubbletea/v2 and charm.land/huh/v2.
//
// It never reimplements the agent. Sessions, tools, model calls and slash-command
// logic all belong to the gateway — the same process the shipped Ink TUI drives —
// so a Go user and an Ink user get identical results from identical state.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jasperan/orahermes-agent/gotui/internal/gateway"
	"github.com/jasperan/orahermes-agent/gotui/internal/skin"
)

// rpcTimeout bounds every request this front-end issues. A gateway that is alive
// but wedged must surface as an error state, not a freeze.
const rpcTimeout = 60 * time.Second

// eventWaitTimeout bounds one blocking read of the event stream. It is long
// because a turn legitimately runs for minutes; it exists only so the pump
// cannot block forever after the gateway dies without closing the pipe.
const eventWaitTimeout = 30 * time.Minute

// maxTranscriptEntries caps the in-memory transcript. The gateway owns the real
// history (session.history), so this is a rendering buffer, not a store.
const maxTranscriptEntries = 500

// screen identifies the active view.
type screen int

const (
	screenLaunch screen = iota
	screenMenu
	screenSessions
	screenChat
	screenStatus
	screenTools
	screenApproval
	screenClarify
	screenSecret
	screenReadout
)

// Entry roles in the transcript.
const (
	RoleYou       = "you"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
	RoleStatus    = "status"
	RoleError     = "error"
)

// Entry is one transcript line.
type Entry struct {
	Role string
	Text string
}

// Options configure a run.
type Options struct {
	// Launch is the prepared gateway invocation.
	Launch gateway.Launch
	// Skin is the palette to start with, replaced by the announced skin.
	Skin skin.Skin

	// StartSession resumes this thread before showing the chat.
	StartSession string
	// StartPrompt is submitted automatically once a session exists.
	StartPrompt string
	// StartAction jumps straight to a screen instead of the menu.
	StartAction string
}

// Model is the root bubbletea model.
type Model struct {
	width  int
	height int
	opts   Options

	scr    screen
	client *gateway.Client
	theme  *Theme
	skin   skin.Skin

	// sessionID is the live gateway session this front-end is attached to.
	sessionID string
	// historySessionID is the stored thread a session was resumed from, shown in
	// the header so a user can tell "new conversation" from "resumed".
	historySessionID string
	model            string

	sessions   []gateway.SessionSummary
	transcript []Entry
	streaming  string
	activity   string
	toolsets   []gateway.Toolset
	readout    string

	form *huh.Form

	launch   LaunchAnswers
	menu     MenuAnswers
	chosen   SessionAnswers
	approval ApprovalAnswers
	clarify  ClarifyAnswers
	secretV  SecretAnswers

	approvalReq gateway.ApprovalRequest
	clarifyReq  gateway.ClarifyRequest
	secretReq   gateway.SecretRequest

	composer textarea.Model

	// scrolling offsets the transcript view from the bottom.
	scrolling int

	busy    string
	failure error
	notice  string

	// quitRequested stops the event pump from re-arming.
	quitRequested bool
}

// New builds the root model and shows the launch form.
func New(opts Options) *Model {
	if opts.Skin.Name == "" {
		opts.Skin = skin.Default()
	}

	composer := textarea.New()
	composer.Placeholder = "Message the agent. Enter sends, ctrl+j adds a line, /help lists commands."
	composer.CharLimit = 20000
	composer.ShowLineNumbers = false
	// Enter is the submit key here, so the newline binding moves to ctrl+j /
	// alt+enter. Without this, Enter would insert a line break instead of
	// sending, which is the one composer behaviour users notice immediately.
	composer.KeyMap.InsertNewline.SetKeys("ctrl+j", "alt+enter")
	composer.Blur()

	m := &Model{
		opts:     opts,
		skin:     opts.Skin,
		width:    100,
		height:   30,
		scr:      screenLaunch,
		launch:   LaunchDefaults("", ""),
		composer: composer,
	}
	m.theme = NewTheme(m.skin)
	m.resize()

	// A flag-supplied session or prompt skips the launch form: the caller has
	// already decided where to attach.
	if opts.StartSession != "" || opts.StartPrompt != "" || opts.StartAction != "" {
		m.scr = screenChat
	}

	if m.scr == screenChat {
		m.clearForm()
	} else {
		m.form = LaunchForm(&m.launch, m.skin)
	}
	return m
}

// setForm installs a freshly built form, sizes it, and returns its first
// command. Every screen transition goes through here so no form can ever be
// left at huh's default zero width (which renders as blank lines).
func (m *Model) setForm(form *huh.Form) tea.Cmd {
	m.form = form
	m.resize()
	return m.form.Init()
}

// clearForm removes the active form.
func (m *Model) clearForm() { m.form = nil }

// cancelForm backs out of the installed form on the screens where returning is
// meaningful, and reports whether it did anything.
//
// Only screens with a real place to return to may be escaped. The launch,
// approval, clarify and secret screens are deliberately excluded. The three
// request screens exist because the gateway is blocked waiting for an answer, so
// dropping the form would strand the request: the screen would stay put with
// nothing left to answer it and no way back. The launch screen is the entry
// point, with nothing behind it. All four advertise enter/ctrl+c only, so
// ignoring esc there keeps the behaviour and the advertisement in agreement.
func (m *Model) cancelForm() (tea.Cmd, bool) {
	switch m.scr {
	case screenMenu:
		// The menu form offers "Back to the conversation"; esc agrees with it.
		m.note("Menu closed.")
		return m.toChat(), true
	case screenSessions:
		// The thread picker offers "Back to the menu".
		m.note("Closed the thread list.")
		return m.toMenu(), true
	}
	return nil, false
}

// resize applies the current window size to the form and the composer.
//
// huh.NewForm leaves the form at width 0 until something sets it, and a
// zero-width field renders as blank lines.
func (m *Model) resize() {
	if m.width < 1 {
		m.width = 1
	}
	if m.height < 1 {
		m.height = 1
	}
	if m.form != nil {
		m.form = m.form.WithWidth(max(m.width-4, MinPaneWidth))
	}
	// textarea panics on a negative width, and a zero width makes it unusable.
	m.composer.SetWidth(max(m.width-4, MinPaneWidth))
	m.composer.SetHeight(3)
}

// Init implements tea.Model.
//
// A plain launch shows the form first; a flag-driven launch goes straight to
// starting the gateway.
func (m *Model) Init() tea.Cmd {
	if m.form != nil {
		return m.form.Init()
	}
	m.busy = "Starting the gateway"
	return m.startGatewayCmd()
}

// startGatewayCmd spawns the Python gateway.
//
// The interpreter and environment come from gateway.BuildLaunch, which mirrors
// the Ink client's own resolution order; nothing about the spawn is decided here.
func (m *Model) startGatewayCmd() tea.Cmd {
	launch := m.opts.Launch
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), gateway.ReadyTimeout+30*time.Second)
		defer cancel()
		client, err := gateway.Start(ctx, launch)
		if err != nil {
			return gatewayStartedMsg{err: err}
		}
		return gatewayStartedMsg{client: client}
	}
}

// Close releases the gateway if this front-end owns it.
func (m *Model) Close() {
	if m.client != nil {
		m.client.Close()
	}
}

// --- async messages -----------------------------------------------------------------

type gatewayStartedMsg struct {
	client *gateway.Client
	err    error
}

type eventMsg struct{ event gateway.Event }
type eventClosedMsg struct{ err error }

type sessionsLoadedMsg struct {
	sessions []gateway.SessionSummary
	err      error
}
type sessionOpenedMsg struct {
	sessionID string
	model     string
	err       error
}
type statusLoadedMsg struct {
	output string
	err    error
}
type toolsLoadedMsg struct {
	toolsets []gateway.Toolset
	err      error
}
type readoutMsg struct {
	title string
	body  string
	err   error
}
type promptDoneMsg struct{ err error }
type respondDoneMsg struct {
	what string
	err  error
}

// --- commands -----------------------------------------------------------------------

// pumpEvents reads exactly one event and re-arms itself.
//
// One event per command is deliberate: bubbletea runs each Cmd on its own
// goroutine, so a loop inside one command would both hide progress and make
// cancellation impossible.
func (m *Model) pumpEvents() tea.Cmd {
	client := m.client
	if client == nil {
		return nil
	}
	return func() tea.Msg {
		if client == nil {
			return eventClosedMsg{err: errors.New("no gateway")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), eventWaitTimeout)
		defer cancel()
		event, err := client.Next(ctx)
		if err != nil {
			return eventClosedMsg{err: err}
		}
		return eventMsg{event: event}
	}
}

// startSession creates or resumes the session the chat screen needs.
func (m *Model) startSession() tea.Cmd {
	client := m.client
	if client == nil {
		return nil
	}
	sessionID := m.opts.StartSession
	cols := max(m.width, 20)

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()

		if sessionID != "" {
			var ref gateway.SessionRef
			if err := client.Call(ctx, gateway.MethodSessionResume, map[string]any{
				"session_id": sessionID,
				"cols":       cols,
			}, &ref); err != nil {
				return sessionOpenedMsg{err: err}
			}
			return sessionOpenedMsg{sessionID: ref.SessionID, model: ref.Model}
		}

		var created gateway.SessionRef
		if err := client.Call(ctx, gateway.MethodSessionCreate, map[string]any{
			"cols": cols,
		}, &created); err != nil {
			return sessionOpenedMsg{err: err}
		}
		_ = m.opts.StartPrompt
		return sessionOpenedMsg{sessionID: created.SessionID, model: created.Model}
	}
}

func loadSessionsCmd(client *gateway.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		var result gateway.SessionListResult
		if err := client.Call(ctx, gateway.MethodSessionList, map[string]any{}, &result); err != nil {
			return sessionsLoadedMsg{err: err}
		}
		return sessionsLoadedMsg{sessions: result.Sessions}
	}
}

// resumeCmd attaches to an existing thread.
func resumeCmd(client *gateway.Client, sessionID string, cols int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		var ref gateway.SessionRef
		if err := client.Call(ctx, gateway.MethodSessionResume, map[string]any{
			"session_id": sessionID,
			"cols":       cols,
		}, &ref); err != nil {
			return sessionOpenedMsg{err: err}
		}
		return sessionOpenedMsg{sessionID: ref.SessionID, model: ref.Model}
	}
}

// mostRecentCmd returns the newest human-facing thread id and resumes it.
//
// session.most_recent returns {"session_id": null} when there is nothing to
// resume, which is a normal answer and not an error to special-case elsewhere.
func mostRecentCmd(client *gateway.Client, cols int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		var ref gateway.SessionRef
		if err := client.Call(ctx, gateway.MethodSessionMostRecent, map[string]any{}, &ref); err != nil {
			return sessionOpenedMsg{err: err}
		}
		if ref.SessionID == "" {
			return sessionOpenedMsg{err: errors.New("no stored thread to resume")}
		}

		var resumed gateway.SessionRef
		if err := client.Call(ctx, gateway.MethodSessionResume, map[string]any{
			"session_id": ref.SessionID,
			"cols":       cols,
		}, &resumed); err != nil {
			return sessionOpenedMsg{err: err}
		}
		return sessionOpenedMsg{sessionID: resumed.SessionID, model: resumed.Model}
	}
}

func loadStatusCmd(client *gateway.Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		var result gateway.StatusResult
		if err := client.Call(ctx, gateway.MethodSessionStatus, map[string]any{
			"session_id": sessionID,
		}, &result); err != nil {
			return statusLoadedMsg{err: err}
		}
		return statusLoadedMsg{output: result.Output}
	}
}

func loadToolsCmd(client *gateway.Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		var result gateway.ToolsListResult
		if err := client.Call(ctx, gateway.MethodToolsList, map[string]any{
			"session_id": sessionID,
		}, &result); err != nil {
			return toolsLoadedMsg{err: err}
		}
		return toolsLoadedMsg{toolsets: result.Toolsets}
	}
}

// doctorCmd runs the repo's own `hermes doctor` through the gateway.
//
// cli.exec exists precisely so a front-end does not have to reimplement the
// Python CLI's subcommands: server.py runs `python -m hermes_cli.main <argv>`
// and returns its captured output.
func doctorCmd(client *gateway.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		var result gateway.CliExecResult
		if err := client.Call(ctx, gateway.MethodCliExec, map[string]any{
			"argv":    []string{"doctor"},
			"timeout": 540,
		}, &result); err != nil {
			return readoutMsg{title: "Doctor", err: err}
		}
		if result.Blocked {
			return readoutMsg{title: "Doctor", err: errors.New("blocked: " + result.Hint)}
		}
		if result.Code != 0 {
			return readoutMsg{
				title: "Doctor",
				body:  result.Output,
				err:   fmt.Errorf("hermes doctor exited %d", result.Code),
			}
		}
		return readoutMsg{title: "Doctor", body: result.Output}
	}
}

// submitCmd sends a prompt. The reply arrives as message.* events, not as the
// RPC result, because prompt.submit acknowledges the turn and the turn streams.
func submitCmd(client *gateway.Client, sessionID, text string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		if err := client.Call(ctx, gateway.MethodPromptSubmit, map[string]any{
			"session_id": sessionID,
			"text":       text,
		}, nil); err != nil {
			return promptDoneMsg{err: err}
		}
		return promptDoneMsg{}
	}
}

func interruptCmd(client *gateway.Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		if err := client.Call(ctx, gateway.MethodSessionInterrupt, map[string]any{
			"session_id": sessionID,
		}, nil); err != nil {
			return promptDoneMsg{err: err}
		}
		return promptDoneMsg{}
	}
}

// respondApprovalCmd answers an approval.request.
//
// The approval is resolved by session_key, which is why this call carries a
// session id and no request id (server.py approval.respond -> _sess(params)).
func respondApprovalCmd(client *gateway.Client, sessionID, choice string, all bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		if err := client.Call(ctx, gateway.MethodApprovalRespond, map[string]any{
			"session_id": sessionID,
			"choice":     choice,
			"all":        all,
		}, nil); err != nil {
			return respondDoneMsg{what: "approval", err: err}
		}
		return respondDoneMsg{what: "approval"}
	}
}

func respondClarifyCmd(client *gateway.Client, requestID, answer string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		if err := client.Call(ctx, gateway.MethodClarifyRespond, map[string]any{
			"request_id": requestID,
			"answer":     answer,
		}, nil); err != nil {
			return respondDoneMsg{what: "clarification", err: err}
		}
		return respondDoneMsg{what: "clarification"}
	}
}

func respondSudoCmd(client *gateway.Client, requestID, password string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		if err := client.Call(ctx, gateway.MethodSudoRespond, map[string]any{
			"request_id": requestID,
			"password":   password,
		}, nil); err != nil {
			return respondDoneMsg{what: "sudo password", err: err}
		}
		return respondDoneMsg{what: "sudo password"}
	}
}

func respondSecretCmd(client *gateway.Client, requestID, value string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		if err := client.Call(ctx, gateway.MethodSecretRespond, map[string]any{
			"request_id": requestID,
			"value":      value,
		}, nil); err != nil {
			return respondDoneMsg{what: "secret", err: err}
		}
		return respondDoneMsg{what: "secret"}
	}
}

// --- transcript helpers --------------------------------------------------------------

// append adds an entry, trimming the oldest so the buffer stays bounded.
func (m *Model) append(role, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	m.transcript = append(m.transcript, Entry{Role: role, Text: text})
	if len(m.transcript) > maxTranscriptEntries {
		m.transcript = m.transcript[len(m.transcript)-maxTranscriptEntries:]
	}
}

// note adds a status entry.
func (m *Model) note(text string) { m.append(RoleStatus, text) }

// toChat switches to the conversation and focuses the composer.
func (m *Model) toChat() tea.Cmd {
	m.scr = screenChat
	m.clearForm()
	m.busy = ""
	m.scrolling = 0
	return m.composer.Focus()
}

// toMenu returns to the top-level menu.
func (m *Model) toMenu() tea.Cmd {
	m.scr = screenMenu
	m.menu = MenuAnswers{}
	m.composer.Blur()
	return m.setForm(MenuForm(&m.menu, m.skin, m.menuContext()))
}

// menuContext describes where the user currently is.
func (m *Model) menuContext() string {
	switch {
	case m.sessionID != "" && m.historySessionID != "":
		return "attached to thread " + m.historySessionID
	case m.sessionID != "":
		return "in a new conversation"
	default:
		return "no conversation open"
	}
}

// --- update ------------------------------------------------------------------------

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		if m.client != nil && m.sessionID != "" {
			return m, m.resizeRemoteCmd(msg.Width)
		}
		return m, nil

	case sessionOpenedMsg:
		if msg.err != nil {
			m.failure = explain(msg.err)
			m.scr = screenMenu
			return m, m.setForm(MenuForm(&m.menu, m.skin, m.menuContext()))
		}
		m.sessionID = msg.sessionID
		m.model = msg.model
		// A session opened from a named thread is a resume, so the header must
		// name the stored thread rather than implying a fresh conversation.
		if m.opts.StartSession != "" {
			m.historySessionID = m.opts.StartSession
		}
		if m.opts.StartPrompt != "" {
			m.append(RoleYou, m.opts.StartPrompt)
			m.busy = "Sending"
			return m, tea.Batch(m.toChat(), submitCmd(m.client, m.sessionID, m.opts.StartPrompt))
		}
		return m, m.toChat()

	case sessionsLoadedMsg:
		m.busy = ""
		if msg.err != nil {
			m.failure = explain(msg.err)
			m.scr = screenMenu
			return m, m.setForm(MenuForm(&m.menu, m.skin, m.menuContext()))
		}
		m.sessions = msg.sessions
		m.chosen = SessionAnswers{}
		m.scr = screenSessions
		return m, m.setForm(SessionForm(&m.chosen, m.skin, m.sessions))

	case statusLoadedMsg:
		m.busy = ""
		if msg.err != nil {
			m.failure = explain(msg.err)
			m.scr = screenMenu
			return m, m.setForm(MenuForm(&m.menu, m.skin, m.menuContext()))
		}
		m.readout = msg.output
		m.scr = screenStatus
		var confirm ConfirmAnswers
		return m, m.setForm(ContinueForm(&confirm, m.skin, "Status", "Back to the menu?"))

	case toolsLoadedMsg:
		m.busy = ""
		if msg.err != nil {
			m.failure = explain(msg.err)
			m.scr = screenMenu
			return m, m.setForm(MenuForm(&m.menu, m.skin, m.menuContext()))
		}
		m.toolsets = msg.toolsets
		m.scr = screenTools
		var confirm ConfirmAnswers
		return m, m.setForm(ContinueForm(&confirm, m.skin, "Toolsets", "Back to the menu?"))

	case readoutMsg:
		m.busy = ""
		m.readout = msg.body
		if msg.err != nil {
			m.failure = explain(msg.err)
		}
		m.scr = screenReadout
		var confirm ConfirmAnswers
		return m, m.setForm(ContinueForm(&confirm, m.skin, msg.title, "Back to the menu?"))

	case promptDoneMsg:
		m.busy = ""
		if msg.err != nil {
			m.failure = explain(msg.err)
		}
		return m, nil

	case respondDoneMsg:
		if msg.err != nil {
			m.failure = explain(msg.err)
		} else {
			m.notice = "Sent the " + msg.what + "."
		}
		return m, tea.Batch(m.toChat())

	case gatewayStartedMsg:
		if msg.err != nil {
			m.busy = ""
			m.failure = explain(msg.err)
			m.scr = screenLaunch
			return m, m.setForm(LaunchForm(&m.launch, m.skin))
		}
		m.client = msg.client
		m.busy = ""
		m.failure = nil
		return m, tea.Batch(m.pumpEvents(), m.startSession())

	case eventMsg:
		cmd := m.handleEvent(msg.event)
		return m, tea.Batch(cmd, m.pumpEvents())

	case eventClosedMsg:
		// The gateway is gone. Say so once, and stop re-arming the pump.
		m.busy = ""
		m.quitRequested = true
		m.failure = describeGatewayExit(msg.err, m.client)
		return m, nil

	case tea.KeyPressMsg:
		// Only key PRESSES are handled. bubbletea v2 also delivers key
		// releases, and acting on both would fire every binding twice.
		return m.handleKey(msg)
	}

	if m.form == nil {
		return m, nil
	}

	updated, cmd := m.form.Update(msg)
	if form, ok := updated.(*huh.Form); ok {
		m.form = form
	}
	if m.form.State != huh.StateNormal {
		return m, m.advance()
	}
	return m, cmd
}

// resizeRemoteCmd tells the gateway the new column count, which it uses when it
// renders assistant output server-side.
func (m *Model) resizeRemoteCmd(cols int) tea.Cmd {
	client, sessionID := m.client, m.sessionID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		if err := client.Call(ctx, gateway.MethodTerminalResize, map[string]any{
			"session_id": sessionID,
			"cols":       max(cols, 20),
		}, nil); err != nil {
			// A failed resize is cosmetic; never surface it as an error.
			return nil
		}
		return nil
	}
}

// handleKey routes a key press for the active screen.
func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global quit. ctrl+c is the one key the shell always keeps for itself, on
	// every screen and whether or not a form is installed.
	if key == "ctrl+c" {
		if m.scr == screenChat && m.busy != "" && m.sessionID != "" {
			// Mid-turn, ctrl+c means "stop the turn", matching the CLI, and only
			// a second press leaves the program.
			m.note("Interrupting the current turn.")
			return m, interruptCmd(m.client, m.sessionID)
		}
		m.quitRequested = true
		return m, tea.Quit
	}

	if m.form != nil {
		// Escape is intercepted before the form sees it.
		//
		// huh binds its Quit key to ctrl+c only and StateAborted is reachable
		// solely through keymap.Quit, so the "form owns escape" behaviour assumed
		// here previously was never true: delegating esc left the form installed,
		// and the invisible form then swallowed every later keystroke. See
		// cancelForm for which screens may actually be escaped.
		if key == "esc" {
			if cmd, handled := m.cancelForm(); handled {
				return m, cmd
			}
		}
		updated, cmd := m.form.Update(msg)
		if form, ok := updated.(*huh.Form); ok {
			m.form = form
		}
		if m.form.State != huh.StateNormal {
			return m, m.advance()
		}
		return m, cmd
	}

	if m.scr != screenChat {
		return m, nil
	}

	switch key {
	case "esc":
		if m.busy != "" {
			m.note("Interrupting the current turn.")
			return m, interruptCmd(m.client, m.sessionID)
		}
		return m, m.toMenu()

	case "pgup":
		m.scrolling = min(m.scrolling+max(m.height/2, 1), maxTranscriptEntries)
		return m, nil

	case "pgdown":
		m.scrolling = max(m.scrolling-max(m.height/2, 1), 0)
		return m, nil

	case "enter":
		text := strings.TrimSpace(m.composer.Value())
		if text == "" {
			return m, nil
		}
		if m.busy != "" {
			m.notice = "A turn is already running. Wait for it, or press esc to interrupt."
			return m, nil
		}
		if m.sessionID == "" {
			m.failure = errors.New("no gateway session is open")
			return m, nil
		}
		m.composer.Reset()
		m.append(RoleYou, text)
		m.busy = "Thinking"
		m.scrolling = 0
		return m, submitCmd(m.client, m.sessionID, text)
	}

	// Everything else is editing input.
	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)
	return m, cmd
}

// handleEvent folds one gateway event into the model.
func (m *Model) handleEvent(event gateway.Event) tea.Cmd {
	switch event.Type {
	case gateway.EventGatewayReady:
		// The payload is {"skin": {...}}; internal/skin unwraps both the nested
		// and the flat form, so the shape is handled in exactly one place.
		m.skin = skin.FromReady(event.Payload)
		m.theme = NewTheme(m.skin)
		m.note("Connected to the gateway. Skin: " + m.skin.Name + ".")
		return nil

	case gateway.EventSessionInfo:
		// A session.info event carries the resolved model; surface it in the
		// header when present.
		var info struct {
			Model string `json:"model"`
		}
		if err := event.Decode(&info); err == nil && info.Model != "" {
			m.model = info.Model
		}
		return nil

	case gateway.EventMessageStart:
		m.busy = "Thinking"
		m.streaming = ""
		return nil

	case gateway.EventMessageDelta:
		var delta gateway.MessageDelta
		_ = event.Decode(&delta)
		m.streaming += delta.Text
		m.scrolling = 0
		return nil

	case gateway.EventMessageDone:
		var done gateway.MessageComplete
		_ = event.Decode(&done)
		text := done.Text
		if strings.TrimSpace(text) == "" {
			text = m.streaming
		}
		m.append(RoleAssistant, text)
		m.streaming = ""
		m.busy = ""
		m.activity = ""
		if done.Warning != "" {
			m.note(done.Warning)
		}
		return nil

	case gateway.EventReasoningDelta, gateway.EventThinkingDelta:
		m.busy = "Reasoning"
		return nil

	case gateway.EventStatusUpdate:
		var update struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		}
		if err := event.Decode(&update); err == nil {
			m.activity = strings.TrimSpace(update.Text)
		}
		return nil

	case gateway.EventToolProgress, gateway.EventToolGenerating:
		var progress struct {
			Name    string `json:"name"`
			Summary string `json:"summary"`
		}
		if err := event.Decode(&progress); err == nil {
			switch {
			case progress.Name != "" && progress.Summary != "":
				m.activity = progress.Name + ": " + progress.Summary
			case progress.Name != "":
				m.activity = progress.Name
			}
		}
		return nil

	case gateway.EventToolComplete:
		var done struct {
			Name    string `json:"name"`
			Summary string `json:"summary"`
			Error   string `json:"error"`
		}
		if err := event.Decode(&done); err == nil {
			m.activity = ""
			if done.Error != "" {
				m.note(done.Name + " failed: " + done.Error)
			}
		}
		return nil

	case gateway.EventApprovalReq:
		var request gateway.ApprovalRequest
		if err := event.Decode(&request); err != nil {
			m.note("approval.request had an unreadable payload")
			return nil
		}
		m.approvalReq = request
		m.approval = ApprovalAnswers{}
		m.scr = screenApproval
		return m.setForm(ApprovalForm(&m.approval, m.skin, request))

	case gateway.EventClarifyReq:
		var request gateway.ClarifyRequest
		if err := event.Decode(&request); err != nil {
			m.note("clarify.request had an unreadable payload")
			return nil
		}
		m.clarifyReq = request
		m.clarify = ClarifyAnswers{}
		m.scr = screenClarify
		return m.setForm(ClarifyForm(&m.clarify, m.skin, request))

	case gateway.EventSudoReq:
		var request gateway.SudoRequest
		if err := event.Decode(&request); err != nil {
			m.note("sudo.request had an unreadable payload")
			return nil
		}
		m.secretReq = gateway.SecretRequest{RequestID: request.RequestID, Prompt: "sudo password"}
		m.secretV = SecretAnswers{}
		m.scr = screenSecret
		return m.setForm(SecretForm(&m.secretV, m.skin, "sudo password required", "Sent to the agent once and never stored."))

	case gateway.EventSecretReq:
		var request gateway.SecretRequest
		if err := event.Decode(&request); err != nil {
			m.note("secret.request had an unreadable payload")
			return nil
		}
		m.secretReq = request
		m.secretV = SecretAnswers{}
		m.scr = screenSecret
		return m.setForm(SecretForm(&m.secretV, m.skin, request.EnvVar, request.Prompt))

	case gateway.EventSkinChanged:
		m.skin = skin.FromReady(event.Payload)
		m.theme = NewTheme(m.skin)
		return nil

	case gateway.EventError:
		var payload struct {
			Message string `json:"message"`
		}
		if err := event.Decode(&payload); err == nil && payload.Message != "" {
			m.append(RoleError, payload.Message)
		}
		m.busy = ""
		return nil
	}

	// An event type this front-end does not model is not an error: the gateway
	// grows event kinds, and an unknown one must be ignored rather than crash
	// the pump.
	return nil
}

// advance reacts to a finished form.
func (m *Model) advance() tea.Cmd {
	state := m.form.State
	m.clearForm()

	if state == huh.StateAborted {
		m.quitRequested = true
		return tea.Quit
	}

	switch m.scr {
	case screenLaunch:
		if err := ValidateProjectRoot(m.launch.ProjectRoot); err != nil {
			m.failure = err
			return m.setForm(LaunchForm(&m.launch, m.skin))
		}
		m.opts.Launch = withLaunchRoot(m.opts.Launch, m.launch.ProjectRoot, m.launch.Python)
		m.busy = "Starting the gateway"
		return m.startGatewayCmd()

	case screenMenu:
		return m.finishMenu()

	case screenSessions:
		return m.finishSessions()

	case screenApproval:
		choice := m.approval.Choice
		if !ValidApprovalChoice(choice) {
			// Never send an unrecognised choice: deny is the safe default.
			choice = gateway.ApprovalDeny
		}
		m.scr = screenChat
		m.notice = "Approval: " + choice
		return respondApprovalCmd(m.client, m.sessionID, choice, m.approval.All)

	case screenClarify:
		answer := m.clarify.Answer
		m.scr = screenChat
		return respondClarifyCmd(m.client, m.clarifyReq.RequestID, answer)

	case screenSecret:
		value := m.secretV.Value
		requestID := m.secretReq.RequestID
		m.scr = screenChat
		switch m.secretReq.Prompt {
		case "sudo password":
			return respondSudoCmd(m.client, requestID, value)
		default:
			return respondSecretCmd(m.client, requestID, value)
		}

	case screenStatus, screenTools, screenReadout:
		return m.toMenu()
	}
	return nil
}

// finishMenu routes the top-level selection.
func (m *Model) finishMenu() tea.Cmd {
	switch m.menu.Action {
	case ActionResume:
		m.busy = "Loading stored threads"
		return loadSessionsCmd(m.client)

	case ActionStatus:
		m.busy = "Reading status"
		return loadStatusCmd(m.client, m.sessionID)

	case ActionTools:
		m.busy = "Loading toolsets"
		return loadToolsCmd(m.client, m.sessionID)

	case ActionDoctor:
		m.busy = "Running hermes doctor"
		return doctorCmd(m.client)

	case ActionMostRecent:
		m.busy = "Resuming the most recent thread"
		return mostRecentCmd(m.client, max(m.width, 20))

	case ActionBack:
		return m.toChat()

	case ActionQuit:
		m.quitRequested = true
		return tea.Quit
	}
	return m.toMenu()
}

// finishSessions routes the thread picker's action.
func (m *Model) finishSessions() tea.Cmd {
	switch m.chosen.Action {
	case ActionResume:
		if strings.TrimSpace(m.chosen.SessionID) == "" {
			m.notice = "No thread is selected."
			return m.toMenu()
		}
		m.busy = "Opening the thread"
		m.historySessionID = m.chosen.SessionID
		return resumeCmd(m.client, m.chosen.SessionID, max(m.width, 20))

	case ActionRefresh:
		m.busy = "Refreshing"
		return loadSessionsCmd(m.client)
	}
	return m.toMenu()
}

// --- view ---------------------------------------------------------------------------

// View implements tea.Model.
func (m *Model) View() tea.View {
	var body strings.Builder

	body.WriteString(m.theme.Header(m.headerLeft(), m.headerRight(), m.width))
	body.WriteString("\n")

	switch m.scr {
	case screenChat:
		body.WriteString(m.viewChat())
	case screenStatus:
		body.WriteString(m.viewStatus())
	case screenTools:
		body.WriteString(m.viewTools())
	case screenReadout:
		body.WriteString(m.viewReadout())
	}

	if m.busy != "" {
		body.WriteString("\n")
		body.WriteString(m.theme.Pane("Working", m.busy+m.activitySuffix(), m.width))
	}
	if m.failure != nil {
		body.WriteString("\n")
		body.WriteString(m.theme.PaneError("Problem", m.failure, m.width))
	}
	if m.notice != "" {
		body.WriteString("\n")
		body.WriteString(m.theme.Pane("Done", m.notice, m.width))
	}

	if m.form != nil {
		body.WriteString("\n\n")
		body.WriteString(m.form.View())
	}

	if m.scr == screenChat {
		body.WriteString("\n")
		body.WriteString(m.viewComposer())
	}

	body.WriteString("\n")
	body.WriteString(m.theme.Footer(m.hint(), m.width))

	view := tea.NewView(body.String())
	view.AltScreen = true
	return view
}

func (m *Model) activitySuffix() string {
	if strings.TrimSpace(m.activity) == "" {
		return ""
	}
	return "\n" + m.activity
}

func (m *Model) headerLeft() string {
	return m.skin.AgentName()
}

func (m *Model) headerRight() string {
	parts := []string{}
	if m.model != "" {
		parts = append(parts, m.model)
	}
	switch {
	case m.historySessionID != "":
		parts = append(parts, "thread "+m.historySessionID)
	case m.sessionID != "":
		parts = append(parts, "new session "+m.sessionID)
	}
	if m.skin.Name != "" {
		parts = append(parts, "skin "+m.skin.Name)
	}
	return strings.Join(parts, " · ")
}

// viewChat renders the transcript plus the live turn.
//
// The height budget is computed before anything is drawn so the composer and
// footer always remain visible; a negative budget is impossible because every
// subtraction is clamped.
func (m *Model) viewChat() string {
	var out strings.Builder

	if m.sessionID == "" && m.busy == "" && m.failure == nil {
		out.WriteString(m.theme.Dim("Starting a conversation…"))
		out.WriteString("\n")
		return out.String()
	}

	lines := m.transcriptLines()
	streaming := m.streamingLines()
	lines = append(lines, streaming...)

	// Budget: header (1) + composer (5) + footer (1) + the activity pane (2).
	budget := m.height - 9
	if budget < 1 {
		budget = 1
	}

	start := 0
	if len(lines) > budget {
		end := len(lines) - m.scrolling
		if end < budget {
			end = budget
		}
		if end > len(lines) {
			end = len(lines)
		}
		start = end - budget
		if start < 0 {
			start = 0
		}
		lines = lines[start:end]
	}

	for _, line := range lines {
		out.WriteString(line)
		out.WriteString("\n")
	}

	if m.scrolling > 0 {
		out.WriteString(m.theme.Dim("  ↑" + strconv.Itoa(m.scrolling) + " lines back (pgdn to return)"))
		out.WriteString("\n")
	}
	return out.String()
}

// transcriptLines renders the transcript to styled, wrapped lines.
func (m *Model) transcriptLines() []string {
	width := max(m.width-6, MinPaneWidth)
	var lines []string
	for _, entry := range m.transcript {
		lines = append(lines, m.renderEntry(entry, width)...)
	}
	return lines
}

// streamingLines renders the in-flight assistant turn.
func (m *Model) streamingLines() []string {
	if m.streaming == "" {
		return nil
	}
	width := max(m.width-6, MinPaneWidth)
	return m.renderEntry(Entry{Role: RoleAssistant, Text: m.streaming}, width)
}

// renderEntry renders one entry: a role label plus wrapped body lines.
func (m *Model) renderEntry(entry Entry, width int) []string {
	var label string
	var body string
	switch entry.Role {
	case RoleYou:
		label = m.theme.Accent(m.skin.PromptSymbol() + " you")
		body = m.theme.Text(entry.Text)
	case RoleAssistant:
		label = m.theme.Label(m.skin.AgentName())
		body = m.theme.Text(entry.Text)
	case RoleTool:
		label = m.theme.Dim("tool")
		body = m.theme.Dim(entry.Text)
	case RoleError:
		label = m.theme.Danger("error")
		body = m.theme.Danger(entry.Text)
	default:
		label = m.theme.Dim("·")
		body = m.theme.Dim(entry.Text)
	}

	lines := []string{label}
	for _, wrapped := range strings.Split(Wrap(body, width), "\n") {
		lines = append(lines, "  "+wrapped)
	}
	lines = append(lines, "")
	return lines
}

// viewComposer renders the input box.
func (m *Model) viewComposer() string {
	if m.busy != "" {
		return m.theme.Dim("  (a turn is running — esc interrupts it)")
	}
	return m.composer.View()
}

func (m *Model) viewStatus() string {
	if strings.TrimSpace(m.readout) == "" {
		return m.theme.Dim("No status was returned.")
	}
	return m.theme.Pane("Status", Wrap(m.readout, max(m.width-6, MinPaneWidth)), m.width)
}

func (m *Model) viewTools() string {
	if len(m.toolsets) == 0 {
		return m.theme.Dim("No toolsets were reported.")
	}
	var out strings.Builder
	for _, toolset := range m.toolsets {
		state := m.theme.Dim("off")
		if toolset.Enabled {
			state = m.theme.Text("on")
		}
		out.WriteString(toolset.Name + "  " + state)
		if toolset.Description != "" {
			out.WriteString("\n  " + m.theme.Dim(Truncate(toolset.Description, max(m.width-10, 20))))
		}
		out.WriteString("\n")
	}
	return m.theme.Pane("Toolsets", strings.TrimRight(out.String(), "\n"), m.width)
}

func (m *Model) viewReadout() string {
	if strings.TrimSpace(m.readout) == "" {
		return m.theme.Dim("No output was returned.")
	}
	return m.theme.Pane("Output", Wrap(m.readout, max(m.width-6, MinPaneWidth)), m.width)
}

// hint is the key hint bar for the active screen.
func (m *Model) hint() string {
	switch m.scr {
	case screenLaunch:
		return "tab next · shift+tab back · enter submit · ctrl+c quit"
	case screenChat:
		if m.busy != "" {
			return "esc interrupts the turn · pgup/pgdn scroll · ctrl+c stops and quits"
		}
		return "enter send · ctrl+j newline · esc menu · pgup/pgdn scroll · ctrl+c quit"
	case screenApproval, screenClarify, screenSecret:
		return "arrows select · enter confirm · ctrl+c quit"
	case screenMenu, screenSessions:
		// These are the screens cancelForm can escape, so say so: the menu goes
		// back to the conversation and the thread picker back to the menu.
		return "arrows move · enter selects · esc back · ctrl+c quits"
	}
	return "arrows move · enter selects · ctrl+c quits"
}

// withLaunchRoot rebuilds the launch command for the answers the user gave.
func withLaunchRoot(launch gateway.Launch, root, python string) gateway.Launch {
	rebuilt, err := gateway.BuildLaunch(gateway.LauncherOptions{ProjectRoot: root, Python: python})
	if err != nil {
		return launch
	}
	return rebuilt
}

// explain turns a transport failure into something actionable. A stopped
// gateway is the common case here, not an exceptional one.
func explain(err error) error {
	if err == nil {
		return nil
	}
	var rpcErr *gateway.RPCError
	if errors.As(err, &rpcErr) {
		// The gateway's own codes are more specific than any advice this layer
		// could add, so they are passed through unchanged.
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("the gateway did not answer in time")
	}
	return err
}

// describeGatewayExit explains why the event stream ended.
func describeGatewayExit(err error, client *gateway.Client) error {
	if client != nil {
		if lines, _ := client.Stderr(); len(lines) > 0 {
			return errors.New("the gateway exited: " + lines[len(lines)-1])
		}
	}
	if err != nil {
		return errors.New("the gateway stopped: " + err.Error())
	}
	return errors.New("the gateway stopped")
}
