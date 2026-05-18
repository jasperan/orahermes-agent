---
sidebar_position: 12
title: "Kanban"
description: "Kanban is disabled until it is Oracle-backed"
---

# Kanban

Kanban is disabled in OraHermes until its board schema is ported to Oracle
Database.

The upstream Kanban board uses a local database file. OraHermes does not allow
local runtime databases, so the CLI and worker tool surface fail closed instead
of creating a board.

Use Oracle-backed session history and normal task tracking until an Oracle
Kanban schema is available.

## How Workers Interact With the Board

Worker board interactions are unavailable while Kanban storage is disabled.
OraHermes does not create or read local board state.

## Auto vs Manual Orchestration

Kanban orchestration modes are disabled until the board schema is implemented
on Oracle Database.

## Kanban Slash Command

Kanban slash commands fail closed in OraHermes instead of creating a local
database file.
