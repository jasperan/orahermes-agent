// Command orahermes-tui is an additional way to run orahermes-agent: a Go
// front-end in the charm v2 + huh stack that drives the same Python
// `tui_gateway` the shipped Ink TUI drives.
//
// It never reimplements the agent. Sessions, tools, model calls and slash
// commands all live in the Python gateway — TypeScript's Ink client and this Go
// client are peers speaking one newline-delimited JSON-RPC protocol, so a Go
// user and an Ink user get identical results from identical state.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/jasperan/orahermes-agent/gotui/internal/gateway"
	"github.com/jasperan/orahermes-agent/gotui/internal/huhstyle"
	"github.com/jasperan/orahermes-agent/gotui/internal/skin"
	"github.com/jasperan/orahermes-agent/gotui/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "orahermes-tui: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	var (
		projectRoot = flag.String("project-root", "", "orahermes-agent checkout holding tui_gateway/ (default: this binary's repo)")
		pythonPath  = flag.String("python", "", "Python interpreter (default: $HERMES_PYTHON, $VIRTUAL_ENV, or the checkout's .venv)")

		thread     = flag.String("thread", "", "open this stored thread id")
		prompt     = flag.String("prompt", "", "send one message, print the answer, then exit")
		statusFlag = flag.Bool("status", false, "print the session status, then exit")
		threadsFlg = flag.Bool("threads", false, "list stored threads, then exit")
		recentFlg  = flag.Bool("most-recent", false, "print the most recent thread id, then exit")
		doctorFlag = flag.Bool("doctor", false, "run `hermes doctor` through the gateway, then exit")

		jsonFlag    = flag.Bool("json", false, "emit machine-readable JSON for scripted actions")
		waitFlag    = flag.Duration("wait", tui.DefaultActionWait, "how long --prompt waits for the answer")
		noInputFlag = flag.Bool("no-input", false, "never prompt; fail instead if input is required")
	)
	flag.Parse()

	root := strings.TrimSpace(*projectRoot)
	if root == "" {
		root = gateway.DefaultProjectRoot()
	}
	if err := tui.ValidateProjectRoot(root); err != nil {
		return err
	}

	launch, err := gateway.BuildLaunch(gateway.LauncherOptions{
		ProjectRoot: root,
		Python:      *pythonPath,
	})
	if err != nil {
		return err
	}

	// --- scripted path -----------------------------------------------------------
	// This runs before any prompt is considered, so a pipeline never blocks on
	// a question.
	action, isAction := tui.ParseActionFlags(*prompt, *thread, *statusFlag, *threadsFlg, *recentFlg, *doctorFlag)
	if isAction {
		client, err := gateway.Start(context.Background(), launch)
		if err != nil {
			return err
		}
		defer client.Close()

		return tui.RunAction(context.Background(), client, tui.ActionRequest{
			Action:    action,
			SessionID: *thread,
			Prompt:    *prompt,
			JSON:      *jsonFlag,
			Wait:      *waitFlag,
		}, os.Stdout)
	}

	// `--thread` alone means "open this thread in the full-screen UI", which the
	// model handles through StartSession rather than as a scripted action.
	startSession := strings.TrimSpace(*thread)
	if startSession != "" && strings.TrimSpace(*prompt) != "" {
		return errors.New("--thread and --prompt together are ambiguous: use --prompt for a scripted turn, or --thread to open the UI")
	}

	// --- screen-reader / piped path ---------------------------------------------
	// huh's accessible rendering only exists in its standalone Run path, so the
	// embedded full-screen UI is skipped entirely here.
	if huhstyle.Accessible() {
		fmt.Fprintln(os.Stdout, tui.AccessibleNotice)
		return runPlain(launch, *noInputFlag)
	}
	if !huhstyle.Interactive() || *noInputFlag {
		return errors.New("no terminal on stdin: pass --status, --threads, --most-recent, --doctor or --prompt \"...\" to run without one (or set ACCESSIBLE for plain prompts)")
	}

	// --- full-screen path --------------------------------------------------------
	model := tui.New(tui.Options{
		Launch:       launch,
		Skin:         skin.Default(),
		StartSession: startSession,
	})
	defer model.Close()

	program := tea.NewProgram(model)
	if _, err := program.Run(); err != nil {
		return err
	}
	return nil
}

// runPlain drives one accessible form and then performs the chosen action.
//
// The form is a SINGLE huh group on purpose: in accessible mode huh runs only
// the first group of a form and then reports the form complete, so a multi-group
// form would silently leave every later answer at its default.
func runPlain(launch gateway.Launch, noInput bool) error {
	if noInput {
		return errors.New("-no-input cannot be combined with ACCESSIBLE plain prompts")
	}

	ctx := context.Background()
	client, err := gateway.Start(ctx, launch)
	if err != nil {
		return err
	}
	defer client.Close()

	// The announced skin decides the form's palette; the default is used until
	// the gateway has been heard from.
	announced := skin.Default()
	sessions, err := plainSessions(ctx, client)
	if err != nil {
		// A missing session database is not fatal for a first run: the form still
		// offers status and a new conversation.
		fmt.Fprintln(os.Stderr, "note: "+err.Error())
	}

	var answers tui.PlainAnswers
	root, python := launchAnswers(launch)
	answers.Launch = tui.LaunchDefaults(root, python)
	form := tui.PlainForm(&answers, announced, sessions).WithInput(os.Stdin).WithOutput(os.Stdout)
	if err := form.Run(); err != nil {
		return err
	}
	if strings.TrimSpace(answers.Action) == "" {
		answers.Action = tui.ActionStatus
	}

	request := tui.ActionRequest{
		Action:    answers.Action,
		SessionID: answers.Session,
		Prompt:    answers.Prompt,
	}
	if answers.Action == tui.ActionQuit {
		return nil
	}
	return tui.RunAction(ctx, client, request, os.Stdout)
}

// plainSessions loads the thread list so the accessible form can offer it.
func plainSessions(ctx context.Context, client *gateway.Client) ([]gateway.SessionSummary, error) {
	listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var list gateway.SessionListResult
	if err := client.Call(listCtx, gateway.MethodSessionList, map[string]any{}, &list); err != nil {
		return nil, err
	}
	return list.Sessions, nil
}

// launchAnswers seeds the accessible form from the prepared launch command.
func launchAnswers(launch gateway.Launch) (root, python string) {
	if len(launch.Argv) > 0 {
		python = launch.Argv[0]
	}
	return launch.Dir, python
}
