"""Shared async client for Hermes tools.

Provides a lazy-initialized async client that all tool modules share.
Routes to Ollama or OCI GenAI based on HERMES_PROVIDER env var.
"""

import os

_client = None


def get_async_client():
    """Return a shared async client for tools."""
    global _client
    if _client is not None:
        return _client

    provider = os.environ.get("HERMES_PROVIDER", "ollama")
    if provider == "ollama":
        from openai import AsyncOpenAI
        from hermes_constants import OLLAMA_BASE_URL
        _client = AsyncOpenAI(base_url=OLLAMA_BASE_URL, api_key="ollama")
    else:
        from oci_client import create_oci_async_client
        _client = create_oci_async_client()
    return _client


def check_api_key() -> bool:
    """Check whether credentials are available."""
    return True  # Ollama needs no key; OCI checks at request time
