---
type: architecture
title: Herrscher — architecture
---
Plugins self-register in init() (xcaddy pattern); the host resolves a PluginConfig
from each plugin's declared Settings. Categories: gateway, backend, memory,
orchestrator. The Orchestrator has a hard dependency on Memory.

## Liens
- [[herrscher/herrscher/index|belongs-to]]
