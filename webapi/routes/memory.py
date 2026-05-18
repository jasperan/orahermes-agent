from fastapi import APIRouter, HTTPException

from webapi.models.memory import (
    MemoryDeleteRequest,
    MemoryMutationResponse,
    MemoryPatchRequest,
    MemoryPostRequest,
    MemoryReadResponse,
)


router = APIRouter(prefix="/api/memory", tags=["memory"])

_DISABLED_DETAIL = (
    "Memory endpoints are disabled in OraHermes. Runtime persistence must use "
    "Oracle Database only."
)


def _memory_disabled() -> None:
    raise HTTPException(status_code=410, detail=_DISABLED_DETAIL)


@router.get("", response_model=MemoryReadResponse)
async def get_memory() -> MemoryReadResponse:
    _memory_disabled()


@router.post("", response_model=MemoryMutationResponse)
async def add_memory(_payload: MemoryPostRequest) -> MemoryMutationResponse:
    _memory_disabled()


@router.patch("", response_model=MemoryMutationResponse)
async def patch_memory(_payload: MemoryPatchRequest) -> MemoryMutationResponse:
    _memory_disabled()


@router.delete("", response_model=MemoryMutationResponse)
async def delete_memory(_payload: MemoryDeleteRequest) -> MemoryMutationResponse:
    _memory_disabled()
