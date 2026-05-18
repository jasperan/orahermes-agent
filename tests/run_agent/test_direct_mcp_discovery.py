"""Direct run_agent entry point startup behavior."""

from unittest.mock import patch

import run_agent


def test_direct_main_discovers_mcp_before_agent_init(capsys):
    events = []

    def fake_discover_mcp_tools():
        events.append("discover_mcp")

    class FakeAgent:
        def __init__(self, **_kwargs):
            events.append("agent_init")

        def run_conversation(self, _query):
            return {
                "completed": True,
                "api_calls": 0,
                "messages": [],
                "final_response": "ok",
            }

    with (
        patch("tools.mcp_tool.discover_mcp_tools", side_effect=fake_discover_mcp_tools),
        patch.object(run_agent, "AIAgent", FakeAgent),
    ):
        run_agent.main(query="hello", max_turns=1)

    capsys.readouterr()
    assert events[:2] == ["discover_mcp", "agent_init"]
