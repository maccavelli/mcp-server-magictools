# Documentation

This tree holds three kinds of document:

- reports that measured something;
- the decisions behind the work;
- guides you follow to do a thing.

- **[architecture.md](architecture.md)** — how the pieces fit together, as they are now.
- **guides/** — task-shaped walkthroughs for install, setup, clients, transports, configuration, the CLI, MCP tools, the dashboard, operations, safe testing, and the repository audit.
- **[decisions/](decisions/)** — numbered MADR/PLAN pairs.
- **reports/** — what was observed, deciding nothing.

## Records

| Number | Kind | Record | Status |
| :--- | :--- | :--- | :--- |
| 0001 | MADR | [0001-MADR-make-configure-updates-lossless.md](decisions/0001-MADR-make-configure-updates-lossless.md) | proposed |
| 0001 | PLAN | [0001-PLAN-make-configure-updates-lossless.md](decisions/0001-PLAN-make-configure-updates-lossless.md) | proposed |
| 0002 | MADR | [0002-MADR-make-configure-truthful-secret-safe-and-testable.md](decisions/0002-MADR-make-configure-truthful-secret-safe-and-testable.md) | proposed |
| 0002 | PLAN | [0002-PLAN-make-configure-truthful-secret-safe-and-testable.md](decisions/0002-PLAN-make-configure-truthful-secret-safe-and-testable.md) | proposed |
| 0003 | MADR | [0003-MADR-bound-the-orphan-reaper-process-walk.md](decisions/0003-MADR-bound-the-orphan-reaper-process-walk.md) | accepted |
| 0003 | PLAN | [0003-PLAN-bound-the-orphan-reaper-process-walk.md](decisions/0003-PLAN-bound-the-orphan-reaper-process-walk.md) | implemented |
| 0004 | MADR | [0004-MADR-a-parse-error-must-not-replace-the-operators-server-registry.md](decisions/0004-MADR-a-parse-error-must-not-replace-the-operators-server-registry.md) | proposed |
| 0004 | PLAN | [0004-PLAN-a-parse-error-must-not-replace-the-operators-server-registry.md](decisions/0004-PLAN-a-parse-error-must-not-replace-the-operators-server-registry.md) | implemented |
| 0005 | MADR | [0005-MADR-make-execute-pipeline-schema-and-handler-agree.md](decisions/0005-MADR-make-execute-pipeline-schema-and-handler-agree.md) | accepted |
| 0005 | PLAN | [0005-PLAN-make-execute-pipeline-schema-and-handler-agree.md](decisions/0005-PLAN-make-execute-pipeline-schema-and-handler-agree.md) | in-progress |
| 0006 | MADR | [0006-MADR-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md](decisions/0006-MADR-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md) | accepted |
| 0006 | PLAN | [0006-PLAN-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md](decisions/0006-PLAN-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md) | in-progress |
| 0007 | MADR | [0007-MADR-install-the-fleet-agent-workspace.md](decisions/0007-MADR-install-the-fleet-agent-workspace.md) | accepted |
| 0007 | PLAN | [0007-PLAN-install-the-fleet-agent-workspace.md](decisions/0007-PLAN-install-the-fleet-agent-workspace.md) | implemented |
| 0008 | MADR | [0008-MADR-redesign-the-list-tools-middleware-under-the-complexity-limit.md](decisions/0008-MADR-redesign-the-list-tools-middleware-under-the-complexity-limit.md) | accepted |
| 0008 | PLAN | [0008-PLAN-redesign-the-list-tools-middleware-under-the-complexity-limit.md](decisions/0008-PLAN-redesign-the-list-tools-middleware-under-the-complexity-limit.md) | implemented |
| 0009 | MADR | [0009-MADR-stop-store-close-racing-background-writes.md](decisions/0009-MADR-stop-store-close-racing-background-writes.md) | accepted |
| 0009 | PLAN | [0009-PLAN-stop-store-close-racing-background-writes.md](decisions/0009-PLAN-stop-store-close-racing-background-writes.md) | implemented |
| 0010 | MADR | [0010-MADR-make-tests-unable-to-signal-real-processes.md](decisions/0010-MADR-make-tests-unable-to-signal-real-processes.md) | accepted |
| 0010 | PLAN | [0010-PLAN-make-tests-unable-to-signal-real-processes.md](decisions/0010-PLAN-make-tests-unable-to-signal-real-processes.md) | implemented |
| 0011 | MADR | [0011-MADR-make-two-telemetry-tests-own-their-state-under-repeated-rounds.md](decisions/0011-MADR-make-two-telemetry-tests-own-their-state-under-repeated-rounds.md) | accepted |
| 0011 | PLAN | [0011-PLAN-make-two-telemetry-tests-own-their-state-under-repeated-rounds.md](decisions/0011-PLAN-make-two-telemetry-tests-own-their-state-under-repeated-rounds.md) | implemented |
| 0012 | MADR | [0012-MADR-make-the-per-user-launchagent-lifecycle-correct-restartable-and-verifiable.md](decisions/0012-MADR-make-the-per-user-launchagent-lifecycle-correct-restartable-and-verifiable.md) | accepted |
| 0012 | PLAN | [0012-PLAN-make-the-per-user-launchagent-lifecycle-correct-restartable-and-verifiable.md](decisions/0012-PLAN-make-the-per-user-launchagent-lifecycle-correct-restartable-and-verifiable.md) | implemented |
| 0013 | MADR | [0013-MADR-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md](decisions/0013-MADR-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md) | accepted |
| 0013 | PLAN | [0013-PLAN-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md](decisions/0013-PLAN-flatten-execute-pipelines-schema-and-build-the-continuation-it-advertises.md) | in-progress |
| 0014 | REPORT | [0014-REPORT-mcp-tool-schema-and-description-adherence.md](reports/0014-REPORT-mcp-tool-schema-and-description-adherence.md) | complete |
| 0015 | MADR | [0015-MADR-carry-the-mcp-standards-title-annotations-and-constraint-keywords-on-every-own-tool.md](decisions/0015-MADR-carry-the-mcp-standards-title-annotations-and-constraint-keywords-on-every-own-tool.md) | proposed |

## I want to…

| I want to… | Start here |
| :--- | :--- |
| understand how this fits together | [architecture.md](architecture.md) |
| know the rules agents follow in this repository | [CLAUDE.md](../CLAUDE.md) |
| know why the repository has the files and gates it has | [0007-MADR-install-the-fleet-agent-workspace.md](decisions/0007-MADR-install-the-fleet-agent-workspace.md) |
| install or upgrade the binary | [Platform installation](guides/platform-installation.md) |
| complete a first working setup | [Getting started](guides/getting-started.md) |
| connect an MCP client | [Client integration](guides/client-integration.md) |
| choose stdio, proxy, or HTTP | [Services and transports](guides/services-and-transports.md) |
| configure providers, search, or managed servers | [Configuration](guides/configuration.md) |
| look up a CLI command | [CLI reference](guides/cli-reference.md) |
| understand or invoke MCP tools | [MCP tools and orchestration](guides/mcp-tools.md) |
| inspect health, logs, or the dashboard | [Dashboard and observability](guides/dashboard-and-observability.md) |
| back up data or review security boundaries | [Operations and security](guides/operations-and-security.md) |
| run the tests without killing the live service's sub-servers | [Testing and process safety](guides/testing-and-process-safety.md) |
| read the code-grounded repository audit | [Repository assessment](guides/repository-assessment.md) |
