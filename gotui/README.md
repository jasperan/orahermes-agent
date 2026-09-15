# `orahermes-tui` — a Go front-end for orahermes-agent

A terminal UI for orahermes-agent written in Go with
[Bubble Tea v2](https://charm.land) and [`huh`](https://charm.land/huh).

It is an **additional** way to run the project, not a replacement. It never
reimplements the agent: it spawns the repository's own Python `tui_gateway` and
speaks the same newline-delimited JSON-RPC protocol over stdio that the shipped
Ink TUI (`ui-tui/src/gatewayClient.ts`) already speaks.

```
orahermes-tui (Go, Bubble Tea v2)
  └─ stdio JSON-RPC ── python -u -m tui_gateway.entry
                          └─ AIAgent + tools + sessions + Oracle
```

TypeScript's Ink client, the `prompt_toolkit` CLI and this Go client are peers.
A Go user and an Ink user get identical results from identical state, because
sessions, tools, model calls and slash-command logic all stay in Python.

## Build

```bash
cd gotui
go build ./cmd/orahermes-tui
```

Go 1.25+ is required by the charm v2 stack (`go.mod` declares 1.26.0; the
toolchain is fetched automatically).

## Run

```bash
# Full-screen UI. Walks a launch form, then chats.
./orahermes-tui

# Open a stored thread directly
./orahermes-tui --thread <session-id>

# Non-interactive and scriptable — never prompts, never needs a TTY
./orahermes-tui --status
./orahermes-tui --threads
./orahermes-tui --threads --json
./orahermes-tui --most-recent
./orahermes-tui --prompt "what changed in the last deploy?" --json
./orahermes-tui --prompt "..." --wait 10m
./orahermes-tui --doctor

# Screen-reader mode: plain prompts from the same form definitions
ACCESSIBLE=1 ./orahermes-tui
```

### Flags

| Flag | Meaning |
|---|---|
| `--project-root` | orahermes-agent checkout holding `tui_gateway/`. Defaults to the repo this binary was built in. |
| `--python` | Interpreter. Defaults to `$HERMES_PYTHON`, `$PYTHON`, `$VIRTUAL_ENV`, then the checkout's `.venv`. |
| `--thread <id>` | Open a stored thread. |
| `--prompt <text>` | Send one message, print the answer, exit. |
| `--status` / `--threads` / `--most-recent` / `--doctor` | Scripted readouts. |
| `--json` | Machine-readable output for the scripted actions. |
| `--wait <dur>` | How long `--prompt` waits for the turn to finish (default 5m). |
| `--no-input` | Never prompt; fail instead if input would be required. |

## What it covers

Three surfaces, chosen as the highest-value interactive flows:

1. **Conversation** — streaming transcript, multi-line composer, `esc` to
   interrupt a running turn, a live tool/status activity line.
2. **Thread browsing** — `session.list` with resume, so a stored conversation
   can be picked up.
3. **Status / doctor** — the session readout, the enabled toolsets, and
   `hermes doctor` run through the gateway's own `cli.exec`.

Interactions the agent itself requests are handled too: tool approval
(`approval.request` with the same four choices as the Ink client: allow once /
this session / always / deny) and the `clarify` / `sudo` / `secret` prompts.

## Design notes

- **The palette is not defined here.** `internal/skin` decodes the skin the
  gateway announces on `gateway.ready` — the same `resolve_skin()` output
  `hermes_cli/skin_engine.py` gives the CLI. Switching skins restyles this UI
  too, and there is one place to change a colour for the whole project.
- **The launch command mirrors `gatewayClient.ts` exactly.** Interpreter search
  order, `PYTHONPATH`, `HERMES_PYTHON_SRC_ROOT` and `HERMES_CWD` all follow the
  Ink client, so both front-ends start the gateway the same way. `-u` and
  `PYTHONUNBUFFERED=1` are both set: Python's stdout is fully buffered on a pipe,
  and without unbuffered output `gateway.ready` never arrives.
- **Non-TTY is a supported mode, not an accident.** Without a terminal, the
  scripted flags run and a bare invocation fails with guidance instead of
  hanging on a prompt that can never be answered.
- **Narrow terminals cannot break it.** Every renderer is guarded
  (`MinRenderWidth`, `MinPaneWidth`); a 1-column resize draws a one-line notice
  rather than panicking on negative width arithmetic.
- **Bubble Tea v2 key semantics.** Only `tea.KeyPressMsg` is handled. v2 also
  delivers key *releases*, and acting on both fires every binding twice.

## Verification

```bash
gofmt -l .          # must print nothing
go build ./...
go test ./...
go vet ./...
```

The suite covers the protocol codec, interpreter discovery, the RPC client
(against a real stub gateway process, plus a stdlib-only Python stub so framing
is proven across two JSON implementations), skin resolution and its fallback
chains, the renderers at every width from 1 to 40 columns, the model's event
routing, and the accessible path.

## Deliberately out of scope

This front-end is intentionally not a full port. Left with the gateway:

- Voice (`voice.toggle` / `voice.record` / `voice.tts`) and browser automation.
- Subagent / delegation dashboards (`delegation.*`, `subagent.*`,
  `spawn_tree.*`).
- The rollback, insights, cron, kanban, plugins, skills-management and model
  picker UIs — all remain reachable as slash commands from the composer.
- Clipboard and image attach (`clipboard.paste`, `image.attach`).
- Attaching to a gateway over WebSocket (Ink supports `--attach`); this client
  spawns its own stdio gateway only.
