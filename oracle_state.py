"""Oracle Database session state storage for OraHermes."""

from __future__ import annotations

import json
import logging
import os
import re
import time
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Sequence, Tuple

try:
    import oracledb
except ImportError as exc:  # pragma: no cover - exercised on minimal installs
    oracledb = None
    _ORACLEDB_IMPORT_ERROR: ImportError | None = exc
else:
    _ORACLEDB_IMPORT_ERROR = None

logger = logging.getLogger(__name__)

if oracledb is not None:
    oracledb.defaults.fetch_lobs = False


def _json_dumps(value: Any) -> Optional[str]:
    if value is None:
        return None
    return json.dumps(value, ensure_ascii=False)


def _json_loads(value: Any, default: Any = None) -> Any:
    if value is None:
        return default
    try:
        return json.loads(str(value))
    except (TypeError, json.JSONDecodeError):
        return default


def _rows_to_dicts(cursor, rows: Iterable[Sequence[Any]]) -> List[Dict[str, Any]]:
    columns = [d[0].lower() for d in cursor.description]
    return [dict(zip(columns, row)) for row in rows]


class OracleSessionDB:
    """Oracle-backed replacement for upstream ``SessionDB``.

    The method names intentionally mirror upstream's session DB interface so
    current Hermes runtime code can continue importing ``hermes_state.SessionDB``
    while OraHermes remains Oracle-only.
    """

    MAX_TITLE_LENGTH = 100
    EMBEDDING_MODEL = "ALL_MINILM_L6_V2"
    _EMBED_CHAR_LIMIT = 1500
    _JSON_PREFIX = "__HERMES_JSON__:"

    def __init__(
        self,
        dsn: Optional[str] = None,
        user: Optional[str] = None,
        password: Optional[str] = None,
        **_: Any,
    ) -> None:
        if oracledb is None:
            raise RuntimeError(
                "Oracle session database requires the 'oracledb' package. "
                "Install OraHermes with its core dependencies."
            ) from _ORACLEDB_IMPORT_ERROR
        self.dsn = dsn or os.getenv("ORACLE_DSN")
        self.user = user or os.getenv("ORACLE_USER")
        self.password = password or os.getenv("ORACLE_PASSWORD")
        missing = [
            name
            for name, value in (
                ("ORACLE_DSN", self.dsn),
                ("ORACLE_USER", self.user),
                ("ORACLE_PASSWORD", self.password),
            )
            if not value
        ]
        if missing:
            raise RuntimeError(f"Oracle session database requires {', '.join(missing)}")

        self.pool = oracledb.create_pool(
            user=self.user,
            password=self.password,
            dsn=self.dsn,
            min=1,
            max=5,
            increment=1,
        )
        self._ensure_schema()

    def _get_conn(self):
        return self.pool.acquire()

    def close(self) -> None:
        if self.pool:
            self.pool.close()

    # ------------------------------------------------------------------
    # Schema
    # ------------------------------------------------------------------

    def _table_exists(self, cur, table_name: str) -> bool:
        cur.execute(
            "SELECT COUNT(*) FROM user_tables WHERE table_name = :name",
            {"name": table_name.upper()},
        )
        return bool(cur.fetchone()[0])

    def _column_exists(self, cur, table_name: str, column_name: str) -> bool:
        cur.execute(
            """SELECT COUNT(*) FROM user_tab_columns
               WHERE table_name = :table_name AND column_name = :column_name""",
            {"table_name": table_name.upper(), "column_name": column_name.upper()},
        )
        return bool(cur.fetchone()[0])

    def _ensure_column(self, cur, table_name: str, column_name: str, ddl: str) -> None:
        if not self._column_exists(cur, table_name, column_name):
            cur.execute(f"ALTER TABLE {table_name} ADD ({column_name} {ddl})")

    def _ensure_schema(self) -> None:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                if not self._table_exists(cur, "sessions"):
                    cur.execute(
                        """CREATE TABLE sessions (
                            id VARCHAR2(128) PRIMARY KEY,
                            source VARCHAR2(64) NOT NULL,
                            user_id VARCHAR2(256),
                            model VARCHAR2(512),
                            model_config CLOB,
                            system_prompt CLOB,
                            parent_session_id VARCHAR2(128),
                            started_at NUMBER NOT NULL,
                            ended_at NUMBER,
                            end_reason VARCHAR2(128),
                            message_count NUMBER DEFAULT 0,
                            tool_call_count NUMBER DEFAULT 0,
                            input_tokens NUMBER DEFAULT 0,
                            output_tokens NUMBER DEFAULT 0,
                            cache_read_tokens NUMBER DEFAULT 0,
                            cache_write_tokens NUMBER DEFAULT 0,
                            reasoning_tokens NUMBER DEFAULT 0,
                            billing_provider VARCHAR2(128),
                            billing_base_url VARCHAR2(1024),
                            billing_mode VARCHAR2(64),
                            estimated_cost_usd NUMBER,
                            actual_cost_usd NUMBER,
                            cost_status VARCHAR2(64),
                            cost_source VARCHAR2(128),
                            pricing_version VARCHAR2(128),
                            title VARCHAR2(256),
                            api_call_count NUMBER DEFAULT 0,
                            handoff_state VARCHAR2(64),
                            handoff_platform VARCHAR2(64),
                            handoff_error VARCHAR2(512)
                        )"""
                    )
                else:
                    for name, ddl in {
                        "cache_read_tokens": "NUMBER DEFAULT 0",
                        "cache_write_tokens": "NUMBER DEFAULT 0",
                        "reasoning_tokens": "NUMBER DEFAULT 0",
                        "billing_provider": "VARCHAR2(128)",
                        "billing_base_url": "VARCHAR2(1024)",
                        "billing_mode": "VARCHAR2(64)",
                        "estimated_cost_usd": "NUMBER",
                        "actual_cost_usd": "NUMBER",
                        "cost_status": "VARCHAR2(64)",
                        "cost_source": "VARCHAR2(128)",
                        "pricing_version": "VARCHAR2(128)",
                        "title": "VARCHAR2(256)",
                        "api_call_count": "NUMBER DEFAULT 0",
                        "handoff_state": "VARCHAR2(64)",
                        "handoff_platform": "VARCHAR2(64)",
                        "handoff_error": "VARCHAR2(512)",
                    }.items():
                        self._ensure_column(cur, "sessions", name, ddl)

                if not self._table_exists(cur, "messages"):
                    cur.execute(
                        """CREATE TABLE messages (
                            id NUMBER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
                            session_id VARCHAR2(128) NOT NULL REFERENCES sessions(id),
                            role VARCHAR2(32) NOT NULL,
                            content CLOB,
                            tool_call_id VARCHAR2(256),
                            tool_calls CLOB,
                            tool_name VARCHAR2(256),
                            timestamp_val NUMBER NOT NULL,
                            token_count NUMBER,
                            finish_reason VARCHAR2(128),
                            reasoning CLOB,
                            reasoning_content CLOB,
                            reasoning_details CLOB,
                            codex_reasoning_items CLOB,
                            codex_message_items CLOB
                        )"""
                    )
                else:
                    for name, ddl in {
                        "reasoning": "CLOB",
                        "reasoning_content": "CLOB",
                        "reasoning_details": "CLOB",
                        "codex_reasoning_items": "CLOB",
                        "codex_message_items": "CLOB",
                    }.items():
                        self._ensure_column(cur, "messages", name, ddl)

                if not self._table_exists(cur, "state_meta"):
                    cur.execute(
                        """CREATE TABLE state_meta (
                            key VARCHAR2(256) PRIMARY KEY,
                            value CLOB
                        )"""
                    )

                for sql in (
                    "CREATE INDEX idx_messages_session ON messages(session_id)",
                    "CREATE INDEX idx_messages_timestamp ON messages(timestamp_val)",
                    "CREATE INDEX idx_sessions_source ON sessions(source)",
                    "CREATE INDEX idx_sessions_parent ON sessions(parent_session_id)",
                    "CREATE INDEX idx_sessions_started ON sessions(started_at DESC)",
                ):
                    try:
                        cur.execute(sql)
                    except oracledb.DatabaseError:
                        pass

                try:
                    cur.execute(
                        """CREATE INDEX idx_messages_content_ft ON messages(content)
                           INDEXTYPE IS CTXSYS.CONTEXT
                           PARAMETERS ('SYNC (ON COMMIT)')"""
                    )
                except oracledb.DatabaseError:
                    pass
            conn.commit()

    # ------------------------------------------------------------------
    # Encoding helpers
    # ------------------------------------------------------------------

    @classmethod
    def _encode_content(cls, content: Any) -> Any:
        if isinstance(content, (list, dict)):
            return cls._JSON_PREFIX + json.dumps(content, ensure_ascii=False)
        return content

    @classmethod
    def _decode_content(cls, content: Any) -> Any:
        if isinstance(content, str) and content.startswith(cls._JSON_PREFIX):
            return _json_loads(content[len(cls._JSON_PREFIX):], content)
        return content

    @staticmethod
    def sanitize_title(title: Optional[str]) -> Optional[str]:
        if not title:
            return None
        cleaned = re.sub(r"[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]", "", title)
        cleaned = re.sub(
            r"[\u200b-\u200f\u2028-\u202e\u2060-\u2069\ufeff\ufffc\ufff9-\ufffb]",
            "",
            cleaned,
        )
        cleaned = re.sub(r"\s+", " ", cleaned).strip()
        if not cleaned:
            return None
        if len(cleaned) > OracleSessionDB.MAX_TITLE_LENGTH:
            raise ValueError(
                f"Title too long ({len(cleaned)} chars, max {OracleSessionDB.MAX_TITLE_LENGTH})"
            )
        return cleaned

    # ------------------------------------------------------------------
    # Session lifecycle
    # ------------------------------------------------------------------

    def create_session(self, session_id: str, source: str, **kwargs: Any) -> str:
        model_config = kwargs.get("model_config")
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """MERGE INTO sessions s
                       USING (SELECT :id AS id FROM dual) src
                       ON (s.id = src.id)
                       WHEN NOT MATCHED THEN INSERT (
                           id, source, user_id, model, model_config, system_prompt,
                           parent_session_id, started_at, message_count,
                           tool_call_count, input_tokens, output_tokens,
                           cache_read_tokens, cache_write_tokens, reasoning_tokens,
                           api_call_count
                       ) VALUES (
                           :id, :source, :user_id, :model, :model_config,
                           :system_prompt, :parent_session_id, :started_at, 0,
                           0, 0, 0, 0, 0, 0, 0
                       )""",
                    {
                        "id": session_id,
                        "source": source,
                        "user_id": kwargs.get("user_id"),
                        "model": kwargs.get("model"),
                        "model_config": _json_dumps(model_config),
                        "system_prompt": kwargs.get("system_prompt"),
                        "parent_session_id": kwargs.get("parent_session_id"),
                        "started_at": time.time(),
                    },
                )
            conn.commit()
        return session_id

    def ensure_session(self, session_id: str, source: str = "unknown", model: str = None, **kwargs: Any) -> str:
        kwargs.setdefault("model", model)
        return self.create_session(session_id=session_id, source=source, **kwargs)

    def end_session(self, session_id: str, end_reason: str = "normal") -> None:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """UPDATE sessions
                       SET ended_at = :ended_at, end_reason = :end_reason
                       WHERE id = :id AND ended_at IS NULL""",
                    {"ended_at": time.time(), "end_reason": end_reason, "id": session_id},
                )
            conn.commit()

    def reopen_session(self, session_id: str) -> None:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE sessions SET ended_at = NULL, end_reason = NULL WHERE id = :id",
                    {"id": session_id},
                )
            conn.commit()

    def update_system_prompt(self, session_id: str, system_prompt: str) -> None:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE sessions SET system_prompt = :prompt WHERE id = :id",
                    {"prompt": system_prompt, "id": session_id},
                )
            conn.commit()

    def update_session_model_config(
        self,
        session_id: str,
        model_config: Any,
        model: Optional[str] = None,
    ) -> None:
        """Update per-session model metadata using Oracle bind parameters."""
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """UPDATE sessions
                       SET model_config = :model_config,
                           model = COALESCE(:model, model)
                       WHERE id = :id""",
                    {
                        "model_config": _json_dumps(model_config),
                        "model": model,
                        "id": session_id,
                    },
                )
            conn.commit()

    def update_token_counts(
        self,
        session_id: str,
        input_tokens: int = 0,
        output_tokens: int = 0,
        model: str = None,
        cache_read_tokens: int = 0,
        cache_write_tokens: int = 0,
        reasoning_tokens: int = 0,
        estimated_cost_usd: Optional[float] = None,
        actual_cost_usd: Optional[float] = None,
        cost_status: Optional[str] = None,
        cost_source: Optional[str] = None,
        pricing_version: Optional[str] = None,
        billing_provider: Optional[str] = None,
        billing_base_url: Optional[str] = None,
        billing_mode: Optional[str] = None,
        api_call_count: int = 0,
        absolute: bool = False,
    ) -> None:
        self.ensure_session(session_id, model=model)
        params = {
            "id": session_id,
            "input_tokens": input_tokens,
            "output_tokens": output_tokens,
            "model": model,
            "cache_read_tokens": cache_read_tokens,
            "cache_write_tokens": cache_write_tokens,
            "reasoning_tokens": reasoning_tokens,
            "estimated_cost_usd": estimated_cost_usd,
            "actual_cost_usd": actual_cost_usd,
            "cost_status": cost_status,
            "cost_source": cost_source,
            "pricing_version": pricing_version,
            "billing_provider": billing_provider,
            "billing_base_url": billing_base_url,
            "billing_mode": billing_mode,
            "api_call_count": api_call_count,
        }
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                if absolute:
                    cur.execute(
                        """UPDATE sessions SET
                           input_tokens = :input_tokens,
                           output_tokens = :output_tokens,
                           cache_read_tokens = :cache_read_tokens,
                           cache_write_tokens = :cache_write_tokens,
                           reasoning_tokens = :reasoning_tokens,
                           estimated_cost_usd = COALESCE(:estimated_cost_usd, 0),
                           actual_cost_usd = COALESCE(:actual_cost_usd, actual_cost_usd),
                           cost_status = COALESCE(:cost_status, cost_status),
                           cost_source = COALESCE(:cost_source, cost_source),
                           pricing_version = COALESCE(:pricing_version, pricing_version),
                           billing_provider = COALESCE(billing_provider, :billing_provider),
                           billing_base_url = COALESCE(billing_base_url, :billing_base_url),
                           billing_mode = COALESCE(billing_mode, :billing_mode),
                           model = COALESCE(model, :model),
                           api_call_count = :api_call_count
                           WHERE id = :id""",
                        params,
                    )
                else:
                    cur.execute(
                        """UPDATE sessions SET
                           input_tokens = COALESCE(input_tokens, 0) + :input_tokens,
                           output_tokens = COALESCE(output_tokens, 0) + :output_tokens,
                           cache_read_tokens = COALESCE(cache_read_tokens, 0) + :cache_read_tokens,
                           cache_write_tokens = COALESCE(cache_write_tokens, 0) + :cache_write_tokens,
                           reasoning_tokens = COALESCE(reasoning_tokens, 0) + :reasoning_tokens,
                           estimated_cost_usd = COALESCE(estimated_cost_usd, 0) + COALESCE(:estimated_cost_usd, 0),
                           actual_cost_usd = CASE WHEN :actual_cost_usd IS NULL THEN actual_cost_usd
                                                 ELSE COALESCE(actual_cost_usd, 0) + :actual_cost_usd END,
                           cost_status = COALESCE(:cost_status, cost_status),
                           cost_source = COALESCE(:cost_source, cost_source),
                           pricing_version = COALESCE(:pricing_version, pricing_version),
                           billing_provider = COALESCE(billing_provider, :billing_provider),
                           billing_base_url = COALESCE(billing_base_url, :billing_base_url),
                           billing_mode = COALESCE(billing_mode, :billing_mode),
                           model = COALESCE(model, :model),
                           api_call_count = COALESCE(api_call_count, 0) + :api_call_count
                           WHERE id = :id""",
                        params,
                    )
            conn.commit()

    # ------------------------------------------------------------------
    # Messages
    # ------------------------------------------------------------------

    def append_message(
        self,
        session_id: str,
        role: str,
        content: Any = None,
        tool_name: str = None,
        tool_calls: Any = None,
        tool_call_id: str = None,
        token_count: int = None,
        finish_reason: str = None,
        reasoning: str = None,
        reasoning_content: str = None,
        reasoning_details: Any = None,
        codex_reasoning_items: Any = None,
        codex_message_items: Any = None,
    ) -> int:
        self.ensure_session(session_id)
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                msg_id = self._append_message_with_cursor(
                    cur,
                    session_id=session_id,
                    role=role,
                    content=content,
                    tool_name=tool_name,
                    tool_calls=tool_calls,
                    tool_call_id=tool_call_id,
                    token_count=token_count,
                    finish_reason=finish_reason,
                    reasoning=reasoning,
                    reasoning_content=reasoning_content,
                    reasoning_details=reasoning_details,
                    codex_reasoning_items=codex_reasoning_items,
                    codex_message_items=codex_message_items,
                )
            conn.commit()
        return msg_id

    def _append_message_with_cursor(
        self,
        cur,
        *,
        session_id: str,
        role: str,
        content: Any = None,
        tool_name: str = None,
        tool_calls: Any = None,
        tool_call_id: str = None,
        token_count: int = None,
        finish_reason: str = None,
        reasoning: str = None,
        reasoning_content: str = None,
        reasoning_details: Any = None,
        codex_reasoning_items: Any = None,
        codex_message_items: Any = None,
    ) -> int:
        stored_content = self._encode_content(content)
        num_tool_calls = len(tool_calls) if isinstance(tool_calls, list) else (1 if tool_calls else 0)
        msg_id_var = cur.var(int)
        cur.execute(
            """INSERT INTO messages (
                session_id, role, content, tool_call_id, tool_calls,
                tool_name, timestamp_val, token_count, finish_reason,
                reasoning, reasoning_content, reasoning_details,
                codex_reasoning_items, codex_message_items
            ) VALUES (
                :session_id, :role, :content, :tool_call_id, :tool_calls,
                :tool_name, :timestamp_val, :token_count, :finish_reason,
                :reasoning, :reasoning_content, :reasoning_details,
                :codex_reasoning_items, :codex_message_items
            ) RETURNING id INTO :msg_id""",
            {
                "session_id": session_id,
                "role": role,
                "content": stored_content,
                "tool_call_id": tool_call_id,
                "tool_calls": _json_dumps(tool_calls),
                "tool_name": tool_name,
                "timestamp_val": time.time(),
                "token_count": token_count,
                "finish_reason": finish_reason,
                "reasoning": reasoning,
                "reasoning_content": reasoning_content,
                "reasoning_details": _json_dumps(reasoning_details),
                "codex_reasoning_items": _json_dumps(codex_reasoning_items),
                "codex_message_items": _json_dumps(codex_message_items),
                "msg_id": msg_id_var,
            },
        )
        msg_id = int(msg_id_var.getvalue()[0])
        cur.execute(
            """UPDATE sessions
               SET message_count = COALESCE(message_count, 0) + 1,
                   tool_call_count = COALESCE(tool_call_count, 0) + :tool_count
               WHERE id = :session_id""",
            {"tool_count": num_tool_calls, "session_id": session_id},
        )
        return msg_id

    def replace_messages(self, session_id: str, messages: List[Dict[str, Any]]) -> None:
        self.ensure_session(session_id)
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute("DELETE FROM messages WHERE session_id = :id", {"id": session_id})
                cur.execute(
                    "UPDATE sessions SET message_count = 0, tool_call_count = 0 WHERE id = :id",
                    {"id": session_id},
                )
                for msg in messages:
                    self._append_message_with_cursor(
                        cur,
                        session_id=session_id,
                        role=msg.get("role", "unknown"),
                        content=msg.get("content"),
                        tool_name=msg.get("tool_name") or msg.get("name"),
                        tool_calls=msg.get("tool_calls"),
                        tool_call_id=msg.get("tool_call_id"),
                        token_count=msg.get("token_count"),
                        finish_reason=msg.get("finish_reason"),
                        reasoning=msg.get("reasoning") if msg.get("role") == "assistant" else None,
                        reasoning_content=msg.get("reasoning_content") if msg.get("role") == "assistant" else None,
                        reasoning_details=msg.get("reasoning_details") if msg.get("role") == "assistant" else None,
                        codex_reasoning_items=msg.get("codex_reasoning_items") if msg.get("role") == "assistant" else None,
                        codex_message_items=msg.get("codex_message_items") if msg.get("role") == "assistant" else None,
                    )
            conn.commit()

    def _hydrate_message(self, row: Dict[str, Any]) -> Dict[str, Any]:
        if "timestamp_val" in row:
            row["timestamp"] = row.pop("timestamp_val")
        if "content" in row:
            row["content"] = self._decode_content(row["content"])
        for key in ("tool_calls", "reasoning_details", "codex_reasoning_items", "codex_message_items"):
            if row.get(key):
                row[key] = _json_loads(row[key], [] if key == "tool_calls" else None)
        return row

    def get_messages(self, session_id: str) -> List[Dict[str, Any]]:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "SELECT * FROM messages WHERE session_id = :id ORDER BY id",
                    {"id": session_id},
                )
                return [self._hydrate_message(r) for r in _rows_to_dicts(cur, cur.fetchall())]

    def get_messages_as_conversation(
        self,
        session_id: str,
        include_ancestors: bool = False,
    ) -> List[Dict[str, Any]]:
        session_ids = [session_id]
        if include_ancestors:
            session_ids = self._session_lineage_root_to_tip(session_id)
        out: List[Dict[str, Any]] = []
        for sid in session_ids:
            for msg in self.get_messages(sid):
                item = {"role": msg.get("role"), "content": msg.get("content")}
                if msg.get("tool_calls"):
                    item["tool_calls"] = msg["tool_calls"]
                if msg.get("tool_call_id"):
                    item["tool_call_id"] = msg["tool_call_id"]
                if msg.get("tool_name"):
                    item["name"] = msg["tool_name"]
                    item["tool_name"] = msg["tool_name"]
                for key in (
                    "finish_reason",
                    "reasoning",
                    "reasoning_content",
                    "reasoning_details",
                    "codex_reasoning_items",
                    "codex_message_items",
                ):
                    if msg.get(key) is not None:
                        item[key] = msg[key]
                out.append(item)
        return out

    def get_messages_around(self, session_id: str, around_message_id: int, window: int = 5) -> Dict[str, Any]:
        window = max(0, int(window or 0))
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "SELECT 1 FROM messages WHERE id = :id AND session_id = :sid",
                    {"id": around_message_id, "sid": session_id},
                )
                if cur.fetchone() is None:
                    return {"window": [], "messages_before": 0, "messages_after": 0}
                cur.execute(
                    """SELECT * FROM (
                           SELECT * FROM messages
                           WHERE session_id = :sid AND id <= :id
                           ORDER BY id DESC
                       ) WHERE ROWNUM <= :lim""",
                    {"sid": session_id, "id": around_message_id, "lim": window + 1},
                )
                before = _rows_to_dicts(cur, cur.fetchall())
                cur.execute(
                    """SELECT * FROM (
                           SELECT * FROM messages
                           WHERE session_id = :sid AND id > :id
                           ORDER BY id ASC
                       ) WHERE ROWNUM <= :lim""",
                    {"sid": session_id, "id": around_message_id, "lim": window},
                )
                after = _rows_to_dicts(cur, cur.fetchall())
        rows = list(reversed(before)) + after
        return {
            "window": [self._hydrate_message(r) for r in rows],
            "messages_before": max(0, len(before) - 1),
            "messages_after": len(after),
        }

    def find_message_session(self, message_id: int) -> Optional[str]:
        """Return the owning session id for a message, if present."""
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "SELECT session_id FROM messages WHERE id = :id",
                    {"id": message_id},
                )
                row = cur.fetchone()
                return row[0] if row else None

    def get_anchored_view(
        self,
        session_id: str,
        around_message_id: int,
        window: int = 5,
        bookend: int = 3,
        keep_roles: Optional[Tuple[str, ...]] = ("user", "assistant"),
    ) -> Dict[str, Any]:
        primitive = self.get_messages_around(session_id, around_message_id, window=window)
        rows = primitive["window"]
        if not rows:
            return {
                "window": [],
                "messages_before": 0,
                "messages_after": 0,
                "bookend_start": [],
                "bookend_end": [],
            }
        keep_set = set(keep_roles) if keep_roles else None
        filtered = [
            m for m in rows if keep_set is None or m.get("id") == around_message_id or m.get("role") in keep_set
        ]
        min_id, max_id = rows[0]["id"], rows[-1]["id"]
        start_rows: List[Dict[str, Any]] = []
        end_rows: List[Dict[str, Any]] = []
        if bookend > 0:
            role_clause, params = self._role_clause("role", keep_roles)
            with self._get_conn() as conn:
                with conn.cursor() as cur:
                    cur.execute(
                        f"""SELECT * FROM (
                               SELECT * FROM messages
                               WHERE session_id = :sid AND id < :min_id
                                 AND content IS NOT NULL {role_clause}
                               ORDER BY id ASC
                           ) WHERE ROWNUM <= :lim""",
                        {"sid": session_id, "min_id": min_id, "lim": bookend, **params},
                    )
                    start_rows = _rows_to_dicts(cur, cur.fetchall())
                    cur.execute(
                        f"""SELECT * FROM (
                               SELECT * FROM messages
                               WHERE session_id = :sid AND id > :max_id
                                 AND content IS NOT NULL {role_clause}
                               ORDER BY id DESC
                           ) WHERE ROWNUM <= :lim""",
                        {"sid": session_id, "max_id": max_id, "lim": bookend, **params},
                    )
                    end_rows = list(reversed(_rows_to_dicts(cur, cur.fetchall())))
        return {
            "window": filtered,
            "messages_before": primitive["messages_before"],
            "messages_after": primitive["messages_after"],
            "bookend_start": [self._hydrate_message(r) for r in start_rows],
            "bookend_end": [self._hydrate_message(r) for r in end_rows],
        }

    # ------------------------------------------------------------------
    # Session lookup/listing/title helpers
    # ------------------------------------------------------------------

    def get_session(self, session_id: str) -> Optional[Dict[str, Any]]:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT * FROM sessions WHERE id = :id", {"id": session_id})
                row = cur.fetchone()
                if not row:
                    return None
                data = _rows_to_dicts(cur, [row])[0]
        if data.get("model_config"):
            data["model_config"] = _json_loads(data["model_config"], data["model_config"])
        return data

    def resolve_session_id(self, session_id_or_prefix: str) -> Optional[str]:
        if self.get_session(session_id_or_prefix):
            return session_id_or_prefix
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """SELECT id FROM sessions
                       WHERE id LIKE :prefix ESCAPE '\\'
                       ORDER BY started_at DESC FETCH FIRST 2 ROWS ONLY""",
                    {"prefix": self._escape_like(session_id_or_prefix) + "%"},
                )
                rows = [r[0] for r in cur.fetchall()]
        return rows[0] if len(rows) == 1 else None

    def set_session_title(self, session_id: str, title: str) -> bool:
        clean = self.sanitize_title(title)
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                if clean:
                    cur.execute(
                        "SELECT id FROM sessions WHERE title = :title AND id != :id FETCH FIRST 1 ROWS ONLY",
                        {"title": clean, "id": session_id},
                    )
                    conflict = cur.fetchone()
                    if conflict:
                        raise ValueError(f"Title '{clean}' is already in use by session {conflict[0]}")
                cur.execute(
                    "UPDATE sessions SET title = :title WHERE id = :id",
                    {"title": clean, "id": session_id},
                )
                count = cur.rowcount
            conn.commit()
        return count > 0

    def get_session_title(self, session_id: str) -> Optional[str]:
        row = self.get_session(session_id)
        return row.get("title") if row else None

    def get_session_by_title(self, title: str) -> Optional[Dict[str, Any]]:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT * FROM sessions WHERE title = :title", {"title": title})
                row = cur.fetchone()
                return _rows_to_dicts(cur, [row])[0] if row else None

    def resolve_session_by_title(self, title: str) -> Optional[str]:
        exact = self.get_session_by_title(title)
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """SELECT id FROM sessions
                       WHERE title LIKE :pattern ESCAPE '\\'
                       ORDER BY started_at DESC FETCH FIRST 1 ROWS ONLY""",
                    {"pattern": self._escape_like(title) + " #_%"},
                )
                numbered = cur.fetchone()
        if numbered:
            return numbered[0]
        return exact["id"] if exact else None

    def get_next_title_in_lineage(self, base_title: str) -> str:
        match = re.match(r"^(.*?) #(\d+)$", base_title)
        base = match.group(1) if match else base_title
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """SELECT title FROM sessions
                       WHERE title = :base OR title LIKE :pattern ESCAPE '\\'""",
                    {"base": base, "pattern": self._escape_like(base) + " #_%"},
                )
                titles = [r[0] for r in cur.fetchall()]
        if not titles:
            return base
        max_num = 1
        for title in titles:
            m = re.match(r"^.* #(\d+)$", title or "")
            if m:
                max_num = max(max_num, int(m.group(1)))
        return f"{base} #{max_num + 1}"

    def list_sessions(self, source: Optional[str] = None, limit: int = 50) -> List[Dict[str, Any]]:
        return self.list_sessions_rich(source=source, limit=limit, include_children=True)

    def list_sessions_rich(
        self,
        source: str = None,
        exclude_sources: List[str] = None,
        limit: int = 20,
        offset: int = 0,
        include_children: bool = False,
        project_compression_tips: bool = True,
        order_by_last_active: bool = False,
    ) -> List[Dict[str, Any]]:
        # Older call sites used non-positive limits for "unlimited". Oracle
        # FETCH requires a positive row count, so keep the public convention
        # while using a practical upper bound.
        if limit is None or limit <= 0:
            limit = 1_000_000
        if offset is None or offset < 0:
            offset = 0
        clauses = []
        params: Dict[str, Any] = {"limit": limit, "offset": offset}
        if not include_children:
            clauses.append(
                """(s.parent_session_id IS NULL OR EXISTS (
                    SELECT 1 FROM sessions p
                    WHERE p.id = s.parent_session_id
                      AND p.end_reason = 'branched'
                      AND s.started_at >= p.ended_at
                ))"""
            )
        if source:
            clauses.append("s.source = :source")
            params["source"] = source
        if exclude_sources:
            names = []
            for i, value in enumerate(exclude_sources):
                key = f"exclude_{i}"
                names.append(f":{key}")
                params[key] = value
            clauses.append(f"s.source NOT IN ({','.join(names)})")
        where = "WHERE " + " AND ".join(clauses) if clauses else ""
        order_col = "last_active" if order_by_last_active else "s.started_at"
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    f"""SELECT s.*,
                              COALESCE((SELECT MAX(m.timestamp_val)
                                        FROM messages m
                                        WHERE m.session_id = s.id), s.started_at) AS last_active,
                              COALESCE((SELECT SUBSTR(REPLACE(REPLACE(m2.content, CHR(10), ' '), CHR(13), ' '), 1, 63)
                                        FROM messages m2
                                        WHERE m2.session_id = s.id
                                          AND m2.role = 'user'
                                          AND m2.content IS NOT NULL
                                        ORDER BY m2.timestamp_val, m2.id
                                        FETCH FIRST 1 ROWS ONLY), '') AS preview
                       FROM sessions s
                       {where}
                       ORDER BY {order_col} DESC, s.id DESC
                       OFFSET :offset ROWS FETCH NEXT :limit ROWS ONLY""",
                    params,
                )
                rows = _rows_to_dicts(cur, cur.fetchall())
        for row in rows:
            preview = (row.get("preview") or "").strip()
            row["preview"] = preview[:60] + ("..." if len(preview) > 60 else "")
        if project_compression_tips and not include_children:
            projected: List[Dict[str, Any]] = []
            for row in rows:
                if row.get("end_reason") != "compression":
                    projected.append(row)
                    continue
                tip = self.get_compression_tip(row["id"])
                tip_row = self._get_session_rich_row(tip) if tip != row["id"] else None
                projected.append({**row, **(tip_row or {})})
            rows = projected
        return rows

    def _get_session_rich_row(self, session_id: str) -> Optional[Dict[str, Any]]:
        rows = self.list_sessions_rich(limit=1, include_children=True, project_compression_tips=False)
        for row in rows:
            if row.get("id") == session_id:
                return row
        session = self.get_session(session_id)
        if not session:
            return None
        messages = self.get_messages(session_id)
        session["last_active"] = max((m.get("timestamp") or session.get("started_at") for m in messages), default=session.get("started_at"))
        session["preview"] = next((str(m.get("content") or "")[:60] for m in messages if m.get("role") == "user"), "")
        return session

    def search_sessions(self, source: str = None, limit: int = 20, offset: int = 0) -> List[Dict[str, Any]]:
        return self.list_sessions_rich(source=source, limit=limit, offset=offset, include_children=True)

    def session_count(self, source: str = None) -> int:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                if source:
                    cur.execute("SELECT COUNT(*) FROM sessions WHERE source = :source", {"source": source})
                else:
                    cur.execute("SELECT COUNT(*) FROM sessions")
                return int(cur.fetchone()[0])

    def message_count(self, session_id: str = None) -> int:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                if session_id:
                    cur.execute("SELECT COUNT(*) FROM messages WHERE session_id = :id", {"id": session_id})
                else:
                    cur.execute("SELECT COUNT(*) FROM messages")
                return int(cur.fetchone()[0])

    # ------------------------------------------------------------------
    # Search
    # ------------------------------------------------------------------

    @staticmethod
    def _escape_like(value: str) -> str:
        return (value or "").replace("\\", "\\\\").replace("%", "\\%").replace("_", "\\_")

    def _role_clause(self, column: str, role_filter: Optional[Sequence[str]]) -> Tuple[str, Dict[str, Any]]:
        if not role_filter:
            return "", {}
        params = {f"role_{i}": role for i, role in enumerate(role_filter)}
        return f" AND {column} IN ({','.join(':' + key for key in params)})", params

    def search_messages(
        self,
        query: str,
        source_filter: List[str] = None,
        exclude_sources: List[str] = None,
        role_filter: List[str] = None,
        limit: int = 20,
        offset: int = 0,
        sort: str = None,
    ) -> List[Dict[str, Any]]:
        if not query or not query.strip():
            return []
        clauses = ["LOWER(m.content) LIKE :query"]
        params: Dict[str, Any] = {
            "query": f"%{self._escape_like(query.lower())}%",
            "limit": limit,
            "offset": offset,
        }
        if source_filter:
            keys = []
            for i, value in enumerate(source_filter):
                key = f"source_{i}"
                keys.append(f":{key}")
                params[key] = value
            clauses.append(f"s.source IN ({','.join(keys)})")
        if exclude_sources:
            keys = []
            for i, value in enumerate(exclude_sources):
                key = f"exclude_{i}"
                keys.append(f":{key}")
                params[key] = value
            clauses.append(f"s.source NOT IN ({','.join(keys)})")
        role_clause, role_params = self._role_clause("m.role", role_filter)
        if role_clause:
            clauses.append(role_clause[5:])
            params.update(role_params)
        order_by = "m.timestamp_val DESC"
        if sort == "oldest":
            order_by = "m.timestamp_val ASC"
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    f"""SELECT m.id, m.session_id, m.role, m.content,
                              m.timestamp_val AS timestamp, m.tool_name,
                              s.source, s.model, s.started_at AS session_started
                       FROM messages m
                       JOIN sessions s ON s.id = m.session_id
                       WHERE {' AND '.join(clauses)}
                       ORDER BY {order_by}
                       OFFSET :offset ROWS FETCH NEXT :limit ROWS ONLY""",
                    params,
                )
                rows = _rows_to_dicts(cur, cur.fetchall())
        results = []
        for row in rows:
            content = str(self._decode_content(row.get("content")) or "")
            idx = content.lower().find(query.lower())
            snippet = content[max(0, idx - 40): idx + len(query) + 80] if idx >= 0 else content[:120]
            row["snippet"] = snippet
            row["content"] = content
            row["relevance"] = 1
            try:
                view = self.get_messages_around(row["session_id"], int(row["id"]), window=1)
                row["context"] = [
                    {"role": m.get("role"), "content": str(m.get("content") or "")[:200]}
                    for m in view.get("window", [])
                ]
            except Exception:
                row["context"] = []
            results.append(row)
        return results

    # ------------------------------------------------------------------
    # Lineage, export, deletion, metadata
    # ------------------------------------------------------------------

    def _session_lineage_root_to_tip(self, session_id: str) -> List[str]:
        lineage = []
        current = session_id
        seen = set()
        while current and current not in seen:
            seen.add(current)
            row = self.get_session(current)
            if not row:
                break
            parent = row.get("parent_session_id")
            if not parent:
                break
            current = parent
        root = current or session_id
        current = root
        while current and current not in lineage:
            lineage.append(current)
            with self._get_conn() as conn:
                with conn.cursor() as cur:
                    cur.execute(
                        """SELECT id FROM sessions
                           WHERE parent_session_id = :id
                           ORDER BY started_at ASC FETCH FIRST 1 ROWS ONLY""",
                        {"id": current},
                    )
                    child = cur.fetchone()
            current = child[0] if child else None
        return lineage

    def get_compression_tip(self, session_id: str) -> Optional[str]:
        current = session_id
        seen = {current}
        for _ in range(32):
            with self._get_conn() as conn:
                with conn.cursor() as cur:
                    cur.execute(
                        """SELECT child.id
                           FROM sessions parent
                           JOIN sessions child ON child.parent_session_id = parent.id
                           WHERE parent.id = :id
                             AND parent.end_reason = 'compression'
                             AND child.started_at >= parent.ended_at
                           ORDER BY child.started_at DESC
                           FETCH FIRST 1 ROWS ONLY""",
                        {"id": current},
                    )
                    row = cur.fetchone()
            if not row or row[0] in seen:
                return current
            current = row[0]
            seen.add(current)
        return current

    def resolve_resume_session_id(self, session_id: str) -> str:
        if not session_id:
            return session_id
        if self.message_count(session_id):
            return session_id
        current = session_id
        seen = {current}
        for _ in range(32):
            with self._get_conn() as conn:
                with conn.cursor() as cur:
                    cur.execute(
                        """SELECT id FROM sessions
                           WHERE parent_session_id = :id
                           ORDER BY started_at DESC, id DESC
                           FETCH FIRST 1 ROWS ONLY""",
                        {"id": current},
                    )
                    row = cur.fetchone()
            if not row or row[0] in seen:
                return session_id
            current = row[0]
            if self.message_count(current):
                return current
            seen.add(current)
        return session_id

    def export_session(self, session_id: str) -> Optional[Dict[str, Any]]:
        session = self.get_session(session_id)
        if not session:
            return None
        return {**session, "messages": self.get_messages(session_id)}

    def export_all(self, source: str = None) -> List[Dict[str, Any]]:
        return [
            self.export_session(row["id"])
            for row in self.search_sessions(source=source, limit=100000)
        ]

    def clear_messages(self, session_id: str) -> None:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute("DELETE FROM messages WHERE session_id = :id", {"id": session_id})
                cur.execute(
                    "UPDATE sessions SET message_count = 0, tool_call_count = 0 WHERE id = :id",
                    {"id": session_id},
                )
            conn.commit()

    @staticmethod
    def _remove_session_files(sessions_dir: Optional[Path], session_id: str) -> None:
        # OraHermes does not maintain local per-session transcript files.
        # The argument is kept for compatibility with upstream call sites.
        return

    def delete_session(self, session_id: str, sessions_dir: Optional[Path] = None) -> bool:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT COUNT(*) FROM sessions WHERE id = :id", {"id": session_id})
                if int(cur.fetchone()[0]) == 0:
                    return False
                cur.execute(
                    "UPDATE sessions SET parent_session_id = NULL WHERE parent_session_id = :id",
                    {"id": session_id},
                )
                cur.execute("DELETE FROM messages WHERE session_id = :id", {"id": session_id})
                cur.execute("DELETE FROM sessions WHERE id = :id", {"id": session_id})
            conn.commit()
        self._remove_session_files(sessions_dir, session_id)
        return True

    def prune_sessions(
        self,
        older_than_days: int = 90,
        source: str = None,
        sessions_dir: Optional[Path] = None,
    ) -> int:
        cutoff = time.time() - older_than_days * 86400
        clauses = ["started_at < :cutoff", "ended_at IS NOT NULL"]
        params: Dict[str, Any] = {"cutoff": cutoff}
        if source:
            clauses.append("source = :source")
            params["source"] = source
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(f"SELECT id FROM sessions WHERE {' AND '.join(clauses)}", params)
                ids = [r[0] for r in cur.fetchall()]
                for sid in ids:
                    cur.execute(
                        "UPDATE sessions SET parent_session_id = NULL WHERE parent_session_id = :id",
                        {"id": sid},
                    )
                    cur.execute("DELETE FROM messages WHERE session_id = :id", {"id": sid})
                    cur.execute("DELETE FROM sessions WHERE id = :id", {"id": sid})
            conn.commit()
        for sid in ids:
            self._remove_session_files(sessions_dir, sid)
        return len(ids)

    def get_meta(self, key: str) -> Optional[str]:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT value FROM state_meta WHERE key = :key", {"key": key})
                row = cur.fetchone()
                return row[0] if row else None

    def query_dicts(self, sql: str, params: Optional[Dict[str, Any]] = None) -> List[Dict[str, Any]]:
        """Run an Oracle query and return lowercase-keyed row dictionaries."""
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(sql, params or {})
                return _rows_to_dicts(cur, cur.fetchall())

    def query_one(self, sql: str, params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
        """Run an Oracle query and return one lowercase-keyed row dictionary."""
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(sql, params or {})
                row = cur.fetchone()
                if not row:
                    return {}
                return _rows_to_dicts(cur, [row])[0]

    def set_meta(self, key: str, value: str) -> None:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """MERGE INTO state_meta m
                       USING (SELECT :key AS key FROM dual) src
                       ON (m.key = src.key)
                       WHEN MATCHED THEN UPDATE SET value = :value
                       WHEN NOT MATCHED THEN INSERT (key, value) VALUES (:key, :value)""",
                    {"key": key, "value": value},
                )
            conn.commit()

    def prune_empty_ghost_sessions(self, sessions_dir: Optional[Path] = None) -> int:
        return 0

    def finalize_orphaned_compression_sessions(self) -> int:
        return 0

    def maybe_auto_prune_and_vacuum(self, *_, **__) -> None:
        return None

    def vacuum(self) -> None:
        return None

    # ------------------------------------------------------------------
    # Handoff and Telegram topic compatibility
    # ------------------------------------------------------------------

    def request_handoff(self, session_id: str, platform: str) -> bool:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """UPDATE sessions
                       SET handoff_state = 'pending',
                           handoff_platform = :platform,
                           handoff_error = NULL
                       WHERE id = :id
                         AND (handoff_state IS NULL OR handoff_state IN ('completed', 'failed'))""",
                    {"platform": platform, "id": session_id},
                )
                count = cur.rowcount
            conn.commit()
        return count > 0

    def get_handoff_state(self, session_id: str) -> Optional[Dict[str, Any]]:
        session = self.get_session(session_id)
        if not session or not session.get("handoff_state"):
            return None
        return {
            "state": session.get("handoff_state"),
            "platform": session.get("handoff_platform"),
            "error": session.get("handoff_error"),
        }

    def list_pending_handoffs(self) -> List[Dict[str, Any]]:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "SELECT * FROM sessions WHERE handoff_state = 'pending' ORDER BY started_at ASC"
                )
                return _rows_to_dicts(cur, cur.fetchall())

    def claim_handoff(self, session_id: str) -> bool:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE sessions SET handoff_state = 'running' WHERE id = :id AND handoff_state = 'pending'",
                    {"id": session_id},
                )
                count = cur.rowcount
            conn.commit()
        return count > 0

    def complete_handoff(self, session_id: str) -> None:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE sessions SET handoff_state = 'completed', handoff_error = NULL WHERE id = :id",
                    {"id": session_id},
                )
            conn.commit()

    def fail_handoff(self, session_id: str, error: str) -> None:
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE sessions SET handoff_state = 'failed', handoff_error = :error WHERE id = :id",
                    {"error": (error or "")[:500], "id": session_id},
                )
            conn.commit()

    def apply_telegram_topic_migration(self) -> None:
        return None

    def enable_telegram_topic_mode(self, *_, **__) -> bool:
        return False

    def disable_telegram_topic_mode(self, *_, **__) -> bool:
        return False

    def is_telegram_topic_mode_enabled(self, *_, **__) -> bool:
        return False

    def get_telegram_topic_binding(self, *_, **__) -> Optional[Dict[str, Any]]:
        return None

    def bind_telegram_topic(self, *_, **__) -> None:
        return None

    def is_telegram_session_linked_to_topic(self, *_, **__) -> bool:
        return False

    def list_unlinked_telegram_sessions_for_user(self, *_, **__) -> List[Dict[str, Any]]:
        return []

    # ------------------------------------------------------------------
    # Oracle AI Vector Search
    # ------------------------------------------------------------------

    def _check_vector_support(self) -> bool:
        try:
            with self._get_conn() as conn:
                with conn.cursor() as cur:
                    cur.execute(
                        """SELECT COUNT(*) FROM user_tab_columns
                           WHERE table_name = 'MESSAGES' AND column_name = 'EMBEDDING'"""
                    )
                    if cur.fetchone()[0] == 0:
                        return False
                    cur.execute(
                        "SELECT COUNT(*) FROM all_mining_models WHERE model_name = :name",
                        {"name": self.EMBEDDING_MODEL},
                    )
                    return cur.fetchone()[0] > 0
        except Exception as exc:
            logger.debug("Vector support check failed: %s", exc)
            return False

    def embed_message(self, message_id: int, content: str) -> bool:
        if not content or not str(content).strip():
            return False
        text = str(content)[: self._EMBED_CHAR_LIMIT]
        try:
            with self._get_conn() as conn:
                with conn.cursor() as cur:
                    cur.execute(
                        f"""UPDATE messages
                           SET embedding = VECTOR_EMBEDDING({self.EMBEDDING_MODEL} USING :text AS data)
                           WHERE id = :id""",
                        {"text": text, "id": message_id},
                    )
                conn.commit()
            return True
        except Exception as exc:
            logger.debug("embed_message failed for msg %s: %s", message_id, exc)
            return False

    def semantic_search(self, query: str, role_filter: List[str] = None, limit: int = 20) -> List[Dict[str, Any]]:
        if not query or not query.strip():
            return []
        role_clause, role_params = self._role_clause("m.role", role_filter)
        params = {"query": query[: self._EMBED_CHAR_LIMIT], "limit": limit, **role_params}
        try:
            with self._get_conn() as conn:
                with conn.cursor() as cur:
                    cur.execute(
                        f"""SELECT m.id, m.session_id, m.role, m.content, m.timestamp_val,
                                  VECTOR_DISTANCE(
                                      m.embedding,
                                      VECTOR_EMBEDDING({self.EMBEDDING_MODEL} USING :query AS data),
                                      COSINE
                                  ) AS distance
                           FROM messages m
                           WHERE m.embedding IS NOT NULL {role_clause}
                           ORDER BY distance ASC
                           FETCH FIRST :limit ROWS ONLY""",
                        params,
                    )
                    rows = _rows_to_dicts(cur, cur.fetchall())
            for row in rows:
                row["timestamp"] = row.pop("timestamp_val", None)
                row["content"] = self._decode_content(row.get("content"))
                distance = row.get("distance")
                row["similarity"] = round(1.0 - distance, 4) if distance is not None else None
            return rows
        except Exception as exc:
            logger.warning("semantic_search failed: %s", exc)
            return []

    def hybrid_search(
        self,
        query: str,
        role_filter: List[str] = None,
        limit: int = 20,
        keyword_weight: float = 0.4,
        vector_weight: float = 0.6,
    ) -> List[Dict[str, Any]]:
        keyword_results = self.search_messages(query=query, role_filter=role_filter, limit=limit * 2)
        vector_results = self.semantic_search(query=query, role_filter=role_filter, limit=limit * 2)
        if not vector_results:
            return keyword_results[:limit]
        if not keyword_results:
            return vector_results[:limit]
        merged: Dict[Tuple[str, str], Dict[str, Any]] = {}
        for row in keyword_results:
            key = (row.get("session_id"), str(row.get("content") or "")[:100])
            row["keyword_score"] = 1.0
            row["vector_score"] = 0.0
            row["relevance"] = keyword_weight
            merged[key] = row
        for row in vector_results:
            key = (row.get("session_id"), str(row.get("content") or "")[:100])
            existing = merged.get(key, row)
            existing["keyword_score"] = existing.get("keyword_score", 0.0)
            existing["vector_score"] = row.get("similarity") or 0.0
            existing["relevance"] = round(
                keyword_weight * existing["keyword_score"] + vector_weight * existing["vector_score"],
                4,
            )
            merged[key] = existing
        return sorted(merged.values(), key=lambda r: r.get("relevance", 0), reverse=True)[:limit]

    def backfill_embeddings(self, batch_size: int = 100) -> int:
        count = 0
        try:
            with self._get_conn() as conn:
                with conn.cursor() as cur:
                    cur.execute(
                        """SELECT id, content FROM messages
                           WHERE embedding IS NULL AND content IS NOT NULL
                           FETCH FIRST :limit ROWS ONLY""",
                        {"limit": batch_size},
                    )
                    rows = cur.fetchall()
                    for msg_id, content in rows:
                        text = str(self._decode_content(content) or "")[: self._EMBED_CHAR_LIMIT]
                        if not text.strip():
                            continue
                        try:
                            cur.execute(
                                f"""UPDATE messages
                                   SET embedding = VECTOR_EMBEDDING({self.EMBEDDING_MODEL} USING :text AS data)
                                   WHERE id = :id""",
                                {"text": text, "id": msg_id},
                            )
                            count += 1
                        except Exception as exc:
                            logger.debug("backfill skip msg %s: %s", msg_id, exc)
                conn.commit()
        except Exception as exc:
            logger.warning("backfill_embeddings failed: %s", exc)
        return count
