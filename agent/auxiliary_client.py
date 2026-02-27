"""Shared auxiliary OpenAI client for cheap/fast side tasks.

Provides a single resolution chain so every consumer (context compression,
session search, web extraction, vision analysis, browser vision) picks up
the best available backend without duplicating fallback logic.

Resolution order (text and vision):
  1. OCI GenAI  (via oci_client.create_oci_client)
  2. None
"""

import logging
from typing import Optional, Tuple

from openai import OpenAI

from oci_client import create_oci_client

logger = logging.getLogger(__name__)

# Cheap/fast model for auxiliary tasks on OCI GenAI
_OCI_MODEL = "meta.llama-3.3-70b-instruct"


# ── Public API ──────────────────────────────────────────────────────────────

def get_text_auxiliary_client() -> Tuple[Optional[OpenAI], Optional[str]]:
    """Return (client, model_slug) for text-only auxiliary tasks.

    Uses OCI GenAI; returns (None, None) on failure.
    """
    try:
        client = create_oci_client()
        logger.debug("Auxiliary text client: OCI GenAI (%s)", _OCI_MODEL)
        return client, _OCI_MODEL
    except Exception as exc:
        logger.debug("Auxiliary text client: OCI GenAI unavailable (%s)", exc)
        return None, None


def get_vision_auxiliary_client() -> Tuple[Optional[OpenAI], Optional[str]]:
    """Return (client, model_slug) for vision/multimodal auxiliary tasks.

    Uses OCI GenAI; returns (None, None) on failure.
    """
    try:
        client = create_oci_client()
        logger.debug("Auxiliary vision client: OCI GenAI (%s)", _OCI_MODEL)
        return client, _OCI_MODEL
    except Exception as exc:
        logger.debug("Auxiliary vision client: OCI GenAI unavailable (%s)", exc)
        return None, None


def get_auxiliary_extra_body() -> dict:
    """Return extra_body kwargs for auxiliary API calls.

    OCI GenAI does not require any extra body parameters.
    """
    return {}


def auxiliary_max_tokens_param(value: int) -> dict:
    """Return the correct max tokens kwarg for the auxiliary client's provider.

    OCI GenAI uses the standard 'max_tokens' parameter.
    """
    return {"max_tokens": value}
