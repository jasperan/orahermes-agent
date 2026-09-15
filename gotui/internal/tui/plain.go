package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jasperan/orahermes-agent/gotui/internal/gateway"
)

// PlainOptions drive the non-full-screen path.
//
// This path is used when ACCESSIBLE is set, when stdin is not a terminal, and
// for the scripted action flags. It issues the SAME gateway RPCs as the
// full-screen UI, so the two front-ends cannot drift apart in what they ask or
// what they return.
type PlainOptions struct {
	Launch gateway.Launch
	Input  io.Reader
	Output io.Writer
}

// ActionRequest is a scripted, non-interactive request.
type ActionRequest struct {
	Action    string
	SessionID string
	Prompt    string
	JSON      bool
	// Wait bounds a streaming prompt's wait for its final message.
	Wait time.Duration
}

// DefaultActionWait bounds how long a scripted prompt waits for the turn to
// finish before reporting what it has.
const DefaultActionWait = 5 * time.Minute

// RunAction executes a scripted request against a started gateway.
//
// Every branch delegates to a gateway method; none of them reimplements agent
// behaviour. A prompt is submitted and then awaited on the event stream, because
// prompt.submit acknowledges the turn while the answer arrives as events.
func RunAction(ctx context.Context, client *gateway.Client, req ActionRequest, out io.Writer) error {
	switch req.Action {
	case ActionStatus:
		return runStatusAction(ctx, client, req, out)

	case ActionResume:
		return runSessionsAction(ctx, client, req, out)

	case ActionMostRecent:
		return runMostRecentAction(ctx, client, req, out)

	case ActionDoctor:
		return runDoctorAction(ctx, client, req, out)

	case ActionNew, "":
		if strings.TrimSpace(req.Prompt) == "" {
			return errors.New("nothing to do: pass --prompt to send a message, or --status / --threads")
		}
		return runPromptAction(ctx, client, req, out)
	}

	return fmt.Errorf("unknown action %q", req.Action)
}

func runStatusAction(ctx context.Context, client *gateway.Client, req ActionRequest, out io.Writer) error {
	sessionID, err := ensureSession(ctx, client, req.SessionID)
	if err != nil {
		return err
	}
	var status gateway.StatusResult
	if err := client.Call(ctx, gateway.MethodSessionStatus, map[string]any{
		"session_id": sessionID,
	}, &status); err != nil {
		return err
	}
	if req.JSON {
		return writeJSON(out, status)
	}
	fmt.Fprintln(out, status.Output)
	return nil
}

func runSessionsAction(ctx context.Context, client *gateway.Client, req ActionRequest, out io.Writer) error {
	var list gateway.SessionListResult
	if err := client.Call(ctx, gateway.MethodSessionList, map[string]any{}, &list); err != nil {
		return err
	}
	if req.JSON {
		return writeJSON(out, list)
	}
	if len(list.Sessions) == 0 {
		fmt.Fprintln(out, "No stored threads yet.")
		return nil
	}
	fmt.Fprintf(out, "%s\n", FormatCount(len(list.Sessions), "stored thread"))
	for _, session := range list.Sessions {
		fmt.Fprintf(out, "  %-10s %-4d %s\n", session.ID, session.MessageCount, sessionLabelPlain(session))
	}
	return nil
}

func runMostRecentAction(ctx context.Context, client *gateway.Client, req ActionRequest, out io.Writer) error {
	var ref gateway.SessionRef
	if err := client.Call(ctx, gateway.MethodSessionMostRecent, map[string]any{}, &ref); err != nil {
		return err
	}
	if req.JSON {
		return writeJSON(out, ref)
	}
	if ref.SessionID == "" {
		fmt.Fprintln(out, "No stored thread to resume.")
		return nil
	}
	fmt.Fprintln(out, ref.SessionID)
	return nil
}

func runDoctorAction(ctx context.Context, client *gateway.Client, req ActionRequest, out io.Writer) error {
	var result gateway.CliExecResult
	if err := client.Call(ctx, gateway.MethodCliExec, map[string]any{
		"argv":    []string{"doctor"},
		"timeout": 540,
	}, &result); err != nil {
		return err
	}
	if req.JSON {
		return writeJSON(out, result)
	}
	if result.Blocked {
		return errors.New("hermes doctor is blocked here: " + result.Hint)
	}
	fmt.Fprint(out, result.Output)
	if result.Code != 0 {
		return fmt.Errorf("hermes doctor exited %d", result.Code)
	}
	return nil
}

// runPromptAction submits one message and prints the finished answer.
func runPromptAction(ctx context.Context, client *gateway.Client, req ActionRequest, out io.Writer) error {
	sessionID, err := ensureSession(ctx, client, req.SessionID)
	if err != nil {
		return err
	}

	if err := client.Call(ctx, gateway.MethodPromptSubmit, map[string]any{
		"session_id": sessionID,
		"text":       req.Prompt,
	}, nil); err != nil {
		return err
	}

	wait := req.Wait
	if wait <= 0 {
		wait = DefaultActionWait
	}
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	// Drain the event stream until this session's turn completes. Events for
	// other sessions are ignored rather than treated as errors.
	var streamed strings.Builder
	for {
		select {
		case <-waitCtx.Done():
			if streamed.Len() > 0 {
				if !req.JSON {
					fmt.Fprint(out, streamed.String())
					fmt.Fprintln(out)
				}
				return errors.New("timed out waiting for the turn to finish; partial answer above")
			}
			return fmt.Errorf("timed out after %s waiting for the answer", wait)
		default:
		}

		event, err := client.Next(waitCtx)
		if err != nil {
			if streamed.Len() > 0 && !req.JSON {
				fmt.Fprint(out, streamed.String())
				fmt.Fprintln(out)
			}
			return err
		}

		if event.SessionID != "" && event.SessionID != sessionID {
			continue
		}

		switch event.Type {
		case gateway.EventMessageDelta:
			var delta gateway.MessageDelta
			if err := event.Decode(&delta); err == nil {
				streamed.WriteString(delta.Text)
			}

		case gateway.EventMessageDone:
			var done gateway.MessageComplete
			if err := event.Decode(&done); err != nil {
				return err
			}
			if req.JSON {
				return writeJSON(out, done)
			}
			text := done.Text
			if strings.TrimSpace(text) == "" {
				text = streamed.String()
			}
			fmt.Fprintln(out, text)
			if done.Warning != "" {
				fmt.Fprintln(out, "warning: "+done.Warning)
			}
			return nil

		case gateway.EventError:
			var payload struct {
				Message string `json:"message"`
			}
			_ = event.Decode(&payload)
			return errors.New("agent error: " + payload.Message)
		}
	}
}

// ensureSession resolves the session a scripted action should use: the one the
// caller named, or a fresh one.
func ensureSession(ctx context.Context, client *gateway.Client, sessionID string) (string, error) {
	if strings.TrimSpace(sessionID) != "" {
		var ref gateway.SessionRef
		if err := client.Call(ctx, gateway.MethodSessionResume, map[string]any{
			"session_id": sessionID,
			"cols":       100,
		}, &ref); err != nil {
			return "", err
		}
		return ref.SessionID, nil
	}

	var created gateway.SessionRef
	if err := client.Call(ctx, gateway.MethodSessionCreate, map[string]any{
		"cols": 100,
	}, &created); err != nil {
		return "", err
	}
	return created.SessionID, nil
}

// sessionLabelPlain renders one thread for the non-interactive listing.
func sessionLabelPlain(session gateway.SessionSummary) string {
	title := strings.TrimSpace(session.Title)
	if title == "" {
		title = strings.TrimSpace(session.Preview)
	}
	if title == "" {
		title = "(untitled)"
	}
	if session.Source != "" {
		return Truncate(title, 60) + "  [" + session.Source + "]"
	}
	return Truncate(title, 60)
}

// writeJSON emits an indented JSON document for scripting.
func writeJSON(out io.Writer, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	_, err = out.Write(append(encoded, '\n'))
	return err
}

// ParseActionFlags decides which scripted action a flag set requests.
//
// The order matters: the most specific flag wins so "--threads --prompt x"
// cannot silently degrade into a promptless read.
func ParseActionFlags(prompt, sessionID string, statusFlag, threadsFlag, mostRecentFlag, doctorFlag bool) (string, bool) {
	switch {
	case strings.TrimSpace(prompt) != "":
		return ActionNew, true
	case statusFlag:
		return ActionStatus, true
	case threadsFlag:
		return ActionResume, true
	case mostRecentFlag:
		return ActionMostRecent, true
	case doctorFlag:
		return ActionDoctor, true
	}
	return "", false
}

// AccessibleNotice explains the mode switch a screen-reader user gets.
const AccessibleNotice = "ACCESSIBLE is set: using plain prompts instead of the full-screen UI."
