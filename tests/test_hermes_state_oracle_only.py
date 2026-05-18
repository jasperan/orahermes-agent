import json
from pathlib import Path

import pytest

import hermes_state
import oracle_state


def test_sessiondb_rejects_sqlite_db_path(tmp_path: Path):
    with pytest.raises(RuntimeError, match="Oracle only"):
        hermes_state.SessionDB(db_path=tmp_path / "state.db")


def test_sessiondb_uses_oracle_facade_without_db_path(monkeypatch):
    def fake_init(self, **kwargs):
        self.dsn = "oracle-test"
        self.user = "user"
        self.password = "password"

    monkeypatch.setattr(oracle_state.OracleSessionDB, "__init__", fake_init)

    db = hermes_state.SessionDB()

    assert isinstance(db, hermes_state.SessionDB)
    assert db.dsn == "oracle-test"
    assert hermes_state.get_last_init_error() is None


def test_get_session_db_returns_oracle_sessiondb(monkeypatch):
    def fake_init(self, **kwargs):
        self.dsn = "oracle-test"
        self.user = "user"
        self.password = "password"

    monkeypatch.setattr(oracle_state.OracleSessionDB, "__init__", fake_init)

    assert isinstance(hermes_state.get_session_db(), hermes_state.SessionDB)


def test_format_session_db_unavailable_names_oracle_env(monkeypatch):
    monkeypatch.setattr(hermes_state, "_last_init_error", None)

    message = hermes_state.format_session_db_unavailable()

    assert "Oracle session database not available" in message
    assert "ORACLE_DSN" in message
    assert "ORACLE_USER" in message
    assert "ORACLE_PASSWORD" in message


def test_file_backed_memory_tool_is_not_available():
    from tools.memory_tool import check_memory_requirements
    from toolsets import _HERMES_CORE_TOOLS, TOOLSETS

    assert check_memory_requirements() is False
    assert "memory" not in _HERMES_CORE_TOOLS
    assert TOOLSETS["memory"]["tools"] == []
    assert "memory" not in TOOLSETS["hermes-acp"]["tools"]
    assert "memory" not in TOOLSETS["hermes-api-server"]["tools"]


def test_external_memory_provider_discovery_is_disabled():
    from plugins.memory import discover_memory_providers, load_memory_provider

    assert discover_memory_providers() == []
    assert load_memory_provider("external") is None


class _FakeCursor:
    description = [("SESSION_ID",)]

    def __init__(self, row=None):
        self.row = row
        self.calls = []
        self.next_id = 1

    def __enter__(self):
        return self

    def __exit__(self, *_exc):
        return False

    def execute(self, sql, params=None):
        self.calls.append((sql, params or {}))
        if "RETURNING id INTO :msg_id" in sql and params and params.get("msg_id") is not None:
            params["msg_id"].setvalue(0, self.next_id)
            self.next_id += 1

    def fetchone(self):
        return self.row

    def var(self, _type):
        return _FakeVar()


class _FakeVar:
    def __init__(self):
        self.value = None

    def setvalue(self, _position, value):
        self.value = value

    def getvalue(self):
        return [self.value]


class _FakeConn:
    def __init__(self, cursor):
        self.cursor_obj = cursor
        self.committed = False
        self.commit_count = 0

    def __enter__(self):
        return self

    def __exit__(self, *_exc):
        return False

    def cursor(self):
        return self.cursor_obj

    def commit(self):
        self.committed = True
        self.commit_count += 1


def test_oracle_helpers_use_oracle_bind_parameters():
    cursor = _FakeCursor(row=("session-1",))
    conn = _FakeConn(cursor)
    db = object.__new__(oracle_state.OracleSessionDB)
    db._get_conn = lambda: conn

    db.update_session_model_config("session-1", {"cwd": "/tmp/project"}, model="oracle-model")
    owner = db.find_message_session(42)

    assert owner == "session-1"
    assert conn.committed is True
    assert all("?" not in sql for sql, _params in cursor.calls)
    assert cursor.calls[0][1]["id"] == "session-1"
    assert cursor.calls[0][1]["model"] == "oracle-model"
    assert cursor.calls[1][1] == {"id": 42}


def test_disabled_local_db_plugins_do_not_embed_query_code():
    root = Path(__file__).resolve().parents[1]
    memory_root = root / "plugins/memory"
    provider_dirs = [
        p.name for p in memory_root.iterdir()
        if p.is_dir() and p.name != "__pycache__"
    ]
    assert provider_dirs == []

    disabled_files = [
        root / "plugins/memory/__init__.py",
        root / "plugins/kanban/dashboard/plugin_api.py",
    ]
    for path in disabled_files:
        text = path.read_text(encoding="utf-8")
        assert ".execute(" not in text
        assert "SELECT " not in text


def test_replace_messages_uses_one_oracle_transaction():
    cursor = _FakeCursor()
    conn = _FakeConn(cursor)
    db = object.__new__(oracle_state.OracleSessionDB)
    db._get_conn = lambda: conn
    db.ensure_session = lambda _session_id: None

    db.replace_messages(
        "session-1",
        [
            {"role": "user", "content": "hello"},
            {
                "role": "assistant",
                "content": "hi",
                "tool_calls": [{"id": "call-1"}],
                "reasoning": "kept",
            },
        ],
    )

    insert_calls = [sql for sql, _params in cursor.calls if "INSERT INTO messages" in sql]
    assert len(insert_calls) == 2
    assert conn.commit_count == 1
    assert all("?" not in sql for sql, _params in cursor.calls)


class _GatewayFakeOracleDB:
    def __init__(self):
        self.meta = {}
        self.sessions = set()
        self.messages = {}
        self.closed = False

    def get_meta(self, key):
        return self.meta.get(key)

    def set_meta(self, key, value):
        self.meta[key] = value

    def session_count(self):
        return len(self.sessions)

    def create_session(self, session_id, source, **_kwargs):
        self.sessions.add(session_id)
        self.messages.setdefault(session_id, [])
        return session_id

    def end_session(self, session_id, _reason):
        self.sessions.discard(session_id)

    def reopen_session(self, session_id):
        self.sessions.add(session_id)

    def append_message(self, session_id, role, content, **kwargs):
        self.messages.setdefault(session_id, []).append(
            {"role": role, "content": content, **{k: v for k, v in kwargs.items() if v is not None}}
        )

    def replace_messages(self, session_id, messages):
        self.messages[session_id] = list(messages)

    def get_messages_as_conversation(self, session_id):
        return list(self.messages.get(session_id, []))

    def close(self):
        self.closed = True


def test_gateway_session_store_uses_oracle_index_without_local_transcripts(monkeypatch, tmp_path: Path):
    from gateway.config import GatewayConfig, Platform
    from gateway.session import GATEWAY_SESSION_INDEX_META_KEY, SessionSource, SessionStore

    fake_db = _GatewayFakeOracleDB()
    monkeypatch.setattr(hermes_state, "SessionDB", lambda: fake_db)

    store = SessionStore(sessions_dir=tmp_path, config=GatewayConfig())
    source = SessionSource(platform=Platform.TELEGRAM, chat_id="123", user_id="u1")

    entry = store.get_or_create_session(source)
    store.append_to_transcript(entry.session_id, {"role": "user", "content": "hello"})
    store.rewrite_transcript(entry.session_id, [{"role": "assistant", "content": "kept"}])

    assert store.load_transcript(entry.session_id) == [{"role": "assistant", "content": "kept"}]
    assert GATEWAY_SESSION_INDEX_META_KEY in fake_db.meta
    persisted_index = json.loads(fake_db.meta[GATEWAY_SESSION_INDEX_META_KEY])
    assert persisted_index[entry.session_key]["session_id"] == entry.session_id
    assert not (tmp_path / "sessions.json").exists()
    assert not list(tmp_path.glob("*.jsonl"))


def test_gateway_session_store_fails_closed_without_oracle(monkeypatch, tmp_path: Path):
    from gateway.config import GatewayConfig, Platform
    from gateway.session import SessionSource, SessionStore

    def unavailable():
        raise RuntimeError("missing Oracle credentials")

    monkeypatch.setattr(hermes_state, "SessionDB", unavailable)

    store = SessionStore(sessions_dir=tmp_path, config=GatewayConfig())
    source = SessionSource(platform=Platform.TELEGRAM, chat_id="123", user_id="u1")

    with pytest.raises(RuntimeError, match="Oracle session store unavailable"):
        store.get_or_create_session(source)
    assert not (tmp_path / "sessions.json").exists()
    assert not list(tmp_path.glob("*.jsonl"))
