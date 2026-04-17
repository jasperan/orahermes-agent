import os
import uuid
import pytest

# Skip if no Oracle DB available
pytestmark = pytest.mark.skipif(
    not os.getenv("ORACLE_DSN"),
    reason="ORACLE_DSN not set — no Oracle DB available",
)


def test_create_session():
    from oracle_state import OracleSessionDB

    db = OracleSessionDB()
    sid = str(uuid.uuid4())
    result = db.create_session(session_id=sid, source="cli", model="xai.grok-3-mini")
    assert result == sid
    session = db.get_session(sid)
    assert session is not None
    assert session["source"] == "cli"


def test_add_and_get_messages():
    from oracle_state import OracleSessionDB

    db = OracleSessionDB()
    sid = str(uuid.uuid4())
    db.create_session(session_id=sid, source="cli", model="test-model")
    db.append_message(sid, role="user", content="Hello")
    db.append_message(sid, role="assistant", content="Hi there")
    msgs = db.get_messages(sid)
    assert len(msgs) == 2
    assert msgs[0]["role"] == "user"
    assert msgs[1]["content"] == "Hi there"


def test_search_messages():
    from oracle_state import OracleSessionDB

    db = OracleSessionDB()
    sid = str(uuid.uuid4())
    db.create_session(session_id=sid, source="cli", model="test-model")
    db.append_message(sid, role="user", content="unique_search_term_xyz")
    results = db.search_messages("unique_search_term_xyz")
    assert len(results) > 0


def test_update_system_prompt():
    from oracle_state import OracleSessionDB

    db = OracleSessionDB()
    sid = str(uuid.uuid4())
    db.create_session(session_id=sid, source="cli", model="test-model")
    db.update_system_prompt(sid, "You are a helpful assistant.")
    # Verify via get_session — system_prompt not in the default get_session return,
    # but the method should not error
