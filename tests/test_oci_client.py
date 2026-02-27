# tests/test_oci_client.py
import os
import pytest


def test_create_oci_client_returns_openai_compatible():
    """OCI client must have chat.completions interface."""
    from oci_client import create_oci_client

    client = create_oci_client(
        profile_name="foosball",
        compartment_id=os.getenv("OCI_COMPARTMENT_ID", "test-compartment"),
        region="us-chicago-1",
    )
    assert hasattr(client, "chat")
    assert hasattr(client.chat, "completions")


def test_create_oci_async_client():
    """Async variant must also have chat.completions."""
    from oci_client import create_oci_async_client

    client = create_oci_async_client(
        profile_name="foosball",
        compartment_id=os.getenv("OCI_COMPARTMENT_ID", "test-compartment"),
        region="us-chicago-1",
    )
    assert hasattr(client, "chat")
    assert hasattr(client.chat, "completions")


def test_get_oci_base_url():
    """Base URL must follow OCI GenAI format."""
    from oci_client import get_oci_base_url

    url = get_oci_base_url("us-chicago-1")
    assert "inference.generativeai.us-chicago-1" in url
    assert "/20231130/actions/v1" in url
