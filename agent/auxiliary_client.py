"""Shared auxiliary OpenAI client for cheap/fast side tasks.

Resolution order:
  1. Ollama (if HERMES_PROVIDER == "ollama") — uses main Ollama instance
  2. OCI GenAI (if HERMES_PROVIDER == "oci")
  3. None
"""

import logging
import os
from typing import Optional, Tuple

from openai import OpenAI

logger = logging.getLogger(__name__)

_OCI_MODEL = "meta.llama-3.3-70b-instruct"
_OLLAMA_MODEL = "qwen3.5:4b"


def _get_provider() -> str:
    return os.environ.get("HERMES_PROVIDER", "ollama")


def get_text_auxiliary_client() -> Tuple[Optional[OpenAI], Optional[str]]:
    """Return (client, model_slug) for text-only auxiliary tasks."""
    provider = _get_provider()
    if provider == "ollama":
        try:
            from hermes_constants import OLLAMA_BASE_URL
            client = OpenAI(base_url=OLLAMA_BASE_URL, api_key="ollama")
            logger.debug("Auxiliary text client: Ollama (%s)", _OLLAMA_MODEL)
            return client, _OLLAMA_MODEL
        except Exception as exc:
            logger.debug("Auxiliary text client: Ollama unavailable (%s)", exc)
            return None, None
    else:
        try:
            from oci_client import create_oci_client
            client = create_oci_client()
            logger.debug("Auxiliary text client: OCI GenAI (%s)", _OCI_MODEL)
            return client, _OCI_MODEL
        except Exception as exc:
            logger.debug("Auxiliary text client: OCI GenAI unavailable (%s)", exc)
            return None, None


def get_vision_auxiliary_client() -> Tuple[Optional[OpenAI], Optional[str]]:
    """Return (client, model_slug) for vision/multimodal auxiliary tasks."""
    provider = _get_provider()
    if provider == "ollama":
        try:
            from hermes_constants import OLLAMA_BASE_URL
            client = OpenAI(base_url=OLLAMA_BASE_URL, api_key="ollama")
            logger.debug("Auxiliary vision client: Ollama (%s)", _OLLAMA_MODEL)
            return client, _OLLAMA_MODEL
        except Exception as exc:
            logger.debug("Auxiliary vision client: Ollama unavailable (%s)", exc)
            return None, None
    else:
        try:
            from oci_client import create_oci_client
            client = create_oci_client()
            logger.debug("Auxiliary vision client: OCI GenAI (%s)", _OCI_MODEL)
            return client, _OCI_MODEL
        except Exception as exc:
            logger.debug("Auxiliary vision client: OCI GenAI unavailable (%s)", exc)
            return None, None


def get_auxiliary_extra_body() -> dict:
    """Return extra_body kwargs for auxiliary API calls."""
    return {}


def auxiliary_max_tokens_param(value: int) -> dict:
    """Return the correct max tokens kwarg for the auxiliary client's provider."""
    return {"max_tokens": value}
