"""Shared OCI GenAI async client for Hermes tools.

Provides a single lazy-initialized AsyncOciOpenAI client that all tool modules
can share, eliminating duplicated client-creation patterns across web_tools,
vision_tools, mixture_of_agents_tool, and session_search_tool.
"""

from oci_client import create_oci_async_client

_client = None


def get_async_client():
    """Return a shared async OCI GenAI client.

    The client is created lazily on first call and reused thereafter.
    """
    global _client
    if _client is None:
        _client = create_oci_async_client()
    return _client


def check_api_key() -> bool:
    """Check whether OCI credentials are available.

    OCI GenAI uses user-principal auth from ~/.oci/config, so this always
    returns True (credential validity is checked at request time).
    """
    return True
