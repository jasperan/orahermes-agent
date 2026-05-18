---
sidebar_position: 4
title: "Memory Providers"
description: "External memory providers are disabled in OraHermes"
---

# Memory Providers

External memory providers are not available in OraHermes. Runtime state must be
stored in Oracle Database only.

Use the Oracle-backed `SessionDB` path for session history and recall. Provider
configuration left in `~/.hermes/config.yaml` is ignored by OraHermes.
