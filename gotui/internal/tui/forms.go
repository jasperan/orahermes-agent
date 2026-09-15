package tui

import (
	"errors"
	"strconv"
	"strings"

	"charm.land/huh/v2"

	"github.com/jasperan/orahermes-agent/gotui/internal/gateway"
	"github.com/jasperan/orahermes-agent/gotui/internal/huhstyle"
	"github.com/jasperan/orahermes-agent/gotui/internal/skin"
)

// Action identifiers shared by the screen UI and the non-interactive path.
const (
	ActionResume     = "resume"
	ActionNew        = "new"
	ActionMostRecent = "most-recent"
	ActionStatus     = "status"
	ActionTools      = "tools"
	ActionDoctor     = "doctor"
	ActionRefresh    = "refresh"
	ActionBack       = "back"
	ActionQuit       = "quit"
)

// LaunchAnswers configure how the gateway process is started.
//
// Both fields exist because interpreter discovery is the one thing a Go
// front-end cannot infer reliably on someone else's machine: the Python CLI's
// own resolution order is already mirrored in gateway.DiscoverPython, and these
// answers only override it.
type LaunchAnswers struct {
	ProjectRoot string
	Python      string
}

// LaunchDefaults seeds the launch form.
func LaunchDefaults(projectRoot, python string) LaunchAnswers {
	if strings.TrimSpace(projectRoot) == "" {
		projectRoot = gateway.DefaultProjectRoot()
	}
	return LaunchAnswers{ProjectRoot: projectRoot, Python: python}
}

// ValidateProjectRoot checks the checkout really is one.
//
// It is exported so the guard can be tested directly rather than through a
// rendered form, which is where the regression risk actually sits.
func ValidateProjectRoot(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return errors.New("a checkout path is required")
	}
	if !gateway.LooksLikeProjectRoot(trimmed) {
		return errors.New("no tui_gateway/entry.py in " + trimmed + " — point this at the orahermes-agent checkout")
	}
	return nil
}

// launchFields are the launch questions.
//
// They are separated from LaunchForm so the accessible path can place them in a
// single form together with the other questions: huh's accessible Run buffers
// its reader, so two sequential forms cannot share stdin (the second one sees
// EOF and every answer comes back empty).
func launchFields(answers *LaunchAnswers) []huh.Field {
	return []huh.Field{
		huh.NewInput().
			Title("orahermes-agent checkout").
			Description("Holds tui_gateway/, which is what the TUI talks to.").
			Placeholder(gateway.DefaultProjectRoot()).
			Value(&answers.ProjectRoot).
			Validate(ValidateDefaultedValue(answers.ProjectRoot, ValidateProjectRoot)),

		huh.NewInput().
			Title("Python interpreter (optional)").
			Description("Leave blank to use $HERMES_PYTHON, $VIRTUAL_ENV, or the checkout's .venv.").
			Placeholder(".venv/bin/python").
			Value(&answers.Python),
	}
}

// LaunchForm builds the launch form used by the full-screen UI.
func LaunchForm(answers *LaunchAnswers, s skin.Skin) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(launchFields(answers)...).Title("Start the gateway"),
	).WithTheme(huhstyle.ThemeFunc(s)).WithAccessible(huhstyle.Accessible())
}

// SessionChoice is one row of the thread picker.
type SessionChoice struct {
	ID   string
	Row  gateway.SessionSummary
	Kind string
}

// MenuOptions are the top-level destinations.
//
// They mirror the three surfaces this front-end implements — chat, thread
// browsing, and the status/doctor readout — plus quit. Everything else the
// Python CLI can do is reachable through the composer's slash commands, which
// are dispatched by the gateway rather than reimplemented here.
func MenuOptions(hasSession bool) []huh.Option[string] {
	options := []huh.Option[string]{
		huh.NewOption("Browse stored threads", ActionResume),
		huh.NewOption("Status and doctor", ActionStatus),
		huh.NewOption("Toolsets", ActionTools),
		huh.NewOption("Quit", ActionQuit),
	}
	if hasSession {
		options = append([]huh.Option[string]{huh.NewOption("Back to the conversation", ActionBack)}, options...)
	}
	return options
}

// MenuAnswers is the top-level menu selection.
type MenuAnswers struct{ Action string }

// MenuForm builds the top-level menu.
func MenuForm(answers *MenuAnswers, s skin.Skin, context string) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("What next?").
				Description(context).
				Options(MenuOptions(false)...).
				Value(&answers.Action),
		),
	).WithTheme(huhstyle.ThemeFunc(s)).WithAccessible(huhstyle.Accessible())
}

// SessionOptions builds the thread picker options.
//
// A fresh slice per load is mandatory: huh silently ignores an empty Options
// call, so a refresh that returns nothing would otherwise keep showing the
// previous threads.
func SessionOptions(sessions []gateway.SessionSummary) []huh.Option[string] {
	if len(sessions) == 0 {
		return []huh.Option[string]{huh.NewOption("(no stored threads)", "")}
	}
	options := make([]huh.Option[string], 0, len(sessions))
	for _, session := range sessions {
		options = append(options, huh.NewOption(SessionLabel(session), session.ID))
	}
	return options
}

// SessionLabel renders one thread row for the picker.
func SessionLabel(session gateway.SessionSummary) string {
	title := strings.TrimSpace(session.Title)
	if title == "" {
		title = strings.TrimSpace(session.Preview)
	}
	if title == "" {
		title = "(untitled)"
	}
	return Truncate(title, 60) + "  ·  " + strconv.Itoa(session.MessageCount) + " msgs"
}

// SessionAnswers is the thread picker's values.
type SessionAnswers struct {
	SessionID string
	Action    string
}

// SessionActions are the actions available once a thread is selected.
var SessionActions = []huh.Option[string]{
	huh.NewOption("Open this thread", ActionResume),
	huh.NewOption("Refresh the list", ActionRefresh),
	huh.NewOption("Back to the menu", ActionBack),
}

// SessionForm builds the thread picker.
func SessionForm(answers *SessionAnswers, s skin.Skin, sessions []gateway.SessionSummary) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Thread").
				Description("Stored conversation sessions, newest first.").
				Options(SessionOptions(sessions)...).
				Value(&answers.SessionID),

			huh.NewSelect[string]().
				Title("Action").
				Options(SessionActions...).
				Value(&answers.Action),
		).Title("Threads"),
	).WithTheme(huhstyle.ThemeFunc(s)).WithAccessible(huhstyle.Accessible())
}

// ApprovalOptions are the four answers tools/approval.py accepts.
//
// Source: ui-tui/src/components/prompts.tsx — `const OPTS = ['once', 'session',
// 'always', 'deny']`. The order is least- to most-permissive, with denial last,
// matching the Ink client so muscle memory carries over.
var ApprovalOptions = []huh.Option[string]{
	huh.NewOption("Allow once — "+gateway.ApprovalOnce, gateway.ApprovalOnce),
	huh.NewOption("Allow this session — "+gateway.ApprovalSession, gateway.ApprovalSession),
	huh.NewOption("Always allow — "+gateway.ApprovalAlways, gateway.ApprovalAlways),
	huh.NewOption("Deny — "+gateway.ApprovalDeny, gateway.ApprovalDeny),
}

// ValidApprovalChoice reports whether a choice is one the backend accepts.
//
// The guard is exported and unit-tested because sending an unrecognised choice
// would silently resolve the approval with a string no caller expects.
func ValidApprovalChoice(choice string) bool {
	switch choice {
	case gateway.ApprovalOnce, gateway.ApprovalSession, gateway.ApprovalAlways, gateway.ApprovalDeny:
		return true
	default:
		return false
	}
}

// ApprovalAnswers is the approval prompt's answer.
type ApprovalAnswers struct {
	Choice string
	All    bool
}

// ApprovalForm builds the tool-approval prompt.
func ApprovalForm(answers *ApprovalAnswers, s skin.Skin, request gateway.ApprovalRequest) *huh.Form {
	body := request.Command
	if strings.TrimSpace(request.Description) != "" {
		body = request.Description + "\n\n" + request.Command
	}
	return huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Approval required").
				Description(body),

			huh.NewSelect[string]().
				Title("Decision").
				Options(ApprovalOptions...).
				Value(&answers.Choice).
				Validate(func(choice string) error {
					if !ValidApprovalChoice(choice) {
						return errors.New("choose one of the four decisions")
					}
					return nil
				}),

			huh.NewConfirm().
				Title("Apply to every queued approval?").
				Description("Sends resolve_all, like /approve all.").
				Affirmative("Yes, all").
				Negative("Just the oldest").
				Value(&answers.All),
		).Title("Approval"),
	).WithTheme(huhstyle.ThemeFunc(s)).WithAccessible(huhstyle.Accessible())
}

// ClarifyAnswers is the clarify prompt's answer.
type ClarifyAnswers struct{ Answer string }

// ClarifyForm builds the agent's clarifying question.
//
// With choices the gateway offered, a Select keeps the answer to one the caller
// can interpret; without them it is free text. Both shapes come from
// ui-tui/src/gatewayTypes.ts's clarify.request payload.
func ClarifyForm(answers *ClarifyAnswers, s skin.Skin, request gateway.ClarifyRequest) *huh.Form {
	if len(request.Choices) == 0 {
		return huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title(request.Question).
					Description("The agent asked for input.").
					Value(&answers.Answer).
					Validate(huh.ValidateNotEmpty()),
			).Title("Clarification"),
		).WithTheme(huhstyle.ThemeFunc(s)).WithAccessible(huhstyle.Accessible())
	}

	options := make([]huh.Option[string], 0, len(request.Choices))
	for _, choice := range request.Choices {
		options = append(options, huh.NewOption(choice, choice))
	}
	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(request.Question).
				Options(options...).
				Value(&answers.Answer),
		).Title("Clarification"),
	).WithTheme(huhstyle.ThemeFunc(s)).WithAccessible(huhstyle.Accessible())
}

// SecretAnswers holds a secret value typed by the user.
//
// The value lives in memory only: it is sent to the gateway once and never
// logged, persisted, or echoed.
type SecretAnswers struct{ Value string }

// SecretForm builds the secret prompt (sudo password, or an env var value).
func SecretForm(answers *SecretAnswers, s skin.Skin, title, description string) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(title).
				Description(description).
				EchoMode(huh.EchoModePassword).
				Value(&answers.Value).
				Validate(huh.ValidateNotEmpty()),
		).Title("Secret"),
	).WithTheme(huhstyle.ThemeFunc(s)).WithAccessible(huhstyle.Accessible())
}

// ConfirmAnswers is a yes/no continuation prompt.
type ConfirmAnswers struct{ Confirmed bool }

// ContinueForm hands control back to the caller after a readout screen: with no
// form installed the root model would have nothing to continue from.
func ContinueForm(answers *ConfirmAnswers, s skin.Skin, title, question string) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(question).
				Affirmative("Continue").
				Negative("Back to the menu").
				Value(&answers.Confirmed),
		).Title(title),
	).WithTheme(huhstyle.ThemeFunc(s)).WithAccessible(huhstyle.Accessible())
}

// PlainAnswers holds every page of the single accessible form.
type PlainAnswers struct {
	Launch  LaunchAnswers
	Action  string
	Session string
	Prompt  string
}

// plainFields are every question the accessible form asks, in order.
//
// Extracted from PlainForm so the wiring can be tested: huh does not export the
// groups or fields of a built form, so the field list is the only inspectable
// surface. PlainForm must place these in exactly ONE group.
func plainFields(answers *PlainAnswers, sessions []gateway.SessionSummary) []huh.Field {
	fields := launchFields(&answers.Launch)
	fields = append(fields,
		huh.NewSelect[string]().
			Title("Action").
			Description("Leave on status for a quick check without a conversation.").
			Options(
				huh.NewOption("Status", ActionStatus),
				huh.NewOption("Browse stored threads, then open one", ActionResume),
				huh.NewOption("New conversation", ActionNew),
				huh.NewOption("Most recent conversation", ActionMostRecent),
				huh.NewOption("Doctor", ActionDoctor),
				huh.NewOption("Quit", ActionQuit),
			).
			Value(&answers.Action),
	)
	if len(sessions) > 0 {
		fields = append(fields, huh.NewSelect[string]().
			Title("Thread (only used by 'Browse stored threads')").
			Options(SessionOptions(sessions)...).
			Value(&answers.Session))
	}
	return append(fields, huh.NewInput().
		Title("Message (only used by 'New conversation')").
		Description("Leave blank to open an empty conversation.").
		Value(&answers.Prompt))
}

// PlainForm is the one-pass form used when ACCESSIBLE is set or stdin is not a
// terminal.
//
// It is deliberately a SINGLE group, not several. In accessible mode huh runs
// only the first group of a form and then reports the form complete: with more
// than one group, every question in the later groups was silently left at its
// default (observed in this workspace's ragcli front-end: question="" while all
// six connection answers landed correctly). One group means every question is
// asked, exactly once, one line each.
func PlainForm(answers *PlainAnswers, s skin.Skin, sessions []gateway.SessionSummary) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(plainFields(answers, sessions)...).Title("orahermes-tui"),
	).WithTheme(huhstyle.ThemeFunc(s)).WithAccessible(huhstyle.Accessible())
}

// ValidateDefaulted accepts an empty answer as "keep the value already in the
// field".
//
// It exists for accessible mode. huh's screen-reader path runs a field's
// validator on the raw line and only afterwards substitutes the field's default
// (internal/accessibility/accessibility.go:PromptString returns
// cmp.Or(strings.TrimSpace(input), defaultValue)), and it never prints that
// default. A pre-filled field whose validator rejects "" therefore keeps
// re-prompting on a bare Enter, so a screen-reader user cannot accept a value
// they cannot see.
func ValidateDefaulted(inner func(string) error) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		return inner(s)
	}
}

// ValidateDefaultedValue is ValidateDefaulted for a field whose pre-filled value
// may itself be empty. Blank stays invalid when there is nothing to keep.
func ValidateDefaultedValue(prefilled string, inner func(string) error) func(string) error {
	if strings.TrimSpace(prefilled) == "" {
		return inner
	}
	return ValidateDefaulted(inner)
}
