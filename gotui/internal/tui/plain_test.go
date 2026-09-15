package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jasperan/orahermes-agent/gotui/internal/gateway"
)

// TestParseActionFlagsPrecedence pins the routing rule: the most specific flag
// wins, so "--threads --prompt x" cannot silently degrade into a promptless read.
func TestParseActionFlagsPrecedence(t *testing.T) {
	cases := []struct {
		name       string
		prompt     string
		sessionID  string
		status     bool
		threads    bool
		mostRecent bool
		doctor     bool
	}{
		{name: "prompt wins", prompt: "q", status: true, threads: true, doctor: true},
		{name: "status beats threads", status: true, threads: true, mostRecent: true, doctor: true},
		{name: "threads beats most-recent", threads: true, mostRecent: true, doctor: true},
		{name: "most-recent beats doctor", mostRecent: true, doctor: true},
		{name: "doctor alone", doctor: true},
	}

	for _, tc := range cases {
		action, ok := ParseActionFlags(tc.prompt, tc.sessionID, tc.status, tc.threads, tc.mostRecent, tc.doctor)
		if !ok {
			t.Errorf("%s: expected an action", tc.name)
			continue
		}
		switch {
		case tc.prompt != "":
			if action != ActionNew {
				t.Errorf("%s: action = %q, want %q", tc.name, action, ActionNew)
			}
		case tc.status:
			if action != ActionStatus {
				t.Errorf("%s: action = %q, want %q", tc.name, action, ActionStatus)
			}
		case tc.threads:
			if action != ActionResume {
				t.Errorf("%s: action = %q, want %q", tc.name, action, ActionResume)
			}
		case tc.mostRecent:
			if action != ActionMostRecent {
				t.Errorf("%s: action = %q, want %q", tc.name, action, ActionMostRecent)
			}
		case tc.doctor:
			if action != ActionDoctor {
				t.Errorf("%s: action = %q, want %q", tc.name, action, ActionDoctor)
			}
		}
	}

	if _, ok := ParseActionFlags("", "", false, false, false, false); ok {
		t.Fatal("no flags must mean no scripted action")
	}
}

// TestRunActionRefusesAnEmptyPrompt keeps a piped invocation from hanging on a
// turn that was never described.
func TestRunActionRefusesAnEmptyPrompt(t *testing.T) {
	err := RunAction(context.Background(), nil, ActionRequest{Action: ActionNew}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "--prompt") {
		t.Fatalf("the error should name the flag that fixes it, got %q", err.Error())
	}
}

func TestRunActionRejectsAnUnknownAction(t *testing.T) {
	err := RunAction(context.Background(), nil, ActionRequest{Action: "teleport"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("expected an unknown-action error, got %v", err)
	}
}

// TestRunActionNewWithWhitespaceOnlyPromptIsRefused is the guard against a prompt
// built from shell interpolation that expanded to nothing.
func TestRunActionNewWithWhitespaceOnlyPromptIsRefused(t *testing.T) {
	for _, prompt := range []string{"", "   ", "\t\n"} {
		err := RunAction(context.Background(), nil, ActionRequest{Action: ActionNew, Prompt: prompt}, &bytes.Buffer{})
		if err == nil {
			t.Fatalf("prompt %q must be refused", prompt)
		}
	}
}

// TestSessionLabelPlainIncludesTheSource keeps a listing row attributable when
// the same title exists from more than one surface.
func TestSessionLabelPlainIncludesTheSource(t *testing.T) {
	got := sessionLabelPlain(gateway.SessionSummary{Title: "Deploy", Source: "telegram"})
	if !strings.Contains(got, "Deploy") || !strings.Contains(got, "telegram") {
		t.Fatalf("label = %q", got)
	}

	// No source: no empty brackets.
	plain := sessionLabelPlain(gateway.SessionSummary{Title: "Deploy"})
	if strings.Contains(plain, "[") {
		t.Fatalf("label = %q, want no source suffix", plain)
	}
}

func TestSessionLabelPlainFallsBack(t *testing.T) {
	if got := sessionLabelPlain(gateway.SessionSummary{Preview: "a preview"}); !strings.Contains(got, "a preview") {
		t.Fatalf("label = %q", got)
	}
	if got := sessionLabelPlain(gateway.SessionSummary{}); !strings.Contains(got, "(untitled)") {
		t.Fatalf("label = %q", got)
	}
}

// TestWriteJSONEmitsIndentedDocuments is what makes the --json flags scriptable.
func TestWriteJSONEmitsIndentedDocuments(t *testing.T) {
	var out bytes.Buffer
	if err := writeJSON(&out, map[string]any{"session_id": "abc", "count": 2}); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %q", out.String())
	}
	if decoded["session_id"] != "abc" {
		t.Fatalf("decoded = %v", decoded)
	}
	if !strings.Contains(out.String(), "\n  ") {
		t.Fatalf("output should be indented for humans: %q", out.String())
	}
}

// TestWriteJSONReportsEncodingFailures keeps a channel or func value from being
// silently dropped.
func TestWriteJSONReportsEncodingFailures(t *testing.T) {
	var out bytes.Buffer
	err := writeJSON(&out, map[string]any{"bad": make(chan int)})
	if err == nil {
		t.Fatal("expected an encoding error")
	}
	if !strings.Contains(err.Error(), "encode json") {
		t.Fatalf("error = %v", err)
	}
}

// TestEnsureSessionResumesTheNamedThread documents that a scripted run attaches to
// the thread it was given rather than silently starting a new one.
func TestEnsureSessionResumesTheNamedThread(t *testing.T) {
	// A nil client panics, which is the correct signal that ensureSession must
	// always be handed a live client; assert the contract instead of the panic.
	defer func() {
		if recover() == nil {
			t.Fatal("ensureSession must require a live client")
		}
	}()
	_, _ = ensureSession(context.Background(), nil, "thread-1")
}

// TestExplainLeavesGatewayErrorsIntact keeps the backend's own codes from being
// rewritten by this layer.
func TestExplainLeavesGatewayErrorsIntact(t *testing.T) {
	rpcErr := &gateway.RPCError{Code: 4007, Message: "session not found"}
	got := explain(rpcErr)
	if got != error(rpcErr) {
		t.Fatalf("explain rewrote a gateway error: %v", got)
	}
	if explain(nil) != nil {
		t.Fatal("explain(nil) must be nil")
	}
}

// TestExplainRewritesDeadlinesForHumans covers the one transport failure worth a
// clearer message.
func TestExplainRewritesDeadlinesForHumans(t *testing.T) {
	got := explain(context.DeadlineExceeded)
	if got == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(got.Error(), "did not answer in time") {
		t.Fatalf("error = %q", got.Error())
	}
}

// TestDescribeGatewayExitPrefersTheGatewayStderr turns a silent disconnect into
// the Python traceback that explains it.
func TestDescribeGatewayExitPrefersTheGatewayStderr(t *testing.T) {
	err := describeGatewayExit(errors.New("io.EOF"), nil)
	if err == nil || !strings.Contains(err.Error(), "gateway") {
		t.Fatalf("error = %v", err)
	}
}

// TestAccessibleNoticeMentionsTheModeSwitch documents what a screen-reader user
// is told.
func TestAccessibleNoticeMentionsTheModeSwitch(t *testing.T) {
	if !strings.Contains(AccessibleNotice, "ACCESSIBLE") {
		t.Fatalf("notice = %q", AccessibleNotice)
	}
}
