"""
Semantic Recall Tool - Vector Similarity Search over Past Conversations

Uses Oracle AI Vector Search to find semantically related messages across all
past sessions.  Three search modes:

  - hybrid (default): Combines Oracle Text keywords + vector cosine similarity,
    weighted 40/60, then re-ranks.  Best overall accuracy.
  - vector: Pure semantic similarity via HNSW index.  Finds conceptually related
    content even with completely different wording.
  - keyword: Traditional Oracle Text CONTAINS search.  Exact term matching.

Falls back to keyword-only session_search when Oracle vector support is
unavailable (e.g. SQLite fallback or vector migration not applied).
"""

import json
import logging
from typing import Any, Dict, List, Optional

logger = logging.getLogger(__name__)


def _has_vector_support(db) -> bool:
    """Check if the session DB supports vector search."""
    return hasattr(db, "semantic_search") and hasattr(db, "hybrid_search")


def semantic_recall(
    query: str,
    mode: str = "hybrid",
    limit: int = 10,
    role_filter: Optional[str] = None,
    db: Optional[Any] = None,
    current_session_id: Optional[str] = None,
) -> str:
    """Search past conversations by meaning using Oracle AI Vector Search.

    Returns matching messages with similarity scores grouped by session.
    """
    if db is None:
        return json.dumps(
            {"success": False, "error": "Session database not available."},
            ensure_ascii=False,
        )

    if not query or not query.strip():
        return json.dumps(
            {"success": False, "error": "Query is required for semantic recall."},
            ensure_ascii=False,
        )

    query = query.strip()
    limit = min(max(limit, 1), 50)
    mode = mode.lower() if mode else "hybrid"
    if mode not in ("hybrid", "vector", "keyword"):
        mode = "hybrid"

    role_list = None
    if role_filter and role_filter.strip():
        role_list = [r.strip() for r in role_filter.split(",") if r.strip()]

    # Fall back to keyword search if vector support isn't available
    if not _has_vector_support(db):
        logger.info("Vector support unavailable, falling back to keyword search")
        try:
            results = db.search_messages(
                query=query,
                role_filter=role_list,
                limit=limit,
                offset=0,
            )
            return json.dumps(
                {
                    "success": True,
                    "query": query,
                    "mode": "keyword (fallback)",
                    "results": results or [],
                    "count": len(results) if results else 0,
                    "note": "Vector search unavailable. Using keyword fallback. "
                    "Apply oracle_setup_vector.sql to enable semantic search.",
                },
                ensure_ascii=False,
            )
        except Exception as e:
            logger.warning("Keyword fallback failed: %s", e)
            return json.dumps(
                {"success": False, "error": f"Search failed: {e}"},
                ensure_ascii=False,
            )

    try:
        if mode == "vector":
            results = db.semantic_search(
                query=query, role_filter=role_list, limit=limit
            )
        elif mode == "keyword":
            results = db.search_messages(
                query=query, role_filter=role_list, limit=limit, offset=0
            )
        else:  # hybrid
            results = db.hybrid_search(
                query=query, role_filter=role_list, limit=limit
            )

        # Filter out current session results
        if current_session_id and results:
            results = [r for r in results if r.get("session_id") != current_session_id]

        # Group by session for readability
        sessions: Dict[str, List[dict]] = {}
        for r in results:
            sid = r.get("session_id", "unknown")
            if sid not in sessions:
                sessions[sid] = []
            sessions[sid].append(r)

        return json.dumps(
            {
                "success": True,
                "query": query,
                "mode": mode,
                "results": results or [],
                "count": len(results) if results else 0,
                "sessions_matched": len(sessions),
            },
            ensure_ascii=False,
            default=str,
        )

    except Exception as e:
        logger.error("Semantic recall failed: %s", e, exc_info=True)
        return json.dumps(
            {"success": False, "error": f"Semantic recall failed: {e}"},
            ensure_ascii=False,
        )


def check_semantic_recall_requirements() -> bool:
    """Semantic recall works with any backend; vector features need Oracle."""
    return True


SEMANTIC_RECALL_SCHEMA = {
    "name": "semantic_recall",
    "description": (
        "Search past conversations by MEANING using Oracle AI Vector Search. "
        "Unlike session_search (keyword-based), this finds conceptually related "
        "content even with completely different wording.\n\n"
        "THREE MODES:\n"
        "- hybrid (default): Keywords + vector similarity, weighted 40/60. Best accuracy.\n"
        "- vector: Pure semantic similarity. Finds related ideas regardless of exact words.\n"
        "- keyword: Traditional text matching via Oracle Text.\n\n"
        "USE THIS when:\n"
        "- You need to recall a past conversation but don't remember exact words\n"
        "- The user describes something conceptually ('that time we fixed the deployment')\n"
        "- session_search returns nothing because the wording doesn't match\n"
        "- You want to find ALL conversations about a topic, not just keyword matches\n\n"
        "Falls back to keyword search if Oracle vector support isn't configured."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "query": {
                "type": "string",
                "description": "Natural language query describing what you want to recall. "
                "Unlike session_search, this can be a full sentence or concept.",
            },
            "mode": {
                "type": "string",
                "enum": ["hybrid", "vector", "keyword"],
                "description": "Search mode: hybrid (default, best accuracy), "
                "vector (pure semantic), keyword (exact terms).",
                "default": "hybrid",
            },
            "limit": {
                "type": "integer",
                "description": "Max results to return (default: 10, max: 50).",
                "default": 10,
            },
            "role_filter": {
                "type": "string",
                "description": "Only search messages from specific roles (comma-separated). "
                "E.g. 'user,assistant' to skip tool outputs.",
            },
        },
        "required": ["query"],
    },
}


# --- Registry ---
from tools.registry import registry

registry.register(
    name="semantic_recall",
    toolset="session_search",
    schema=SEMANTIC_RECALL_SCHEMA,
    handler=lambda args, **kw: semantic_recall(
        query=args.get("query", ""),
        mode=args.get("mode", "hybrid"),
        limit=args.get("limit", 10),
        role_filter=args.get("role_filter"),
        db=kw.get("db"),
        current_session_id=kw.get("current_session_id"),
    ),
    check_fn=check_semantic_recall_requirements,
    emoji="🧠",
)
