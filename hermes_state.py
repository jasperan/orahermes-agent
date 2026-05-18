#!/usr/bin/env python3
"""Oracle-only session state facade for OraHermes.

The upstream project exposes a ``hermes_state.SessionDB`` class.  OraHermes
keeps that public import path for compatibility, but the implementation is
strictly Oracle-backed. There is no local database fallback.
"""

from __future__ import annotations

from pathlib import Path
from typing import Optional

from oracle_state import OracleSessionDB


DEFAULT_DB_PATH = None
_last_init_error: Optional[str] = None


def _set_last_init_error(msg: Optional[str]) -> None:
    global _last_init_error
    _last_init_error = msg


def get_last_init_error() -> Optional[str]:
    return _last_init_error


def format_session_db_unavailable(prefix: str = "Oracle session database not available") -> str:
    cause = get_last_init_error()
    if cause:
        return f"{prefix}: {cause}."
    return (
        f"{prefix}. Set ORACLE_DSN, ORACLE_USER, and ORACLE_PASSWORD, "
        "then apply oracle_setup.sql."
    )


def apply_wal_with_fallback(*_args, **_kwargs) -> str:
    raise RuntimeError("Local database storage is disabled in OraHermes; configure Oracle-backed storage.")


class SessionDB(OracleSessionDB):
    """Compatibility name for the Oracle-backed session database."""

    def __init__(self, db_path: Optional[Path] = None, **kwargs):
        if db_path is not None:
            raise RuntimeError("Local db_path is not supported; OraHermes uses Oracle only.")
        try:
            super().__init__(**kwargs)
            _set_last_init_error(None)
        except Exception as exc:
            _set_last_init_error(f"{type(exc).__name__}: {exc}")
            raise


def get_session_db(**kwargs) -> SessionDB:
    return SessionDB(**kwargs)
