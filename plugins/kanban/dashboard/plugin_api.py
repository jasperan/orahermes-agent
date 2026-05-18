"""Disabled Kanban dashboard plugin API for OraHermes.

The upstream Kanban dashboard is backed by local board persistence. OraHermes
permits only Oracle Database-backed persistence, so the routes stay mounted
only to return a clear unavailable response until the board is ported.
"""

from __future__ import annotations

from fastapi import APIRouter, HTTPException, WebSocket

from hermes_cli import kanban_db


router = APIRouter()


def _disabled() -> HTTPException:
    return HTTPException(status_code=503, detail=kanban_db.disabled_reason())


@router.api_route("", methods=["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"])
async def disabled_root():
    raise _disabled()


@router.api_route("/{path:path}", methods=["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"])
async def disabled_route(path: str):
    raise _disabled()


@router.websocket("/{path:path}")
async def disabled_websocket(websocket: WebSocket, path: str):
    await websocket.accept()
    await websocket.close(code=1013, reason=kanban_db.disabled_reason()[:120])
