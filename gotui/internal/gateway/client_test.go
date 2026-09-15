package gateway

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubEnv turns this test binary into a fake gateway.
//
// TestMain intercepts it before the testing framework runs, so the stub owns
// stdout completely: anything the framework printed there would corrupt the
// protocol stream. This is the same technique the standard library's os/exec
// tests use for helper processes.
const stubEnv = "GOTUI_STUB_GATEWAY"

func TestMain(m *testing.M) {
	if os.Getenv(stubEnv) == "1" {
		runStubGateway()
		return // runStubGateway exits.
	}
	os.Exit(m.Run())
}

// stubLaunch starts the test binary as a gateway.
func stubLaunch(t *testing.T) Launch {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	env := append(os.Environ(), stubEnv+"=1")
	return Launch{Argv: []string{executable}, Env: env}
}

func TestStartReachesReady(t *testing.T) {
	client, err := Start(context.Background(), stubLaunch(t))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Close()

	if client.PID() == 0 {
		t.Fatal("a started client must report a pid")
	}
	if got := client.Launch().CommandLine(); got == "" {
		t.Fatal("Launch().CommandLine() must not be empty")
	}
}

func TestCallDecodesResult(t *testing.T) {
	client, err := Start(context.Background(), stubLaunch(t))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var list SessionListResult
	if err := client.Call(ctx, MethodSessionList, map[string]any{}, &list); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(list.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(list.Sessions))
	}
	if list.Sessions[0].ID != "stub-thread" {
		t.Fatalf("unexpected session: %+v", list.Sessions[0])
	}
}

// TestCallReturnsRPCError proves the gateway's own error envelope reaches the
// caller as a typed error, not as a decode failure.
func TestCallReturnsRPCError(t *testing.T) {
	client, err := Start(context.Background(), stubLaunch(t))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = client.Call(ctx, MethodSessionResume, map[string]any{"session_id": "missing"}, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("err = %v (%T), want *RPCError", err, err)
	}
	if rpcErr.Code != 4007 || rpcErr.Message != "session not found" {
		t.Fatalf("unexpected rpc error: %+v", rpcErr)
	}
}

// TestEventsAreDeliveredInOrder checks the queue preserves the gateway's order,
// which matters because message.delta frames must concatenate correctly.
func TestEventsAreDeliveredInOrder(t *testing.T) {
	client, err := Start(context.Background(), stubLaunch(t))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// gateway.ready is the first event the gateway emits and Start leaves it
	// queued, so the UI still observes it.
	first, err := client.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if first.Type != EventGatewayReady {
		t.Fatalf("first event = %q, want %q", first.Type, EventGatewayReady)
	}

	// Trigger the streaming sequence.
	if err := client.Call(ctx, MethodPromptSubmit, map[string]any{"text": "hi"}, nil); err != nil {
		t.Fatalf("prompt.submit: %v", err)
	}

	var sequence []string
	for len(sequence) < 8 {
		event, err := client.Next(ctx)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		sequence = append(sequence, event.Type)
		if event.Type == EventMessageDone {
			break
		}
	}

	if len(sequence) < 3 {
		t.Fatalf("only %d events arrived: %v", len(sequence), sequence)
	}
	if sequence[0] != EventMessageStart {
		t.Fatalf("first streamed event = %q, want %q", sequence[0], EventMessageStart)
	}
	if last := sequence[len(sequence)-1]; last != EventMessageDone {
		t.Fatalf("last streamed event = %q, want %q", last, EventMessageDone)
	}
	// Every event between the start and the completion must be a delta, and the
	// stub emits three of them.
	deltas := 0
	for _, event := range sequence[1 : len(sequence)-1] {
		if event != EventMessageDelta {
			t.Fatalf("unexpected event %q inside the stream: %v", event, sequence)
		}
		deltas++
	}
	if deltas != 3 {
		t.Fatalf("deltas = %d, want 3: %v", deltas, sequence)
	}
}

// TestDeltaTextConcatenates is the end-to-end property the chat view depends on.
func TestDeltaTextConcatenates(t *testing.T) {
	client, err := Start(context.Background(), stubLaunch(t))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Call(ctx, MethodPromptSubmit, map[string]any{"text": "hi"}, nil); err != nil {
		t.Fatalf("prompt.submit: %v", err)
	}

	var assembled strings.Builder
	for {
		event, err := client.Next(ctx)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		switch event.Type {
		case EventMessageDelta:
			var delta MessageDelta
			if err := event.Decode(&delta); err != nil {
				t.Fatalf("decode delta: %v", err)
			}
			assembled.WriteString(delta.Text)
		case EventMessageDone:
			var done MessageComplete
			if err := event.Decode(&done); err != nil {
				t.Fatalf("decode complete: %v", err)
			}
			if assembled.String() != done.Text {
				t.Fatalf("assembled %q, complete said %q", assembled.String(), done.Text)
			}
			return
		}
	}
}

// TestCallRespectsContextDeadline proves a wedged gateway cannot freeze the UI:
// the RPC returns when the caller's context expires.
func TestCallRespectsContextDeadline(t *testing.T) {
	client, err := Start(context.Background(), stubLaunch(t))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = client.Call(ctx, "stub.sleep", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected the call to fail")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("the call took %s; the deadline was not honoured", elapsed)
	}
}

// TestConcurrentCallsDoNotInterleaveFrames checks the write mutex: two writers
// must not split each other's JSON across the pipe.
func TestConcurrentCallsDoNotInterleaveFrames(t *testing.T) {
	client, err := Start(context.Background(), stubLaunch(t))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	const workers = 12
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			var list SessionListResult
			errs <- client.Call(ctx, MethodSessionList, map[string]any{}, &list)
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent call: %v", err)
		}
	}
}

func TestCloseIsIdempotentAndReapsTheProcess(t *testing.T) {
	client, err := Start(context.Background(), stubLaunch(t))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	client.Close()
	client.Close() // must not panic or block

	select {
	case <-client.Exited():
	case <-time.After(10 * time.Second):
		t.Fatal("the gateway process was not reaped after Close")
	}

	// A call after Close reports the state instead of hanging.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Call(ctx, MethodSessionList, nil, nil); err == nil {
		t.Fatal("expected an error after Close")
	}
}

// TestStartFailsWhenTheInterpreterIsMissing keeps the failure a clear error
// rather than a hang.
func TestStartFailsWhenTheInterpreterIsMissing(t *testing.T) {
	launch := Launch{Argv: []string{filepath.Join(t.TempDir(), "definitely-not-a-binary")}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := Start(ctx, launch); err == nil {
		t.Fatal("expected a start error")
	}
}

// TestStartRejectsAnEmptyArgv guards the programmer error.
func TestStartRejectsAnEmptyArgv(t *testing.T) {
	if _, err := Start(context.Background(), Launch{}); err == nil {
		t.Fatal("expected an error for an empty command")
	}
}

// TestEventQueueDropsOldestUnderBackPressure pins the bound: a slow consumer must
// not grow memory without limit, and the newest events are the ones kept.
func TestEventQueueDropsOldestUnderBackPressure(t *testing.T) {
	queue := newEventQueue(3)
	for i := 0; i < 10; i++ {
		queue.push(Event{Type: fmt.Sprintf("e%d", i)})
	}
	if got := queue.Dropped(); got != 7 {
		t.Fatalf("Dropped() = %d, want 7", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, want := range []string{"e7", "e8", "e9"} {
		event, err := queue.pop(ctx)
		if err != nil {
			t.Fatalf("pop: %v", err)
		}
		if event.Type != want {
			t.Fatalf("event = %q, want %q", event.Type, want)
		}
	}
}

// TestStderrIsRetainedForDiagnostics keeps a Python traceback available to the
// problem pane rather than lost.
func TestStderrIsRetainedForDiagnostics(t *testing.T) {
	client, err := Start(context.Background(), stubLaunch(t))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Close()

	// The stub writes one stderr line during startup.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if lines, _ := client.Stderr(); len(lines) > 0 {
			if !strings.Contains(strings.Join(lines, "\n"), "stub gateway starting") {
				t.Fatalf("unexpected stderr: %v", lines)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the stub's stderr line was never retained")
}

// --- the stub gateway ---------------------------------------------------------------

// runStubGateway speaks the protocol well enough to exercise every client path,
// then exits. It is deliberately strict: an unparseable request produces the
// same -32700 error entry.py produces.
func runStubGateway() {
	fmt.Fprintln(os.Stderr, "stub gateway starting")

	writeLine(`{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{"skin":{"name":"stub","colors":{"ui_accent":"#cba6f7"}}}}}`)

	decoder := newLineReader(os.Stdin)
	for {
		line, ok := decoder.next()
		if !ok {
			// stdin EOF: entry.py logs this and exits 0.
			os.Exit(0)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		id, method, params := parseStubRequest(line)
		if method == "" {
			writeLine(`{"jsonrpc":"2.0","error":{"code":-32700,"message":"parse error"},"id":null}`)
			continue
		}

		switch method {
		case MethodSessionList:
			writeLine(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"sessions":[{"id":"stub-thread","title":"Stub thread","preview":"hello","started_at":1700000000,"message_count":3,"source":"tui"}]}}`, id))

		case MethodSessionCreate:
			writeLine(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"session_id":"stub-session","model":"stub-model"}}`, id))

		case MethodSessionResume:
			if !strings.Contains(params, "stub-session") {
				writeLine(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":4007,"message":"session not found"}}`, id))
				continue
			}
			writeLine(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"session_id":"stub-session"}}`, id))

		case MethodPromptSubmit:
			writeLine(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"accepted":true}}`, id))
			writeLine(`{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"stub-session"}}`)
			for _, piece := range []string{"Hel", "lo ", "there"} {
				writeLine(fmt.Sprintf(`{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","session_id":"stub-session","payload":{"text":%q}}}`, piece))
			}
			writeLine(`{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","session_id":"stub-session","payload":{"text":"Hello there","status":"complete"}}}`)

		case "stub.sleep":
			// Deliberately never answer; the caller's context must be what ends
			// the wait. The read loop continues so the stub stays alive.
			continue

		default:
			writeLine(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{}}`, id))
		}
	}
}

// writeLine writes one frame and flushes immediately, as the gateway's
// StdioTransport does.
func writeLine(line string) {
	fmt.Fprintln(os.Stdout, line)
	_ = os.Stdout.Sync()
}

// parseStubRequest extracts the id, method and raw params. It is intentionally
// crude: it only has to cope with what this test suite sends.
func parseStubRequest(line string) (id, method, params string) {
	id = "null"
	for _, field := range []struct {
		key string
		dst *string
	}{
		{`"id":`, &id},
		{`"method":"`, &method},
		{`"params":`, &params},
	} {
		index := strings.Index(line, field.key)
		if index < 0 {
			continue
		}
		rest := line[index+len(field.key):]
		if field.key == `"method":"` {
			if end := strings.Index(rest, `"`); end >= 0 {
				*field.dst = rest[:end]
			}
			continue
		}
		end := 0
		for end < len(rest) && rest[end] != ',' && rest[end] != '}' {
			end++
		}
		*field.dst = strings.TrimSpace(rest[:end])
	}
	return id, method, params
}

// lineReader reads stdin line by line without bufio so the stub cannot buffer
// past a request the caller is waiting on.
type lineReader struct{ r *os.File }

func newLineReader(f *os.File) *lineReader { return &lineReader{r: f} }

func (l *lineReader) next() (string, bool) {
	var buffer []byte
	one := make([]byte, 1)
	for {
		n, err := l.r.Read(one)
		if n > 0 {
			if one[0] == '\n' {
				return string(buffer), true
			}
			buffer = append(buffer, one[0])
		}
		if err != nil {
			if len(buffer) > 0 {
				return string(buffer), true
			}
			return "", false
		}
	}
}

// TestPythonStubProvesCrossLanguageFraming runs a stdlib-only Python gateway.
//
// The Go stub above shares a JSON encoder with the client, so it could hide a
// serializer-specific bug. This test removes that doubt: the frames are produced
// by Python's json.dumps, exactly as tui_gateway/transport.py produces them.
func TestPythonStubProvesCrossLanguageFraming(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		python, err = exec.LookPath("python")
	}
	if err != nil {
		t.Skip("no python interpreter available")
	}

	script := filepath.Join(t.TempDir(), "stub_gateway.py")
	if err := os.WriteFile(script, []byte(pythonStubSource), 0o644); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	// -u mirrors the real spawn, and is required for the same reason.
	launch := Launch{Argv: []string{python, "-u", script}}
	client, err := Start(context.Background(), launch)
	if err != nil {
		t.Fatalf("Start against the Python stub: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	first, err := client.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if first.Type != EventGatewayReady {
		t.Fatalf("first event = %q", first.Type)
	}
	// The skin in the frame must reach internal/skin unchanged.
	if !strings.Contains(string(first.Payload), "python-stub") {
		t.Fatalf("payload did not survive: %s", first.Payload)
	}

	if err := client.Call(ctx, MethodSessionList, map[string]any{}, nil); err != nil {
		t.Fatalf("session.list against the Python stub: %v", err)
	}

	if err := client.Call(ctx, MethodSessionResume, map[string]any{"session_id": "nope"}, nil); err == nil {
		t.Fatal("expected the Python stub's error envelope to surface")
	} else {
		var rpcErr *RPCError
		if !errors.As(err, &rpcErr) || rpcErr.Code != 4007 {
			t.Fatalf("err = %v, want an RPCError with code 4007", err)
		}
	}
}

// pythonStubSource is a minimal, stdlib-only gateway. It emits the same frame
// shapes as tui_gateway/entry.py.
const pythonStubSource = `import json, sys

def emit(obj):
    sys.stdout.write(json.dumps(obj, ensure_ascii=False) + "\n")
    sys.stdout.flush()

emit({"jsonrpc": "2.0", "method": "event",
      "params": {"type": "gateway.ready",
                 "payload": {"skin": {"name": "python-stub"}}}})

for raw in sys.stdin:
    raw = raw.strip()
    if not raw:
        continue
    try:
        req = json.loads(raw)
    except json.JSONDecodeError:
        emit({"jsonrpc": "2.0", "id": None,
              "error": {"code": -32700, "message": "parse error"}})
        continue

    rid, method = req.get("id"), req.get("method")
    if method == "session.list":
        emit({"jsonrpc": "2.0", "id": rid, "result": {
            "sessions": [{"id": "py-thread", "title": "From Python",
                          "preview": "", "started_at": 0,
                          "message_count": 1, "source": "tui"}]}})
    elif method == "session.resume":
        emit({"jsonrpc": "2.0", "id": rid,
              "error": {"code": 4007, "message": "session not found"}})
    else:
        emit({"jsonrpc": "2.0", "id": rid, "result": {}})
`
