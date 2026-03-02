"""
OCI profile-based authentication for OraHermes Agent.

OCI GenAI uses ~/.oci/config profiles for authentication. No OAuth device
code flows or API key minting needed — the OCI SDK handles auth via config
profiles, instance principals, or resource principals.

Architecture:
- resolve_provider() returns "oci" by default or "custom" for explicit overrides
- login_command() / logout_command() are stubs (OCI uses config profiles)
- Legacy symbols (ProviderConfig, AuthError, etc.) are preserved as stubs
  so that other modules can still import them without breaking.
"""

from __future__ import annotations

import logging
import os
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Dict, List, Optional

import yaml

from hermes_cli.config import get_hermes_home, get_config_path

logger = logging.getLogger(__name__)

# =============================================================================
# Constants (kept for compatibility)
# =============================================================================

AUTH_STORE_VERSION = 1


# =============================================================================
# Provider Registry (kept for compatibility)
# =============================================================================

@dataclass
class ProviderConfig:
    """Describes a known provider."""
    id: str
    name: str
    auth_type: str  # "oci_profile" or "api_key"
    portal_base_url: str = ""
    inference_base_url: str = ""
    client_id: str = ""
    scope: str = ""
    extra: Dict[str, Any] = field(default_factory=dict)


PROVIDER_REGISTRY: Dict[str, ProviderConfig] = {
    "oci": ProviderConfig(
        id="oci",
        name="OCI GenAI",
        auth_type="oci_profile",
    ),
    "ollama": ProviderConfig(
        id="ollama",
        name="Ollama (local)",
        auth_type="none",
        inference_base_url="http://localhost:11434/v1",
    ),
}


# =============================================================================
# Error Types (kept for compatibility)
# =============================================================================

class AuthError(RuntimeError):
    """Structured auth error with UX mapping hints."""

    def __init__(
        self,
        message: str,
        *,
        provider: str = "",
        code: Optional[str] = None,
        relogin_required: bool = False,
    ) -> None:
        super().__init__(message)
        self.provider = provider
        self.code = code
        self.relogin_required = relogin_required


def format_auth_error(error: Exception) -> str:
    """Map auth failures to concise user-facing guidance."""
    if not isinstance(error, AuthError):
        return str(error)

    if error.code == "oci_config_missing":
        return (
            "OCI config not found. Create ~/.oci/config with your tenancy details.\n"
            "See: https://docs.oracle.com/iaas/Content/API/Concepts/sdkconfig.htm"
        )

    return str(error)


# =============================================================================
# Provider Resolution
# =============================================================================

def resolve_provider(
    requested: Optional[str] = None,
    *,
    explicit_api_key: Optional[str] = None,
    explicit_base_url: Optional[str] = None,
) -> str:
    """
    Determine which inference provider to use.

    Priority: explicit args > requested > config file > default (ollama).
    """
    if explicit_base_url and explicit_api_key:
        return "custom"
    if requested and requested != "auto":
        return requested

    # Read from config
    from hermes_cli.config import load_config
    config = load_config()
    provider = config.get("provider", "ollama")
    if provider in ("ollama", "oci", "custom"):
        return provider
    return "ollama"


# =============================================================================
# Auth Store stubs — kept so other modules can import them
# =============================================================================

def get_provider_auth_state(provider_id: str) -> Optional[Dict[str, Any]]:
    """Return persisted auth state for a provider, or None.

    OCI auth is handled by the SDK via ~/.oci/config, so there is no
    per-provider JSON state. Returns a synthetic dict for OCI.
    """
    if provider_id == "ollama":
        return {
            "provider": "ollama",
            "base_url": "http://localhost:11434/v1",
            "config_exists": True,
        }
    if provider_id == "oci":
        oci_profile = os.getenv("OCI_PROFILE", "foosball")
        oci_config = Path.home() / ".oci" / "config"
        return {
            "provider": "oci",
            "profile": oci_profile,
            "config_exists": oci_config.exists(),
        }
    return None


def get_active_provider() -> Optional[str]:
    """Return the currently active provider ID."""
    from hermes_cli.config import load_config
    config = load_config()
    return config.get("provider", "ollama")


def clear_provider_auth(provider_id: Optional[str] = None) -> bool:
    """Clear auth state for a provider (no-op for OCI)."""
    return False


def deactivate_provider() -> None:
    """Deactivate the current provider (no-op for OCI)."""
    pass


# =============================================================================
# OCI credential resolution (replaces resolve_nous_runtime_credentials)
# =============================================================================

def resolve_nous_runtime_credentials(
    *,
    min_key_ttl_seconds: int = 1800,
    timeout_seconds: float = 15.0,
    insecure: Optional[bool] = None,
    ca_bundle: Optional[str] = None,
    force_mint: bool = False,
) -> Dict[str, Any]:
    """
    Resolve OCI GenAI credentials for runtime use.

    This is a compatibility stub — OCI authentication is handled by the SDK
    via config profiles. Returns a dict with provider info so callers that
    expect the old Nous Portal credential dict structure still work.
    """
    oci_profile = os.getenv("OCI_PROFILE", "foosball")
    oci_config = Path.home() / ".oci" / "config"

    if not oci_config.exists():
        raise AuthError(
            f"OCI config file not found at {oci_config}",
            provider="oci",
            code="oci_config_missing",
        )

    return {
        "provider": "oci",
        "base_url": "oci://genai",
        "api_key": f"oci-profile:{oci_profile}",
        "key_id": None,
        "expires_at": None,
        "expires_in": None,
        "source": "oci_config",
    }


# =============================================================================
# Status helpers
# =============================================================================

def get_nous_auth_status() -> Dict[str, Any]:
    """Status snapshot for `hermes status` output — adapted for OCI."""
    oci_profile = os.getenv("OCI_PROFILE", "foosball")
    oci_config = Path.home() / ".oci" / "config"
    return {
        "logged_in": oci_config.exists(),
        "portal_base_url": None,
        "inference_base_url": "oci://genai",
        "access_expires_at": None,
        "agent_key_expires_at": None,
        "has_refresh_token": False,
        "oci_profile": oci_profile,
    }


def get_auth_status(provider_id: Optional[str] = None) -> Dict[str, Any]:
    """Generic auth status dispatcher."""
    effective = provider_id or get_active_provider()
    if effective == "ollama":
        return {
            "logged_in": True,
            "portal_base_url": None,
            "inference_base_url": "http://localhost:11434/v1",
            "access_expires_at": None,
            "agent_key_expires_at": None,
            "has_refresh_token": False,
        }
    return get_nous_auth_status()


# =============================================================================
# Model helpers (kept for compatibility)
# =============================================================================

def fetch_nous_models(
    *,
    inference_base_url: str = "",
    api_key: str = "",
    timeout_seconds: float = 15.0,
    verify: bool | str = True,
) -> List[str]:
    """Fetch available model IDs.

    OCI GenAI models are configured statically via hermes_constants.
    Returns an empty list — model listing is handled elsewhere.
    """
    return []


def _prompt_model_selection(model_ids: List[str], current_model: str = "") -> Optional[str]:
    """Interactive model selection. Returns chosen model ID or None."""
    if not model_ids:
        return None

    ordered = []
    if current_model and current_model in model_ids:
        ordered.append(current_model)
    for mid in model_ids:
        if mid not in ordered:
            ordered.append(mid)

    def _label(mid):
        if mid == current_model:
            return f"{mid}  <- currently in use"
        return mid

    print("Select default model:")
    for i, mid in enumerate(ordered, 1):
        print(f"  {i}. {_label(mid)}")
    n = len(ordered)
    print(f"  {n + 1}. Enter custom model name")
    print(f"  {n + 2}. Skip (keep current)")
    print()

    while True:
        try:
            choice = input(f"Choice [1-{n + 2}] (default: skip): ").strip()
            if not choice:
                return None
            idx = int(choice)
            if 1 <= idx <= n:
                return ordered[idx - 1]
            elif idx == n + 1:
                custom = input("Enter model name: ").strip()
                return custom if custom else None
            elif idx == n + 2:
                return None
            print(f"Please enter 1-{n + 2}")
        except ValueError:
            print("Please enter a number")
        except (KeyboardInterrupt, EOFError):
            return None


def _save_model_choice(model_id: str) -> None:
    """Save the selected model to config.yaml and .env."""
    from hermes_cli.config import save_config, load_config, save_env_value

    config = load_config()
    if isinstance(config.get("model"), dict):
        config["model"]["default"] = model_id
    else:
        config["model"] = model_id
    save_config(config)
    save_env_value("LLM_MODEL", model_id)


# =============================================================================
# Config helpers (kept for compatibility)
# =============================================================================

def _update_config_for_provider(provider_id: str, inference_base_url: str) -> Path:
    """Update config.yaml to reflect the active provider."""
    config_path = get_config_path()
    config_path.parent.mkdir(parents=True, exist_ok=True)

    config: Dict[str, Any] = {}
    if config_path.exists():
        try:
            loaded = yaml.safe_load(config_path.read_text()) or {}
            if isinstance(loaded, dict):
                config = loaded
        except Exception:
            config = {}

    current_model = config.get("model")
    if isinstance(current_model, dict):
        model_cfg = dict(current_model)
    elif isinstance(current_model, str) and current_model.strip():
        model_cfg = {"default": current_model.strip()}
    else:
        model_cfg = {}

    model_cfg["provider"] = provider_id
    if inference_base_url:
        model_cfg["base_url"] = inference_base_url.rstrip("/")
    config["model"] = model_cfg

    config_path.write_text(yaml.safe_dump(config, sort_keys=False))
    return config_path


# =============================================================================
# CLI Commands — login / logout (stubs for OCI)
# =============================================================================

def login_command(args) -> None:
    """OCI authentication uses config profiles (~/.oci/config). No login needed."""
    print("OCI GenAI authentication uses your OCI config profile.")
    print("Configure your profile in ~/.oci/config")
    print(f"Current profile: {os.getenv('OCI_PROFILE', 'foosball')}")

    oci_config = Path.home() / ".oci" / "config"
    if oci_config.exists():
        print(f"Config file found: {oci_config}")
    else:
        print(f"WARNING: Config file not found at {oci_config}")
        print("Create one following: https://docs.oracle.com/iaas/Content/API/Concepts/sdkconfig.htm")


def _login_nous(args, pconfig: ProviderConfig) -> None:
    """Legacy stub — redirects to OCI login guidance."""
    login_command(args)


def logout_command(args) -> None:
    """OCI profile auth has no session to clear."""
    print("OCI GenAI uses config profiles — no session to clear.")
    print(f"Current profile: {os.getenv('OCI_PROFILE', 'foosball')}")
