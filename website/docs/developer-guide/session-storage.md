---
title: Session Storage
---

# Session Storage

OraHermes stores session state in Oracle Database only.

The public `hermes_state.SessionDB` name is retained as a compatibility facade,
but it instantiates `oracle_state.OracleSessionDB`. Passing a local database
path raises an error; there is no local database fallback.

## Requirements

Set:

```bash
ORACLE_DSN=host:port/service
ORACLE_USER=hermes
ORACLE_PASSWORD=...
```

Then apply `oracle_setup.sql` before starting the CLI, gateway, ACP adapter, or
TUI gateway.

## Contract

- Session rows, messages, metadata, and recall data live in Oracle.
- Startup paths fail fast when Oracle is unavailable.
- SQLite state files are not created or migrated by OraHermes.
- Kanban remains disabled until its board schema is ported to Oracle.
