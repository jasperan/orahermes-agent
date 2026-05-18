---
title: Sessions
---

# Sessions

OraHermes persists sessions in Oracle Database through the `SessionDB`
compatibility facade.

## Resume

`/resume` and `hermes --continue` load conversation history from Oracle. If
Oracle is not configured or unavailable, OraHermes fails closed instead of
falling back to a local database.

## Search

The `session_search` tool reads Oracle-backed session history and returns real
messages from prior conversations. Oracle AI Vector Search can provide semantic
and hybrid recall when the schema is configured for embeddings.

## Cross-Platform Handoff

Cross-platform handoff uses the same Oracle-backed session record. A messaging
or CLI resume fails closed if Oracle is unavailable.

## Conversation Recap on Resume

Resume recaps are generated from Oracle-backed conversation history. OraHermes
does not fall back to local session files for recap context.

## Deletion

`/exit --delete` removes the current session's Oracle history and local
transcript artifacts for privacy-sensitive work.
