"""
Canonical model lists for CLI and setup wizards.

Add, remove, or reorder entries here — both `hermes setup` and
`hermes` provider-selection will pick up the change automatically.
"""

# (model_id, display description shown in menus)
OLLAMA_MODELS: list[tuple[str, str]] = [
    ("qwen3.5:4b",    "recommended, good balance of speed and capability"),
    ("qwen3.5:9b",    "most capable small model"),
    ("qwen3.5:2b",    "fast and light"),
    ("qwen3.5:0.8b",  "minimal footprint, testing only"),
]

OCI_GENAI_MODELS: list[tuple[str, str]] = [
    ("xai.grok-3-mini",                                   "recommended"),
    ("xai.grok-3",                                        ""),
    ("meta.llama-3.3-70b-instruct",                       ""),
    ("meta.llama-4-maverick-17b-128e-instruct-fp8",       ""),
    ("meta.llama-4-scout-17b-16e-instruct-fp8",           ""),
]

# Legacy alias
OPENROUTER_MODELS = OCI_GENAI_MODELS


def ollama_model_ids() -> list[str]:
    return [mid for mid, _ in OLLAMA_MODELS]


def ollama_menu_labels() -> list[str]:
    labels = []
    for mid, desc in OLLAMA_MODELS:
        labels.append(f"{mid} ({desc})" if desc else mid)
    return labels


def model_ids() -> list[str]:
    """Return OCI GenAI model IDs (backward compat)."""
    return [mid for mid, _ in OCI_GENAI_MODELS]


def menu_labels() -> list[str]:
    """Return OCI GenAI display labels (backward compat)."""
    labels = []
    for mid, desc in OCI_GENAI_MODELS:
        labels.append(f"{mid} ({desc})" if desc else mid)
    return labels
