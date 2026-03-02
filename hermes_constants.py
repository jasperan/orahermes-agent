"""Shared constants for Hermes Agent.

Import-safe module with no dependencies — can be imported from anywhere
without risk of circular imports.
"""

# Ollama (default local provider)
OLLAMA_BASE_URL = "http://localhost:11434/v1"
DEFAULT_MODEL = "qwen3.5:4b"

# OCI GenAI (OpenAI-compatible endpoint)
OCI_GENAI_BASE_URL = "https://inference.generativeai.us-chicago-1.oci.oraclecloud.com/20231130/actions/v1"

# Legacy aliases for compatibility with code that references OPENROUTER_*
OPENROUTER_BASE_URL = OCI_GENAI_BASE_URL
OPENROUTER_MODELS_URL = None  # Not used — model metadata is local
OPENROUTER_CHAT_URL = f"{OCI_GENAI_BASE_URL}/chat/completions"
