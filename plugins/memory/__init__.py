"""Disabled memory provider discovery for OraHermes.

OraHermes keeps runtime persistence Oracle-only. Legacy file-backed memory and
external memory providers are not discoverable, loadable, or allowed to register
CLI commands until they are ported to Oracle Database.
"""

from __future__ import annotations

import logging
from pathlib import Path
from typing import List, Optional, Tuple

logger = logging.getLogger(__name__)


def find_provider_dir(name: str) -> Optional[Path]:
    """Return no provider directory in Oracle-only builds."""
    logger.debug("Memory provider '%s' ignored: OraHermes is Oracle-only", name)
    return None


def discover_memory_providers() -> List[Tuple[str, str, bool]]:
    """Return no external memory providers in Oracle-only builds."""
    return []


def load_memory_provider(name: str):
    """Return no provider instance in Oracle-only builds."""
    logger.debug("Memory provider '%s' ignored: OraHermes is Oracle-only", name)
    return None


def discover_plugin_cli_commands() -> List[dict]:
    """Return no provider CLI commands in Oracle-only builds."""
    return []
