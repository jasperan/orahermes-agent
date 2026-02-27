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
