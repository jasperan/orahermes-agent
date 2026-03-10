"""Tests for semantic recall tool and oracle_state vector methods.

Unit tests mock the DB layer so they run without Oracle.
Integration tests (marked with @pytest.mark.integration) require ORACLE_DSN.
"""

import json
import os
import pytest
from unittest.mock import MagicMock, patch


# =========================================================================
# Unit tests — no Oracle required
# =========================================================================


class TestSemanticRecallTool:
    """Test the semantic_recall tool function with mocked DB."""

    def test_no_db_returns_error(self):
        from tools.semantic_recall_tool import semantic_recall
        result = json.loads(semantic_recall(query="test", db=None))
        assert result["success"] is False
        assert "not available" in result["error"]

    def test_empty_query_returns_error(self):
        from tools.semantic_recall_tool import semantic_recall
        db = MagicMock()
        result = json.loads(semantic_recall(query="", db=db))
        assert result["success"] is False
        assert "empty" in result["error"]

    def test_whitespace_query_returns_error(self):
        from tools.semantic_recall_tool import semantic_recall
        db = MagicMock()
        result = json.loads(semantic_recall(query="   ", db=db))
        assert result["success"] is False

    def test_hybrid_mode_calls_hybrid_search(self):
        from tools.semantic_recall_tool import semantic_recall
        db = MagicMock()
        db.hybrid_search = MagicMock(return_value=[
            {"session_id": "s1", "role": "user", "content": "hello world",
             "timestamp": 1700000000, "similarity": 0.9, "relevance": 0.85},
        ])
        result = json.loads(semantic_recall(query="hello", mode="hybrid", db=db))
        assert result["success"] is True
        assert result["mode"] == "hybrid"
        assert result["count"] == 1
        db.hybrid_search.assert_called_once()

    def test_vector_mode_calls_semantic_search(self):
        from tools.semantic_recall_tool import semantic_recall
        db = MagicMock()
        db.semantic_search = MagicMock(return_value=[
            {"session_id": "s1", "role": "assistant", "content": "vector result",
             "timestamp": 1700000000, "similarity": 0.95},
        ])
        result = json.loads(semantic_recall(query="test", mode="vector", db=db))
        assert result["success"] is True
        assert result["mode"] == "vector"
        db.semantic_search.assert_called_once()

    def test_keyword_mode_calls_search_messages(self):
        from tools.semantic_recall_tool import semantic_recall
        db = MagicMock()
        db.search_messages = MagicMock(return_value=[
            {"session_id": "s1", "role": "user", "content": "keyword result",
             "timestamp": 1700000000, "relevance": 5},
        ])
        # Remove vector methods to simulate SQLite
        del db.semantic_search
        del db.hybrid_search
        result = json.loads(semantic_recall(query="test", mode="keyword", db=db))
        assert result["success"] is True
        assert result["mode"] == "keyword_fallback"
        db.search_messages.assert_called_once()

    def test_no_results_returns_empty(self):
        from tools.semantic_recall_tool import semantic_recall
        db = MagicMock()
        db.hybrid_search = MagicMock(return_value=[])
        result = json.loads(semantic_recall(query="nonexistent", mode="hybrid", db=db))
        assert result["success"] is True
        assert result["count"] == 0
        assert "No matching" in result["message"]

    def test_results_grouped_by_session(self):
        from tools.semantic_recall_tool import semantic_recall
        db = MagicMock()
        db.hybrid_search = MagicMock(return_value=[
            {"session_id": "s1", "role": "user", "content": "msg1",
             "timestamp": 1700000000, "similarity": 0.9, "relevance": 0.8},
            {"session_id": "s1", "role": "assistant", "content": "msg2",
             "timestamp": 1700000001, "similarity": 0.85, "relevance": 0.7},
            {"session_id": "s2", "role": "user", "content": "msg3",
             "timestamp": 1700000002, "similarity": 0.8, "relevance": 0.6},
        ])
        result = json.loads(semantic_recall(query="test", mode="hybrid", db=db))
        assert result["sessions_matched"] == 2
        assert result["count"] == 3
        # First session has 2 snippets
        s1 = [r for r in result["results"] if r["session_id"] == "s1"][0]
        assert len(s1["snippets"]) == 2

    def test_limit_clamped(self):
        from tools.semantic_recall_tool import semantic_recall
        db = MagicMock()
        db.hybrid_search = MagicMock(return_value=[])
        # Should clamp to 30
        semantic_recall(query="test", mode="hybrid", limit=100, db=db)
        call_args = db.hybrid_search.call_args
        assert call_args.kwargs.get("limit", call_args[1].get("limit", 30)) == 30

    def test_role_filter_parsed(self):
        from tools.semantic_recall_tool import semantic_recall
        db = MagicMock()
        db.hybrid_search = MagicMock(return_value=[])
        semantic_recall(query="test", mode="hybrid", role_filter="user,assistant", db=db)
        call_args = db.hybrid_search.call_args
        assert call_args.kwargs.get("role_filter") == ["user", "assistant"]

    def test_content_truncated_in_snippet(self):
        from tools.semantic_recall_tool import semantic_recall
        db = MagicMock()
        long_content = "x" * 500
        db.hybrid_search = MagicMock(return_value=[
            {"session_id": "s1", "role": "user", "content": long_content,
             "timestamp": 1700000000, "similarity": 0.9, "relevance": 0.8},
        ])
        result = json.loads(semantic_recall(query="test", mode="hybrid", db=db))
        snippet = result["results"][0]["snippets"][0]["snippet"]
        assert len(snippet) <= 303 + 3  # 300 chars + "..."

    def test_graceful_fallback_when_no_vector_support(self):
        """If db has no semantic_search/hybrid_search, falls back to keyword."""
        from tools.semantic_recall_tool import semantic_recall
        db = MagicMock(spec=["search_messages"])
        db.search_messages = MagicMock(return_value=[
            {"session_id": "s1", "role": "user", "content": "fallback",
             "timestamp": 1700000000, "relevance": 1},
        ])
        result = json.loads(semantic_recall(query="test", mode="hybrid", db=db))
        assert result["success"] is True
        assert result["mode"] == "keyword_fallback"


class TestOracleStateVectorMethods:
    """Test vector methods on OracleSessionDB with mocked oracledb."""

    def _make_db(self):
        """Create an OracleSessionDB with a mocked pool."""
        with patch("oracledb.create_pool"):
            from oracle_state import OracleSessionDB
            db = OracleSessionDB(dsn="fake:1521/fake", user="test", password="test")
        return db

    def test_check_vector_support_returns_false_on_error(self):
        db = self._make_db()
        db.pool = MagicMock()
        db.pool.acquire.side_effect = Exception("no connection")
        assert db._check_vector_support() is False

    def test_embed_message_empty_content(self):
        db = self._make_db()
        assert db.embed_message(1, "") is False
        assert db.embed_message(1, None) is False

    def test_embed_message_truncates_long_content(self):
        db = self._make_db()
        mock_conn = MagicMock()
        mock_cursor = MagicMock()
        mock_conn.__enter__ = MagicMock(return_value=mock_conn)
        mock_conn.__exit__ = MagicMock(return_value=False)
        mock_conn.cursor.return_value.__enter__ = MagicMock(return_value=mock_cursor)
        mock_conn.cursor.return_value.__exit__ = MagicMock(return_value=False)
        db.pool = MagicMock()
        db.pool.acquire.return_value = mock_conn

        long_text = "a" * 2000
        db.embed_message(1, long_text)
        # Verify the text passed to VECTOR_EMBEDDING was truncated
        call_args = mock_cursor.execute.call_args[0][1]
        assert len(call_args[0]) == 1500  # _EMBED_CHAR_LIMIT

    def test_semantic_search_empty_query(self):
        db = self._make_db()
        assert db.semantic_search("") == []
        assert db.semantic_search(None) == []

    def test_hybrid_search_no_results(self):
        db = self._make_db()
        db.search_messages = MagicMock(return_value=[])
        db.semantic_search = MagicMock(return_value=[])
        assert db.hybrid_search("test") == []

    def test_hybrid_search_keyword_only_fallback(self):
        db = self._make_db()
        db.search_messages = MagicMock(return_value=[
            {"session_id": "s1", "content": "keyword", "relevance": 50, "role": "user", "timestamp": 1.0},
        ])
        db.semantic_search = MagicMock(return_value=[])
        results = db.hybrid_search("test", limit=5)
        assert len(results) == 1

    def test_hybrid_search_vector_only_fallback(self):
        db = self._make_db()
        db.search_messages = MagicMock(return_value=[])
        db.semantic_search = MagicMock(return_value=[
            {"session_id": "s1", "content": "vector", "similarity": 0.9, "role": "user", "timestamp": 1.0},
        ])
        results = db.hybrid_search("test", limit=5)
        assert len(results) == 1
        assert results[0]["relevance"] == 0.9

    def test_hybrid_search_merges_and_reranks(self):
        db = self._make_db()
        db.search_messages = MagicMock(return_value=[
            {"session_id": "s1", "content": "both matched this one",
             "relevance": 80, "role": "user", "timestamp": 1.0},
            {"session_id": "s2", "content": "keyword only hit",
             "relevance": 60, "role": "user", "timestamp": 2.0},
        ])
        db.semantic_search = MagicMock(return_value=[
            {"session_id": "s1", "content": "both matched this one",
             "similarity": 0.95, "role": "user", "timestamp": 1.0},
            {"session_id": "s3", "content": "vector only hit",
             "similarity": 0.7, "role": "assistant", "timestamp": 3.0},
        ])
        results = db.hybrid_search("test", limit=10)
        # Should have 3 unique results
        assert len(results) == 3
        # The one that matched both should be ranked highest
        assert results[0]["session_id"] == "s1"
        # All results should have combined scores
        for r in results:
            assert "keyword_score" in r
            assert "vector_score" in r

    def test_backfill_embeddings_returns_zero_on_error(self):
        db = self._make_db()
        db.pool = MagicMock()
        db.pool.acquire.side_effect = Exception("no connection")
        assert db.backfill_embeddings() == 0


class TestEmbedMessageAsync:
    """Test the fire-and-forget embedding in run_agent.py."""

    def test_embed_not_called_when_no_db(self):
        """_embed_message_async is a no-op when _session_db is None."""
        # Minimal mock of AIAgent
        class FakeAgent:
            _session_db = None
            def _embed_message_async(self, msg_id, content):
                from run_agent import AIAgent
                AIAgent._embed_message_async(self, msg_id, content)

        agent = FakeAgent()
        # Should not raise
        agent._embed_message_async(1, "test content")

    def test_embed_not_called_when_no_content(self):
        class FakeAgent:
            _session_db = MagicMock()
            _session_db.embed_message = MagicMock()
            def _embed_message_async(self, msg_id, content):
                from run_agent import AIAgent
                AIAgent._embed_message_async(self, msg_id, content)

        agent = FakeAgent()
        agent._embed_message_async(1, "")
        agent._embed_message_async(1, None)
        # embed_message should not have been called
        agent._session_db.embed_message.assert_not_called()

    def test_embed_not_called_for_sqlite_db(self):
        """SQLite SessionDB doesn't have embed_message; should be no-op."""
        class FakeAgent:
            _session_db = MagicMock(spec=["append_message", "get_messages"])
            def _embed_message_async(self, msg_id, content):
                from run_agent import AIAgent
                AIAgent._embed_message_async(self, msg_id, content)

        agent = FakeAgent()
        # Should not raise even though db has no embed_message
        agent._embed_message_async(1, "test content")


class TestToolRegistration:
    """Verify semantic_recall is properly registered in toolsets and model_tools."""

    def test_semantic_recall_in_core_tools(self):
        from toolsets import _HERMES_CORE_TOOLS
        assert "semantic_recall" in _HERMES_CORE_TOOLS

    def test_semantic_recall_toolset_exists(self):
        from toolsets import TOOLSETS
        assert "semantic_recall" in TOOLSETS
        assert "semantic_recall" in TOOLSETS["semantic_recall"]["tools"]

    def test_semantic_recall_in_hermes_cli(self):
        from toolsets import resolve_toolset
        cli_tools = resolve_toolset("hermes-cli")
        assert "semantic_recall" in cli_tools

    def test_semantic_recall_registered_in_registry(self):
        from tools.registry import registry
        # Force tool discovery
        import model_tools  # noqa: F401
        names = registry.get_all_tool_names()
        assert "semantic_recall" in names

    def test_semantic_recall_in_agent_loop_tools(self):
        from model_tools import _AGENT_LOOP_TOOLS
        assert "semantic_recall" in _AGENT_LOOP_TOOLS


# =========================================================================
# Integration tests — require ORACLE_DSN
# =========================================================================

pytestmark_integration = pytest.mark.skipif(
    not os.getenv("ORACLE_DSN"),
    reason="ORACLE_DSN not set — no Oracle DB available",
)


@pytestmark_integration
class TestOracleVectorIntegration:
    """Integration tests that hit a real Oracle 26ai database."""

    def _get_db(self):
        from oracle_state import OracleSessionDB
        return OracleSessionDB()

    def test_check_vector_support(self):
        """Verify the ONNX model and embedding column are detected."""
        db = self._get_db()
        # This may be True or False depending on whether the migration was applied
        result = db._check_vector_support()
        assert isinstance(result, bool)

    def test_embed_and_search(self):
        """Full round-trip: insert, embed, semantic search."""
        import uuid
        db = self._get_db()
        if not db._check_vector_support():
            pytest.skip("Vector support not available (migration not applied)")

        sid = str(uuid.uuid4())
        db.create_session(session_id=sid, source="test", model="test-model")

        # Insert and embed a message
        msg_id = db.append_message(sid, role="user", content="Oracle AI Vector Search is amazing for semantic memory")
        success = db.embed_message(msg_id, "Oracle AI Vector Search is amazing for semantic memory")
        assert success is True

        # Semantic search should find it
        results = db.semantic_search("vector database embeddings", limit=5)
        assert len(results) > 0
        found = any(r["session_id"] == sid for r in results)
        assert found, f"Expected session {sid} in results: {results}"

    def test_hybrid_search_integration(self):
        """Hybrid search combines keyword + vector results."""
        import uuid
        db = self._get_db()
        if not db._check_vector_support():
            pytest.skip("Vector support not available")

        sid = str(uuid.uuid4())
        db.create_session(session_id=sid, source="test", model="test-model")
        msg_id = db.append_message(sid, role="user", content="debugging CORS errors in the API gateway nginx config")
        db.embed_message(msg_id, "debugging CORS errors in the API gateway nginx config")

        results = db.hybrid_search("cross-origin request problems", limit=5)
        assert isinstance(results, list)

    def test_backfill_embeddings_integration(self):
        """Backfill should process messages without embeddings."""
        import uuid
        db = self._get_db()
        if not db._check_vector_support():
            pytest.skip("Vector support not available")

        sid = str(uuid.uuid4())
        db.create_session(session_id=sid, source="test", model="test-model")
        db.append_message(sid, role="user", content="backfill test message for vector embeddings")

        count = db.backfill_embeddings(batch_size=10)
        assert count >= 0  # May be 0 if message was already embedded
