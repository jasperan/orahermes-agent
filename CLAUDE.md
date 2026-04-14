# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Is

**orahermes-agent** is a fork of [NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent) — a persistent AI agent harness built around an OpenAI-compatible tool-calling loop. Two provider swaps from upstream:

- **OpenRouter → OCI GenAI** (xAI Grok models via `oci-openai` SDK)
- **SQLite → Oracle 26ai Free** (`oracledb` with Oracle Text full-text search + optional vector search)

Everything else (skills, messaging gateways, cron scheduler, context compression, terminal backends, batch processing, ACP adapter, web API) is preserved from upstream.

## Commands

```bash
# Setup (first time) — venv preferred over conda here; project ships a venv/
source venv/bin/activate          # Always activate venv first
pip install -e ".[dev]"           # Core + pytest + mcp
pip install -e ".[oracle]"        # Add oracledb + oci-openai
pip install -e ".[all]"           # Everything (messaging gateways, voice, RL, etc.)

# Run (orahermes is the primary binary; hermes also works as an alias)
orahermes                            # Interactive chat
orahermes chat -q "test message"     # Single query
orahermes doctor                     # Diagnostics
orahermes setup                      # Config wizard

# Gateway (messaging platforms — Telegram, Discord, Slack, etc.)
python -m gateway.run                # Start all configured gateway platforms

# Web API server (FastAPI, SSE streaming)
python -m webapi                     # Starts on configured port

# ACP adapter (Agent Client Protocol)
hermes-acp                           # Entry point from pyproject.toml

# Batch / RL
python batch_runner.py --config datagen-config-examples/example.yaml
python rl_cli.py                     # RL training CLI

# Tests
pytest                               # Unit tests (integration skipped via pytest.ini)
pytest tests/test_oracle_state.py    # Oracle tests (needs ORACLE_DSN env var)
pytest -m integration                # Integration tests only (need external services)
pytest tests/tools/test_file_tools.py -k "test_read"  # Single test

# Oracle DB setup (run once against FREEPDB1)
sqlplus hermes/password@localhost:1523/FREEPDB1 @oracle_setup.sql
sqlplus hermes/password@localhost:1523/FREEPDB1 @oracle_setup_vector.sql  # Optional: AI Vector Search
```

## Environment / Config

User config lives in `~/.hermes/` (config.yaml + .env). `orahermes setup` populates it interactively.

Key env vars (set in `~/.hermes/.env` or shell):

| Variable | Required | Purpose |
|----------|----------|---------|
| `ORACLE_DSN` | For Oracle backend | e.g. `localhost:1523/FREEPDB1` |
| `ORACLE_USER` / `ORACLE_PASSWORD` | For Oracle backend | DB credentials |
| `OCI_CONFIG_FILE` | For OCI GenAI | defaults to `~/.oci/config` |
| `OCI_PROFILE` | For OCI GenAI | defaults to `foosball` |
| `EXA_API_KEY` | For web search tool | Exa search |
| `FIRECRAWL_API_KEY` | For web scraping tool | Firecrawl |

OCI profile reads from `~/.oci/config` (default profile: `foosball`). If OCI auth fails, check `oci_client.py`'s `OciUserPrincipalAuth` — it uses the profile's key_file path.

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

`hermes_state.get_session_db()` returns `OracleSessionDB` if `ORACLE_DSN` is set, else falls back to `SessionDB` (SQLite). Both implement identical method signatures: `create_session()`, `end_session()`, `append_message()`, `get_messages()`, `search_messages()`.

### Key Oracle-Specific Files

| File | Purpose |
|------|---------|
| `oci_client.py` | Sync/async `OciOpenAI` client factory via `OciUserPrincipalAuth` |
| `oracle_state.py` | `OracleSessionDB` with connection pool, CLOB columns, `CTXSYS.CONTEXT` full-text search |
| `oracle_setup.sql` | DDL: sessions + messages tables, Oracle Text index |
| `oracle_setup_vector.sql` | DDL migration v2: adds `VECTOR` column + `ALL_MINILM_L6_V2` ONNX embedding model for semantic recall |
| `hermes_constants.py` | `DEFAULT_MODEL = "xai.grok-3-mini"`, OCI GenAI endpoint URLs |
| `tools/openrouter_client.py` | Shared lazy `AsyncOciOpenAI` for tool modules (kept upstream filename) |
| `agent/auxiliary_client.py` | OCI GenAI auxiliary client (Llama 3.3 70B) for compression/extraction |
| `agent/model_metadata.py` | Static local model catalogue (replaces upstream's live API fetch) |

### Async Bridging

`model_tools._run_async()` is the single sync-to-async bridge. If a running event loop is detected (e.g., gateway's async stack calling sync tool handlers), it dispatches via `ThreadPoolExecutor` to avoid loop conflicts.

### Other Subsystems

- **`gateway/platforms/`** — 15+ messaging platform adapters (Telegram, Discord, Slack, Signal, WhatsApp, Matrix, email, webhook, etc.). Add new ones following `ADDING_A_PLATFORM.md`.
- **`webapi/`** — FastAPI server with SSE streaming for browser/API clients.
- **`acp_adapter/`** — Agent Client Protocol adapter (`hermes-acp` binary).
- **`cron/`** — croniter-based task scheduler.
- **`tools/semantic_recall_tool.py`** — uses Oracle AI Vector Search when `oracle_setup_vector.sql` has been applied; falls back gracefully if vectors aren't provisioned.
- **`tools/mcp_tool.py`** — MCP client that exposes external MCP server tools into the agent loop.

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

- Tool handlers use `task_id` parameter for session isolation in concurrent tasks
- Messages follow OpenAI format (`role`/`content`/`tool_calls`/`tool_call_id`)
- Trajectories export in ShareGPT format with `<tool_call>`, `<tool_response>`, `<think>` XML tags
- `agent/prompt_builder.py` scans context files for prompt injection before including them in system prompt
- `skills/` directory mirrors upstream structure (skill bundles, not Python modules)

## Gotchas

- **`[oracle]` extra is not in `[all]`** by design — install it explicitly: `pip install -e ".[oracle]"`. The `[all]` group omits oracle to avoid forcing OCI/oracledb on non-Oracle installs.
- **`tools/openrouter_client.py`** is named after the upstream file but actually wraps `AsyncOciOpenAI`. Don't rename it — too many imports reference it by that path.
- **Vector search** requires `oracle_setup_vector.sql` applied AND the `ALL_MINILM_L6_V2` ONNX model loaded into the DB (see SQL file header for `DBMS_VECTOR.LOAD_ONNX_MODEL` instructions). Missing model → `semantic_recall_tool` silently falls back to keyword search.
- **pytest runs with `-n auto`** (parallel workers via pytest-xdist). Tests that share global state or hit the same DB need `@pytest.mark.usefixtures` isolation.
- **OCI auth errors** almost always mean the key_file path in `~/.oci/config` is wrong or the tenancy OCID doesn't match the compartment used in `hermes_constants.py`.

## Git Practices

- Do NOT include AI attribution in commit messages
- Push to GitHub is allowed — run `git diff --cached` first to audit for sensitive data. Never commit .env, credentials, or API keys
