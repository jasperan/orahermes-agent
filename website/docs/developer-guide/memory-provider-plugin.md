---
title: Memory Provider Plugins
---

# Memory Provider Plugins

External memory-provider plugins are disabled in OraHermes.

The upstream Hermes Agent plugin interface is intentionally not exposed because
OraHermes requires runtime persistence to use Oracle Database only. A memory
provider can be reintroduced only after its storage, queues, search, and
configuration path are ported to Oracle-backed persistence.

## Adding CLI Commands

Memory-provider CLI commands are not available in OraHermes. Any future provider
CLI surface must target Oracle-backed persistence and must not create local
database, cache, queue, or sidecar state.
