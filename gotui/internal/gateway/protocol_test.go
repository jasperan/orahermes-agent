package gateway

import (
	"encoding/json"
	"errors"
	"testing"
)

// The frames below are byte-for-byte the shapes tui_gateway emits, so a
// regression in the codec is caught against the real wire format rather than a
// shape invented by this test.

func TestDecodeFrameResponse(t *testing.T) {
	frame, err := DecodeFrame([]byte(`{"jsonrpc":"2.0","id":7,"result":{"session_id":"abc123"}}`))
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if frame.IsEvent() {
		t.Fatal("a frame with an id and no method is a response, not an event")
	}
	if frame.ID == nil || *frame.ID != 7 {
		t.Fatalf("id = %v, want 7", frame.ID)
	}

	var ref SessionRef
	if err := json.Unmarshal(frame.Result, &ref); err != nil {
		t.Fatalf("result is not a session ref: %v", err)
	}
	if ref.SessionID != "abc123" {
		t.Fatalf("session_id = %q", ref.SessionID)
	}
}

func TestDecodeFrameError(t *testing.T) {
	// server.py: _err(rid, 4007, "session not found")
	frame, err := DecodeFrame([]byte(`{"jsonrpc":"2.0","id":3,"error":{"code":4007,"message":"session not found"}}`))
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if frame.Error == nil {
		t.Fatal("expected an error object")
	}
	if frame.Error.Code != 4007 || frame.Error.Message != "session not found" {
		t.Fatalf("unexpected error: %+v", frame.Error)
	}

	var rpcErr *RPCError = frame.Error
	if !errors.As(error(rpcErr), &rpcErr) {
		t.Fatal("RPCError should satisfy errors.As")
	}
	if got := frame.Error.Error(); got == "" {
		t.Fatal("RPCError.Error() must not be empty")
	}
}

func TestDecodeFrameEvent(t *testing.T) {
	// entry.py's first frame.
	line := `{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{"skin":{"name":"default"}}}}`
	frame, err := DecodeFrame([]byte(line))
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if !frame.IsEvent() {
		t.Fatal("a frame with method=event is an event")
	}
	if got := frame.EventType(); got != EventGatewayReady {
		t.Fatalf("EventType() = %q, want %q", got, EventGatewayReady)
	}
	if frame.Params == nil || len(frame.Params.Payload) == 0 {
		t.Fatal("the payload must survive decoding")
	}
}

func TestDecodeFrameEventCarriesSessionID(t *testing.T) {
	// server.py _emit() sets params.session_id.
	frame, err := DecodeFrame([]byte(`{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","session_id":"d41d8c","payload":{"text":"hi"}}}`))
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if frame.Params.SessionID != "d41d8c" {
		t.Fatalf("session_id = %q", frame.Params.SessionID)
	}

	event := Event{Type: frame.EventType(), SessionID: frame.Params.SessionID, Payload: frame.Params.Payload}
	var delta MessageDelta
	if err := event.Decode(&delta); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if delta.Text != "hi" {
		t.Fatalf("text = %q", delta.Text)
	}
}

func TestDecodeFrameRejectsGarbage(t *testing.T) {
	for name, line := range map[string]string{
		"empty":      "",
		"whitespace": "   ",
		"not json":   "hello",
		"truncated":  `{"jsonrpc":"2.0",`,
		"array":      `[1,2,3]`,
	} {
		if _, err := DecodeFrame([]byte(line)); !errors.Is(err, ErrMalformedFrame) {
			t.Errorf("%s: err = %v, want ErrMalformedFrame", name, err)
		}
	}
}

func TestMessageCompleteDecodesOptionalFields(t *testing.T) {
	// server.py: payload = {"text": raw, "usage": _get_usage(agent), "status": status}
	// plus optional "reasoning" and "warning".
	line := `{"text":"done","status":"complete","usage":{"total_tokens":12},"reasoning":"why","warning":"budget low","rendered":"done"}`
	var done MessageComplete
	if err := json.Unmarshal([]byte(line), &done); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if done.Text != "done" || done.Status != "complete" || done.Warning != "budget low" {
		t.Fatalf("unexpected payload: %+v", done)
	}
	if done.Usage["total_tokens"] != float64(12) {
		t.Fatalf("usage did not survive: %+v", done.Usage)
	}
}

func TestSessionListResultDecodes(t *testing.T) {
	// server.py session.list result.
	line := `{"sessions":[{"id":"a1","title":"Thread","preview":"hello","started_at":1700000000,"message_count":4,"source":"tui"}]}`
	var result SessionListResult
	if err := json.Unmarshal([]byte(line), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Sessions) != 1 {
		t.Fatalf("sessions = %d", len(result.Sessions))
	}
	row := result.Sessions[0]
	if row.ID != "a1" || row.MessageCount != 4 || row.Source != "tui" || row.StartedAt != 1700000000 {
		t.Fatalf("unexpected row: %+v", row)
	}
}

// TestApprovalRequestHasNoRequestID pins the shape that makes approval.respond
// different from clarify/sudo/secret: tools/approval.py resolves by session key,
// so the payload carries the command and description only.
func TestApprovalRequestHasNoRequestID(t *testing.T) {
	line := `{"command":"rm -rf /tmp/x","description":"delete scratch files"}`
	var request ApprovalRequest
	if err := json.Unmarshal([]byte(line), &request); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if request.Command != "rm -rf /tmp/x" || request.Description != "delete scratch files" {
		t.Fatalf("unexpected payload: %+v", request)
	}
}

func TestClarifyRequestSupportsBothShapes(t *testing.T) {
	// With choices.
	var withChoices ClarifyRequest
	if err := json.Unmarshal([]byte(`{"choices":["a","b"],"question":"which?","request_id":"r1"}`), &withChoices); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(withChoices.Choices) != 2 || withChoices.RequestID != "r1" {
		t.Fatalf("unexpected: %+v", withChoices)
	}

	// Free-form: choices is null.
	var freeForm ClarifyRequest
	if err := json.Unmarshal([]byte(`{"choices":null,"question":"why?","request_id":"r2"}`), &freeForm); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(freeForm.Choices) != 0 {
		t.Fatalf("null choices should decode to an empty slice, got %+v", freeForm.Choices)
	}
}

func TestSecretRequestShape(t *testing.T) {
	// ui-tui/src/gatewayTypes.ts: { env_var: string; prompt: string; request_id: string }
	var request SecretRequest
	if err := json.Unmarshal([]byte(`{"env_var":"OPENAI_API_KEY","prompt":"Paste your key","request_id":"s1"}`), &request); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if request.EnvVar != "OPENAI_API_KEY" || request.RequestID != "s1" {
		t.Fatalf("unexpected: %+v", request)
	}
}

// TestNewRequestAlwaysCarriesParams keeps every frame the same shape in a log.
func TestNewRequestAlwaysCarriesParams(t *testing.T) {
	req := NewRequest(1, MethodSessionList, nil)
	if req.JSONRPC != JSONRPCVersion {
		t.Fatalf("jsonrpc = %q", req.JSONRPC)
	}
	encoded, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(encoded); got != `{"jsonrpc":"2.0","id":1,"method":"session.list","params":{}}` {
		t.Fatalf("encoded request = %s", got)
	}
}

// TestEventTypeIsEmptyForResponses guards the routing decision: only an event
// frame may yield a type.
func TestEventTypeIsEmptyForResponses(t *testing.T) {
	frame, err := DecodeFrame([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if got := frame.EventType(); got != "" {
		t.Fatalf("EventType() = %q for a response", got)
	}
}

// TestMethodNamesMatchTheBackend pins the strings against tui_gateway/server.py's
// @method declarations, so a rename there fails here instead of at runtime.
func TestMethodNamesMatchTheBackend(t *testing.T) {
	want := map[string]string{
		"MethodSessionCreate":     "session.create",
		"MethodSessionList":       "session.list",
		"MethodSessionMostRecent": "session.most_recent",
		"MethodSessionResume":     "session.resume",
		"MethodSessionHistory":    "session.history",
		"MethodSessionStatus":     "session.status",
		"MethodSessionInterrupt":  "session.interrupt",
		"MethodPromptSubmit":      "prompt.submit",
		"MethodApprovalRespond":   "approval.respond",
		"MethodClarifyRespond":    "clarify.respond",
		"MethodSudoRespond":       "sudo.respond",
		"MethodSecretRespond":     "secret.respond",
		"MethodToolsList":         "tools.list",
		"MethodCliExec":           "cli.exec",
		"MethodTerminalResize":    "terminal.resize",
	}
	got := map[string]string{
		"MethodSessionCreate":     MethodSessionCreate,
		"MethodSessionList":       MethodSessionList,
		"MethodSessionMostRecent": MethodSessionMostRecent,
		"MethodSessionResume":     MethodSessionResume,
		"MethodSessionHistory":    MethodSessionHistory,
		"MethodSessionStatus":     MethodSessionStatus,
		"MethodSessionInterrupt":  MethodSessionInterrupt,
		"MethodPromptSubmit":      MethodPromptSubmit,
		"MethodApprovalRespond":   MethodApprovalRespond,
		"MethodClarifyRespond":    MethodClarifyRespond,
		"MethodSudoRespond":       MethodSudoRespond,
		"MethodSecretRespond":     MethodSecretRespond,
		"MethodToolsList":         MethodToolsList,
		"MethodCliExec":           MethodCliExec,
		"MethodTerminalResize":    MethodTerminalResize,
	}
	for name, wantValue := range want {
		if got[name] != wantValue {
			t.Errorf("%s = %q, want %q", name, got[name], wantValue)
		}
	}
}

// TestApprovalChoicesMatchTheInkClient pins the four values server-side callers
// accept. An unrecognised choice resolves the approval with a string no caller
// expects, which is why the UI validates before sending.
func TestApprovalChoicesMatchTheInkClient(t *testing.T) {
	got := []string{ApprovalOnce, ApprovalSession, ApprovalAlways, ApprovalDeny}
	want := []string{"once", "session", "always", "deny"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("choice %d = %q, want %q", i, got[i], want[i])
		}
	}
}
