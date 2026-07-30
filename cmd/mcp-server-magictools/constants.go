package main

const (
	cmdName           = "mcp-server-magictools"
	goOSWindows       = "windows"
	goOSLinux         = "linux"
	goOSDarwin        = "darwin"
	systemctlUserFlag = "--user"
	envGeminiAPIKey   = "GEMINI_API_KEY" //nolint:gosec // environment variable name, not a secret value
	envOpenAIAPIKey   = "OPENAI_API_KEY" //nolint:gosec // environment variable name, not a secret value
	envClaudeAPIKey   = "CLAUDE_API_KEY" //nolint:gosec // environment variable name, not a secret value
	dashNA            = "N/A"
	colMetric         = "Metric"
	colValue          = "Value"
	colStatus         = "Status"
	colServer         = "Server"
	colCalls          = "Calls"
	colURN            = "URN"
	colProperty       = "Property"
	colDelta5m        = "5m Δ"
	colDelta15m       = "15m Δ"
	colDelta1h        = "1h Δ"
)
