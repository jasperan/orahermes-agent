"""Shared constants for Hermes Agent.

Import-safe module with no dependencies — can be imported from anywhere
without risk of circular imports.
"""

# OCI GenAI (OpenAI-compatible endpoint)
OCI_GENAI_BASE_URL = "https://inference.generativeai.us-chicago-1.oci.oraclecloud.com/20231130/actions/v1"

# Legacy aliases for compatibility with code that references OPENROUTER_*
OPENROUTER_BASE_URL = OCI_GENAI_BASE_URL
OPENROUTER_MODELS_URL = None  # Not used — model metadata is local
OPENROUTER_CHAT_URL = f"{OCI_GENAI_BASE_URL}/chat/completions"

# Default model
DEFAULT_MODEL = "xai.grok-3-mini"
