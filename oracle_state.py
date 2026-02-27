# oracle_state.py
"""Oracle 26ai Free session state storage — replaces SQLite hermes_state.py."""

import os
import time
import uuid
import json
from typing import Any, Dict, Optional

import oracledb

# Auto-convert LOB objects to Python strings/bytes
oracledb.defaults.fetch_lobs = False


class OracleSessionDB:
    """Drop-in replacement for SessionDB using Oracle Database.

    Method signatures match hermes_state.SessionDB exactly so callers
    (run_agent.py, cli.py, gateway/) can use either backend transparently.
    """

    def __init__(
        self,
        dsn: str = None,
        user: str = None,
        password: str = None,
        **kwargs,
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

    # =========================================================================
    # Session lifecycle (matches SessionDB interface)
    # =========================================================================

    def create_session(
        self,
        session_id: str,
        source: str,
        model: str = None,
        model_config: Dict[str, Any] = None,
        system_prompt: str = None,
        user_id: str = None,
        parent_session_id: str = None,
    ) -> str:
        """Create a new session record. Returns the session_id."""
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """INSERT INTO sessions
                       (id, source, user_id, model, model_config, system_prompt,
                        parent_session_id, started_at)
                       VALUES (:1, :2, :3, :4, :5, :6, :7, :8)""",
                    [
                        session_id, source, user_id, model,
                        json.dumps(model_config) if model_config else None,
                        system_prompt, parent_session_id, time.time(),
                    ],
                )
            conn.commit()
        return session_id

    def end_session(self, session_id: str, end_reason: str = "normal"):
        """Mark a session as ended."""
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """UPDATE sessions SET ended_at = :1, end_reason = :2 WHERE id = :3""",
                    [time.time(), end_reason, session_id],
                )
            conn.commit()

    def update_system_prompt(self, session_id: str, system_prompt: str) -> None:
        """Store the full assembled system prompt snapshot."""
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE sessions SET system_prompt = :1 WHERE id = :2",
                    [system_prompt, session_id],
                )
            conn.commit()

    def update_token_counts(
        self, session_id: str, input_tokens: int = 0, output_tokens: int = 0
    ) -> None:
        """Increment token counters on a session."""
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

    # =========================================================================
    # Message storage (matches SessionDB interface)
    # =========================================================================

    def append_message(
        self,
        session_id: str,
        role: str,
        content: str = None,
        tool_name: str = None,
        tool_calls: Any = None,
        tool_call_id: str = None,
        token_count: int = None,
        finish_reason: str = None,
    ) -> int:
        """Append a message to a session. Returns the message row ID."""
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                msg_id_var = cur.var(int)
                cur.execute(
                    """INSERT INTO messages
                       (session_id, role, content, tool_call_id, tool_calls,
                        tool_name, timestamp_val, token_count, finish_reason)
                       VALUES (:1, :2, :3, :4, :5, :6, :7, :8, :9)
                       RETURNING id INTO :10""",
                    [
                        session_id, role, content, tool_call_id,
                        json.dumps(tool_calls) if tool_calls else None,
                        tool_name, time.time(), token_count, finish_reason,
                        msg_id_var,
                    ],
                )
                msg_id = msg_id_var.getvalue()[0]

                # Update counters
                is_tool_related = role == "tool" or tool_calls is not None
                if is_tool_related:
                    cur.execute(
                        """UPDATE sessions SET message_count = message_count + 1,
                           tool_call_count = tool_call_count + 1 WHERE id = :1""",
                        [session_id],
                    )
                else:
                    cur.execute(
                        """UPDATE sessions SET message_count = message_count + 1
                           WHERE id = :1""",
                        [session_id],
                    )
            conn.commit()
        return msg_id

    # Keep add_message as alias for backward compat with tests
    add_message = append_message

    def get_messages(self, session_id: str) -> list:
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

    def search_messages(self, query: str, limit: int = 20) -> list:
        """Full-text search using Oracle Text CONTAINS with LIKE fallback."""
        with self._get_conn() as conn:
            with conn.cursor() as cur:
                # Sync Oracle Text index before search
                try:
                    cur.execute(
                        "BEGIN CTX_DDL.SYNC_INDEX('idx_messages_content_ft'); END;"
                    )
                except Exception:
                    pass
                try:
                    cur.execute(
                        """SELECT m.session_id, m.role, m.content, m.timestamp_val,
                                  SCORE(1) as relevance
                           FROM messages m
                           WHERE CONTAINS(m.content, :1, 1) > 0
                           ORDER BY relevance DESC
                           FETCH FIRST :2 ROWS ONLY""",
                        [query, limit],
                    )
                    results = cur.fetchall()
                except Exception:
                    results = []
                if not results:
                    cur.execute(
                        """SELECT m.session_id, m.role, m.content, m.timestamp_val,
                                  1 as relevance
                           FROM messages m
                           WHERE m.content LIKE '%' || :1 || '%'
                           ORDER BY m.timestamp_val DESC
                           FETCH FIRST :2 ROWS ONLY""",
                        [query, limit],
                    )
                    results = cur.fetchall()
                return [
                    {
                        "session_id": r[0], "role": r[1],
                        "content": r[2], "timestamp": r[3],
                        "relevance": r[4],
                    }
                    for r in results
                ]

    def list_sessions(self, source: str = None, limit: int = 50) -> list:
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
