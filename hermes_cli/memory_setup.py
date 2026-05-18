"""Disabled memory-provider CLI for OraHermes.

OraHermes only supports Oracle-backed runtime persistence. Legacy file-backed
memory and external memory providers are disabled until they are ported to
Oracle Database.
"""

from __future__ import annotations


def _disable_memory_provider_config() -> None:
    from hermes_cli.config import load_config, save_config

    config = load_config()
    if not isinstance(config.get("memory"), dict):
        config["memory"] = {}
    config["memory"]["provider"] = ""
    config["memory"]["memory_enabled"] = False
    config["memory"]["user_profile_enabled"] = False
    save_config(config)


def _disabled_message() -> None:
    print("\n  Memory providers are disabled in OraHermes.")
    print("  Runtime persistence must use Oracle Database only.\n")


def _get_available_providers() -> list:
    """Return no external memory providers in Oracle-only builds."""
    return []


def cmd_setup_provider(provider_name: str) -> None:
    """Disable memory-provider config instead of setting up a provider."""
    _disable_memory_provider_config()
    _disabled_message()


def cmd_setup(args) -> None:
    """Disable memory-provider config instead of running provider setup."""
    _disable_memory_provider_config()
    _disabled_message()


def cmd_status(args) -> None:
    """Show Oracle-only memory status."""
    from hermes_cli.config import load_config

    config = load_config()
    mem_config = config.get("memory", {}) if isinstance(config, dict) else {}
    provider_name = mem_config.get("provider", "") if isinstance(mem_config, dict) else ""

    print("\nMemory status\n" + "-" * 40)
    print("  Built-in:  disabled (file-backed MEMORY.md/USER.md)")
    print("  Provider:  disabled (Oracle-only runtime)")
    if provider_name:
        print(f"  Ignored config: memory.provider={provider_name!r}")
    print()


def memory_command(args) -> None:
    """Route memory subcommands."""
    sub = getattr(args, "memory_command", None)
    if sub == "setup":
        cmd_setup(args)
    elif sub == "status":
        cmd_status(args)
    else:
        cmd_status(args)
