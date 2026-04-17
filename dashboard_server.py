#!/usr/bin/env python3
"""Live dashboard API server for orahermes-agent Oracle DB visualizer.

Serves a real-time dashboard and JSON API endpoints that query
Oracle 26ai Free for session/message telemetry.

Usage:
    ORACLE_DSN=localhost:1521/FREEPDB1 ORACLE_USER=hermes ORACLE_PASSWORD=HermesAgent_2025 \
        python dashboard_server.py [--port 8501]
"""

import json
import os
import time
from contextlib import contextmanager
from http.server import HTTPServer, SimpleHTTPRequestHandler
from typing import Any, Dict, Iterator, List, Optional
from urllib.parse import urlparse, parse_qs

import oracledb
import tiktoken

oracledb.defaults.fetch_lobs = False

# Initialize tiktoken encoder once (cl100k_base works well for modern LLMs)
ENCODER = tiktoken.get_encoding("cl100k_base")

DSN = os.getenv("ORACLE_DSN", "localhost:1521/FREEPDB1")
USER = os.getenv("ORACLE_USER", "hermes")
PASSWORD = os.getenv("ORACLE_PASSWORD", "")


def get_conn() -> oracledb.Connection:
    return oracledb.connect(user=USER, password=PASSWORD, dsn=DSN)


@contextmanager
def cursor() -> Iterator[Any]:
    """Yield a cursor on a fresh connection, always closing on exit."""
    conn = get_conn()
    try:
        yield conn.cursor()
    finally:
        conn.close()


def query_overview() -> Dict[str, Any]:
    with cursor() as cur:
        cur.execute("SELECT COUNT(*) FROM sessions")
        total_sessions = cur.fetchone()[0]
        cur.execute("SELECT COUNT(*) FROM messages")
        total_messages = cur.fetchone()[0]
        cur.execute("SELECT SUM(input_tokens), SUM(output_tokens) FROM sessions")
        row = cur.fetchone()
        total_input = row[0] or 0
        total_output = row[1] or 0
        cur.execute("SELECT COUNT(*) FROM messages WHERE tool_calls IS NOT NULL OR tool_name IS NOT NULL")
        tool_msgs = cur.fetchone()[0]
    return {
        "total_sessions": total_sessions,
        "total_messages": total_messages,
        "total_input_tokens": total_input,
        "total_output_tokens": total_output,
        "tool_messages": tool_msgs,
        "timestamp": time.time(),
    }


def query_sessions() -> List[Dict[str, Any]]:
    with cursor() as cur:
        cur.execute("""
            SELECT id, source, model, message_count, tool_call_count,
                   input_tokens, output_tokens, started_at, ended_at, end_reason
            FROM sessions ORDER BY started_at DESC FETCH FIRST 50 ROWS ONLY
        """)
        rows = cur.fetchall()
    return [
        {
            "id": r[0], "source": r[1], "model": r[2],
            "message_count": r[3], "tool_call_count": r[4],
            "input_tokens": r[5], "output_tokens": r[6],
            "started_at": r[7], "ended_at": r[8], "end_reason": r[9],
        }
        for r in rows
    ]


def query_messages(session_id: Optional[str] = None, limit: int = 100) -> List[Dict[str, Any]]:
    with cursor() as cur:
        if session_id:
            cur.execute("""
                SELECT m.id, m.session_id, m.role, m.content, m.tool_name,
                       m.tool_calls, m.timestamp_val, m.token_count, m.finish_reason
                FROM messages m WHERE m.session_id = :1
                ORDER BY m.id FETCH FIRST :2 ROWS ONLY
            """, [session_id, limit])
        else:
            cur.execute("""
                SELECT m.id, m.session_id, m.role, m.content, m.tool_name,
                       m.tool_calls, m.timestamp_val, m.token_count, m.finish_reason
                FROM messages m
                ORDER BY m.id DESC FETCH FIRST :1 ROWS ONLY
            """, [limit])
        rows = cur.fetchall()
    return [
        {
            "id": r[0], "session_id": r[1], "role": r[2],
            "content": (r[3][:300] + "...") if r[3] and len(r[3]) > 300 else r[3],
            "tool_name": r[4],
            "has_tool_calls": r[5] is not None,
            "timestamp": r[6], "token_count": r[7], "finish_reason": r[8],
        }
        for r in rows
    ]


def query_role_distribution() -> List[Dict[str, Any]]:
    with cursor() as cur:
        cur.execute("SELECT role, COUNT(*) FROM messages GROUP BY role ORDER BY COUNT(*) DESC")
        rows = cur.fetchall()
    return [{"role": r[0], "count": r[1]} for r in rows]


def query_timeline() -> List[Dict[str, Any]]:
    """Message count per session in chronological order."""
    with cursor() as cur:
        cur.execute("""
            SELECT s.id, s.started_at, s.message_count, s.tool_call_count, s.model
            FROM sessions s ORDER BY s.started_at ASC
        """)
        rows = cur.fetchall()
    return [
        {
            "session_id": r[0][:16], "started_at": r[1],
            "message_count": r[2], "tool_call_count": r[3], "model": r[4],
        }
        for r in rows
    ]


def query_tool_usage() -> List[Dict[str, Any]]:
    """Extract tool names from tool_calls JSON and count usage."""
    tool_counts: Dict[str, int] = {}
    with cursor() as cur:
        cur.execute("""
            SELECT tool_calls FROM messages WHERE tool_calls IS NOT NULL
        """)
        for (tc_raw,) in cur.fetchall():
            try:
                calls = json.loads(tc_raw) if isinstance(tc_raw, str) else tc_raw
                if isinstance(calls, list):
                    for call in calls:
                        name = call.get("function", {}).get("name") or call.get("name", "unknown")
                        tool_counts[name] = tool_counts.get(name, 0) + 1
            except (json.JSONDecodeError, TypeError):
                pass
    return [{"tool": k, "count": v} for k, v in sorted(tool_counts.items(), key=lambda x: -x[1])]


def query_content_lengths() -> List[Dict[str, Any]]:
    """Message content length distribution by role."""
    with cursor() as cur:
        cur.execute("""
            SELECT role, LENGTH(content) as len FROM messages
            WHERE content IS NOT NULL ORDER BY id
        """)
        rows = cur.fetchall()
    return [{"role": r[0], "length": r[1]} for r in rows]


def query_token_estimates() -> Dict[str, Any]:
    """Estimate token counts from message content using tiktoken.

    Returns per-role totals, per-session breakdown, and grand totals.
    """
    with cursor() as cur:
        cur.execute("""
            SELECT m.session_id, m.role, m.content, m.tool_calls
            FROM messages m ORDER BY m.id
        """)
        rows = cur.fetchall()

    role_totals: Dict[str, int] = {}
    session_totals: Dict[str, Dict[str, int]] = {}
    grand_total = 0

    for session_id, role, content, tool_calls in rows:
        tokens = 0
        if content:
            tokens += len(ENCODER.encode(content))
        if tool_calls:
            tc_str = tool_calls if isinstance(tool_calls, str) else json.dumps(tool_calls)
            tokens += len(ENCODER.encode(tc_str))

        grand_total += tokens
        role_totals[role] = role_totals.get(role, 0) + tokens

        if session_id not in session_totals:
            session_totals[session_id] = {"input": 0, "output": 0, "total": 0}
        if role == "user":
            session_totals[session_id]["input"] += tokens
        elif role == "assistant":
            session_totals[session_id]["output"] += tokens
        session_totals[session_id]["total"] += tokens

    # Per-session list sorted by session order
    session_list = [
        {"session_id": sid[:16], "input": v["input"], "output": v["output"], "total": v["total"]}
        for sid, v in session_totals.items()
    ]

    return {
        "grand_total": grand_total,
        "by_role": [{"role": k, "tokens": v} for k, v in sorted(role_totals.items(), key=lambda x: -x[1])],
        "by_session": session_list,
        "estimated_input": role_totals.get("user", 0),
        "estimated_output": role_totals.get("assistant", 0),
    }


DASHBOARD_HTML = None


def load_dashboard_html() -> None:
    global DASHBOARD_HTML
    html_path = os.path.join(os.path.dirname(__file__), "dashboard.html")
    with open(html_path, "r") as f:
        DASHBOARD_HTML = f.read()


class DashboardHandler(SimpleHTTPRequestHandler):
    def do_GET(self):
        parsed = urlparse(self.path)
        path = parsed.path
        params = parse_qs(parsed.query)

        if path == "/" or path == "/dashboard":
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.end_headers()
            self.wfile.write(DASHBOARD_HTML.encode())
        elif path == "/api/overview":
            self._json_response(query_overview())
        elif path == "/api/sessions":
            self._json_response(query_sessions())
        elif path == "/api/messages":
            sid = params.get("session_id", [None])[0]
            limit = int(params.get("limit", [100])[0])
            self._json_response(query_messages(sid, limit))
        elif path == "/api/roles":
            self._json_response(query_role_distribution())
        elif path == "/api/timeline":
            self._json_response(query_timeline())
        elif path == "/api/tools":
            self._json_response(query_tool_usage())
        elif path == "/api/content_lengths":
            self._json_response(query_content_lengths())
        elif path == "/api/tokens":
            self._json_response(query_token_estimates())
        else:
            self.send_error(404)

    def _json_response(self, data):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()
        self.wfile.write(json.dumps(data).encode())

    def log_message(self, format, *args):
        pass  # Suppress request logging


def main():
    import argparse
    parser = argparse.ArgumentParser(description="orahermes-agent live dashboard")
    parser.add_argument("--port", type=int, default=8501)
    args = parser.parse_args()

    load_dashboard_html()

    server = HTTPServer(("0.0.0.0", args.port), DashboardHandler)
    print(f"orahermes dashboard running at http://localhost:{args.port}")
    print(f"Oracle DB: {USER}@{DSN}")
    print("Press Ctrl+C to stop")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nShutting down...")
        server.shutdown()


if __name__ == "__main__":
    main()
