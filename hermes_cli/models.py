"""
Canonical list of OCI GenAI models offered in CLI and setup wizards.

Add, remove, or reorder entries here — both `hermes setup` and
`hermes` provider-selection will pick up the change automatically.
"""

# (model_id, display description shown in menus)
OCI_GENAI_MODELS: list[tuple[str, str]] = [
    ("xai.grok-3-mini",                                   "recommended"),
    ("xai.grok-3",                                        ""),
    ("meta.llama-3.3-70b-instruct",                       ""),
    ("meta.llama-4-maverick-17b-128e-instruct-fp8",       ""),
    ("meta.llama-4-scout-17b-16e-instruct-fp8",           ""),
]

# Legacy alias for code that references OPENROUTER_MODELS
OPENROUTER_MODELS = OCI_GENAI_MODELS


def model_ids() -> list[str]:
    """Return just the model-id strings (convenience helper)."""
    return [mid for mid, _ in OPENROUTER_MODELS]


def menu_labels() -> list[str]:
    """Return display labels like 'anthropic/claude-opus-4.6 (recommended)'."""
    labels = []
    for mid, desc in OPENROUTER_MODELS:
        labels.append(f"{mid} ({desc})" if desc else mid)
    return labels
