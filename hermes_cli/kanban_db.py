"""OraHermes Kanban compatibility surface.

Upstream Hermes ships a local file-backed Kanban board. OraHermes is
Oracle-only, so that storage path is disabled until the board is ported to
Oracle Database. This module keeps the public import surface stable for CLI,
gateway, dashboard, and tool modules while every operation fails explicitly.
"""

from __future__ import annotations

import os
import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Optional


DEFAULT_BOARD = "default"
DEFAULT_CLAIM_TTL_SECONDS = 15 * 60
DEFAULT_FAILURE_LIMIT = 2
DEFAULT_SPAWN_FAILURE_LIMIT = 2
VALID_STATUSES = {"triage", "todo", "ready", "running", "blocked", "done", "archived"}
VALID_WORKSPACE_KINDS = {"scratch", "worktree", "dir"}

_BOARD_SLUG_RE = re.compile(r"^[a-z0-9][a-z0-9\-_]{0,63}$")
_DISABLED_REASON = (
    "Kanban is disabled in OraHermes because the upstream board uses a local "
    "database. Configure/use Oracle-backed session history only, or port the "
    "Kanban board schema to Oracle before enabling this feature."
)


class KanbanDisabledError(RuntimeError):
    """Raised when a caller tries to use the disabled Kanban board."""


class HallucinatedCardsError(KanbanDisabledError):
    """Compatibility exception kept for worker-tool imports."""


def disabled_reason() -> str:
    return _DISABLED_REASON


def _disabled(*_args, **_kwargs):
    raise KanbanDisabledError(_DISABLED_REASON)


def _normalize_board_slug(slug: Optional[str]) -> Optional[str]:
    if slug is None:
        return None
    s = str(slug).strip().lower()
    if not s:
        return None
    if not _BOARD_SLUG_RE.match(s):
        raise ValueError(
            f"invalid board slug {slug!r}: must be 1-64 chars, lowercase "
            "alphanumerics / hyphens / underscores, not starting with '-' or '_'"
        )
    return s


def kanban_home() -> Path:
    override = os.environ.get("HERMES_KANBAN_HOME", "").strip()
    if override:
        return Path(override).expanduser()
    from hermes_constants import get_default_hermes_root

    return get_default_hermes_root()


def boards_root() -> Path:
    return kanban_home() / "kanban" / "boards"


def current_board_path() -> Path:
    return kanban_home() / "kanban" / "current"


def board_dir(board: Optional[str] = None) -> Path:
    slug = _normalize_board_slug(board) or DEFAULT_BOARD
    return boards_root() / slug


def kanban_db_path(board: Optional[str] = None) -> Path:
    slug = _normalize_board_slug(board) or DEFAULT_BOARD
    if slug == DEFAULT_BOARD:
        return kanban_home() / "kanban-disabled"
    return board_dir(slug) / "kanban-disabled"


def workspaces_root(board: Optional[str] = None) -> Path:
    slug = _normalize_board_slug(board) or DEFAULT_BOARD
    if slug == DEFAULT_BOARD:
        return kanban_home() / "kanban" / "workspaces"
    return board_dir(slug) / "workspaces"


def logs_root(board: Optional[str] = None) -> Path:
    slug = _normalize_board_slug(board) or DEFAULT_BOARD
    if slug == DEFAULT_BOARD:
        return kanban_home() / "kanban" / "logs"
    return board_dir(slug) / "logs"


def get_current_board() -> str:
    env = os.environ.get("HERMES_KANBAN_BOARD", "").strip()
    if env:
        try:
            return _normalize_board_slug(env) or DEFAULT_BOARD
        except ValueError:
            return DEFAULT_BOARD
    return DEFAULT_BOARD


def board_exists(board: Optional[str] = None) -> bool:
    return (_normalize_board_slug(board) or DEFAULT_BOARD) == DEFAULT_BOARD


def list_boards(include_archived: bool = False) -> list[dict[str, Any]]:
    return [
        {
            "slug": DEFAULT_BOARD,
            "name": "Default",
            "description": _DISABLED_REASON,
            "status": "disabled",
            "db_path": str(kanban_db_path(DEFAULT_BOARD)),
            "archived": False,
            "task_counts": {},
        }
    ]


def read_board_metadata(board: Optional[str] = None) -> dict[str, Any]:
    slug = _normalize_board_slug(board) or DEFAULT_BOARD
    return {
        "slug": slug,
        "name": "Default" if slug == DEFAULT_BOARD else slug,
        "description": _DISABLED_REASON,
        "status": "disabled",
        "db_path": str(kanban_db_path(slug)),
    }


@dataclass
class Task:
    id: str
    title: str
    body: str = ""
    assignee: Optional[str] = None
    status: str = "blocked"
    priority: int = 0
    tenant: Optional[str] = None
    workspace_kind: str = "scratch"
    workspace_path: Optional[str] = None
    created_by: Optional[str] = None
    created_at: Optional[int] = None
    started_at: Optional[int] = None
    completed_at: Optional[int] = None
    result: Optional[str] = None
    skills: list[str] = field(default_factory=list)
    max_retries: Optional[int] = None


@dataclass
class Run:
    id: int = 0
    task_id: str = ""
    status: str = "blocked"
    summary: Optional[str] = None


@dataclass
class Comment:
    id: int = 0
    task_id: str = ""
    author: str = ""
    body: str = ""
    created_at: Optional[int] = None


@dataclass
class Event:
    id: int = 0
    task_id: Optional[str] = None
    event_type: str = "disabled"
    payload: dict[str, Any] = field(default_factory=dict)
    created_at: Optional[int] = None


def connect(*_args, **_kwargs):
    _disabled()


def init_db(*_args, **_kwargs):
    _disabled()


def run_daemon(*_args, **_kwargs):
    _disabled()


def dispatch_once(*_args, **_kwargs):
    return 0


def has_spawnable_ready(*_args, **_kwargs):
    return False


def board_stats(*_args, **_kwargs) -> dict[str, Any]:
    return {"enabled": False, "reason": _DISABLED_REASON}


def known_assignees(*_args, **_kwargs) -> list[dict[str, Any]]:
    return []


def list_profiles_on_disk(*_args, **_kwargs) -> list[str]:
    return []


def list_tasks(*_args, **_kwargs) -> list[Task]:
    return []


def list_comments(*_args, **_kwargs) -> list[Comment]:
    return []


def list_events(*_args, **_kwargs) -> list[Event]:
    return []


def list_runs(*_args, **_kwargs) -> list[Run]:
    return []


def list_notify_subs(*_args, **_kwargs) -> list[dict[str, Any]]:
    return []


def parent_ids(*_args, **_kwargs) -> list[str]:
    return []


def child_ids(*_args, **_kwargs) -> list[str]:
    return []


def latest_summary(*_args, **_kwargs) -> Optional[str]:
    return None


def latest_run(*_args, **_kwargs) -> Optional[Run]:
    return None


def active_run(*_args, **_kwargs) -> Optional[Run]:
    return None


def get_run(*_args, **_kwargs) -> Optional[Run]:
    return None


def get_task(*_args, **_kwargs) -> Optional[Task]:
    return None


def read_worker_log(*_args, **_kwargs) -> str:
    return ""


def build_worker_context(*_args, **_kwargs) -> str:
    raise KanbanDisabledError(_DISABLED_REASON)


def resolve_workspace(*_args, **_kwargs) -> Path:
    raise KanbanDisabledError(_DISABLED_REASON)


def __getattr__(name: str):
    if name.startswith("__"):
        raise AttributeError(name)
    return _disabled
