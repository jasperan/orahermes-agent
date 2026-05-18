"""Regression tests for disabled memory provider selection during AIAgent init."""

from unittest.mock import patch

import agent.model_metadata


def test_blank_memory_provider_does_not_load_external_provider():
    """Blank memory.provider remains opt-out in the Oracle-only runtime."""
    cfg = {"memory": {"provider": ""}, "agent": {}}

    with (
        patch("hermes_cli.config.load_config", return_value=cfg),
        patch("hermes_cli.config.save_config") as save_config,
        patch("plugins.memory.load_memory_provider") as load_memory_provider,
        patch("agent.model_metadata.get_model_context_length", return_value=204_800),
        patch("run_agent.get_tool_definitions", return_value=[]),
        patch("run_agent.check_toolset_requirements", return_value={}),
        patch("run_agent.OpenAI"),
    ):
        from run_agent import AIAgent

        agent = AIAgent(
            api_key="test-key-1234567890",
            base_url="https://openrouter.ai/api/v1",
            quiet_mode=True,
            skip_context_files=True,
            skip_memory=False,
        )

    assert agent._memory_manager is None
    load_memory_provider.assert_not_called()
    save_config.assert_not_called()
