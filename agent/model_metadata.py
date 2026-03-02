"""Model metadata, context lengths, and token estimation utilities.

Pure utility functions with no AIAgent dependency. Used by ContextCompressor
and run_agent.py for pre-flight context checks.

Model metadata is sourced from a local static map of OCI GenAI models
instead of fetching from an external API.
"""

import logging
from typing import Any, Dict, List

logger = logging.getLogger(__name__)

_MODEL_CACHE_TTL = 3600  # Kept for backward compatibility

# ── OCI GenAI model catalogue (static, no network call needed) ──────────
OCI_GENAI_MODELS: Dict[str, Dict[str, Any]] = {
    "xai.grok-3-mini": {"context_length": 131072, "name": "Grok 3 Mini"},
    "xai.grok-3": {"context_length": 131072, "name": "Grok 3"},
    "meta.llama-3.3-70b-instruct": {"context_length": 128000, "name": "Llama 3.3 70B"},
    "meta.llama-4-maverick-17b-128e-instruct-fp8": {"context_length": 1048576, "name": "Llama 4 Maverick"},
    "meta.llama-4-scout-17b-16e-instruct-fp8": {"context_length": 10485760, "name": "Llama 4 Scout"},
}

# ── Ollama model catalogue (local inference) ──────────────────────────
OLLAMA_MODELS: Dict[str, Dict[str, Any]] = {
    "qwen3.5:0.8b": {"context_length": 32768, "name": "Qwen 3.5 0.8B"},
    "qwen3.5:2b": {"context_length": 32768, "name": "Qwen 3.5 2B"},
    "qwen3.5:4b": {"context_length": 32768, "name": "Qwen 3.5 4B"},
    "qwen3.5:9b": {"context_length": 32768, "name": "Qwen 3.5 9B"},
}

# Fallback defaults for models not in the OCI catalogue
DEFAULT_CONTEXT_LENGTHS: Dict[str, int] = {
    "xai.grok-3-mini": 131072,
    "xai.grok-3": 131072,
    "meta.llama-3.3-70b-instruct": 128000,
    "meta.llama-4-maverick-17b-128e-instruct-fp8": 1048576,
    "meta.llama-4-scout-17b-16e-instruct-fp8": 10485760,
    "qwen3.5:0.8b": 32768,
    "qwen3.5:2b": 32768,
    "qwen3.5:4b": 32768,
    "qwen3.5:9b": 32768,
}


def fetch_model_metadata(force_refresh: bool = False) -> Dict[str, Dict[str, Any]]:
    """Return merged model metadata map (Ollama + OCI GenAI)."""
    merged = dict(OLLAMA_MODELS)
    merged.update(OCI_GENAI_MODELS)
    return merged


def get_model_context_length(model: str) -> int:
    """Get the context length for a model (local map first, then fallback defaults)."""
    metadata = fetch_model_metadata()
    if model in metadata:
        return metadata[model].get("context_length", 128000)

    for default_model, length in DEFAULT_CONTEXT_LENGTHS.items():
        if default_model in model or model in default_model:
            return length

    return 128000


def estimate_tokens_rough(text: str) -> int:
    """Rough token estimate (~4 chars/token) for pre-flight checks."""
    if not text:
        return 0
    return len(text) // 4


def estimate_messages_tokens_rough(messages: List[Dict[str, Any]]) -> int:
    """Rough token estimate for a message list (pre-flight only)."""
    total_chars = sum(len(str(msg)) for msg in messages)
    return total_chars // 4
