<p align="center">
  <img src="assets/banner.png" alt="orahermes-agent" width="100%">
</p>

# orahermes-agent

**Hermes Agent powered by Oracle AI Database & OCI GenAI**

<p align="center">
  <a href="https://www.oracle.com/database/free/"><img src="https://img.shields.io/badge/Oracle-26ai_Free-F80000.svg?style=for-the-badge&logo=oracle&logoColor=white" alt="Oracle Database"></a>&nbsp;
  <a href="https://docs.oracle.com/en-us/iaas/Content/generative-ai/home.htm"><img src="https://img.shields.io/badge/OCI-GenAI-F80000.svg?style=for-the-badge&logo=oracle&logoColor=white" alt="OCI GenAI"></a>&nbsp;
  <a href="https://docs.oracle.com/en-us/iaas/Content/search-opensearch/home.htm"><img src="https://img.shields.io/badge/Oracle-Text_Search-F80000.svg?style=for-the-badge&logo=oracle&logoColor=white" alt="Oracle Text"></a>&nbsp;
  <a href="https://x.ai/"><img src="https://img.shields.io/badge/xAI-Grok-000000.svg?style=for-the-badge&logo=x&logoColor=white" alt="xAI Grok"></a>&nbsp;
  <a href="https://ollama.com/"><img src="https://img.shields.io/badge/Backend-Ollama-00DC82.svg?style=for-the-badge&logo=ollama&logoColor=white" alt="Ollama"></a>&nbsp;
  <a href="https://docs.oracle.com/en-us/iaas/Content/generative-ai/home.htm"><img src="https://img.shields.io/badge/Backend-OCI_GenAI_(xAI)-F80000.svg?style=for-the-badge&logo=oracle&logoColor=white" alt="OCI GenAI xAI"></a>&nbsp;
  <a href="https://www.python.org/"><img src="https://img.shields.io/badge/Python-3.11+-3776AB.svg?style=for-the-badge&logo=python&logoColor=white" alt="Python"></a>&nbsp;
  <a href="https://github.com/NousResearch/hermes-agent/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg?style=for-the-badge" alt="License: MIT"></a>
</p>

---

A fork of [NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent) that replaces the default inference provider and storage layer with Oracle Cloud Infrastructure services:

- **OCI GenAI** (xAI Grok models by default) instead of OpenRouter
- **Oracle 26ai Free** instead of SQLite for session and message storage

Everything else -- the tool-calling engine, skills system, messaging gateways, cron scheduler, and multi-model architecture -- remains fully intact from upstream.

---

## Quick Start

### Prerequisites

| Requirement | Details |
|---|---|
| **Python** | 3.11 or newer |
| **OCI CLI** | Configured with a profile in `~/.oci/config` |
| **Oracle 26ai Free** | Running container (see [Database Setup](#database-setup) below) |

### Installation

```bash
# Clone
git clone https://github.com/jasperan/orahermes-agent.git
cd orahermes-agent

# Run the setup script (installs uv, creates venv, installs deps)
./setup-hermes.sh

# Or install manually
pip install -e ".[all]"
```

### OCI Configuration

The agent authenticates to OCI GenAI through your `~/.oci/config` profile. A minimal profile looks like this:

```ini
[DEFAULT]
user=ocid1.user.oc1..aaaaaaaaexample
fingerprint=aa:bb:cc:dd:ee:ff:00:11:22:33:44:55:66:77:88:99
tenancy=ocid1.tenancy.oc1..aaaaaaaaexample
region=us-chicago-1
key_file=~/.oci/oci_api_key.pem
```

Then set the following in your `.env` (copy from `.env.example`):

```bash
cp .env.example .env
```

```bash
# OCI GenAI
OCI_PROFILE=DEFAULT
OCI_REGION=us-chicago-1
OCI_COMPARTMENT_ID=ocid1.compartment.oc1..your-compartment-ocid

# Default model
LLM_MODEL=xai.grok-3-mini
```

### Database Setup

Start Oracle 26ai Free as a container:

```bash
docker run -d \
  --name oracle-26ai \
  -p 1521:1521 \
  -e ORACLE_PWD=YourPassword123 \
  container-registry.oracle.com/database/free:latest-lite
```

Wait for the database to be ready, then create the Hermes schema:

```bash
# Connect as SYSDBA and create the hermes user
sqlplus sys/YourPassword123@localhost:1521/FREEPDB1 as sysdba <<'SQL'
CREATE USER hermes IDENTIFIED BY HermesPass123
  DEFAULT TABLESPACE users QUOTA UNLIMITED ON users;
GRANT CONNECT, RESOURCE, CTX_APP TO hermes;
SQL

# Apply the schema
sqlplus hermes/HermesPass123@localhost:1521/FREEPDB1 @oracle_setup.sql
```

Add the connection details to `.env`:

```bash
ORACLE_DSN=localhost:1521/FREEPDB1
ORACLE_USER=hermes
ORACLE_PASSWORD=HermesPass123
```

### Run

```bash
# Interactive CLI
hermes

# Or run the agent directly
python run_agent.py
```

---

## Architecture

```
+------------------+       +---------------------+       +---------------------+
|                  |       |                     |       |                     |
|   OCI GenAI      | <---> |   orahermes-agent   | <---> |  Oracle 26ai Free   |
|   (xAI Grok)    |       |                     |       |     (FREEPDB1)      |
|                  |       |                     |       |                     |
+------------------+       +---------------------+       +---------------------+
   Inference API             Tool-calling engine           Session storage
   OpenAI-compatible         Skills & scheduling           Message history
   via oci-openai            Messaging gateways            Full-text search
                             Memory & compression          Oracle Text indexes
```

All inference calls go through the `oci-openai` SDK, which wraps OCI GenAI's OpenAI-compatible endpoint. The agent's session state -- conversations, tool-call history, token counts -- lives in Oracle Database tables instead of a local SQLite file. Oracle Text `CTXSYS.CONTEXT` indexes provide full-text search over message content.

---

## What's Different from Upstream

This fork makes exactly **two provider swaps** while keeping everything else untouched:

### 1. OpenRouter --> OCI GenAI

| | Upstream | orahermes-agent |
|---|---|---|
| **Provider** | OpenRouter | OCI GenAI |
| **Auth** | API key (`OPENROUTER_API_KEY`) | OCI config profile (`~/.oci/config`) |
| **Default model** | `anthropic/claude-opus-4.6` | `xai.grok-3-mini` |
| **SDK** | `openai` | `oci-openai` (wraps `openai`) |
| **Endpoint** | `https://openrouter.ai/api/v1` | `https://inference.generativeai.{region}.oci.oraclecloud.com/...` |

New files: `oci_client.py` -- thin wrapper that creates sync/async OCI GenAI clients.

### 2. SQLite --> Oracle 26ai Free

| | Upstream | orahermes-agent |
|---|---|---|
| **Database** | SQLite (local file) | Oracle 26ai Free (container) |
| **Driver** | `sqlite3` (stdlib) | `oracledb` (python-oracledb) |
| **Connection** | File path | Connection pool (`oracledb.create_pool`) |
| **Full-text search** | FTS5 | Oracle Text (`CTXSYS.CONTEXT`) |

New files: `oracle_state.py` (drop-in `OracleSessionDB`), `oracle_setup.sql` (DDL).

---

## Upstream Features

All features from [NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent) are preserved:

- **Persistent agent** -- runs on your server, remembers what it learns across sessions
- **Tool calling** -- terminal, file operations, web search, browser automation, image generation, TTS
- **Skills system** -- learns reusable procedures, can share via Skills Hub
- **Messaging gateways** -- Telegram, Discord, WhatsApp, Slack
- **Cron scheduler** -- schedule recurring tasks
- **Context compression** -- automatic conversation summarization when context window fills
- **Memory system** -- persistent notes and user profile across sessions
- **Subagent delegation** -- spawn child agents for parallel task execution
- **Multiple terminal backends** -- local, SSH, Docker, Singularity, Modal
- **Batch processing** -- data generation and RL training environments (Atropos)

---

## Live Dashboard

A real-time D3.js dashboard visualizes all data orahermes-agent produces in Oracle Database -- sessions, messages, tool usage, token counts, and content lengths. Auto-refreshes every 3 seconds.

```bash
ORACLE_DSN=localhost:1521/FREEPDB1 ORACLE_USER=hermes ORACLE_PASSWORD=<password> \
    python dashboard_server.py --port 8501
```

<p align="center">
  <img src="assets/dashboard-viewport.png" alt="Dashboard KPIs" width="100%">
</p>

<p align="center">
  <img src="assets/dashboard-full.png" alt="Full Dashboard" width="100%">
</p>

**Charts included:**
- KPI cards (sessions, messages, tool calls, input/output tokens) with animated counters and delta indicators
- Role distribution donut chart (user / assistant / tool)
- Tool usage horizontal bar chart (memory, session_search, execute_code, etc.)
- Messages per session timeline with tool call overlay
- Content length scatter plot colored by role
- Recent sessions table with model, message count, and status
- Live message feed with role-colored entries

---

## License

MIT -- same as upstream. See [LICENSE](LICENSE).

---

## Credit

Based on [NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent) by [Nous Research](https://nousresearch.com).

---

<div align="center">

[![GitHub](https://img.shields.io/badge/GitHub-jasperan-181717?style=for-the-badge&logo=github&logoColor=white)](https://github.com/jasperan)&nbsp;
[![LinkedIn](https://img.shields.io/badge/LinkedIn-jasperan-0077B5?style=for-the-badge&logo=linkedin&logoColor=white)](https://www.linkedin.com/in/jasperan/)&nbsp;
[![Oracle](https://img.shields.io/badge/Oracle_Database-Free-F80000?style=for-the-badge&logo=oracle&logoColor=white)](https://www.oracle.com/database/free/)

</div>
