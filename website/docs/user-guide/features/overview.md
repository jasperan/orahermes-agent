---
title: "Features Overview"
sidebar_label: "Overview"
sidebar_position: 1
---

# Features Overview

Hermes Agent includes a rich set of capabilities that extend far beyond basic chat. From persistent memory and file-aware context to browser automation and voice conversations, these features work together to make Hermes a powerful autonomous assistant.

## Core

- **[Tools & Toolsets](tools.md)** — Tools are functions that extend the agent's capabilities. They're organized into logical toolsets that can be enabled or disabled per platform, covering web search, terminal execution, file editing, memory, delegation, and more.
- **[Skills System](skills.md)** — On-demand knowledge documents the agent can load when needed. Skills follow a progressive disclosure pattern to minimize token usage and are compatible with the [agentskills.io](https://agentskills.io/specification) open standard.
- **[Memory](memory.md)** — OraHermes disables local file-backed memory and uses Oracle Database for runtime persistence and recall.
- **[Context Files](context-files.md)** — Hermes automatically discovers and loads project context files (`.hermes.md`, `AGENTS.md`, `CLAUDE.md`, `SOUL.md`, `.cursorrules`) that shape how it behaves in your project.
- **[Context References](context-references.md)** — Type `@` followed by a reference to inject files, folders, git diffs, and URLs directly into your messages. Hermes expands the reference inline and appends the content automatically.
- **[Checkpoints](../checkpoints-and-rollback.md)** — Hermes automatically snapshots your working directory before making file changes, giving you a safety net to roll back with `/rollback` if something goes wrong.

## Automation

- **[Scheduled Tasks (Cron)](cron.md)** — Schedule tasks to run automatically with natural language or cron expressions. Jobs can attach skills, deliver results to any platform, and support pause/resume/edit operations.
- **[Subagent Delegation](delegation.md)** — The `delegate_task` tool spawns child agent instances with isolated context, restricted toolsets, and their own terminal sessions. Run 3 concurrent subagents by default (configurable) for parallel workstreams.
- **[Code Execution](code-execution.md)** — The `execute_code` tool lets the agent write Python scripts that call Hermes tools programmatically, collapsing multi-step workflows into a single LLM turn via sandboxed RPC execution.
- **[Event Hooks](hooks.md)** — Run custom code at key lifecycle points. Gateway hooks handle logging, alerts, and webhooks; plugin hooks handle tool interception, metrics, and guardrails.
- **[Batch Processing](batch-processing.md)** — Run the Hermes agent across hundreds or thousands of prompts in parallel, generating structured ShareGPT-format trajectory data for training data generation or evaluation.

## Media & Web

- **[Voice Mode](voice-mode.md)** — Full voice interaction across CLI and messaging platforms. Talk to the agent using your microphone, hear spoken replies, and have live voice conversations in Discord voice channels.
- **[Browser Automation](browser.md)** — Full browser automation with multiple backends: Browserbase cloud, Browser Use cloud, local Chrome via CDP, or local Chromium. Navigate websites, fill forms, and extract information.
- **[Vision & Image Paste](vision.md)** — Multimodal vision support. Paste images from your clipboard into the CLI and ask the agent to analyze, describe, or work with them using any vision-capable model.
- **[Image Generation](image-generation.md)** — Generate images from text prompts using FAL.ai. Nine models supported (FLUX 2 Klein/Pro, GPT-Image 1.5/2, Nano Banana Pro, Ideogram V3, Recraft V4 Pro, Qwen, Z-Image Turbo); pick one via `hermes tools`.
- **[Voice & TTS](tts.md)** — Text-to-speech output and voice message transcription across all messaging platforms, with ten native provider options: Edge TTS (free), ElevenLabs, OpenAI TTS, MiniMax, Mistral Voxtral, Google Gemini, xAI, NeuTTS, KittenTTS, and Piper — plus custom command providers for any local TTS CLI.

## Integrations

- **[MCP Integration](mcp.md)** — Connect to any MCP server via stdio or HTTP transport. Access external tools from GitHub, databases, file systems, and internal APIs without writing native Hermes tools. Includes per-server tool filtering and sampling support.
- **[Provider Routing](provider-routing.md)** — Fine-grained control over which AI providers handle your requests. Optimize for cost, speed, or quality with sorting, whitelists, blacklists, and priority ordering.
- **[Fallback Providers](fallback-providers.md)** — Automatic failover to backup LLM providers when your primary model encounters errors, including independent fallback for auxiliary tasks like vision and compression.
- **[Credential Pools](credential-pools.md)** — Distribute API calls across multiple keys for the same provider. Automatic rotation on rate limits or failures.
- **[Memory](memory.md)** — OraHermes stores session history and recall data in Oracle Database only. External memory providers and local memory stores are disabled.
- **[API Server](api-server.md)** — Expose Hermes as an OpenAI-compatible HTTP endpoint. Connect any frontend that speaks the OpenAI format — Open WebUI, LobeChat, LibreChat, and more.
- **[IDE Integration (ACP)](acp.md)** — Use Hermes inside ACP-compatible editors such as VS Code, Zed, and JetBrains. Chat, tool activity, file diffs, and terminal commands render inside your editor.
- **[Trajectory Export](../../developer-guide/trajectory-format.md)** — Save ShareGPT-format conversation traces for debugging, analysis, and downstream training workflows.

## Customization

- **[Personality & SOUL.md](personality.md)** — Fully customizable agent personality. `SOUL.md` is the primary identity file — the first thing in the system prompt — and you can swap in built-in or custom `/personality` presets per session.
- **[Skins & Themes](skins.md)** — Customize the CLI's visual presentation: banner colors, spinner faces and verbs, response-box labels, branding text, and the tool activity prefix.
- **[Plugins](plugins.md)** — Add custom tools, hooks, and integrations without modifying core code. Memory-provider plugins are disabled in OraHermes unless they are ported to Oracle Database.
