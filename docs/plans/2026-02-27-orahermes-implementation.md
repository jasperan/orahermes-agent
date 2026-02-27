# orahermes-agent Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fork hermes-agent, replacing OpenRouter with OCI GenAI (xAI Grok default) and SQLite with Oracle 26ai Free.

**Architecture:** Full snapshot fork. LLM calls go through `oci-openai` wrapper (OCI auth + OpenAI SDK). Session state stored in Oracle DB via `python-oracledb` thin mode. All upstream features preserved.

**Tech Stack:** Python 3.11, oci-openai, python-oracledb, OpenAI SDK (underlying), Oracle 26ai Free, OCI GenAI (us-chicago-1)

---

### Task 1: Clone Upstream and Initialize Fork

**Files:**
- Clone: `https://github.com/NousResearch/hermes-agent` → `/home/ubuntu/git/orahermes-agent/`

**Step 1: Clone upstream into project directory**

```bash
cd /tmp
git clone https://github.com/NousResearch/hermes-agent hermes-agent-upstream 2>/dev/null || true
cd /home/ubuntu/git/orahermes-agent
# Copy all upstream files (excluding .git)
rsync -av --exclude='.git' /tmp/hermes-agent-upstream/ ./
# Also copy from the already-cloned /tmp/hermes-agent/ if upstream clone fails
```

**Step 2: Initialize git and make initial commit**

```bash
cd /home/ubuntu/git/orahermes-agent
git add -A
git commit -m "Initial snapshot of NousResearch/hermes-agent upstream"
```

**Step 3: Add .gitignore entries for local plans**

Append `docs/plans/` to `.gitignore`.

**Step 4: Commit gitignore**

```bash
git add .gitignore
git commit -m "Add docs/plans/ to gitignore"
```

---

### Task 2: Add Dependencies (oci-openai, oracledb)

**Files:**
- Modify: `requirements.txt`
- Modify: `pyproject.toml`

**Step 1: Add oci-openai and oracledb to requirements.txt**

Add these lines:
```
oci-openai
oracledb
```

**Step 2: Add to pyproject.toml dependencies array**

In the `[project] dependencies` list, add:
```
"oci-openai",
"oracledb",
```

**Step 3: Verify install**

```bash
cd /home/ubuntu/git/orahermes-agent
pip install oci-openai oracledb 2>&1 | tail -5
python -c "from oci_openai import OciOpenAI; print('oci-openai OK')"
python -c "import oracledb; print('oracledb OK')"
```

**Step 4: Commit**

```bash
git add requirements.txt pyproject.toml
git commit -m "Add oci-openai and oracledb dependencies"
```

---

### Task 3: Create OCI Client Wrapper

**Files:**
- Create: `oci_client.py`
- Test: `tests/test_oci_client.py`

**Step 1: Write test for OCI client factory**

```python
# tests/test_oci_client.py
import os
import pytest


def test_create_oci_client_returns_openai_compatible():
    """OCI client must have chat.completions interface."""
    from oci_client import create_oci_client

    client = create_oci_client(
        profile_name="foosball",
        compartment_id=os.getenv("OCI_COMPARTMENT_ID", "test-compartment"),
        region="us-chicago-1",
    )
    assert hasattr(client, "chat")
    assert hasattr(client.chat, "completions")


def test_create_oci_async_client():
    """Async variant must also have chat.completions."""
    from oci_client import create_oci_async_client

    client = create_oci_async_client(
        profile_name="foosball",
        compartment_id=os.getenv("OCI_COMPARTMENT_ID", "test-compartment"),
        region="us-chicago-1",
    )
    assert hasattr(client, "chat")
    assert hasattr(client.chat, "completions")


def test_get_oci_base_url():
    """Base URL must follow OCI GenAI format."""
    from oci_client import get_oci_base_url

    url = get_oci_base_url("us-chicago-1")
    assert "inference.generativeai.us-chicago-1" in url
    assert "/20231130/actions/v1" in url
```

**Step 2: Run tests to verify they fail**

```bash
pytest tests/test_oci_client.py -v
```
Expected: FAIL (module not found)

**Step 3: Implement oci_client.py**

```python
# oci_client.py
"""OCI GenAI client wrapper using oci-openai."""

import os

from oci_openai import OciOpenAI, AsyncOciOpenAI, OciUserPrincipalAuth


OCI_GENAI_URL_TEMPLATE = (
    "https://inference.generativeai.{region}.oci.oraclecloud.com/20231130/actions/v1"
)

DEFAULT_REGION = "us-chicago-1"
DEFAULT_PROFILE = "foosball"
DEFAULT_COMPARTMENT_ID = os.getenv(
    "OCI_COMPARTMENT_ID",
    "ocid1.compartment.oc1..aaaaaaaaksv5b2aasfqfrmco2r2wh33vxldqhbsok67w5ldk6thkx4hn3mxa",
)


def get_oci_base_url(region: str = DEFAULT_REGION) -> str:
    """Return the OCI GenAI OpenAI-compatible base URL for a region."""
    return OCI_GENAI_URL_TEMPLATE.format(region=region)


def create_oci_client(
    profile_name: str = DEFAULT_PROFILE,
    compartment_id: str = DEFAULT_COMPARTMENT_ID,
    region: str = DEFAULT_REGION,
    **kwargs,
) -> OciOpenAI:
    """Create a synchronous OCI GenAI client."""
    return OciOpenAI(
        base_url=get_oci_base_url(region),
        auth=OciUserPrincipalAuth(profile_name=profile_name),
        compartment_id=compartment_id,
        **kwargs,
    )


def create_oci_async_client(
    profile_name: str = DEFAULT_PROFILE,
    compartment_id: str = DEFAULT_COMPARTMENT_ID,
    region: str = DEFAULT_REGION,
    **kwargs,
) -> AsyncOciOpenAI:
    """Create an asynchronous OCI GenAI client."""
    return AsyncOciOpenAI(
        base_url=get_oci_base_url(region),
        auth=OciUserPrincipalAuth(profile_name=profile_name),
        compartment_id=compartment_id,
        **kwargs,
    )
```

**Step 4: Run tests to verify they pass**

```bash
pytest tests/test_oci_client.py -v
```
Expected: PASS

**Step 5: Commit**

```bash
git add oci_client.py tests/test_oci_client.py
git commit -m "Add OCI GenAI client wrapper"
```

---

### Task 4: Swap Constants (hermes_constants.py)

**Files:**
- Modify: `hermes_constants.py`

**Step 1: Replace OpenRouter URLs with OCI GenAI**

Replace:
```python
OPENROUTER_BASE_URL = "https://openrouter.ai/api/v1"
OPENROUTER_MODELS_URL = f"{OPENROUTER_BASE_URL}/models"
OPENROUTER_CHAT_URL = f"{OPENROUTER_BASE_URL}/chat/completions"
```

With:
```python
# OCI GenAI (OpenAI-compatible endpoint)
OCI_GENAI_BASE_URL = "https://inference.generativeai.us-chicago-1.oci.oraclecloud.com/20231130/actions/v1"

# Legacy aliases for compatibility with code that references OPENROUTER_*
OPENROUTER_BASE_URL = OCI_GENAI_BASE_URL
OPENROUTER_MODELS_URL = None  # Not used — model metadata is local
OPENROUTER_CHAT_URL = f"{OCI_GENAI_BASE_URL}/chat/completions"

# Default model
DEFAULT_MODEL = "xai.grok-3-mini"
```

**Step 2: Commit**

```bash
git add hermes_constants.py
git commit -m "Swap OpenRouter constants to OCI GenAI endpoint"
```

---

### Task 5: Swap Main Agent Client (run_agent.py)

**Files:**
- Modify: `run_agent.py`

This is the largest change. Three sections to modify.

**Step 1: Add OCI imports at top of file**

Near the existing imports, add:
```python
from oci_client import create_oci_client, get_oci_base_url, DEFAULT_PROFILE, DEFAULT_COMPARTMENT_ID, DEFAULT_REGION
```

**Step 2: Modify AIAgent.__init__ client initialization**

Find the client initialization block (around lines 271-291). Replace the OpenRouter client setup with:

```python
# --- OCI GenAI client ---
oci_profile = os.getenv("OCI_PROFILE", DEFAULT_PROFILE)
oci_compartment = os.getenv("OCI_COMPARTMENT_ID", DEFAULT_COMPARTMENT_ID)
oci_region = os.getenv("OCI_REGION", DEFAULT_REGION)

if base_url and api_key:
    # Custom endpoint — use standard OpenAI client (preserves flexibility)
    from openai import OpenAI
    self.client = OpenAI(base_url=base_url, api_key=api_key)
else:
    # Default: OCI GenAI
    self.client = create_oci_client(
        profile_name=oci_profile,
        compartment_id=oci_compartment,
        region=oci_region,
    )
    self.base_url = get_oci_base_url(oci_region)
```

**Step 3: Modify _build_api_kwargs to remove OpenRouter-specific extra_body**

In `_build_api_kwargs`, remove or comment out the `extra_body` logic that adds:
- `provider` preferences (OpenRouter-specific)
- `reasoning` config (OpenRouter-specific)
- `tags` (Nous-specific)

The cleaned-up method should just be:
```python
def _build_api_kwargs(self, api_messages: list) -> dict:
    api_kwargs = {
        "model": self.model,
        "messages": api_messages,
        "tools": self.tools if self.tools else None,
        "timeout": 600.0,
    }
    # Remove None tools to avoid API errors
    if api_kwargs["tools"] is None:
        del api_kwargs["tools"]
    return api_kwargs
```

**Step 4: Disable prompt caching (Anthropic/OpenRouter-specific)**

Find the prompt caching setup (around line 213-219) and force it off:
```python
self._use_prompt_caching = False  # OCI GenAI does not support Anthropic cache control
```

**Step 5: Update main() function defaults**

Change the `main()` function signature defaults:
```python
def main(
    query: str = None,
    model: str = "xai.grok-3-mini",
    api_key: str = None,
    base_url: str = None,  # Defaults to OCI GenAI via oci_client
    ...
```

**Step 6: Commit**

```bash
git add run_agent.py
git commit -m "Swap run_agent.py to OCI GenAI client"
```

---

### Task 6: Swap Auxiliary Client (agent/auxiliary_client.py)

**Files:**
- Modify: `agent/auxiliary_client.py`

**Step 1: Replace the resolution chain**

Replace the OpenRouter→Nous→Custom chain with OCI GenAI:

```python
from oci_client import create_oci_client, create_oci_async_client

_OCI_MODEL = "meta.llama-3.3-70b-instruct"  # Fast, cheap model for auxiliary tasks

def get_text_auxiliary_client():
    try:
        client = create_oci_client()
        return client, _OCI_MODEL
    except Exception:
        return None, None

def get_vision_auxiliary_client():
    try:
        client = create_oci_client()
        return client, _OCI_MODEL
    except Exception:
        return None, None
```

**Step 2: Commit**

```bash
git add agent/auxiliary_client.py
git commit -m "Swap auxiliary client to OCI GenAI"
```

---

### Task 7: Swap Shared Async Client (tools/openrouter_client.py)

**Files:**
- Modify: `tools/openrouter_client.py`

**Step 1: Replace the shared async client**

Replace the entire file's client creation with:

```python
from oci_client import create_oci_async_client

_client = None

def get_shared_client():
    global _client
    if _client is None:
        _client = create_oci_async_client()
    return _client
```

Keep the existing function signature/interface so callers don't break.

**Step 2: Commit**

```bash
git add tools/openrouter_client.py
git commit -m "Swap shared async client to OCI GenAI"
```

---

### Task 8: Replace Model Metadata (agent/model_metadata.py)

**Files:**
- Modify: `agent/model_metadata.py`

**Step 1: Replace OpenRouter model fetch with local mapping**

Replace the `fetch_model_metadata()` function with a static model map for OCI GenAI models:

```python
OCI_GENAI_MODELS = {
    "xai.grok-3-mini": {"context_length": 131072, "name": "Grok 3 Mini"},
    "xai.grok-3": {"context_length": 131072, "name": "Grok 3"},
    "meta.llama-3.3-70b-instruct": {"context_length": 128000, "name": "Llama 3.3 70B"},
    "meta.llama-4-maverick-17b-128e-instruct-fp8": {"context_length": 1048576, "name": "Llama 4 Maverick"},
    "meta.llama-4-scout-17b-16e-instruct-fp8": {"context_length": 10485760, "name": "Llama 4 Scout"},
}

def get_model_context_length(model: str) -> int:
    if model in OCI_GENAI_MODELS:
        return OCI_GENAI_MODELS[model]["context_length"]
    return 128000  # Safe default

def fetch_model_metadata(model: str = None) -> dict:
    """Return local model metadata (no API call needed)."""
    return OCI_GENAI_MODELS
```

**Step 2: Commit**

```bash
git add agent/model_metadata.py
git commit -m "Replace OpenRouter model metadata with OCI GenAI local map"
```

---

### Task 9: Update CLI Model List (hermes_cli/models.py)

**Files:**
- Modify: `hermes_cli/models.py`

**Step 1: Replace model list**

Replace `OPENROUTER_MODELS` with:
```python
OCI_GENAI_MODELS: list[tuple[str, str]] = [
    ("xai.grok-3-mini",                                   "recommended"),
    ("xai.grok-3",                                        ""),
    ("meta.llama-3.3-70b-instruct",                       ""),
    ("meta.llama-4-maverick-17b-128e-instruct-fp8",       ""),
    ("meta.llama-4-scout-17b-16e-instruct-fp8",           ""),
]

# Legacy alias
OPENROUTER_MODELS = OCI_GENAI_MODELS
```

**Step 2: Commit**

```bash
git add hermes_cli/models.py
git commit -m "Update model list for OCI GenAI"
```

---

### Task 10: Update CLI Config (hermes_cli/config.py)

**Files:**
- Modify: `hermes_cli/config.py`

**Step 1: Change default model**

Replace `DEFAULT_CONFIG["model"]` value from `"anthropic/claude-opus-4.6"` to `"xai.grok-3-mini"`.

**Step 2: Replace OPENROUTER_API_KEY in OPTIONAL_ENV_VARS**

Replace with OCI-relevant env vars:
```python
"OCI_PROFILE": {"desc": "OCI config profile name", "advanced": False},
"OCI_REGION": {"desc": "OCI region for GenAI", "advanced": False},
"OCI_COMPARTMENT_ID": {"desc": "OCI compartment OCID", "advanced": False},
```

**Step 3: Commit**

```bash
git add hermes_cli/config.py
git commit -m "Update CLI config defaults for OCI GenAI"
```

---

### Task 11: Update Auth (hermes_cli/auth.py)

**Files:**
- Modify: `hermes_cli/auth.py`

**Step 1: Simplify resolve_provider()**

Replace the OpenRouter/Nous resolution chain with OCI:
```python
def resolve_provider(requested=None, *, explicit_api_key=None, explicit_base_url=None) -> str:
    if explicit_base_url and explicit_api_key:
        return "custom"
    return "oci"
```

**Step 2: Remove or stub out Nous Portal OAuth**

The `login()` and related OAuth device code flow functions can be stubbed to print a message about using OCI config profiles instead.

**Step 3: Commit**

```bash
git add hermes_cli/auth.py
git commit -m "Replace auth with OCI profile-based resolution"
```

---

### Task 12: Update CLI Entry Point (cli.py)

**Files:**
- Modify: `cli.py`

**Step 1: Update HermesCLI.__init__ defaults**

Change model/base_url/api_key resolution:
```python
self.model = model or os.getenv("LLM_MODEL") or "xai.grok-3-mini"
self.base_url = base_url or os.getenv("OPENAI_BASE_URL") or get_oci_base_url()
self.api_key = api_key or os.getenv("OPENAI_API_KEY") or "OCI"  # OCI auth handles this
```

**Step 2: Update load_cli_config defaults**

Change `CLI_CONFIG["model"]["default"]` to `"xai.grok-3-mini"` and `CLI_CONFIG["model"]["base_url"]` to the OCI GenAI URL.

**Step 3: Commit**

```bash
git add cli.py
git commit -m "Update CLI defaults for OCI GenAI"
```

---

### Task 13: Create Oracle DB Schema (oracle_setup.sql)

**Files:**
- Create: `oracle_setup.sql`

**Step 1: Write the Oracle DDL**

```sql
-- oracle_setup.sql
-- Oracle 26ai Free schema for orahermes-agent

-- Create user
-- Run as SYSDBA: ALTER SESSION SET CONTAINER = FREEPDB1;
-- CREATE USER hermes IDENTIFIED BY <password> DEFAULT TABLESPACE users QUOTA UNLIMITED ON users;
-- GRANT CONNECT, RESOURCE, CTX_APP TO hermes;

-- Sessions table
CREATE TABLE sessions (
    id VARCHAR2(128) PRIMARY KEY,
    source VARCHAR2(32) NOT NULL,
    user_id VARCHAR2(256),
    model VARCHAR2(256),
    model_config CLOB CHECK (model_config IS JSON),
    system_prompt CLOB,
    parent_session_id VARCHAR2(128),
    started_at NUMBER NOT NULL,
    ended_at NUMBER,
    end_reason VARCHAR2(64),
    message_count NUMBER DEFAULT 0,
    tool_call_count NUMBER DEFAULT 0,
    input_tokens NUMBER DEFAULT 0,
    output_tokens NUMBER DEFAULT 0
);

-- Messages table
CREATE TABLE messages (
    id NUMBER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    session_id VARCHAR2(128) NOT NULL REFERENCES sessions(id),
    role VARCHAR2(32) NOT NULL,
    content CLOB,
    tool_call_id VARCHAR2(256),
    tool_calls CLOB CHECK (tool_calls IS JSON),
    tool_name VARCHAR2(256),
    timestamp_val NUMBER NOT NULL,
    token_count NUMBER,
    finish_reason VARCHAR2(64)
);

CREATE INDEX idx_messages_session ON messages(session_id);
CREATE INDEX idx_messages_timestamp ON messages(timestamp_val);

-- Oracle Text index for full-text search on message content
CREATE INDEX idx_messages_content_ft ON messages(content)
    INDEXTYPE IS CTXSYS.CONTEXT
    PARAMETERS ('SYNC (ON COMMIT)');

-- Schema version tracking
CREATE TABLE schema_version (
    version NUMBER PRIMARY KEY,
    applied_at TIMESTAMP DEFAULT SYSTIMESTAMP
);

INSERT INTO schema_version (version) VALUES (1);
COMMIT;
```

**Step 2: Commit**

```bash
git add oracle_setup.sql
git commit -m "Add Oracle 26ai Free schema DDL"
```

---

### Task 14: Implement Oracle Session DB (oracle_state.py)

**Files:**
- Create: `oracle_state.py`
- Test: `tests/test_oracle_state.py`

**Step 1: Write tests for Oracle SessionDB**

```python
# tests/test_oracle_state.py
import os
import time
import pytest

# Skip if no Oracle DB available
pytestmark = pytest.mark.skipif(
    not os.getenv("ORACLE_DSN"),
    reason="ORACLE_DSN not set — no Oracle DB available",
)


def test_create_session():
    from oracle_state import OracleSessionDB

    db = OracleSessionDB()
    sid = db.create_session(source="cli", model="xai.grok-3-mini")
    assert sid is not None
    assert len(sid) > 0


def test_add_and_get_messages():
    from oracle_state import OracleSessionDB

    db = OracleSessionDB()
    sid = db.create_session(source="cli", model="test-model")
    db.add_message(sid, role="user", content="Hello")
    db.add_message(sid, role="assistant", content="Hi there")
    msgs = db.get_messages(sid)
    assert len(msgs) == 2
    assert msgs[0]["role"] == "user"
    assert msgs[1]["content"] == "Hi there"


def test_search_messages():
    from oracle_state import OracleSessionDB

    db = OracleSessionDB()
    sid = db.create_session(source="cli", model="test-model")
    db.add_message(sid, role="user", content="unique_search_term_xyz")
    results = db.search_messages("unique_search_term_xyz")
    assert len(results) > 0
```

**Step 2: Run tests to verify they fail**

```bash
ORACLE_DSN=localhost:1521/FREEPDB1 ORACLE_USER=hermes ORACLE_PASSWORD=<pw> pytest tests/test_oracle_state.py -v
```
Expected: FAIL (module not found)

**Step 3: Implement oracle_state.py**

```python
# oracle_state.py
"""Oracle 26ai Free session state storage — replaces SQLite hermes_state.py."""

import os
import time
import uuid
import json
from typing import Optional

import oracledb


class OracleSessionDB:
    """Drop-in replacement for SessionDB using Oracle Database."""

    def __init__(
        self,
        dsn: str = None,
        user: str = None,
        password: str = None,
    ):
        self.dsn = dsn or os.getenv("ORACLE_DSN", "localhost:1521/FREEPDB1")
        self.user = user or os.getenv("ORACLE_USER", "hermes")
        self.password = password or os.getenv("ORACLE_PASSWORD", "")
        self.pool = oracledb.create_pool(
            user=self.user,
            password=self.password,
            dsn=self.dsn,
            min=1,
            max=5,
            increment=1,
        )

    def _get_conn(self):
        return self.pool.acquire()

    def create_session(
        self,
        source: str,
        model: str,
        user_id: str = None,
        model_config: dict = None,
        system_prompt: str = None,
        parent_session_id: str = None,
    ) -> str:
        sid = str(uuid.uuid4())
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """INSERT INTO sessions
                       (id, source, user_id, model, model_config, system_prompt,
                        parent_session_id, started_at)
                       VALUES (:1, :2, :3, :4, :5, :6, :7, :8)""",
                    [
                        sid, source, user_id, model,
                        json.dumps(model_config) if model_config else None,
                        system_prompt, parent_session_id, time.time(),
                    ],
                )
            conn.commit()
        return sid

    def end_session(self, session_id: str, reason: str = "normal"):
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """UPDATE sessions SET ended_at = :1, end_reason = :2 WHERE id = :3""",
                    [time.time(), reason, session_id],
                )
            conn.commit()

    def add_message(
        self,
        session_id: str,
        role: str,
        content: str = None,
        tool_call_id: str = None,
        tool_calls: list = None,
        tool_name: str = None,
        token_count: int = None,
        finish_reason: str = None,
    ):
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """INSERT INTO messages
                       (session_id, role, content, tool_call_id, tool_calls,
                        tool_name, timestamp_val, token_count, finish_reason)
                       VALUES (:1, :2, :3, :4, :5, :6, :7, :8, :9)""",
                    [
                        session_id, role, content, tool_call_id,
                        json.dumps(tool_calls) if tool_calls else None,
                        tool_name, time.time(), token_count, finish_reason,
                    ],
                )
                # Update session counters
                cur.execute(
                    """UPDATE sessions SET message_count = message_count + 1 WHERE id = :1""",
                    [session_id],
                )
            conn.commit()

    def get_messages(self, session_id: str) -> list[dict]:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """SELECT role, content, tool_call_id, tool_calls, tool_name,
                              timestamp_val, token_count, finish_reason
                       FROM messages WHERE session_id = :1
                       ORDER BY id""",
                    [session_id],
                )
                rows = cur.fetchall()
                return [
                    {
                        "role": r[0],
                        "content": r[1],
                        "tool_call_id": r[2],
                        "tool_calls": json.loads(r[3]) if r[3] else None,
                        "tool_name": r[4],
                        "timestamp": r[5],
                        "token_count": r[6],
                        "finish_reason": r[7],
                    }
                    for r in rows
                ]

    def get_session(self, session_id: str) -> Optional[dict]:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """SELECT id, source, user_id, model, started_at, ended_at,
                              message_count, tool_call_count, input_tokens, output_tokens
                       FROM sessions WHERE id = :1""",
                    [session_id],
                )
                row = cur.fetchone()
                if not row:
                    return None
                return {
                    "id": row[0], "source": row[1], "user_id": row[2],
                    "model": row[3], "started_at": row[4], "ended_at": row[5],
                    "message_count": row[6], "tool_call_count": row[7],
                    "input_tokens": row[8], "output_tokens": row[9],
                }

    def search_messages(self, query: str, limit: int = 20) -> list[dict]:
        """Full-text search using Oracle Text CONTAINS."""
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """SELECT m.session_id, m.role, m.content, m.timestamp_val,
                              SCORE(1) as relevance
                       FROM messages m
                       WHERE CONTAINS(m.content, :1, 1) > 0
                       ORDER BY relevance DESC
                       FETCH FIRST :2 ROWS ONLY""",
                    [query, limit],
                )
                return [
                    {
                        "session_id": r[0], "role": r[1],
                        "content": r[2], "timestamp": r[3],
                        "relevance": r[4],
                    }
                    for r in cur.fetchall()
                ]

    def update_token_counts(
        self, session_id: str, input_tokens: int, output_tokens: int
    ):
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """UPDATE sessions
                       SET input_tokens = input_tokens + :1,
                           output_tokens = output_tokens + :2
                       WHERE id = :3""",
                    [input_tokens, output_tokens, session_id],
                )
            conn.commit()

    def list_sessions(self, source: str = None, limit: int = 50) -> list[dict]:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                if source:
                    cur.execute(
                        """SELECT id, source, model, started_at, ended_at, message_count
                           FROM sessions WHERE source = :1
                           ORDER BY started_at DESC FETCH FIRST :2 ROWS ONLY""",
                        [source, limit],
                    )
                else:
                    cur.execute(
                        """SELECT id, source, model, started_at, ended_at, message_count
                           FROM sessions
                           ORDER BY started_at DESC FETCH FIRST :1 ROWS ONLY""",
                        [limit],
                    )
                return [
                    {
                        "id": r[0], "source": r[1], "model": r[2],
                        "started_at": r[3], "ended_at": r[4], "message_count": r[5],
                    }
                    for r in cur.fetchall()
                ]

    def close(self):
        if self.pool:
            self.pool.close()
```

**Step 4: Run tests**

```bash
ORACLE_DSN=localhost:1521/FREEPDB1 ORACLE_USER=hermes ORACLE_PASSWORD=<pw> pytest tests/test_oracle_state.py -v
```
Expected: PASS (requires Oracle DB running and schema applied)

**Step 5: Commit**

```bash
git add oracle_state.py tests/test_oracle_state.py
git commit -m "Add Oracle session state DB implementation"
```

---

### Task 15: Wire Oracle DB into hermes_state.py

**Files:**
- Modify: `hermes_state.py`

**Step 1: Add Oracle backend selection**

At the top of `hermes_state.py`, add a factory that returns Oracle or SQLite based on config:

```python
import os

def get_session_db(**kwargs):
    """Return Oracle SessionDB if configured, else SQLite."""
    if os.getenv("ORACLE_DSN"):
        from oracle_state import OracleSessionDB
        return OracleSessionDB(**kwargs)
    else:
        return SessionDB(**kwargs)  # Original SQLite
```

This preserves backward compatibility — SQLite still works if no Oracle env vars are set.

**Step 2: Update all callers of SessionDB to use get_session_db()**

Search for `SessionDB(` in the codebase and replace with `get_session_db()`. Key locations:
- `run_agent.py` where SessionDB is instantiated
- `cli.py` where SessionDB is instantiated
- `gateway/` files

**Step 3: Commit**

```bash
git add hermes_state.py run_agent.py cli.py
git commit -m "Wire Oracle DB as session storage backend"
```

---

### Task 16: Update .env.example

**Files:**
- Modify: `.env.example`

**Step 1: Replace OpenRouter section with OCI GenAI + Oracle DB**

Replace the LLM PROVIDER section at the top:

```
# =============================================================================
# LLM PROVIDER (OCI GenAI)
# =============================================================================
# OCI GenAI provides access to xAI Grok, Meta Llama, and other models
# Authentication uses OCI config profiles (~/.oci/config)

# OCI config profile name (from ~/.oci/config)
OCI_PROFILE=foosball

# OCI region for GenAI inference
OCI_REGION=us-chicago-1

# OCI compartment OCID
OCI_COMPARTMENT_ID=ocid1.compartment.oc1..aaaaaaaaksv5b2aasfqfrmco2r2wh33vxldqhbsok67w5ldk6thkx4hn3mxa

# Default model (OCI GenAI format)
LLM_MODEL=xai.grok-3-mini

# =============================================================================
# ORACLE DATABASE (Session Storage)
# =============================================================================
# Oracle 26ai Free replaces SQLite for session/message storage
# Container: container-registry.oracle.com/database/free:latest-lite

ORACLE_DSN=localhost:1521/FREEPDB1
ORACLE_USER=hermes
ORACLE_PASSWORD=
```

**Step 2: Commit**

```bash
git add .env.example
git commit -m "Update .env.example for OCI GenAI and Oracle DB"
```

---

### Task 17: Write Custom README

**Files:**
- Modify: `README.md`

**Step 1: Write Oracle-branded README**

Create a README that positions Oracle AI Database + OCI GenAI as the headline features. Follow the badge style from CLAUDE.md (style=for-the-badge). Include:

- Project name: **orahermes-agent**
- Tagline: "Hermes Agent powered by Oracle AI Database & OCI GenAI"
- Badges: Oracle Database, OCI, xAI Grok, Python
- Quick start with OCI config + Oracle DB setup
- Architecture diagram showing OCI GenAI ↔ Agent ↔ Oracle 26ai Free
- Credit to NousResearch/hermes-agent upstream
- Footer social badges (jasperan GitHub/LinkedIn, Oracle Database Free)

**Step 2: Commit**

```bash
git add README.md
git commit -m "Add Oracle-branded README"
```

---

### Task 18: Smoke Test End-to-End

**Step 1: Set up Oracle schema**

```bash
# Connect to Oracle and run the DDL
python3 -c "
import oracledb
conn = oracledb.connect(user='hermes', password='...', dsn='localhost:1521/FREEPDB1')
with open('oracle_setup.sql') as f:
    for stmt in f.read().split(';'):
        stmt = stmt.strip()
        if stmt and not stmt.startswith('--'):
            try:
                conn.cursor().execute(stmt)
            except Exception as e:
                print(f'Skipping: {e}')
conn.commit()
conn.close()
print('Schema applied')
"
```

**Step 2: Test OCI GenAI connectivity**

```bash
python3 -c "
from oci_client import create_oci_client
client = create_oci_client()
resp = client.chat.completions.create(
    model='xai.grok-3-mini',
    messages=[{'role': 'user', 'content': 'Say hello in one word'}],
)
print(resp.choices[0].message.content)
"
```

**Step 3: Test full agent run**

```bash
cd /home/ubuntu/git/orahermes-agent
python run_agent.py --query "What is 2+2?" --model xai.grok-3-mini
```

**Step 4: Verify Oracle DB has session data**

```bash
python3 -c "
import oracledb
conn = oracledb.connect(user='hermes', password='...', dsn='localhost:1521/FREEPDB1')
cur = conn.cursor()
cur.execute('SELECT COUNT(*) FROM sessions')
print(f'Sessions: {cur.fetchone()[0]}')
cur.execute('SELECT COUNT(*) FROM messages')
print(f'Messages: {cur.fetchone()[0]}')
"
```

**Step 5: Final commit**

```bash
git add -A
git commit -m "orahermes-agent: complete Oracle AI Database + OCI GenAI fork"
```
