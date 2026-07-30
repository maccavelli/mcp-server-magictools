# Brainstorming: Session Stats Telemetry Expansion

## Overview
The goal is to expand the `get_session_stats` tool within the MagicTools Orchestrator to act as a comprehensive telemetry and performance dashboard, measuring more than just arbitrary token context.

## Core Problem
Currently, the tool reports:
*   Calls (Overall)
*   Available Context Tokens
*   Context Usage & Savings

While functional, this ignores the orchestrator's primary jobs: **JIT Process Management** and **Payload transformation** (The Dual-Stack Protocol).

## Proposed Telemetry Enhancements

### 1. Data Pipeline & Optimization Metrics
Since MagicTools heavily enforces the "Transformation Bridge" to prevent IDE Extension Host OOM crashes (via Payload minification/truncation), we should measure its effectiveness:
*   `Raw Backplane Bytes Processed`: Total bytes ingested from sub-servers.
*   `Bytes Sent to Frontend (IDE)`: Total bytes after transformation and JSON truncation.
*   `Overall Optimization Ratio`: Showing exactly how much bloat was removed from the SSH pipe.

### 2. JIT (Just-In-Time) Mechanics
Since tools are hot-loaded strictly on demand:
*   `Active Sub-Servers`: Count of currently hot processes vs total ecosystem processes.
*   `Average Proxy Spin-up Latency`: Time taken to boot a sub-server and complete the HTTP RPC handshake.

### 3. Stability & Resilience Tracking
The Orchestrator acts as a firewall between bad tools and the IDE. We should track fault tolerance:
*   `Total Sub-Server Faults`: Number of times the "Recovery Block" caught a plugin-level panic or EOF crash.
*   `Force-Sync Recoveries`: Number of times the `reload_servers` or automated sync healed a dropped process.

---

## Architectural Options for Implementation

### Approach A: Global Atomic Counters 
Implement a simple `pkg/telemetry` library with `atomic.Int64` integers. 
*   **Pros**: Incredible speed and highly concurrent. Lowest implementation effort.
*   **Cons**: Lacks granularity. You will see "5 Faults" but won't know *which* server faulted.

### Approach B: Concurrent Sub-Server Map (Recommended)
Store a `sync.Map` mapping Sub-Server IDs (`devops`, `filesystem`, `brainstorm`) to custom `Metric` structs.
*   **Pros**: The `get_session_stats` tool can output a per-server breakdown. You'll know if the `git` server is contributing to 90% of your payload weight.
*   **Cons**: Slightly heavier engineering requirement in the Proxy handler to ensure concurrent safe increments.

> [!IMPORTANT]
> Please review the expanded telemetry metrics above. Does Approach B sound like the right architectural direction for you?
