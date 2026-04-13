"""Multi-platform messaging gateway (Telegram, Discord, WhatsApp, etc.)."""

from .config import GatewayConfig, PlatformConfig, HomeChannel, load_gateway_config
from .session import (
    SessionContext,
    SessionStore,
    SessionResetPolicy,
    build_session_context_prompt,
)
from .delivery import DeliveryRouter, DeliveryTarget

__all__ = [
    # Config
    "GatewayConfig",
    "PlatformConfig",
    "HomeChannel",
    "load_gateway_config",
    # Session
    "SessionContext",
    "SessionStore",
    "SessionResetPolicy",
    "build_session_context_prompt",
    # Delivery
    "DeliveryRouter",
    "DeliveryTarget",
]
