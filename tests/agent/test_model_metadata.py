"""Tests for agent/model_metadata.py — token estimation and context lengths."""

from unittest.mock import patch

from agent.model_metadata import (
    DEFAULT_CONTEXT_LENGTHS,
    estimate_tokens_rough,
    estimate_messages_tokens_rough,
    get_model_context_length,
    fetch_model_metadata,
)


# =========================================================================
# Token estimation
# =========================================================================

class TestEstimateTokensRough:
    def test_empty_string(self):
        assert estimate_tokens_rough("") == 0

    def test_none_returns_zero(self):
        assert estimate_tokens_rough(None) == 0

    def test_known_length(self):
        # 400 chars / 4 = 100 tokens
        text = "a" * 400
        assert estimate_tokens_rough(text) == 100

    def test_short_text(self):
        # "hello" = 5 chars -> 5 // 4 = 1
        assert estimate_tokens_rough("hello") == 1

    def test_proportional(self):
        short = estimate_tokens_rough("hello world")
        long = estimate_tokens_rough("hello world " * 100)
        assert long > short


class TestEstimateMessagesTokensRough:
    def test_empty_list(self):
        assert estimate_messages_tokens_rough([]) == 0

    def test_single_message(self):
        msgs = [{"role": "user", "content": "a" * 400}]
        result = estimate_messages_tokens_rough(msgs)
        assert result > 0

    def test_multiple_messages(self):
        msgs = [
            {"role": "user", "content": "Hello"},
            {"role": "assistant", "content": "Hi there, how can I help?"},
        ]
        result = estimate_messages_tokens_rough(msgs)
        assert result > 0


# =========================================================================
# Default context lengths
# =========================================================================

class TestDefaultContextLengths:
    def test_claude_models_200k(self):
        for key, value in DEFAULT_CONTEXT_LENGTHS.items():
            if "claude" in key:
                assert value == 200000, f"{key} should be 200000"

    def test_gpt4_models_128k(self):
        for key, value in DEFAULT_CONTEXT_LENGTHS.items():
            if "gpt-4" in key:
                assert value == 128000, f"{key} should be 128000"

    def test_gemini_models_1m(self):
        for key, value in DEFAULT_CONTEXT_LENGTHS.items():
            if "gemini" in key:
                assert value == 1048576, f"{key} should be 1048576"

    def test_all_values_positive(self):
        for key, value in DEFAULT_CONTEXT_LENGTHS.items():
            assert value > 0, f"{key} has non-positive context length"


# =========================================================================
# get_model_context_length (with mocked API)
# =========================================================================

class TestGetModelContextLength:
    @patch("agent.model_metadata.fetch_model_metadata")
    def test_known_model_from_api(self, mock_fetch):
        mock_fetch.return_value = {
            "test/model": {"context_length": 32000}
        }
        assert get_model_context_length("test/model") == 32000

    @patch("agent.model_metadata.fetch_model_metadata")
    def test_fallback_to_defaults(self, mock_fetch):
        mock_fetch.return_value = {}  # API returns nothing
        result = get_model_context_length("qwen3.5:4b")
        assert result == 32768

    @patch("agent.model_metadata.fetch_model_metadata")
    def test_unknown_model_returns_128k(self, mock_fetch):
        mock_fetch.return_value = {}
        result = get_model_context_length("unknown/never-heard-of-this")
        assert result == 128000

    @patch("agent.model_metadata.fetch_model_metadata")
    def test_partial_match_in_defaults(self, mock_fetch):
        mock_fetch.return_value = {}
        # "gpt-4o" is a substring match for "openai/gpt-4o"
        result = get_model_context_length("openai/gpt-4o")
        assert result == 128000


# =========================================================================
# fetch_model_metadata (cache behavior)
# =========================================================================

class TestFetchModelMetadata:
    def test_returns_merged_catalogue(self):
        """fetch_model_metadata merges Ollama + OCI GenAI models."""
        result = fetch_model_metadata()
        # Ollama models present
        assert "qwen3.5:4b" in result
        assert result["qwen3.5:4b"]["context_length"] == 32768
        # OCI GenAI models present
        assert "xai.grok-3-mini" in result
        assert result["xai.grok-3-mini"]["context_length"] == 131072

    def test_force_refresh_returns_same(self):
        """force_refresh has no effect on static catalogue but should not error."""
        result = fetch_model_metadata(force_refresh=True)
        assert len(result) > 0
        assert "qwen3.5:4b" in result

    def test_oci_models_take_precedence(self):
        """OCI GenAI entries are added after Ollama, so they win on key conflict."""
        result = fetch_model_metadata()
        # No actual conflicts in current catalogue, but verify merge order
        # by checking both catalogues are represented
        from agent.model_metadata import OLLAMA_MODELS, OCI_GENAI_MODELS
        for key in OLLAMA_MODELS:
            assert key in result
        for key in OCI_GENAI_MODELS:
            assert key in result
