package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var showVarsCmd = &cobra.Command{
	Use:   "showvars",
	Short: "Show available environment variables for configuration",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("=== MagicTools Environment Variables ===")
		fmt.Println("\nThe following environment variables can be used to configure or override MagicTools behavior:")
		fmt.Println()

		vars := []struct {
			Name        string
			Description string
		}{
			{"MCP_MAGIC_TOOLS_CONFIG_DIR", "Overrides base configuration directory (default: ~/.config/mcp-server-magictools)"},
			{"MCP_MAGIC_TOOLS_CONFIG", "Overrides exact path to config.yaml"},
			{"MCP_MAGIC_TOOLS_DB_PATH", "Overrides exact path to BadgerDB"},
			{"MCP_SESSION_TIMEOUT_SECONDS", "IDE session timeout duration in seconds"},
			{envGeminiAPIKey, "Google Gemini API Key"},
			{envOpenAIAPIKey, "OpenAI API Key"},
			{envClaudeAPIKey, "Anthropic Claude API Key"},
			{"MAGIC_TOOLS_*", "Viper auto-env overrides for deep config fields (e.g. MAGIC_TOOLS_INTELLIGENCE_MODEL)"},
		}

		for _, v := range vars {
			currentVal := os.Getenv(v.Name)
			status := ""
			if currentVal != "" {
				if v.Name == "GEMINI_API_KEY" || v.Name == "OPENAI_API_KEY" || v.Name == "CLAUDE_API_KEY" {
					status = " (set: ****)"
				} else {
					status = fmt.Sprintf(" (set: %s)", currentVal)
				}
			}
			fmt.Printf("  %s%s\n", v.Name, status)
			fmt.Printf("    %s\n\n", v.Description)
		}
	},
}

func init() {
	rootCmd.AddCommand(showVarsCmd)
}
