---
sidebar_position: 3
title: "Memory"
description: "OraHermes memory and session recall use Oracle Database only"
---

# Memory

OraHermes disables the legacy `MEMORY.md` / `USER.md` file store and all
external memory-provider plugins. Runtime persistence must go through Oracle
Database.

## What Is Available

- Session history is stored in the Oracle-backed `SessionDB` compatibility
  facade.
- `session_search` uses Oracle-backed session data.
- `hermes memory status` reports the Oracle-only policy.
- `hermes memory off` clears legacy provider configuration.
- `hermes memory reset` removes old local `MEMORY.md` and `USER.md` files.

## What Is Disabled

- Local file-backed memory.
- External memory providers.
- Any SQLite-backed memory store.

Configure `ORACLE_DSN`, `ORACLE_USER`, and `ORACLE_PASSWORD`, then apply the
OraHermes Oracle schema before using session persistence.
