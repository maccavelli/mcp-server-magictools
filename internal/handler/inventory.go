package handler

// InternalToolsInventoryJSON provides a static, SDK-adherent definition
// for all internal magictools orchestrator capabilities.
// This list is used to guarantee tool integrity during tools/list responses.
var InternalToolsInventoryJSON = []byte(`
[
  {
    "name": "sync_servers",
    "title": "Refresh server tool index",
    "description": "[DIRECTIVE: Lifecycle Sync] Refreshes and updates the local metadata index and tool schemas of registered sub-servers. Use when new tools are added or schemas changed. Keywords: refresh-schema sync-metadata metadata-index update-registry",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": false,
        "destructiveHint": false,
        "idempotentHint": true,
        "openWorldHint": false
      },
    "inputSchema": {
      "type": "object",
      "properties": {
        "names": {
          "type": "string",
          "description": "Target server names to sync metadata for (e.g. 'recall'). If omitted, refreshes the entire directory index."
        }
      },
      "additionalProperties": false
    }
  },
  {
    "name": "wake_servers",
    "title": "Start sleeping servers",
    "description": "[DIRECTIVE: Process Standby] Wakes sleeping, inactive, or suspended sub-server processes to hot standby to eliminate cold-start tool latencies. Keywords: warm-up-servers unpause-processes prevent-cold-starts wake-standby",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": false,
        "destructiveHint": false,
        "idempotentHint": true,
        "openWorldHint": false
      },
    "inputSchema": {
      "type": "object",
      "properties": {},
      "additionalProperties": false
    }
  },
  {
    "name": "reload_servers",
    "title": "Restart servers",
    "description": "[DIRECTIVE: Process Reboot] Force-terminates and reboots sub-server processes to recover from socket timeouts, process hangs, or memory leaks. Keywords: hard-restart crash-recovery reboot-process kill-hangs",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": false,
        "destructiveHint": true,
        "idempotentHint": true,
        "openWorldHint": false
      },
    "inputSchema": {
      "type": "object",
      "properties": {
        "names": {
          "type": "string",
          "description": "Target server processes to terminate and restart (e.g. 'go-modernizer'). If omitted, reboots all active processes."
        }
      },
      "additionalProperties": false
    }
  },
  {
    "name": "get_internal_logs",
    "title": "Read gateway logs",
    "description": "[DIRECTIVE: Orchestrator Log Dump] Retrieves raw, unfiltered stdout/stderr logs directly from the magictools orchestrator process itself. Use strictly for debugging orchestrator-level boot errors, proxy routing failures, or config crashes. Keywords: raw-stdout log-dump debug-orchestrator process-stderr",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": true,
        "openWorldHint": false
      },
    "inputSchema": {
      "type": "object",
      "properties": {
        "session_id": {
          "type": "string",
          "description": "Optional session correlation ID to isolate process logs for a specific run."
        },
        "max_lines": {
          "type": "integer",
          "description": "Integer limit of recent raw log lines to return (0-1000). Defaults to 25. Values outside the range are clamped, not refused; 0 or less uses the default.",
          "minimum": 0,
          "maximum": 1000,
          "default": 25
        }
      },
      "additionalProperties": false
    }
  },
  {
    "name": "get_session_stats",
    "title": "Show session metrics",
    "description": "[DIRECTIVE: Telemetry Overhead Analyzer] Emits dynamic network metrics, overhead latencies, and payload throughput calculations for active sub-server channels. Keywords: network-latency throughput-bytes payload-sizes serialization-time",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": true,
        "openWorldHint": false
      },
    "inputSchema": {
      "type": "object",
      "properties": {},
      "additionalProperties": false
    }
  },
  {
    "name": "get_health_report",
    "title": "Show server health",
    "description": "[DIRECTIVE: Server Health Status] Inspects and returns the up/down running status, uptime duration, memory allocation, and CPU load for all registered MCP servers. Keywords: query-uptime server-up-down process-load memory-rss",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": false,
        "destructiveHint": false,
        "idempotentHint": true,
        "openWorldHint": false
      },
    "inputSchema": {
      "type": "object",
      "properties": {},
      "additionalProperties": false
    }
  },
  {
    "name": "analyze_system_logs",
    "title": "Filter gateway logs by severity",
    "description": "[DIRECTIVE: Semantic Log Parser] Analyzes and filters orchestrator logs to detect organic error patterns, trace exception severity levels, and isolate multi-server error telemetry. Keywords: error-telemetry filter-severity trace-exception parse-syslog",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": true,
        "openWorldHint": false
      },
    "inputSchema": {
      "type": "object",
      "properties": {
        "server_id": {
          "type": "string",
          "description": "Server identifier to narrow down log evaluation (e.g. 'ddg-search')."
        },
        "lines": {
          "type": "integer",
          "description": "Integer limit of logs to parse (default 50, maximum 1000). Values above 1000 are clamped; 0 or less uses the default.",
          "minimum": 1,
          "maximum": 1000,
          "default": 50
        },
        "severity": {
          "type": "string",
          "description": "Optional: Filter by ERROR, WARNING, or CRITICAL.",
          "enum": [
            "ERROR",
            "WARNING",
            "CRITICAL"
          ]
        }
      },
      "additionalProperties": false
    }
  },
  {
    "name": "align_tools",
    "title": "Find the right tool",
    "description": "[DIRECTIVE: Tool Discovery Catalog] Resolves natural language intent queries into matched tool names, server URNs, and usage schemas. MANDATE: You MUST run this tool with full_schema=true before invoking any downstream tool via call_proxy to verify its required parameters. Keywords: find-tool tool-catalog schema-lookup intent-resolve",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": false,
        "destructiveHint": true,
        "idempotentHint": false,
        "openWorldHint": true
      },
    "inputSchema": {
      "type": "object",
      "properties": {
        "query": {
          "type": "string",
          "description": "Semantic search text describing the desired tool task or name."
        },
        "server_name": {
          "type": "string",
          "description": "Restrict search to this specific sub-server (e.g. 'recall')."
        },
        "category": {
          "type": "string",
          "description": "Optional category filter"
        },
        "full_schema": {
          "type": "boolean",
          "description": "Force returning full JSON schema descriptors in the response body."
        },
        "arguments": {
          "type": "object",
          "description": "Optional: arguments to pass to the resolved tool for inline execution. If provided and a single high-confidence match is found on a trusted server, the tool executes inline and returns the result directly. If execution is not possible, falls back to discovery mode with call templates for call_proxy."
        },
        "bypass_minification": {
          "type": "boolean",
          "description": "Skip Markdown transformation on inline execution results. Only applies when arguments are provided and inline execution succeeds."
        }
      },
      "required": ["query"],
      "additionalProperties": false
    }
  },
  {
    "name": "call_proxy",
    "title": "Run a downstream tool",
    "description": "[DIRECTIVE: Proxy Execution Runner] Executes a verified tool URN with the mapped argument payload on a downstream MCP sub-server. MANDATE: Before calling this tool for a URN, you MUST first run align_tools with full_schema=true to retrieve its exact JSON schema and parameter requirements. Guessed arguments are strictly prohibited. Keywords: execute-urn run-downstream-tool dispatch-payload proxy-execution",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": false,
        "destructiveHint": true,
        "idempotentHint": false,
        "openWorldHint": true
      },
    "inputSchema": {
      "type": "object",
      "properties": {
        "urn": {
          "type": "string",
          "description": "The unique resource name of the target tool (e.g. 'recall:get')."
        },
        "arguments": {
          "type": "object",
          "description": "JSON object containing parameter inputs matching the target tool schema."
        },
        "bypass_minification": {
          "type": "boolean",
          "description": "Skip the Markdown transformation and return raw JSON (Still protected by 10MB Slicer)"
        }
      },
      "required": [
        "urn",
        "arguments"
      ],
      "additionalProperties": false
    }
  },
  {
    "name": "update_config",
    "title": "Change a gateway setting",
    "description": "[DIRECTIVE: Runtime Config Modifier] Writes persistent configuration changes (e.g. logLevel, trustServers) to storage instantly. Keywords: modify-config config-yaml change-parameter persist-setting",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": false,
        "destructiveHint": false,
        "idempotentHint": true,
        "openWorldHint": false
      },
    "inputSchema": {
      "type": "object",
      "properties": {
        "key": {
          "type": "string",
          "description": "Name of the configuration variable to alter.",
          "enum": [
            "logLevel",
            "mcpLogLevel",
            "squeezeLevel",
            "logFormat",
            "scoreThreshold",
            "confidenceGap",
            "vector_enabled",
            "validateProxyCalls",
            "pinnedServers",
            "trustServers",
            "squeezeBypass",
            "ringBufferTargets",
            "tokenSpendThresh",
            "lruLimit"
          ]
        },
        "value": {
          "type": "string",
          "description": "String payload value to write to configuration. Per-key rules: squeezeLevel integer 0-5; scoreThreshold and confidenceGap float 0.0-1.0; logFormat json or text; tokenSpendThresh integer >= 0; lruLimit integer >= 1; boolean keys (vector_enabled, validateProxyCalls, squeezeBypass) accept true/1/yes/on or false/0/no/off; logLevel and mcpLogLevel accept ERROR, WARN, INFO, DEBUG, TRACE; pinnedServers, trustServers and ringBufferTargets take space-separated names."
        }
      },
      "required": [
        "key",
        "value"
      ],
      "additionalProperties": false
    }
  },
  {
    "name": "self_check",
    "title": "Run gateway self-diagnostics",
    "description": "[DIRECTIVE: Internal Diagnostic Auditor] Audits the orchestrator's database sync status, cache hits/misses, and internal memory buffers. Keywords: cache-efficiency database-sync self-diagnostic internal-audit",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": true,
        "openWorldHint": false
      },
    "inputSchema": {
      "type": "object",
      "properties": {},
      "additionalProperties": false
    }
  },
  {
    "name": "list_tools",
    "title": "List a server's tools",
    "description": "[DIRECTIVE: Server Tool Enumerator] Dumps the flat list of all registered tool names from a specific sub-server. Use when explicitly prompted to view a server's complete API index. Keywords: dump-api-list flat-tool-index enumerate-server-tools",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": true,
        "openWorldHint": false
      },
    "inputSchema": {
      "type": "object",
      "properties": {
        "server_name": {
          "type": "string",
          "description": "The sub-server whose tools should be exhaustively indexed."
        },
        "options": {
          "type": "object",
          "description": "Optional listing controls.",
          "properties": {
            "max_tools": {
              "type": "integer",
              "description": "Maximum number of tools to return. Defaults to 1000; no upper bound.",
              "minimum": 1,
              "default": 1000
            }
          },
          "additionalProperties": false
        }
      },
      "additionalProperties": false
    }
  },
  {
    "name": "semantic_similarity",
    "title": "Find overlapping tools",
    "description": "[DIRECTIVE: Description Redundancy Auditor] Computes the cosine distance between tool schemas across the ecosystem to identify overlapping descriptions or naming redundancies. Keywords: cosine-distance description-overlap naming-redundancy audit-similarity",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": false,
        "destructiveHint": false,
        "idempotentHint": true,
        "openWorldHint": false
      },
    "inputSchema": {
      "type": "object",
      "properties": {
        "servers": {
          "type": "string",
          "description": "Space-separated sub-server names to evaluate."
        },
        "artifact_path": {
          "type": "string",
          "description": "Optional file path where the deduplication markdown map artifact should be written."
        }
      },
      "additionalProperties": false
    }
  },
  {
    "name": "query_compliance",
    "title": "Search coding standards",
    "description": "[DIRECTIVE: Vector Standards Finder] Searches the vector database for standards, mathematical design rules, and project-specific compliance guidelines using semantic embeddings. Keywords: find-standards compliance-rules query-embeddings design-boundaries",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": true,
        "openWorldHint": false
      },
    "inputSchema": {
      "type": "object",
      "properties": {
        "query": {
          "type": "string",
          "description": "Text query to evaluate against standards embeddings."
        }
      },
      "required": [
        "query"
      ],
      "additionalProperties": false
    }
  },
  {
    "name": "execute_pipeline",
    "title": "Run an analysis pipeline",
    "description": "[DIRECTIVE: Pipeline Workflow Runner] Runs an orchestrated multi-stage analysis and mutation DAG. Two modes: fresh (target + intent, optional dry_run to preview and store a plan) or continuation (session_id alone to run a stored plan, session_id + reject: true to discard it). Keywords: run-workflow-dag sequential-pipeline execute-stages workflow-orchestrator",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": false,
        "destructiveHint": true,
        "idempotentHint": false,
        "openWorldHint": true
      },
    "inputSchema": {
      "type": "object",
      "properties": {
        "session_id": {
          "type": "string",
          "description": "Pipeline session id. On a fresh call: optional correlation label (a dry run always issues its own server id). To continue a stored plan: pass the issued session_id alone. To discard it: pass session_id with reject: true."
        },
        "target": {
          "type": "string",
          "description": "Absolute path to the project root or package to analyze and refactor. Required for a fresh run (with intent); omit when continuing with session_id."
        },
        "intent": {
          "type": "string",
          "description": "The goal/objective of the pipeline run. Required for a fresh run (with target); omit when continuing with session_id."
        },
        "plan_hash": {
          "type": "string",
          "description": "Optional SHA-256 hash of an approved implementation plan for MUTATOR integrity verification. On a continuation it must equal the stored plan_hash, if any."
        },
        "dry_run": {
          "type": "boolean",
          "description": "When true on a fresh run, compose and store the plan without executing it; returns the issued session_id and the approval disclosure. Not accepted on a continuation."
        },
        "target_roles": {
          "type": "array",
          "items": { "type": "string" },
          "description": "Optional role filter (e.g. ['ANALYZER', 'CRITIC']) for a fresh run. Only tools matching these roles enter the DAG. Not accepted on a continuation."
        },
        "reject": {
          "type": "boolean",
          "description": "Set to true with session_id to discard a stored plan; nothing runs. Requires session_id and no fresh-run arguments."
        }
      },
      "additionalProperties": false,
      "minProperties": 1
    }
  },
  {
    "name": "validate_pipeline_step",
    "title": "Check a pipeline step's output",
    "description": "[DIRECTIVE: Pipeline Step Evaluator] Evaluates if the output of a specific workflow step meets the defined completion boundaries and post-run criteria. Keywords: evaluate-step-output verify-step-completion check-bounds post-step-check",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": true,
        "openWorldHint": false
      },
    "inputSchema": {
      "type": "object",
      "properties": {
        "step_name": {
          "type": "string",
          "description": "Workflow step to evaluate."
        },
        "step_output": {
          "type": "string",
          "description": "The output from the pipeline step to validate."
        },
        "project_path": {
          "type": "string",
          "description": "Absolute path to the project being analyzed."
        }
      },
      "required": [
        "step_name",
        "step_output"
      ],
      "additionalProperties": false
    }
  },
  {
    "name": "cross_server_quality_gate",
    "title": "Check pipeline readiness",
    "description": "[DIRECTIVE: Execution Safety Firewall] A blocking pre-execution quality gate that prevents file mutations until all safety limits and implementation plans are verified. Keywords: safety-firewall block-mutations verify-plan-safety pre-mutation-gate",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": true,
        "openWorldHint": false
      },
    "inputSchema": {
      "type": "object",
      "properties": {
        "project_path": {
          "type": "string",
          "description": "Absolute path to the project root."
        },
        "plan_hash": {
          "type": "string",
          "description": "Approved plan SHA-256 signature to enforce."
        }
      },
      "required": [
        "project_path",
        "plan_hash"
      ],
      "additionalProperties": false
    }
  },
  {
    "name": "generate_audit_report",
    "title": "Build a pipeline audit report",
    "description": "[DIRECTIVE: Telemetry Diff Publisher] Generates the final git diff and audit telemetry report upon pipeline completion. Keywords: publish-git-diff generate-audit-report finalize-session telemetry-report",
    "category": "orchestrator",
      "annotations": {
        "readOnlyHint": false,
        "destructiveHint": false,
        "idempotentHint": true,
        "openWorldHint": false
      },
    "inputSchema": {
      "type": "object",
      "properties": {
        "target": {
          "type": "string",
          "description": "Absolute path to the project root."
        },
        "session_id": {
          "type": "string",
          "description": "Orchestrator tracking session ID to close."
        }
      },
      "required": [
        "target",
        "session_id"
      ],
      "additionalProperties": false
    }
  }
]
`)
