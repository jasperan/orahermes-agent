"""Oracle 26ai Free session state storage — replaces SQLite hermes_state.py."""

import logging
import os
import time
import json
from typing import Any, Dict, List, Optional

import oracledb

logger = logging.getLogger(__name__)

oracledb.defaults.fetch_lobs = False


class OracleSessionDB:
    """Drop-in replacement for SessionDB using Oracle Database.

    Method signatures match hermes_state.SessionDB exactly so callers
    (run_agent.py, cli.py, gateway/) can use either backend transparently.
    """

    def __init__(
        self,
        dsn: Optional[str] = None,
        user: Optional[str] = None,
        password: Optional[str] = None,
        **kwargs: Any,
    ) -> None:
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
        model: Optional[str] = None,
        model_config: Optional[Dict[str, Any]] = None,
        system_prompt: Optional[str] = None,
        user_id: Optional[str] = None,
        parent_session_id: Optional[str] = None,
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

    def end_session(self, session_id: str, end_reason: str = "normal") -> None:
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
        content: Optional[str] = None,
        tool_name: Optional[str] = None,
        tool_calls: Any = None,
        tool_call_id: Optional[str] = None,
        token_count: Optional[int] = None,
        finish_reason: Optional[str] = None,
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

    def get_messages(self, session_id: str) -> List[Dict[str, Any]]:
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

    def search_messages(
        self,
        query: str,
        source_filter: Optional[List[str]] = None,
        role_filter: Optional[List[str]] = None,
        limit: int = 20,
        offset: int = 0,
    ) -> List[Dict[str, Any]]:
        """Full-text search using Oracle Text CONTAINS with LIKE fallback.

        Accepts the same kwargs as SQLite SessionDB.search_messages so tools
        like session_search can call either backend transparently.
        """
        if not query or not query.strip():
            return []

        with self._get_conn() as conn:
            with conn.cursor() as cur:
                # Sync Oracle Text index before search
                try:
                    cur.execute(
                        "BEGIN CTX_DDL.SYNC_INDEX('idx_messages_content_ft'); END;"
                    )
                except Exception:
                    pass

                # Build dynamic WHERE filters
                extra_where = ""
                params_extra = []
                param_idx = 3

                if source_filter:
                    placeholders = ",".join(f":{param_idx + i}" for i in range(len(source_filter)))
                    extra_where += f" AND m.session_id IN (SELECT id FROM sessions WHERE source IN ({placeholders}))"
                    params_extra.extend(source_filter)
                    param_idx += len(source_filter)

                if role_filter:
                    placeholders = ",".join(f":{param_idx + i}" for i in range(len(role_filter)))
                    extra_where += f" AND m.role IN ({placeholders})"
                    params_extra.extend(role_filter)
                    param_idx += len(role_filter)

                # Try Oracle Text CONTAINS first
                try:
                    cur.execute(
                        f"""SELECT m.session_id, m.role, m.content, m.timestamp_val,
                                  SCORE(1) as relevance
                           FROM messages m
                           WHERE CONTAINS(m.content, :1, 1) > 0{extra_where}
                           ORDER BY relevance DESC
                           OFFSET :o ROWS FETCH NEXT :l ROWS ONLY""",
                        dict(
                            {str(i+3): v for i, v in enumerate(params_extra)},
                            **{"1": query, "o": offset, "l": limit}
                        ),
                    )
                    results = cur.fetchall()
                except Exception:
                    results = []

                # Fallback to LIKE
                if not results:
                    cur.execute(
                        f"""SELECT m.session_id, m.role, m.content, m.timestamp_val,
                                  1 as relevance
                           FROM messages m
                           WHERE m.content LIKE '%' || :1 || '%'{extra_where}
                           ORDER BY m.timestamp_val DESC
                           OFFSET :o ROWS FETCH NEXT :l ROWS ONLY""",
                        dict(
                            {str(i+3): v for i, v in enumerate(params_extra)},
                            **{"1": query, "o": offset, "l": limit}
                        ),
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

    def list_sessions(self, source: Optional[str] = None, limit: int = 50) -> List[Dict[str, Any]]:
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

    # =========================================================================
    # Oracle AI Vector Search — semantic memory
    # =========================================================================

    EMBEDDING_MODEL = "ALL_MINILM_L6_V2"
    # MiniLM-L6-v2 has a 512-token cap; ~1500 chars leaves headroom for UTF-8 tokens.
    _EMBED_CHAR_LIMIT = 1500

    def _check_vector_support(self) -> bool:
        """Check if the embedding column and ONNX model exist."""
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
                        """SELECT COUNT(*) FROM all_mining_models
                           WHERE model_name = :1""",
                        [self.EMBEDDING_MODEL],
                    )
                    return cur.fetchone()[0] > 0
        except Exception as e:
            logger.debug("Vector support check failed: %s", e)
            return False

    def embed_message(self, message_id: int, content: str) -> bool:
        """Generate and store a vector embedding for a message using in-DB ONNX model.

        Called after append_message to asynchronously back-fill the embedding.
        Safe to call even if the embedding column doesn't exist (returns False).
        """
        if not content or not content.strip():
            return False
        # Truncate to fit ONNX model's token limit
        text = content[:self._EMBED_CHAR_LIMIT]
        try:
            with self._get_conn() as conn:
                with conn.cursor() as cur:
                    cur.execute(
                        f"""UPDATE messages
                           SET embedding = VECTOR_EMBEDDING({self.EMBEDDING_MODEL} USING :1 AS data)
                           WHERE id = :2""",
                        [text, message_id],
                    )
                conn.commit()
            return True
        except Exception as e:
            logger.debug("embed_message failed for msg %s: %s", message_id, e)
            return False

    def semantic_search(
        self,
        query: str,
        role_filter: List[str] = None,
        limit: int = 20,
    ) -> List[dict]:
        """Pure vector similarity search using Oracle AI Vector Search.

        Computes the query embedding in-DB using the same ONNX model and finds
        the nearest neighbors by cosine distance.
        """
        if not query or not query.strip():
            return []
        text = query[:self._EMBED_CHAR_LIMIT]
        try:
            with self._get_conn() as conn:
                with conn.cursor() as cur:
                    role_where = ""
                    params: dict = {"q": text, "lim": limit}
                    if role_filter:
                        placeholders = ",".join(f":r{i}" for i in range(len(role_filter)))
                        role_where = f"AND m.role IN ({placeholders})"
                        for i, r in enumerate(role_filter):
                            params[f"r{i}"] = r
                    cur.execute(
                        f"""SELECT m.id, m.session_id, m.role, m.content, m.timestamp_val,
                                   VECTOR_DISTANCE(m.embedding,
                                       VECTOR_EMBEDDING({self.EMBEDDING_MODEL} USING :q AS data),
                                       COSINE) AS distance
                            FROM messages m
                            WHERE m.embedding IS NOT NULL {role_where}
                            ORDER BY distance ASC
                            FETCH FIRST :lim ROWS ONLY""",
                        params,
                    )
                    rows = cur.fetchall()
                    return [
                        {
                            "id": r[0],
                            "session_id": r[1],
                            "role": r[2],
                            "content": r[3],
                            "timestamp": r[4],
                            "distance": round(r[5], 4) if r[5] is not None else None,
                            "similarity": round(1.0 - r[5], 4) if r[5] is not None else None,
                        }
                        for r in rows
                    ]
        except Exception as e:
            logger.warning("semantic_search failed: %s", e)
            return []

    def hybrid_search(
        self,
        query: str,
        role_filter: List[str] = None,
        limit: int = 20,
        keyword_weight: float = 0.4,
        vector_weight: float = 0.6,
    ) -> List[dict]:
        """Hybrid search combining Oracle Text keyword search with vector similarity.

        Runs both searches, normalizes scores to [0,1], and re-ranks by weighted
        combination. Falls back gracefully: if vector search is unavailable, returns
        keyword results only; if keyword search fails, returns vector results only.
        """
        keyword_results = self.search_messages(
            query=query, role_filter=role_filter, limit=limit * 2,
        )
        vector_results = self.semantic_search(
            query=query, role_filter=role_filter, limit=limit * 2,
        )

        if not keyword_results and not vector_results:
            return []
        if not vector_results:
            return keyword_results[:limit]
        if not keyword_results:
            # Convert vector results to the keyword result format
            for r in vector_results:
                r["relevance"] = r.get("similarity", 0.5)
            return vector_results[:limit]

        # Normalize keyword scores (Oracle Text SCORE is 0-100)
        max_kw = max((r.get("relevance", 0) for r in keyword_results), default=1) or 1
        kw_map: Dict[tuple, float] = {}
        kw_data: Dict[tuple, dict] = {}
        for r in keyword_results:
            key = (r["session_id"], r.get("content", "")[:100])
            kw_map[key] = (r.get("relevance", 0) / max_kw)
            kw_data[key] = r

        # Normalize vector scores (similarity is already 0-1)
        vec_map: Dict[tuple, float] = {}
        vec_data: Dict[tuple, dict] = {}
        for r in vector_results:
            key = (r["session_id"], (r.get("content") or "")[:100])
            vec_map[key] = r.get("similarity", 0)
            vec_data[key] = r

        # Merge and score
        all_keys = set(kw_map.keys()) | set(vec_map.keys())
        scored = []
        for key in all_keys:
            kw_score = kw_map.get(key, 0)
            vec_score = vec_map.get(key, 0)
            combined = (keyword_weight * kw_score) + (vector_weight * vec_score)
            data = kw_data.get(key) or vec_data.get(key)
            data["relevance"] = round(combined, 4)
            data["keyword_score"] = round(kw_score, 4)
            data["vector_score"] = round(vec_score, 4)
            scored.append(data)

        scored.sort(key=lambda x: x["relevance"], reverse=True)
        return scored[:limit]

    def backfill_embeddings(self, batch_size: int = 100) -> int:
        """Backfill embeddings for messages that don't have one yet.

        Useful after enabling vector search on an existing database.
        Returns the number of messages embedded.
        """
        count = 0
        try:
            with self._get_conn() as conn:
                with conn.cursor() as cur:
                    cur.execute(
                        """SELECT id, content FROM messages
                           WHERE embedding IS NULL AND content IS NOT NULL
                           FETCH FIRST :1 ROWS ONLY""",
                        [batch_size],
                    )
                    rows = cur.fetchall()
                    for msg_id, content in rows:
                        text = (content or "")[:self._EMBED_CHAR_LIMIT]
                        if not text.strip():
                            continue
                        try:
                            cur.execute(
                                f"""UPDATE messages
                                   SET embedding = VECTOR_EMBEDDING({self.EMBEDDING_MODEL} USING :1 AS data)
                                   WHERE id = :2""",
                                [text, msg_id],
                            )
                            count += 1
                        except Exception as e:
                            logger.debug("backfill skip msg %s: %s", msg_id, e)
                conn.commit()
        except Exception as e:
            logger.warning("backfill_embeddings failed: %s", e)
        return count

    def close(self) -> None:
        if self.pool:
            self.pool.close()
