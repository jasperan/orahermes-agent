// Package gateway speaks the orahermes-agent TUI gateway protocol.
//
// The protocol is defined by the repository's own Python backend,
// tui_gateway/entry.py + tui_gateway/server.py, and is the same newline-
// delimited JSON-RPC stream the shipped Ink TUI (ui-tui/src/gatewayClient.ts)
// already speaks over stdio:
//
//	{"jsonrpc":"2.0","id":1,"method":"session.list","params":{...}}        client -> gateway
//	{"jsonrpc":"2.0","id":1,"result":{...}}                                 gateway -> client
//	{"jsonrpc":"2.0","id":1,"error":{"code":4006,"message":"..."}}          gateway -> client
//	{"jsonrpc":"2.0","method":"event","params":{"type":"...","session_id":"...","payload":{...}}}
//
// Field names and method names here are copied from that source. Nothing in
// this package is guessed: see docs in each file for the exact line of Python
// each shape came from.
package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// JSONRPCVersion is the only protocol version the gateway accepts.
const JSONRPCVersion = "2.0"

// Request is one client -> gateway call.
type Request struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      uint64         `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

// NewRequest builds a request with the protocol version filled in.
//
// Params is never nil so an argument-less method still serializes as {} —
// server.py:_normalize_request accepts a missing params, but an explicit empty
// object keeps every frame the same shape in a log.
func NewRequest(id uint64, method string, params map[string]any) Request {
	if params == nil {
		params = map[string]any{}
	}
	return Request{JSONRPC: JSONRPCVersion, ID: id, Method: method, Params: params}
}

// RPCError is the JSON-RPC error object.
//
// The numeric codes are the gateway's own (server.py): -32700 parse error,
// -32600 invalid request, -32602 invalid params, and app codes in the 4xxx/5xxx
// range such as 4006 "session_id required", 4007 "session not found",
// 4009 "session busy", 5006 "session database unavailable".
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error implements error.
func (e *RPCError) Error() string {
	if e == nil {
		return "gateway error"
	}
	return fmt.Sprintf("gateway error %d: %s", e.Code, e.Message)
}

// Frame is any single line the gateway can emit.
//
// A frame is either a response (ID set) or an event (Method == "event"). The
// gateway never mixes the two in one line, so one struct can decode both and
// DecodeFrame routes on which fields are present.
type Frame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uint64         `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	Params  *EventParams    `json:"params,omitempty"`
}

// EventParams is the params object of an event frame.
type EventParams struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// Event types emitted by tui_gateway/server.py via _emit().
//
// The list is the full set of literal event names passed to _emit in
// server.py; the gateway may grow more, so consumers must ignore an unknown
// type rather than treat it as a protocol error.
const (
	EventGatewayReady    = "gateway.ready"
	EventSessionInfo     = "session.info"
	EventMessageStart    = "message.start"
	EventMessageDelta    = "message.delta"
	EventMessageDone     = "message.complete"
	EventReasoningDelta  = "reasoning.delta"
	EventReasoningAvail  = "reasoning.available"
	EventThinkingDelta   = "thinking.delta"
	EventToolProgress    = "tool.progress"
	EventToolComplete    = "tool.complete"
	EventToolGenerating  = "tool.generating"
	EventApprovalReq     = "approval.request"
	EventClarifyReq      = "clarify.request"
	EventSudoReq         = "sudo.request"
	EventSecretReq       = "secret.request"
	EventStatusUpdate    = "status.update"
	EventSkinChanged     = "skin.changed"
	EventVoiceStatus     = "voice.status"
	EventVoiceTranscript = "voice.transcript"
	EventBrowserProgress = "browser.progress"
	EventError           = "error"
)

// Method names used by this front-end.
//
// Taken from the @method("...") declarations in tui_gateway/server.py.
const (
	MethodSessionCreate     = "session.create"
	MethodSessionList       = "session.list"
	MethodSessionMostRecent = "session.most_recent"
	MethodSessionResume     = "session.resume"
	MethodSessionHistory    = "session.history"
	MethodSessionStatus     = "session.status"
	MethodSessionInterrupt  = "session.interrupt"
	MethodPromptSubmit      = "prompt.submit"
	MethodApprovalRespond   = "approval.respond"
	MethodClarifyRespond    = "clarify.respond"
	MethodSudoRespond       = "sudo.respond"
	MethodSecretRespond     = "secret.respond"
	MethodToolsList         = "tools.list"
	MethodCommandsCatalog   = "commands.catalog"
	MethodCliExec           = "cli.exec"
	MethodTerminalResize    = "terminal.resize"
)

// ErrMalformedFrame reports a line that is not a JSON object with a usable
// shape. entry.py answers an unparseable line with a -32700 error but never
// sends one itself, so this is a local diagnostic, not a protocol state.
var ErrMalformedFrame = errors.New("malformed gateway frame")

// DecodeFrame parses one line emitted by the gateway.
//
// Blank lines are skipped by the caller, not here: a blank line is not a frame.
func DecodeFrame(line []byte) (Frame, error) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return Frame{}, ErrMalformedFrame
	}
	var frame Frame
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	if err := decoder.Decode(&frame); err != nil {
		return Frame{}, fmt.Errorf("%w: %v", ErrMalformedFrame, err)
	}
	return frame, nil
}

// IsEvent reports whether the frame is an event rather than a response.
func (f Frame) IsEvent() bool { return f.Method == "event" }

// EventType returns the event name for an event frame, or "" otherwise.
func (f Frame) EventType() string {
	if !f.IsEvent() || f.Params == nil {
		return ""
	}
	return f.Params.Type
}

// MessageDelta is the payload of a message.delta event.
//
// server.py: def _stream(delta): payload = {"text": delta}; ...; if streamer and
// (r := streamer.feed(delta)) is not None: payload["rendered"] = r
type MessageDelta struct {
	Text     string `json:"text"`
	Rendered string `json:"rendered,omitempty"`
}

// MessageComplete is the payload of a message.complete event.
//
// server.py: payload = {"text": raw, "usage": _get_usage(agent), "status": status}
// plus optional "reasoning" and "warning".
type MessageComplete struct {
	Text      string         `json:"text"`
	Status    string         `json:"status"`
	Usage     map[string]any `json:"usage,omitempty"`
	Reasoning string         `json:"reasoning,omitempty"`
	Warning   string         `json:"warning,omitempty"`
	Rendered  string         `json:"rendered,omitempty"`
}

// Ready is the payload of the gateway.ready event, the first frame the
// gateway emits.
//
// entry.py: {"type": "gateway.ready", "payload": {"skin": resolve_skin()}}
type Ready struct {
	Skin map[string]any `json:"skin,omitempty"`
}

// SessionSummary is one row of a session.list result.
//
// server.py session.list returns:
//
//	{"sessions": [{"id","title","preview","started_at","message_count","source"}]}
type SessionSummary struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Preview      string `json:"preview"`
	StartedAt    int64  `json:"started_at"`
	MessageCount int    `json:"message_count"`
	Source       string `json:"source"`
}

// SessionListResult is the session.list result envelope.
type SessionListResult struct {
	Sessions []SessionSummary `json:"sessions"`
}

// SessionRef is the result of session.create / session.resume / session.most_recent.
//
// session.create and session.resume both return {"session_id": sid, ...}. The
// extra fields differ between the two, so only session_id is modelled; anything
// else is left in raw for display.
type SessionRef struct {
	SessionID string `json:"session_id"`
	Model     string `json:"model,omitempty"`
}

// StatusResult is the session.status result.
//
// server.py: return _ok(rid, {"output": "\n".join(lines)})
type StatusResult struct {
	Output string `json:"output"`
}

// HistoryResult is the session.history result.
//
// server.py: return _ok(rid, {"count": len(history), "messages": _history_to_messages(history)})
type HistoryResult struct {
	Count    int              `json:"count"`
	Messages []map[string]any `json:"messages"`
}

// Toolset is one row of a tools.list result.
type Toolset struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled,omitempty"`
	ToolCount   int    `json:"tool_count,omitempty"`
}

// ToolsListResult is the tools.list result envelope. The gateway nests the rows
// under "toolsets"; the front-end renders whatever it finds and never assumes a
// fixed number of toolsets.
type ToolsListResult struct {
	Toolsets []Toolset `json:"toolsets"`
}

// CliExecResult is the cli.exec result.
//
// server.py: {"blocked":..., "hint":..., "code":..., "output":...}
type CliExecResult struct {
	Blocked bool   `json:"blocked"`
	Hint    string `json:"hint"`
	Code    int    `json:"code"`
	Output  string `json:"output"`
}

// ApprovalRequest is the payload of an approval.request event.
//
// Source: ui-tui/src/gatewayTypes.ts — `{ command: string; description: string }`.
// There is deliberately no request_id: server.py's approval.respond resolves by
// session_key through tools.approval.resolve_gateway_approval(), not by a
// pending-request id, so an approval is answered with a session id and a choice.
type ApprovalRequest struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// Approval choices accepted by tools/approval.py's resolve_gateway_approval.
//
// Source: ui-tui/src/components/prompts.tsx — `const OPTS = ['once', 'session',
// 'always', 'deny']`. 'deny' is the only value the Ink client treats as a
// refusal; the other three all resolve the approval and differ in how long the
// permission lasts.
const (
	ApprovalOnce    = "once"
	ApprovalSession = "session"
	ApprovalAlways  = "always"
	ApprovalDeny    = "deny"
)

// ClarifyRequest is the payload of a clarify.request event.
//
// Source: ui-tui/src/gatewayTypes.ts — `{ choices: string[] | null; question:
// string; request_id: string }`. A nil Choices means free-form text.
type ClarifyRequest struct {
	Choices   []string `json:"choices"`
	Question  string   `json:"question"`
	RequestID string   `json:"request_id"`
}

// SudoRequest is the payload of a sudo.request event.
//
// Source: ui-tui/src/gatewayTypes.ts — `{ request_id: string }`.
type SudoRequest struct {
	RequestID string `json:"request_id"`
}

// SecretRequest is the payload of a secret.request event.
//
// Source: ui-tui/src/gatewayTypes.ts — `{ env_var: string; prompt: string;
// request_id: string }`.
type SecretRequest struct {
	EnvVar    string `json:"env_var"`
	Prompt    string `json:"prompt"`
	RequestID string `json:"request_id"`
}
