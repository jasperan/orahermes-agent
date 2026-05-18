#!/usr/bin/env python3
"""Disabled memory tool for OraHermes.

OraHermes keeps runtime persistence Oracle-only. The upstream file-backed
memory tool is intentionally unavailable until memory storage is implemented
against Oracle Database.
"""

from __future__ import annotations

import json
from typing import Any, Optional


_DISABLED_ERROR = (
    "Memory is disabled in OraHermes. Runtime persistence must use Oracle "
    "Database only."
)


class MemoryStore:
    """Compatibility stub for imports that still type against MemoryStore."""

    memory_entries: list[str]
    user_entries: list[str]

    def __init__(self, *_args, **_kwargs) -> None:
        self.memory_entries = []
        self.user_entries = []

    def load_from_disk(self) -> None:
        """No-op: local memory files are not loaded in OraHermes."""

    def add(self, *_args, **_kwargs) -> dict[str, Any]:
        return {"success": False, "error": _DISABLED_ERROR}

    def replace(self, *_args, **_kwargs) -> dict[str, Any]:
        return {"success": False, "error": _DISABLED_ERROR}

    def remove(self, *_args, **_kwargs) -> dict[str, Any]:
        return {"success": False, "error": _DISABLED_ERROR}

    def format_for_system_prompt(self, *_args, **_kwargs) -> Optional[str]:
        return None

    def _success_response(self, target: str, *_args, **_kwargs) -> dict[str, Any]:
        return {
            "success": True,
            "target": target,
            "entries": [],
            "usage": "disabled",
            "entry_count": 0,
        }


def memory_tool(
    action: str,
    target: str = "memory",
    content: str | None = None,
    old_text: str | None = None,
    store: Optional[MemoryStore] = None,
) -> str:
    """Return an explicit disabled response for all memory tool calls."""
    return json.dumps(
        {
            "success": False,
            "action": action,
            "target": target,
            "error": _DISABLED_ERROR,
        },
        ensure_ascii=False,
    )


def check_memory_requirements() -> bool:
    """Keep the memory tool out of all tool surfaces."""
    return False


MEMORY_SCHEMA = {
    "name": "memory",
    "description": (
        "Disabled in OraHermes. Runtime persistence must use Oracle Database "
        "only; local and external memory providers are unavailable."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "action": {
                "type": "string",
                "enum": ["add", "replace", "remove"],
                "description": "The requested memory action.",
            },
            "target": {
                "type": "string",
                "enum": ["memory", "user"],
                "description": "The requested memory target.",
            },
            "content": {
                "type": "string",
                "description": "Ignored while memory is disabled.",
            },
            "old_text": {
                "type": "string",
                "description": "Ignored while memory is disabled.",
            },
        },
        "required": ["action", "target"],
    },
}


from tools.registry import registry

registry.register(
    name="memory",
    toolset="memory",
    schema=MEMORY_SCHEMA,
    handler=lambda args, **kw: memory_tool(
        action=args.get("action", ""),
        target=args.get("target", "memory"),
        content=args.get("content"),
        old_text=args.get("old_text"),
        store=kw.get("store"),
    ),
    check_fn=check_memory_requirements,
    emoji="🧠",
)
