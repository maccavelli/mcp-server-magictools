package handler

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterPrompts registers the system prompts for the magictools orchestrator.
func (h *OrchestratorHandler) RegisterPrompts(s *mcp.Server) {
	s.AddPrompt(&mcp.Prompt{
		Name:        "pipeline-start",
		Description: "Initializes the Autonomous DAG Orchestrator to dynamically generate and execute a pipeline based on a natural language intent or an artifact.",
		Arguments: []*mcp.PromptArgument{
			{
				Name:        "load",
				Description: "The context to load. Can be a natural language prompt, a filepath, a project directory, or an artifact name (e.g., 'implementation_plan').",
				Required:    true,
			},
		},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		load := ""
		if req.Params != nil && req.Params.Arguments != nil {
			load = req.Params.Arguments["load"]
		}

		if load == "" {
			return nil, fmt.Errorf("the 'load' argument is required")
		}

		text := fmt.Sprintf(`You are the Autonomous DAG Orchestrator. The user has provided the following payload to load: "%s".

Because you govern the dynamic execution of Directed Acyclic Graphs (DAGs) rather than a rigid state machine, you DO NOT execute code natively. Your sole purpose is to prime, observe, and manage the 'execute_pipeline' DAG engine. You must follow these strict operational rules:

1. PREPROCESSING (Data Loss Prevention): Evaluate the 'load' payload. If it references a structured artifact (e.g. JSON, YAML, AST), you MUST NOT summarize it. Instead, embed the exact absolute file path into the 'intent' string so downstream nodes can losslessly parse it. For unstructured text, extract only the core semantic constraints to prevent token bloat.
2. TRANSLATION: Synthesize the context into a focused 'intent' string and determine the precise absolute 'target' path for the operation.
3. HUMAN-IN-THE-LOOP GATE (Blast Radius): You MUST present your derived 'intent', 'target', and a 'Blast Radius Assessment' (detailing exact sub-directories/files affected) to the user for explicit confirmation before proceeding.
4. INITIALIZATION (Self-Healing Loop): Once approved, invoke 'execute_pipeline' with the 'intent', 'target', and strictly setting 'dry_run: true'. If 'execute_pipeline' returns a fatal compilation error instead of a session_id, you MUST evaluate the error trace and regenerate the mapping variables rather than blindly continuing.
5. CONTINUATION (Abort Protocol): If the generated sequence plan is sound, extract the 'session_id' and invoke 'execute_pipeline' AGAIN using ONLY the 'session_id' (omit intent/target). If the user REJECTS the previewed DAG, you MUST invoke 'execute_pipeline' using the 'session_id' AND the explicit 'reject: true' parameter to cleanly terminate the paused backend sequence.`, load)

		return &mcp.GetPromptResult{
			Description: "Autonomous DAG Orchestrator Instructions",
			Messages: []*mcp.PromptMessage{
				{
					Role: mcp.Role("user"),
					Content: &mcp.TextContent{
						Text: text,
					},
				},
			},
		}, nil
	})
}

// MinifyPromptResponse provides type-safe payload minification.
// It removes empty Description fields to neutralize runtime type-assertion panics in IDE renderers.
// It also enforces a strict 5MB character/byte cap limit on all TextContent to prevent OOM crashes.
func (h *OrchestratorHandler) MinifyPromptResponse(res *mcp.GetPromptResult) *mcp.GetPromptResult {
	if res == nil {
		return nil
	}
	if res.Description == "" {
		res.Description = "Proxy Prompt"
	}

	// Enforce 5MB limit
	const maxBytes = 5 * 1024 * 1024
	if len(res.Messages) > 0 {
		for _, msg := range res.Messages {
			if content, ok := msg.Content.(*mcp.TextContent); ok {
				if len(content.Text) > maxBytes {
					content.Text = content.Text[:maxBytes] + "\n\n[TRUNCATED: Exceeded 5MB Limit]"
				}
			}
		}
	}

	return res
}

// PromptsMiddleware intercepts 'prompts/list' and 'prompts/get' to inject Tier 1 cache results
// and route namespaced requests to downstream servers.
func (h *OrchestratorHandler) PromptsMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		switch method {
		case "prompts/list":
			return h.handlePromptsList(ctx, next, method, req)
		case "prompts/get":
			return h.handlePromptsGet(ctx, next, method, req)
		default:
			return next(ctx, method, req)
		}
	}
}
