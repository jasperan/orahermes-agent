# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Is

**orahermes-agent** is a fork of [NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent) — a persistent AI agent harness built around an OpenAI-compatible tool-calling loop. Two provider swaps from upstream:

- **OpenRouter → OCI GenAI** (xAI Grok models via `oci-openai` SDK)
- **SQLite → Oracle 26ai Free** (`oracledb` with Oracle Text full-text search)

Everything else (skills, messaging gateways, cron scheduler, context compression, terminal backends, batch processing) is preserved from upstream.

## Commands

```bash
# Setup (first time)
python3 -m venv venv              # Create virtual environment
source venv/bin/activate          # Always activate venv first
pip install -e ".[dev]"           # Install with dev deps (pytest)
pip install -e ".[all]"           # Install everything

# Run (orahermes is the primary binary; hermes also works as an alias)
orahermes                            # Interactive chat
orahermes chat -q "test message"     # Single query
orahermes doctor                     # Diagnostics
orahermes setup                      # Config wizard

# Tests
pytest                            # Unit tests (integration skipped by default)
pytest tests/test_oracle_state.py # Oracle tests (needs ORACLE_DSN env var)
pytest -m integration             # Integration tests only (need external services)
pytest tests/tools/test_file_tools.py -k "test_read"  # Single test
```

## Architecture

### Core Loop

```
User → AIAgent.chat() [run_agent.py] → _run_agent_loop()
  → OCI GenAI API (oci-openai) → tool calls?
    → Yes: model_tools.handle_function_call() → tools/registry.py dispatch → loop back
    → No: return text response
```

### Import/Dependency Chain (circular-import safe)

```
tools/registry.py          ← no deps, singleton ToolRegistry
       ↑
tools/*.py                 ← each registers itself at import time
       ↑
model_tools.py             ← imports registry + triggers discovery via _discover_tools()
       ↑
run_agent.py, cli.py, batch_runner.py
```

### Session Backend (Dependency Injection)

`hermes_state.get_session_db()` returns `OracleSessionDB` if `ORACLE_DSN` is set, else falls back to `SessionDB` (SQLite). Both implement identical method signatures: `create_session()`, `end_session()`, `append_message()`, `get_messages()`, `search_messages()`, etc.

### Key Oracle-Specific Files

| File | Purpose |
|------|---------|
| `oci_client.py` | Sync/async `OciOpenAI` client factory via `OciUserPrincipalAuth` |
| `oracle_state.py` | `OracleSessionDB` with connection pool, CLOB columns, `CTXSYS.CONTEXT` full-text search |
| `oracle_setup.sql` | DDL: sessions, messages tables, Oracle Text index |
| `hermes_constants.py` | `DEFAULT_MODEL = "xai.grok-3-mini"`, OCI GenAI endpoint URLs |
| `tools/openrouter_client.py` | Shared lazy `AsyncOciOpenAI` for tool modules (file kept its upstream name) |
| `agent/auxiliary_client.py` | OCI GenAI auxiliary client (Llama 3.3 70B) for compression/extraction |
| `agent/model_metadata.py` | Static local model catalogue (replaces upstream's live API fetch) |

### Async Bridging

`model_tools._run_async()` is the single sync-to-async bridge. If a running event loop is detected (e.g., gateway's async stack calling sync tool handlers), it uses `ThreadPoolExecutor` to avoid loop conflicts.

## Adding a New Tool

Only **2 files** needed (+ optional 3rd):

1. **`tools/your_tool.py`** — handler, schema, check function, `registry.register()` call
2. **`toolsets.py`** — add tool name to `_HERMES_CORE_TOOLS` or a specific toolset
3. **`model_tools.py`** — add `"tools.your_tool"` to `_discover_tools()` import list

All handlers must return a JSON string. The registry wraps exceptions in `{"error": "..."}` automatically. Tools needing agent-level state (like `todo`, `memory`) are intercepted in `run_agent.py` before `handle_function_call()`.

## Adding Config Options

- **`config.yaml` options**: Add to `DEFAULT_CONFIG` in `hermes_cli/config.py`, bump `_config_version`
- **`.env` variables**: Add to `REQUIRED_ENV_VARS` or `OPTIONAL_ENV_VARS` in `hermes_cli/config.py`

## Project Conventions

- User config lives in `~/.hermes/` (config.yaml + .env)
- OCI profile reads from `~/.oci/config` (default profile: `foosball`)
- Tool handlers use `task_id` parameter for session isolation in concurrent tasks
- Messages follow OpenAI format (`role`/`content`/`tool_calls`/`tool_call_id`)
- Trajectories export in ShareGPT format with `<tool_call>`, `<tool_response>`, `<think>` XML tags
- `agent/prompt_builder.py` scans context files for prompt injection before including them in system prompt

## Git Practices

- Do NOT include AI attribution in commit messages
- Push to GitHub is allowed — run `git diff --cached` first to audit for sensitive data. Never commit .env, credentials, or API keys
