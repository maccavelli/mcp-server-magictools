package handler

// InternalToolsInventoryJSON provides a static, SDK-adherent definition
// for all internal magictools orchestrator capabilities.
// This list is used to guarantee tool integrity during tools/list responses.
var InternalToolsInventoryJSON = []byte(`
[
  {
    "name": "sync_servers",
    "description": "[DIRECTIVE: Lifecycle Sync] Refreshes and updates the local metadata index and tool schemas of registered sub-servers. Use when new tools are added or schemas changed. Keywords: refresh-schema sync-metadata metadata-index update-registry",
    "category": "orchestrator",
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
    "description": "[DIRECTIVE: Process Standby] Wakes sleeping, inactive, or suspended sub-server processes to hot standby to eliminate cold-start tool latencies. Keywords: warm-up-servers unpause-processes prevent-cold-starts wake-standby",
    "category": "orchestrator",
    "inputSchema": {
      "type": "object",
      "properties": {},
      "additionalProperties": false
    }
  },
  {
    "name": "reload_servers",
    "description": "[DIRECTIVE: Process Reboot] Force-terminates and reboots sub-server processes to recover from socket timeouts, process hangs, or memory leaks. Keywords: hard-restart crash-recovery reboot-process kill-hangs",
    "category": "orchestrator",
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
    "description": "[DIRECTIVE: Orchestrator Log Dump] Retrieves raw, unfiltered stdout/stderr logs directly from the magictools orchestrator process itself. Use strictly for debugging orchestrator-level boot errors, proxy routing failures, or config crashes. Keywords: raw-stdout log-dump debug-orchestrator process-stderr",
    "category": "orchestrator",
    "inputSchema": {
      "type": "object",
      "properties": {
        "session_id": {
          "type": "string",
          "description": "Optional session correlation ID to isolate process logs for a specific run."
        },
        "max_lines": {
          "type": "integer",
          "description": "Integer limit of recent raw log lines to return (0-1000). Defaults to 25."
        }
      },
      "additionalProperties": false
    }
  },
  {
    "name": "get_session_stats",
    "description": "[DIRECTIVE: Telemetry Overhead Analyzer] Emits dynamic network metrics, overhead latencies, and payload throughput calculations for active sub-server channels. Keywords: network-latency throughput-bytes payload-sizes serialization-time",
    "category": "orchestrator",
    "inputSchema": {
      "type": "object",
      "properties": {},
      "additionalProperties": false
    }
  },
  {
    "name": "get_health_report",
    "description": "[DIRECTIVE: Server Health Status] Inspects and returns the up/down running status, uptime duration, memory allocation, and CPU load for all registered MCP servers. Keywords: query-uptime server-up-down process-load memory-rss",
    "category": "orchestrator",
    "inputSchema": {
      "type": "object",
      "properties": {},
      "additionalProperties": false
    }
  },
  {
    "name": "analyze_system_logs",
    "description": "[DIRECTIVE: Semantic Log Parser] Analyzes and filters orchestrator logs to detect organic error patterns, trace exception severity levels, and isolate multi-server error telemetry. Keywords: error-telemetry filter-severity trace-exception parse-syslog",
    "category": "orchestrator",
    "inputSchema": {
      "type": "object",
      "properties": {
        "server_id": {
          "type": "string",
          "description": "Server identifier to narrow down log evaluation (e.g. 'ddg-search')."
        },
        "lines": {
          "type": "integer",
          "description": "Integer limit of logs to parse (default 50)."
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
    "description": "[DIRECTIVE: Tool Discovery Catalog] Resolves natural language intent queries into matched tool names, server URNs, and usage schemas. MANDATE: You MUST run this tool with full_schema=true before invoking any downstream tool via call_proxy to verify its required parameters. Keywords: find-tool tool-catalog schema-lookup intent-resolve",
    "category": "orchestrator",
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
    "description": "[DIRECTIVE: Proxy Execution Runner] Executes a verified tool URN with the mapped argument payload on a downstream MCP sub-server. MANDATE: Before calling this tool for a URN, you MUST first run align_tools with full_schema=true to retrieve its exact JSON schema and parameter requirements. Guessed arguments are strictly prohibited. Keywords: execute-urn run-downstream-tool dispatch-payload proxy-execution",
    "category": "orchestrator",
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
    "description": "[DIRECTIVE: Runtime Config Modifier] Writes persistent configuration changes (e.g. logLevel, trustServers) to storage instantly. Keywords: modify-config config-yaml change-parameter persist-setting",
    "category": "orchestrator",
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
            "validateProxyCalls",
            "pinnedServers",
            "trustServers",
            "squeezeBypass"
          ]
        },
        "value": {
          "type": "string",
          "description": "String payload value to write to configuration."
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
    "description": "[DIRECTIVE: Internal Diagnostic Auditor] Audits the orchestrator's database sync status, cache hits/misses, and internal memory buffers. Keywords: cache-efficiency database-sync self-diagnostic internal-audit",
    "category": "orchestrator",
    "inputSchema": {
      "type": "object",
      "properties": {},
      "additionalProperties": false
    }
  },
  {
    "name": "list_tools",
    "description": "[DIRECTIVE: Server Tool Enumerator] Dumps the flat list of all registered tool names from a specific sub-server. Use when explicitly prompted to view a server's complete API index. Keywords: dump-api-list flat-tool-index enumerate-server-tools",
    "category": "orchestrator",
    "inputSchema": {
      "type": "object",
      "properties": {
        "server_name": {
          "type": "string",
          "description": "The sub-server whose tools should be exhaustively indexed."
        },
        "options": {
          "type": "object",
          "properties": {
            "max_tools": {
              "type": "integer",
              "description": "Maximum number of tools to return."
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
    "description": "[DIRECTIVE: Description Redundancy Auditor] Computes the cosine distance between tool schemas across the ecosystem to identify overlapping descriptions or naming redundancies. Keywords: cosine-distance description-overlap naming-redundancy audit-similarity",
    "category": "orchestrator",
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
    "description": "[DIRECTIVE: Vector Standards Finder] Searches the vector database for standards, mathematical design rules, and project-specific compliance guidelines using semantic embeddings. Keywords: find-standards compliance-rules query-embeddings design-boundaries",
    "category": "orchestrator",
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
    "description": "[DIRECTIVE: Pipeline Workflow Runner] Runs an orchestrated multi-stage analysis and mutation DAG from start to finish. Keywords: run-workflow-dag sequential-pipeline execute-stages workflow-orchestrator",
    "category": "orchestrator",
    "inputSchema": {
      "type": "object",
      "properties": {
        "session_id": {
          "type": "string",
          "description": "Optional CSSA session correlation ID. Auto-generated if omitted. For CONTINUATION MODE: pass session_id WITHOUT target/intent to resume a paused pipeline after human approval."
        },
        "target": {
          "type": "string",
          "description": "Absolute path to the project root or package to analyze and refactor."
        },
        "intent": {
          "type": "string",
          "description": "The goal/objective of the pipeline run."
        },
        "plan_hash": {
          "type": "string",
          "description": "Optional SHA-256 hash of an approved implementation plan for MUTATOR integrity verification."
        },
        "dry_run": {
          "type": "boolean",
          "description": "When true, return the DAG plan as markdown without executing. Equivalent to the old compose_pipeline preview."
        },
        "target_roles": {
          "type": "array",
          "items": { "type": "string" },
          "description": "Optional role filter (e.g. ['ANALYZER', 'CRITIC']). Only tools matching these roles enter the DAG."
        },
        "reject": {
          "type": "boolean",
          "description": "Set to true when continuing a paused pipeline to reject the plan and abort without mutations."
        }
      },
      "additionalProperties": false,
      "anyOf": [
        {
          "required": [
            "target",
            "intent"
          ]
        },
        {
          "required": [
            "session_id"
          ]
        }
      ]
    }
  },
  {
    "name": "validate_pipeline_step",
    "description": "[DIRECTIVE: Pipeline Step Evaluator] Evaluates if the output of a specific workflow step meets the defined completion boundaries and post-run criteria. Keywords: evaluate-step-output verify-step-completion check-bounds post-step-check",
    "category": "orchestrator",
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
    "description": "[DIRECTIVE: Execution Safety Firewall] A blocking pre-execution quality gate that prevents file mutations until all safety limits and implementation plans are verified. Keywords: safety-firewall block-mutations verify-plan-safety pre-mutation-gate",
    "category": "orchestrator",
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
    "description": "[DIRECTIVE: Telemetry Diff Publisher] Generates the final git diff and audit telemetry report upon pipeline completion. Keywords: publish-git-diff generate-audit-report finalize-session telemetry-report",
    "category": "orchestrator",
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
