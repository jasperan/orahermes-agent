#!/usr/bin/env python3
"""
Semantic Recall Tool — Vector-Based Long-Term Memory Search

Uses Oracle AI Vector Search to find semantically similar past conversations
by meaning rather than exact keywords. Complements the existing session_search
(keyword-based) tool with fuzzy, meaning-aware recall.

Flow:
  1. User query is embedded in-DB using the ALL_MINILM_L6_V2 ONNX model
  2. Cosine-similarity search over the messages.embedding column
  3. Optionally combines with Oracle Text keyword search (hybrid mode)
  4. Groups results by session, returns top matching snippets with metadata

Requires:
  - Oracle 26ai Free with AI Vector Search
  - ONNX embedding model loaded (ALL_MINILM_L6_V2)
  - Schema migration from oracle_setup_vector.sql applied
"""

import json
import logging
from typing import Any, Dict, List, Optional

logger = logging.getLogger(__name__)


def _format_timestamp(ts) -> str:
    """Convert Unix timestamp to human-readable date."""
    if ts is None:
        return "unknown"
    try:
        if isinstance(ts, (int, float)):
            from datetime import datetime
            return datetime.fromtimestamp(ts).strftime("%B %d, %Y at %I:%M %p")
    except Exception:
        pass
    return str(ts)


def semantic_recall(
    query: str,
    mode: str = "hybrid",
    role_filter: str = None,
    limit: int = 10,
    db=None,
) -> str:
    """Search past conversations by meaning using Oracle AI Vector Search.

    Args:
        query: Natural language description of what to recall.
        mode: Search mode — "vector" (pure similarity), "keyword" (Oracle Text),
              or "hybrid" (combined, default).
        role_filter: Comma-separated roles to filter (e.g. "user,assistant").
        limit: Max results to return (default 10, max 30).
        db: OracleSessionDB instance (injected by agent loop).

    Returns:
        JSON string with search results.
    """
    if db is None:
        return json.dumps({
            "success": False,
            "error": "Session database not available.",
        })

    if not query or not query.strip():
        return json.dumps({
            "success": False,
            "error": "Query cannot be empty.",
        })

    query = query.strip()
    limit = min(max(limit, 1), 30)

    # Parse role filter
    role_list = None
    if role_filter and role_filter.strip():
        role_list = [r.strip() for r in role_filter.split(",") if r.strip()]

    try:
        # Check if the db supports vector search
        has_vector = hasattr(db, "semantic_search") and hasattr(db, "hybrid_search")

        if mode == "vector" and has_vector:
            results = db.semantic_search(
                query=query, role_filter=role_list, limit=limit,
            )
        elif mode == "hybrid" and has_vector:
            results = db.hybrid_search(
                query=query, role_filter=role_list, limit=limit,
            )
        else:
            # Fallback to keyword search (works with both SQLite and Oracle)
            results = db.search_messages(
                query=query, role_filter=role_list, limit=limit,
            )

        if not results:
            return json.dumps({
                "success": True,
                "query": query,
                "mode": mode if has_vector else "keyword_fallback",
                "results": [],
                "count": 0,
                "message": "No matching messages found.",
            })

        # Group results by session for cleaner output
        sessions_seen: Dict[str, dict] = {}
        for r in results:
            sid = r.get("session_id", "unknown")
            if sid not in sessions_seen:
                sessions_seen[sid] = {
                    "session_id": sid,
                    "when": _format_timestamp(r.get("timestamp")),
                    "snippets": [],
                }
            content = r.get("content") or ""
            snippet = content[:300] + ("..." if len(content) > 300 else "")
            entry = {
                "role": r.get("role"),
                "snippet": snippet,
            }
            if "similarity" in r and r["similarity"] is not None:
                entry["similarity"] = r["similarity"]
            if "relevance" in r and r["relevance"] is not None:
                entry["relevance"] = r["relevance"]
            sessions_seen[sid]["snippets"].append(entry)

        return json.dumps({
            "success": True,
            "query": query,
            "mode": mode if has_vector else "keyword_fallback",
            "results": list(sessions_seen.values()),
            "count": len(results),
            "sessions_matched": len(sessions_seen),
        }, ensure_ascii=False)

    except Exception as e:
        logger.error("semantic_recall failed: %s", e)
        return json.dumps({
            "success": False,
            "error": f"Search failed: {str(e)}",
        })


def check_semantic_recall_requirements() -> bool:
    """Check if Oracle vector search is available.

    The tool gracefully degrades to keyword search if vector support
    is missing, so we only require that a session DB exists.
    """
    try:
        import os
        # Available if Oracle DB is configured OR SQLite fallback exists
        if os.getenv("ORACLE_DSN"):
            return True
        from hermes_state import DEFAULT_DB_PATH
        return DEFAULT_DB_PATH.parent.exists()
    except ImportError:
        return False


SEMANTIC_RECALL_SCHEMA = {
    "name": "semantic_recall",
    "description": (
        "Search your long-term memory by MEANING using Oracle AI Vector Search. "
        "Unlike session_search (keyword-based), this tool finds past conversations "
        "that are semantically similar to your query — even if they used different words.\n\n"
        "USE THIS when:\n"
        "- You want to recall a conversation by concept, not exact keywords\n"
        "- 'Find conversations about deployment issues' (even if they said 'rollout problems')\n"
        "- 'What did we discuss about database performance?' (matches 'query optimization', 'slow queries', etc.)\n"
        "- session_search returned no results but you suspect relevant conversations exist\n"
        "- You want to find similar problems you've solved before\n\n"
        "MODES:\n"
        "- 'hybrid' (default): combines keyword + vector search for best results\n"
        "- 'vector': pure semantic similarity (ignores exact keyword matches)\n"
        "- 'keyword': falls back to traditional Oracle Text search\n\n"
        "TIP: Use session_search for exact keyword recall, semantic_recall for conceptual recall. "
        "They complement each other."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "query": {
                "type": "string",
                "description": (
                    "Natural language description of what you want to recall. "
                    "Be descriptive — 'how we fixed the CORS issue in the gateway' "
                    "works better than just 'CORS'."
                ),
            },
            "mode": {
                "type": "string",
                "enum": ["hybrid", "vector", "keyword"],
                "description": "Search mode: 'hybrid' (default, best results), 'vector' (pure meaning), 'keyword' (exact terms).",
                "default": "hybrid",
            },
            "role_filter": {
                "type": "string",
                "description": "Optional: only search messages from specific roles (comma-separated). E.g. 'user,assistant'.",
            },
            "limit": {
                "type": "integer",
                "description": "Max results (default: 10, max: 30).",
                "default": 10,
            },
        },
        "required": ["query"],
    },
}


# --- Registry ---
from tools.registry import registry

registry.register(
    name="semantic_recall",
    toolset="semantic_recall",
    schema=SEMANTIC_RECALL_SCHEMA,
    handler=lambda args, **kw: semantic_recall(
        query=args.get("query", ""),
        mode=args.get("mode", "hybrid"),
        role_filter=args.get("role_filter"),
        limit=args.get("limit", 10),
        db=kw.get("db"),
    ),
    check_fn=check_semantic_recall_requirements,
)
