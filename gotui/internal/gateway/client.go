package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Timeouts and caps. Every one of them exists so a wedged gateway surfaces as
// an error state in the UI instead of a freeze.
const (
	// ReadyTimeout bounds how long Start waits for the gateway.ready frame.
	// Python import cost dominates cold start on a large checkout, so this is
	// generous compared with an ordinary RPC.
	ReadyTimeout = 60 * time.Second
	// RequestTimeout bounds an ordinary RPC.
	RequestTimeout = 120 * time.Second
	// ShutdownGrace is how long Close waits after closing stdin for the
	// gateway to exit on its own before the process is killed.
	ShutdownGrace = 3 * time.Second
	// maxQueuedEvents bounds the event backlog. A front-end that stops
	// draining (a modal form left open during a long turn) must not grow
	// without limit; the oldest events are dropped and counted.
	maxQueuedEvents = 20000
	// stderrRingSize is how many stderr lines are kept for diagnostics.
	stderrRingSize = 200
	// maxLineBytes bounds one frame. A transcript message can be large, but a
	// single line past this is a runaway producer, not a message.
	maxLineBytes = 16 << 20
)

// Event is one decoded event frame.
type Event struct {
	Type      string
	SessionID string
	Payload   json.RawMessage
}

// Decode unmarshals the event payload into v.
func (e Event) Decode(v any) error {
	if len(e.Payload) == 0 {
		return nil
	}
	return json.Unmarshal(e.Payload, v)
}

// Client is a running gateway subprocess and its RPC connection.
type Client struct {
	launch Launch
	cmd    *exec.Cmd
	stdin  io.WriteCloser

	nextID atomic.Uint64

	writeMu sync.Mutex

	mu      sync.Mutex
	pending map[uint64]chan callResult

	queue *eventQueue

	stderrMu   sync.Mutex
	stderrRing []string
	stderrDrop int

	closed    atomic.Bool
	closeOnce sync.Once
	waitOnce  sync.Once
	waitErr   error
	exited    chan struct{}

	// ready is closed exactly once, on the gateway.ready event.
	ready     chan struct{}
	readyOnce sync.Once
}

// callResult is one RPC outcome.
type callResult struct {
	result json.RawMessage
	err    error
}

// Start launches the gateway and waits for its gateway.ready event.
//
// A non-nil error means the process could not be started or never became ready;
// in either case any process that did start has already been reaped, so the
// caller does not need to clean up.
func Start(ctx context.Context, launch Launch) (*Client, error) {
	if len(launch.Argv) == 0 {
		return nil, errors.New("empty launch command")
	}

	cmd := exec.Command(launch.Argv[0], launch.Argv[1:]...)
	cmd.Env = launch.Env
	cmd.Dir = launch.Dir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("gateway stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("gateway stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("gateway stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", launch.Argv[0], err)
	}

	c := &Client{
		launch:  launch,
		cmd:     cmd,
		stdin:   stdin,
		pending: make(map[uint64]chan callResult),
		queue:   newEventQueue(maxQueuedEvents),
		exited:  make(chan struct{}),
		ready:   make(chan struct{}),
	}

	go c.readStdout(stdout)
	go c.readStderr(stderr)
	go c.reap()

	if err := c.waitReady(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// Launch returns the command this client started.
func (c *Client) Launch() Launch { return c.launch }

// PID returns the gateway process id, or 0 once it has exited.
func (c *Client) PID() int {
	if c.cmd == nil || c.cmd.Process == nil || c.closed.Load() {
		return 0
	}
	return c.cmd.Process.Pid
}

// Next blocks until the next event, the context is done, or the gateway exits.
//
// The returned error is io.EOF once the gateway is gone, which callers render
// as "gateway exited" rather than as a crash.
func (c *Client) Next(ctx context.Context) (Event, error) { return c.queue.pop(ctx) }

// Stderr returns the retained stderr lines and how many were dropped.
//
// The Ink client surfaces these lines as gateway.stderr events; this front-end
// keeps them for the problem pane instead of interleaving them with the
// transcript, because a Python traceback mid-conversation is noise.
func (c *Client) Stderr() (lines []string, dropped int) {
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	return append([]string(nil), c.stderrRing...), c.stderrDrop
}

// Exited is closed when the gateway process has been reaped.
func (c *Client) Exited() <-chan struct{} { return c.exited }

// WaitErr returns the process exit error, once Exited is closed.
func (c *Client) WaitErr() error { return c.waitErr }

// Call performs one JSON-RPC request and decodes the result into v.
//
// v may be nil when the result is not needed. A gateway error is returned as
// *RPCError so callers can branch on Code.
func (c *Client) Call(ctx context.Context, method string, params map[string]any, v any) error {
	raw, err := c.CallRaw(ctx, method, params)
	if err != nil {
		return err
	}
	if v == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("decode %s result: %w", method, err)
	}
	return nil
}

// CallRaw performs one request and returns the raw result.
func (c *Client) CallRaw(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	if c.closed.Load() {
		return nil, errors.New("gateway is not running")
	}

	id := c.nextID.Add(1)
	wait := make(chan callResult, 1)

	c.mu.Lock()
	c.pending[id] = wait
	c.mu.Unlock()

	if err := c.write(Request{JSONRPC: JSONRPCVersion, ID: id, Method: method, Params: orEmpty(params)}); err != nil {
		c.forget(id)
		return nil, err
	}

	select {
	case res := <-wait:
		return res.result, res.err
	case <-ctx.Done():
		c.forget(id)
		return nil, ctx.Err()
	case <-c.exited:
		c.forget(id)
		return nil, c.gatewayGone()
	}
}

// orEmpty keeps an argument-less call's params as {} rather than null.
func orEmpty(params map[string]any) map[string]any {
	if params == nil {
		return map[string]any{}
	}
	return params
}

// forget drops a pending call, so a late reply does not leak the channel.
func (c *Client) forget(id uint64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// write serializes one frame onto the gateway's stdin.
//
// The mutex is required: bubbletea commands run on their own goroutines, so two
// concurrent Calls would otherwise interleave halves of two JSON documents on
// the same pipe.
func (c *Client) write(req Request) error {
	line, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode %s request: %w", req.Method, err)
	}
	line = append(line, '\n')

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if _, err := c.stdin.Write(line); err != nil {
		return fmt.Errorf("send %s: %w", req.Method, err)
	}
	return nil
}

// readStdout decodes every frame the gateway emits.
func (c *Client) readStdout(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	for scanner.Scan() {
		frame, err := DecodeFrame(scanner.Bytes())
		if err != nil {
			// A malformed line is diagnostic, not fatal: the gateway itself
			// answers unparseable input with a -32700 error and keeps going.
			c.appendStderr("malformed frame: " + err.Error())
			continue
		}
		c.route(frame)
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		c.appendStderr("stdout read: " + err.Error())
	}
	c.queue.close()
}

// route dispatches one frame to either a pending call or the event queue.
func (c *Client) route(frame Frame) {
	if !frame.IsEvent() {
		if frame.ID == nil {
			// A response without an id is not addressable; report it rather
			// than silently dropping it.
			c.appendStderr("response without id")
			return
		}
		c.mu.Lock()
		wait, ok := c.pending[*frame.ID]
		delete(c.pending, *frame.ID)
		c.mu.Unlock()
		if !ok {
			c.appendStderr(fmt.Sprintf("response for unknown id %d", *frame.ID))
			return
		}

		res := callResult{result: frame.Result}
		if frame.Error != nil {
			res.err = frame.Error
		}
		wait <- res
		return
	}

	event := Event{Type: frame.EventType()}
	if frame.Params != nil {
		event.SessionID = frame.Params.SessionID
		event.Payload = frame.Params.Payload
	}
	if event.Type == "" {
		c.appendStderr("event without type")
		return
	}
	if event.Type == EventGatewayReady {
		c.readyOnce.Do(func() { close(c.ready) })
	}
	c.queue.push(event)
}

// waitReady blocks until gateway.ready arrives, the process exits, or the
// context expires.
//
// It waits on the c.ready signal that route() raises, and deliberately does NOT
// read from the event queue: the gateway.ready event stays queued so the UI is
// still the one that observes it (it carries the skin the Ink client also uses).
func (c *Client) waitReady(ctx context.Context) error {
	deadline := ReadyTimeout
	if existing, ok := ctx.Deadline(); ok {
		if remaining := time.Until(existing); remaining < deadline {
			deadline = remaining
		}
	}
	waitCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	select {
	case <-c.ready:
		return nil
	case <-c.exited:
		return fmt.Errorf("gateway exited before it was ready: %w", c.gatewayGone())
	case <-waitCtx.Done():
		return fmt.Errorf("gateway did not become ready within %s: %w", deadline, waitCtx.Err())
	}
}

// readStderr retains the gateway's stderr for diagnostics.
func (c *Client) readStderr(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 8*1024), 256*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		c.appendStderr(line)
	}
}

func (c *Client) appendStderr(line string) {
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	if len(c.stderrRing) == stderrRingSize {
		copy(c.stderrRing, c.stderrRing[1:])
		c.stderrRing[len(c.stderrRing)-1] = line
		c.stderrDrop++
		return
	}
	c.stderrRing = append(c.stderrRing, line)
}

// reap waits for the process so the zombie is collected exactly once.
func (c *Client) reap() {
	c.waitOnce.Do(func() {
		c.waitErr = c.cmd.Wait()
		c.closed.Store(true)
		c.queue.close()
		close(c.exited)
	})
}

// gatewayGone describes an exit in terms a user can act on.
func (c *Client) gatewayGone() error {
	if lines, _ := c.Stderr(); len(lines) > 0 {
		return fmt.Errorf("gateway exited: %s", lines[len(lines)-1])
	}
	if c.waitErr != nil {
		return fmt.Errorf("gateway exited: %v", c.waitErr)
	}
	return errors.New("gateway exited")
}

// Close shuts the gateway down.
//
// Closing stdin is the protocol's own stop signal: entry.py's read loop ends on
// stdin EOF and logs "stdin EOF (TUI closed the command pipe)". The process is
// killed only if it does not exit inside ShutdownGrace. Close is idempotent.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		c.closed.Store(true)

		c.writeMu.Lock()
		_ = c.stdin.Close()
		c.writeMu.Unlock()

		select {
		case <-c.exited:
			return
		case <-time.After(ShutdownGrace):
		}

		// entry.py gives itself a shutdown grace and then calls os._exit(0) on
		// a timer; SIGKILL is the backstop for a process that ignores both.
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		select {
		case <-c.exited:
		case <-time.After(ShutdownGrace):
		}
	})
}

// eventQueue is an unbounded-ish FIFO that decouples the stdout reader from the
// UI. A plain channel cannot be used: the reader must never block on a slow
// consumer, or a reply to a request issued by that same consumer would never be
// read and the two would deadlock.
type eventQueue struct {
	mu      sync.Mutex
	items   []Event
	cap     int
	dropped int
	closed  bool
	notify  chan struct{}
}

func newEventQueue(capacity int) *eventQueue {
	return &eventQueue{cap: capacity, notify: make(chan struct{}, 1)}
}

func (q *eventQueue) push(event Event) {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	if q.cap > 0 && len(q.items) >= q.cap {
		// Drop the oldest: the newest events describe the current state of the
		// turn, which is what a late consumer needs.
		copy(q.items, q.items[1:])
		q.items[len(q.items)-1] = event
		q.dropped++
	} else {
		q.items = append(q.items, event)
	}
	q.mu.Unlock()
	q.wake()
}

func (q *eventQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.wake()
}

func (q *eventQueue) wake() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// Dropped reports how many events were discarded under back-pressure.
func (q *eventQueue) Dropped() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.dropped
}

// pop returns the oldest event, blocking until one arrives.
func (q *eventQueue) pop(ctx context.Context) (Event, error) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			event := q.items[0]
			q.items = q.items[1:]
			q.mu.Unlock()
			return event, nil
		}
		closed := q.closed
		q.mu.Unlock()

		if closed {
			return Event{}, io.EOF
		}

		select {
		case <-q.notify:
		case <-ctx.Done():
			return Event{}, ctx.Err()
		}
	}
}
