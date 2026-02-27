# orahermes-agent Design Document

**Date:** 2026-02-27
**Status:** Approved
**Base:** NousResearch/hermes-agent (snapshot fork)

## Overview

Full fork of hermes-agent with two provider swaps:
1. **LLM**: OpenRouter → OCI GenAI (via `oci-openai`) with xAI Grok as default model
2. **DB**: SQLite → Oracle 26ai Free (via `python-oracledb`)

All other features preserved: CLI, gateway, tools, RL training, skills, scheduling.

## LLM Provider Swap

### Approach
Use Oracle's official `oci-openai` package which wraps the standard OpenAI SDK with OCI authentication. Since hermes-agent already uses `openai.OpenAI` throughout, this is a drop-in replacement.

### Configuration
```
OCI_PROFILE=foosball
OCI_REGION=us-chicago-1
OCI_COMPARTMENT_ID=ocid1.compartment.oc1..aaaaaaaaksv5b2aasfqfrmco2r2wh33vxldqhbsok67w5ldk6thkx4hn3mxa
LLM_MODEL=xai.grok-3-mini
```

### Base URL
```
https://inference.generativeai.us-chicago-1.oci.oraclecloud.com/20231130/actions/v1
```

### Files Modified

| File | Change |
|------|--------|
| `hermes_constants.py` | Replace `OPENROUTER_BASE_URL` → OCI GenAI endpoint |
| `run_agent.py` (`AIAgent.__init__`) | Replace `OpenAI(...)` → `OciOpenAI(...)` with `OciUserPrincipalAuth` |
| `run_agent.py` (`_build_api_kwargs`) | Remove OpenRouter-specific `extra_body` |
| `agent/auxiliary_client.py` | Same swap for auxiliary client |
| `tools/openrouter_client.py` | Replace `AsyncOpenAI` → `AsyncOciOpenAI` |
| `agent/model_metadata.py` | Replace OpenRouter `/models` endpoint with local model map |
| `hermes_cli/models.py` | Replace model list with OCI GenAI models |
| `hermes_cli/config.py` | Default model → xAI Grok, default profile → foosball |
| `hermes_cli/auth.py` | Replace Nous Portal OAuth with OCI profile auth |
| `.env.example` | Replace OpenRouter keys with OCI config |

### New Files

| File | Purpose |
|------|---------|
| `oci_client.py` | OCI GenAI client wrapper encapsulating oci-openai setup |

## Database Swap (SQLite → Oracle 26ai Free)

### Approach
Replace `hermes_state.py` `SessionDB` class with Oracle DB using `python-oracledb` in thin mode (no Oracle Client needed).

### Connection
```python
import oracledb
connection = oracledb.connect(
    user="hermes", password="...",
    dsn="localhost:1521/FREEPDB1"
)
```

### Schema Translation

| SQLite | Oracle 26ai |
|--------|-------------|
| `TEXT PRIMARY KEY` | `VARCHAR2(128) PRIMARY KEY` |
| `REAL NOT NULL` (timestamp) | `NUMBER NOT NULL` |
| `INTEGER PRIMARY KEY AUTOINCREMENT` | `NUMBER GENERATED ALWAYS AS IDENTITY` |
| `TEXT` (JSON blobs) | `CLOB CHECK (content IS JSON)` |
| `PRAGMA journal_mode=WAL` | Connection pooling via `oracledb.create_pool()` |
| FTS5 virtual table | Oracle Text index (`CTX_DDL`, `CONTAINS()`) |
| `INSERT OR REPLACE` | `MERGE INTO` |

### Configuration
```
ORACLE_DSN=localhost:1521/FREEPDB1
ORACLE_USER=hermes
ORACLE_PASSWORD=...
```

### New Files

| File | Purpose |
|------|---------|
| `oracle_state.py` | Oracle DB session state implementation |
| `oracle_setup.sql` | DDL script for Oracle schema, tables, Oracle Text index |

## Fork Strategy

Following the Oracle AI Database fork strategy:
- **Snapshot approach**: Clone upstream, add Oracle layer on top
- **Oracle customizations isolated**: OCI-specific code in separate modules
- **Custom README**: Positions Oracle AI Database + OCI GenAI as headline features
- **Repo name**: `orahermes-agent`

### Files Unchanged from Upstream
- `agent/` (core agent logic, except client init)
- `tools/` (tool implementations)
- `skills/` (skill system)
- `gateway/` (messaging gateway)
- `cron/` (scheduled tasks)
- `environments/` (RL environments)
- `tests/`

## Dependencies

### Added
- `oci-openai` — OCI GenAI OpenAI-compatible wrapper
- `oracledb` — Python Oracle Database driver

### Kept
- `openai` — Still used under the hood by oci-openai
- All other upstream dependencies

## Infrastructure

- **OCI Profile**: `foosball` (us-chicago-1)
- **Oracle DB**: Existing `oracle-free` container on port 1521
- **Database**: Oracle 26ai Free (`container-registry.oracle.com/database/free:latest-lite`)
