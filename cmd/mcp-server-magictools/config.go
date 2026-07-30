// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/maccavelli/mcplib/llmprovider"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

// ConfigSyncFunc is a callback for the config sync logic
var ConfigSyncFunc func(configPath string) error

var forceInit bool
var nonInteractive bool

var configureCmd = &cobra.Command{
	Use:     "configure",
	Aliases: []string{"init"},
	Short:   "Interactive setup wizard for the MagicTools orchestrator",
	RunE:    runConfigure,
}

// providerEnvVars maps provider names to their standard environment variable names.
var providerEnvVars = map[string]string{
	"gemini": "GEMINI_API_KEY",
	"openai": "OPENAI_API_KEY",
	"claude": "CLAUDE_API_KEY",
	"voyage": "VOYAGE_API_KEY",
}

func runConfigure(cmd *cobra.Command, args []string) error {
	var configDir string
	var configPath string

	if CfgPath != "" {
		configPath = CfgPath
		configDir = filepath.Dir(configPath)
	} else {
		configDir = config.DefaultConfigDir()
		configPath = filepath.Join(configDir, config.ToolConfigFile)
	}
	serversPath := filepath.Join(configDir, config.ServersConfigFile)
	overridesPath := filepath.Join(configDir, "tool_overrides.yaml")

	if err := ensureInitialized(configDir, configPath, serversPath, overridesPath, forceInit); err != nil {
		return err
	}

	cfg, err := config.New(Version, CfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if nonInteractive || (!forceInit && !term.IsTerminal(int(os.Stdin.Fd()))) {
		// If we are non-interactive, bypass the wizard naturally.
		// If they explicitly passed --non-interactive, also bypass.
		pterm.Info.Println("Running in non-interactive mode. Bypassing wizard.")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)

	for {
		pterm.DefaultHeader.WithFullWidth().Println("MagicTools Configuration Wizard")
		pterm.Info.Println("Configure your MagicTools orchestrator. Each option can be\nconfigured independently — changes are saved immediately.")

		options := []string{
			"1) Fast Tier LLM        — Primary model for hydration & intelligence",
			"2) Thinking Tier LLM    — Dedicated model for deep reasoning (optional)",
			"3) Embedding Engine     — Vector search model for semantic alignment",
			"4) Shared LLM Backplane — Centralized LLM service for sub-servers",
			"5) Show Current Config  — Display active configuration",
			"0) Exit",
		}

		choice, _ := pterm.DefaultInteractiveSelect.
			WithDefaultText("Select option").
			WithOptions(options).
			Show()

		if len(choice) > 0 {
			switch choice[:1] {
			case "1":
				configureFastTier(cfg, reader)
			case "2":
				configureThinkingTier(cfg, reader)
			case "3":
				configureEmbeddingEngine(cfg, reader)
			case "4":
				configureBackplane(cfg)
			case "5":
				showCurrentConfig(cfg)
			case "0":
				pterm.Success.Println("Configuration complete.")
				return nil
			}
		}
	}
}

// --- Task 5.2: Option 1 — Fast Tier LLM ---

func configureFastTier(cfg *config.Config, reader *bufio.Reader) {
	pterm.DefaultSection.Println("Fast Tier LLM Configuration")

	provider := selectProvider()
	if provider == "" {
		return
	}

	var apiKey string
	if provider == "ollama" {
		ollamaURL := promptOllamaURL()
		cfg.Intelligence.APIURL = ollamaURL
		cfg.Intelligence.EmbeddingAPIURL = "" // Fast tier doesn't use embedding URL
	} else {
		apiKey = resolveAPIKey(cfg.Intelligence.APIKey, provider, reader)
		if apiKey == "" {
			fmt.Println("No API key provided. Returning to menu.")
			return
		}
	}

	// Discover models
	pterm.Info.Println("Fetching available models...")
	ctx := context.Background()
	models, err := llmprovider.ListAvailableModels(ctx, provider, apiKey)
	if err != nil || len(models) == 0 {
		pterm.Warning.Println("Could not reach API, using default model list.")
		models = staticModelsForProvider(provider)
	}

	selectedModel := selectModel(models)
	if selectedModel == "" {
		pterm.Info.Println("No model selected. Returning to menu.")
		return
	}

	// Build fallback list from remaining models
	var fallbacks []string
	for _, m := range models {
		if m != selectedModel {
			fallbacks = append(fallbacks, m)
		}
	}

	// Apply to config
	cfg.Intelligence.Provider = provider
	cfg.Intelligence.APIKey = apiKey
	cfg.Intelligence.Model = selectedModel
	cfg.Intelligence.FallbackModels = fallbacks

	if cfg.Intelligence.RetryCount <= 0 {
		cfg.Intelligence.RetryCount = 2
	}
	if cfg.Intelligence.RetryDelay <= 0 {
		cfg.Intelligence.RetryDelay = 5
	}
	if cfg.Intelligence.TimeoutSeconds <= 0 {
		cfg.Intelligence.TimeoutSeconds = 120
	}

	if err := cfg.SaveConfiguration(); err != nil {
		pterm.Error.Printf("Failed to save configuration: %v\nYour changes were NOT saved.\n", err)
		return
	}

	pterm.Success.Println("Fast Tier configured!")
	pterm.Info.Printf("Provider:  %s\n", provider)
	pterm.Info.Printf("Model:     %s\n", selectedModel)
	if len(fallbacks) > 0 {
		pterm.Info.Printf("Fallbacks: %s\n", strings.Join(fallbacks, ", "))
	}
}

// --- Task 5.3: Option 2 — Thinking Tier LLM ---

func configureThinkingTier(cfg *config.Config, reader *bufio.Reader) {
	pterm.DefaultSection.Println("Thinking Tier LLM Configuration")
	pterm.Info.Println("The Thinking Tier provides a dedicated model for deep reasoning tasks\n(Socratic analysis, complex code review). If not configured, the Fast\nTier model handles all requests.")

	options := []string{
		"1) Gemini    (Google Gemini API)",
		"2) Claude    (Anthropic Claude API)",
		"3) OpenAI    (OpenAI API)",
		"4) Ollama    (Local Ollama API)",
		"0) None/Clear (disable thinking tier)",
	}

	choice, _ := pterm.DefaultInteractiveSelect.
		WithDefaultText("Select provider").
		WithOptions(options).
		Show()

	if len(choice) > 0 && choice[:1] == "0" {
		cfg.Intelligence.ThinkingProvider = ""
		cfg.Intelligence.ThinkingModel = ""
		cfg.Intelligence.ThinkingAPIKey = ""
		if err := cfg.SaveConfiguration(); err != nil {
			pterm.Error.Printf("Failed to save configuration: %v\nYour changes were NOT saved.\n", err)
			return
		}
		pterm.Success.Println("Thinking Tier cleared.")
		return
	}

	provider := choiceToProvider(choice)
	if provider == "" {
		pterm.Warning.Println("Invalid choice. Returning to menu.")
		return
	}

	var apiKey string
	if provider != "ollama" {
		// Cross-tier key reuse
		if provider == cfg.Intelligence.Provider && cfg.Intelligence.APIKey != "" {
			fmt.Printf("You already have a %s API key configured for the Fast Tier.\n", provider)
			fmt.Print("Press Enter to reuse it, or enter a different key: ")
			override := readHiddenSecret(reader)
			if override != "" {
				apiKey = override
			} else {
				apiKey = cfg.Intelligence.APIKey
			}
		} else {
			apiKey = resolveAPIKey(cfg.Intelligence.ThinkingAPIKey, provider, reader)
			if apiKey == "" {
				pterm.Warning.Println("No API key provided. Returning to menu.")
				return
			}
		}
	}

	// Discover models — present top 3 thinking-capable
	pterm.Info.Println("Fetching available models...")
	ctx := context.Background()
	models, err := llmprovider.ListAvailableModels(ctx, provider, apiKey)
	if err != nil || len(models) == 0 {
		models = staticModelsForProvider(provider)
	}
	if len(models) > 3 {
		models = models[:3]
	}

	selectedModel := selectModel(models)
	if selectedModel == "" {
		pterm.Info.Println("No model selected. Returning to menu.")
		return
	}

	cfg.Intelligence.ThinkingProvider = provider
	cfg.Intelligence.ThinkingModel = selectedModel
	cfg.Intelligence.ThinkingAPIKey = apiKey

	if err := cfg.SaveConfiguration(); err != nil {
		pterm.Error.Printf("Failed to save configuration: %v\nYour changes were NOT saved.\n", err)
		return
	}

	pterm.Success.Println("Thinking Tier configured!")
	pterm.Info.Printf("Provider: %s\n", provider)
	pterm.Info.Printf("Model:    %s\n", selectedModel)
}

// --- Task 5.4: Option 3 — Embedding Engine ---

func configureEmbeddingEngine(cfg *config.Config, reader *bufio.Reader) {
	pterm.DefaultSection.Println("Embedding Engine Configuration")

	options := []string{
		"1) gemini (Google Gemini API)",
		"2) voyage (Claude Embeddings via Voyage API)",
		"3) openai (OpenAI API)",
		"4) ollama (Local API)",
	}

	choiceStr, _ := pterm.DefaultInteractiveSelect.
		WithDefaultText("Which Provider would you like to use for Vector Embeddings?").
		WithOptions(options).
		Show()

	if len(choiceStr) > 0 {
		choiceStr = choiceStr[:1]
	}

	var provider string
	var models []string
	var dimsMap map[string]int

	switch choiceStr {
	case "1":
		provider = "gemini"
		models = []string{
			"gemini-embedding-2 (768 dims)",
			"text-embedding-005 (768 dims)",
			"text-embedding-004 (768 dims)",
			"text-embedding-004 (256 dims)",
		}
		dimsMap = map[string]int{
			"gemini-embedding-2 (768 dims)": 768,
			"text-embedding-005 (768 dims)": 768,
			"text-embedding-004 (768 dims)": 768,
			"text-embedding-004 (256 dims)": 256,
		}
	case "2":
		provider = "voyage"
		models = []string{"voyage-3-lite", "voyage-3", "voyage-code-3"}
		dimsMap = map[string]int{
			"voyage-3-lite": 512,
			"voyage-3":      1024,
			"voyage-code-3": 1024,
		}
	case "3":
		provider = "openai"
		models = []string{
			"text-embedding-3-small (512 dims)",
			"text-embedding-3-small (1536 dims)",
			"text-embedding-3-large (256 dims)",
			"text-embedding-3-large (1024 dims)",
		}
		dimsMap = map[string]int{
			"text-embedding-3-small (512 dims)":  512,
			"text-embedding-3-small (1536 dims)": 1536,
			"text-embedding-3-large (256 dims)":  256,
			"text-embedding-3-large (1024 dims)": 1024,
		}
	case "4":
		provider = "ollama"
		models = []string{"granite-embedding:30m", "snowflake-arctic-embed:33m", "all-minilm:33m", "nomic-embed-text"}
		dimsMap = map[string]int{
			"granite-embedding:30m":      384,
			"snowflake-arctic-embed:33m": 384,
			"all-minilm:33m":             384,
			"nomic-embed-text":           768,
		}
	default:
		pterm.Warning.Println("Invalid choice. Returning to menu.")
		return
	}

	var apiKey string
	var apiURL string
	if provider == "ollama" {
		apiURL = promptOllamaURL()
	} else {
		// Cross-tier key reuse
		if provider == cfg.Intelligence.Provider && cfg.Intelligence.APIKey != "" {
			fmt.Printf("You already have a %s API key configured for the Fast Tier.\n", provider)
			fmt.Print("Press Enter to reuse it, or enter a different key: ")
			override := readHiddenSecret(reader)
			if override != "" {
				apiKey = override
			} else {
				apiKey = cfg.Intelligence.APIKey
			}
		} else if provider == cfg.Intelligence.ThinkingProvider && cfg.Intelligence.ThinkingAPIKey != "" {
			fmt.Printf("You already have a %s API key configured for the Thinking Tier.\n", provider)
			fmt.Print("Press Enter to reuse it, or enter a different key: ")
			override := readHiddenSecret(reader)
			if override != "" {
				apiKey = override
			} else {
				apiKey = cfg.Intelligence.ThinkingAPIKey
			}
		} else {
			apiKey = resolveAPIKey(cfg.Intelligence.EmbeddingAPIKey, provider, reader)
			if apiKey == "" {
				pterm.Warning.Println("No API key provided. Returning to menu.")
				return
			}
		}
	}

	var modelOptions []string
	for i, m := range models {
		label := ""
		if i == 0 && provider == "gemini" {
			label = " (DEFAULT)"
		}
		modelOptions = append(modelOptions, m+label)
	}
	modelOptions = append(modelOptions, "Other (enter manually)")

	modelChoice, _ := pterm.DefaultInteractiveSelect.
		WithDefaultText("Select model").
		WithOptions(modelOptions).
		Show()

	var selectedDisplay string
	if modelChoice == "Other (enter manually)" {
		custom, _ := pterm.DefaultInteractiveTextInput.WithDefaultText("Enter model name").Show()
		selectedDisplay = strings.TrimSpace(custom)
	} else {
		selectedDisplay = strings.TrimSuffix(modelChoice, " (DEFAULT)")
	}

	if selectedDisplay == "" {
		pterm.Info.Println("No model selected. Returning to menu.")
		return
	}

	dims := dimsMap[selectedDisplay]
	actualModel := strings.Split(selectedDisplay, " ")[0] // Strip " (256 dims)"

	if dims == 0 {
		dimOptions := []string{"384", "768", "1024", "1536"}
		dimStr, _ := pterm.DefaultInteractiveSelect.
			WithDefaultText("Select dimensionality for custom model").
			WithOptions(dimOptions).
			WithDefaultOption("768").
			Show()
		fmt.Sscanf(strings.TrimSpace(dimStr), "%d", &dims)
	}

	// HNSW warning if dimensions change
	if cfg.Intelligence.EmbeddingDimensionality > 0 && cfg.Intelligence.EmbeddingDimensionality != dims {
		pterm.Warning.Println("Changing embedding dimensions requires rebuilding the vector index.\nThe index will be rebuilt on next server start.")
	}

	cfg.Intelligence.EmbeddingProvider = provider
	cfg.Intelligence.EmbeddingModel = actualModel
	cfg.Intelligence.EmbeddingAPIKey = apiKey
	if provider == "ollama" {
		cfg.Intelligence.EmbeddingAPIURL = apiURL
	}
	cfg.Intelligence.EmbeddingDimensionality = dims
	cfg.Intelligence.VectorEnabled = true

	if err := cfg.SaveConfiguration(); err != nil {
		pterm.Error.Printf("Failed to save configuration: %v\nYour changes were NOT saved.\n", err)
		return
	}

	pterm.Success.Println("Embedding Engine configured!")
	pterm.Info.Printf("Provider:   %s\n", provider)
	pterm.Info.Printf("Model:      %s\n", actualModel)
	pterm.Info.Printf("Dimensions: %d\n", dims)
	pterm.Info.Printf("Enabled:    %v\n", cfg.Intelligence.VectorEnabled)
}

// --- Task 5.5: Option 4 — Shared LLM Backplane ---

func configureBackplane(cfg *config.Config) {
	pterm.DefaultSection.Println("Shared LLM Backplane Configuration")
	pterm.Info.Println("The shared LLM backplane allows all sub-servers to use the orchestrator's\nconfigured LLM instead of running their own. This centralizes rate limiting,\ntoken tracking, and eliminates the need for sub-servers to have their own\nAPI keys. Requires the Fast Tier LLM to be configured first.")

	if cfg.Intelligence.Provider == "" {
		pterm.Warning.Println("You must configure the Fast Tier LLM (Option 1) before enabling the Shared LLM Backplane.\nReturning to menu.")
		return
	}

	currentEnabled := "disabled"
	if cfg.Intelligence.SharedLLMEnabled {
		currentEnabled = "enabled"
	}

	options := []string{
		fmt.Sprintf("Keep current (%s)", currentEnabled),
		"Enable shared LLM backplane",
		"Disable shared LLM backplane",
	}

	toggle, _ := pterm.DefaultInteractiveSelect.WithOptions(options).Show()

	switch toggle {
	case "Enable shared LLM backplane":
		cfg.Intelligence.SharedLLMEnabled = true
	case "Disable shared LLM backplane":
		cfg.Intelligence.SharedLLMEnabled = false
		if err := cfg.SaveConfiguration(); err != nil {
			pterm.Error.Printf("Failed to save configuration: %v\nYour changes were NOT saved.\n", err)
			return
		}
		pterm.Success.Println("Shared LLM Backplane disabled.")
		return
	default:
		// Empty input: keep current value
	}

	if cfg.Intelligence.SharedLLMEnabled {
		cfg.Intelligence.LLMPort = promptInt("LLM Port", cfg.Intelligence.LLMPort, 48081)
		cfg.Intelligence.MaxConcurrent = promptInt("Max Concurrent Requests", cfg.Intelligence.MaxConcurrent, 4)
		cfg.Intelligence.MaxRPM = promptInt("Max RPM", cfg.Intelligence.MaxRPM, 30)
		cfg.Intelligence.MaxBurstPerSecond = promptInt("Max Burst/sec", cfg.Intelligence.MaxBurstPerSecond, 5)
		cfg.Intelligence.SubServerTokenMax = promptInt("Sub-Server Token Threshold", cfg.Intelligence.SubServerTokenMax, 500000)
		cfg.Intelligence.OrphanStreamTTL = promptInt("Orphan Stream TTL (minutes)", cfg.Intelligence.OrphanStreamTTL, 5)
	}

	if err := cfg.SaveConfiguration(); err != nil {
		pterm.Error.Printf("Failed to save configuration: %v\nYour changes were NOT saved.\n", err)
		return
	}

	pterm.Success.Println("Shared LLM Backplane configured!")
	pterm.Info.Printf("Enabled: %v\n", cfg.Intelligence.SharedLLMEnabled)
	if cfg.Intelligence.SharedLLMEnabled {
		pterm.Info.Printf("Port:    %d\n", cfg.Intelligence.LLMPort)
	}
}

// --- Task 5.6: Option 5 — Show Current Config ---

func showCurrentConfig(cfg *config.Config) {
	fmt.Println("\n=== Current MagicTools Configuration ===")

	fmt.Println("\nFast Tier LLM:")
	if cfg.Intelligence.Provider != "" {
		fmt.Printf("  Provider:  %s\n", cfg.Intelligence.Provider)
		fmt.Printf("  Model:     %s\n", cfg.Intelligence.Model)
		fmt.Printf("  API Key:   %s\n", maskKey(cfg.Intelligence.APIKey))
		if len(cfg.Intelligence.FallbackModels) > 0 {
			fmt.Printf("  Fallbacks: %s\n", strings.Join(cfg.Intelligence.FallbackModels, ", "))
		}
	} else {
		fmt.Println("  (not configured)")
	}

	fmt.Println("\nThinking Tier LLM:")
	if cfg.Intelligence.ThinkingProvider != "" {
		fmt.Printf("  Provider:  %s\n", cfg.Intelligence.ThinkingProvider)
		fmt.Printf("  Model:     %s\n", cfg.Intelligence.ThinkingModel)
		fmt.Printf("  API Key:   %s\n", maskKey(cfg.Intelligence.ThinkingAPIKey))
	} else {
		fmt.Println("  (not configured)")
	}

	fmt.Println("\nEmbedding Engine:")
	if cfg.Intelligence.EmbeddingProvider != "" {
		fmt.Printf("  Provider:       %s\n", cfg.Intelligence.EmbeddingProvider)
		fmt.Printf("  Model:          %s\n", cfg.Intelligence.EmbeddingModel)
		fmt.Printf("  Dimensions:     %d\n", cfg.Intelligence.EmbeddingDimensionality)
		fmt.Printf("  Vector Enabled: %v\n", cfg.Intelligence.VectorEnabled)
		fmt.Printf("  API Key:        %s\n", maskKey(cfg.Intelligence.EmbeddingAPIKey))
	} else {
		fmt.Println("  (not configured)")
	}

	fmt.Println("\nShared LLM Backplane:")
	fmt.Printf("  Enabled:               %v\n", cfg.Intelligence.SharedLLMEnabled)
	fmt.Printf("  Port:                  %d\n", valOrDefault(cfg.Intelligence.LLMPort, 48081))
	fmt.Printf("  Max Concurrent:        %d\n", valOrDefault(cfg.Intelligence.MaxConcurrent, 4))
	fmt.Printf("  Max RPM:               %d\n", valOrDefault(cfg.Intelligence.MaxRPM, 30))
	fmt.Printf("  Max Burst/sec:         %d\n", valOrDefault(cfg.Intelligence.MaxBurstPerSecond, 5))
	fmt.Printf("  Sub-Server Token Max:  %d\n", valOrDefault(cfg.Intelligence.SubServerTokenMax, 500000))
	fmt.Printf("  Orphan Stream TTL:     %dm\n", valOrDefault(cfg.Intelligence.OrphanStreamTTL, 5))
}

// ===== Shared Helpers =====

// readHiddenSecret reads an API key with masked input on supported terminals.
// Falls back to visible input with an explicit warning for Git Bash PTY, piped input, or CI/CD.
func readHiddenSecret(reader *bufio.Reader) string {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		result, _ := pterm.DefaultInteractiveTextInput.WithMask("*").Show()
		return strings.TrimSpace(result)
	}
	// Explicit warning for Git Bash PTY, piped input, CI/CD
	pterm.Warning.Println("Terminal does not support hidden input. Your key will be visible as you type.")
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// maskKey masks an API key for display, showing only the last 4 characters.
// Short or empty keys are fully masked.
func maskKey(key string) string {
	if key == "" {
		return "—"
	}
	if len(key) >= 5 {
		return "****" + key[len(key)-4:]
	}
	return "****"
}

// selectProvider prompts for a generative LLM provider choice.
func selectProvider() string {
	options := []string{
		"1) Gemini    (Google Gemini API)",
		"2) Claude    (Anthropic Claude API)",
		"3) OpenAI    (OpenAI API)",
		"4) Ollama    (Local Ollama API)",
	}
	choice, _ := pterm.DefaultInteractiveSelect.
		WithDefaultText("Which LLM Provider would you like to use?").
		WithOptions(options).
		Show()

	if len(choice) > 0 {
		return choiceToProvider(choice[:1])
	}
	return ""
}

// choiceToProvider converts a numeric choice string to a provider name.
func choiceToProvider(choice string) string {
	switch choice {
	case "1":
		return "gemini"
	case "2":
		return "claude"
	case "3":
		return "openai"
	case "4":
		return "ollama"
	default:
		return ""
	}
}

// resolveAPIKey handles the 3-way API key precedence: env var → existing config → manual entry.
func resolveAPIKey(existingKey, provider string, reader *bufio.Reader) string {
	envVar := providerEnvVars[provider]
	envVal := ""
	if envVar != "" {
		envVal = os.Getenv(envVar)
	}

	hasEnv := envVal != ""
	hasExisting := existingKey != ""

	if hasEnv && hasExisting {
		options := []string{
			fmt.Sprintf("a) Use environment variable (%s)", envVar),
			fmt.Sprintf("b) Keep existing config key (%s)", maskKey(existingKey)),
			"c) Enter a new key",
		}
		choice, _ := pterm.DefaultInteractiveSelect.
			WithDefaultText(fmt.Sprintf("API key sources available for %s", provider)).
			WithOptions(options).
			Show()

		if strings.HasPrefix(choice, "a)") {
			pterm.Info.Println("Using environment key.")
			return envVal
		} else if strings.HasPrefix(choice, "b)") {
			pterm.Info.Println("Keeping existing key.")
			return existingKey
		} else {
			pterm.Print("Enter API key: ")
			return readHiddenSecret(reader)
		}
	} else if hasEnv {
		pterm.Success.Printf("Detected %s in environment.\n", envVar)
		options := []string{"Use environment key", "Enter a different key"}
		choice, _ := pterm.DefaultInteractiveSelect.WithOptions(options).Show()
		if choice == "Enter a different key" {
			pterm.Print("Enter API key: ")
			return readHiddenSecret(reader)
		}
		return envVal
	} else if hasExisting {
		pterm.Info.Printf("Existing key found: %s\n", maskKey(existingKey))
		options := []string{"Keep existing key", "Enter a new key"}
		choice, _ := pterm.DefaultInteractiveSelect.WithOptions(options).Show()
		if choice == "Enter a new key" {
			pterm.Print("Enter API key: ")
			return readHiddenSecret(reader)
		}
		return existingKey
	}

	pterm.Print(fmt.Sprintf("\nEnter your %s API Key: ", provider))
	return readHiddenSecret(reader)
}

// promptOllamaURL prompts for an Ollama base URL with validation.
func promptOllamaURL() string {
	apiURL, _ := pterm.DefaultInteractiveTextInput.
		WithDefaultText("Enter Ollama API URL").
		WithDefaultValue("http://localhost:11434").
		Show()
	apiURL = strings.TrimSpace(apiURL)
	if apiURL == "" {
		apiURL = "http://localhost:11434"
	}

	// Validate connectivity
	pterm.Info.Println("Validating Ollama connectivity...")
	if err := llmprovider.ValidateOllamaURL(context.Background(), apiURL); err != nil {
		pterm.Warning.Printf("Could not reach Ollama at %s: %v\n", apiURL, err)
		pterm.Info.Println("Continuing anyway — you can update the URL later.")
	} else {
		pterm.Success.Printf("Ollama reachable at %s\n", apiURL)
	}
	return apiURL
}

// selectModel presents a numbered list of models and returns the user's selection.
func selectModel(models []string) string {
	options := append([]string{}, models...)
	options = append(options, "Other (enter manually)")

	result, _ := pterm.DefaultInteractiveSelect.
		WithDefaultText("Select model").
		WithOptions(options).
		Show()

	if result == "Other (enter manually)" {
		custom, _ := pterm.DefaultInteractiveTextInput.WithDefaultText("Enter model name").Show()
		return strings.TrimSpace(custom)
	}
	return result
}

// staticModelsForProvider returns the curated static fallback list for a provider.
func staticModelsForProvider(provider string) []string {
	switch provider {
	case "gemini":
		return llmprovider.StaticGemini
	case "claude":
		return llmprovider.StaticClaude
	case "openai":
		return llmprovider.StaticOpenAI
	default:
		return nil
	}
}

// promptInt prompts for an integer value with current and default display.
func promptInt(label string, current, defaultVal int) int {
	effective := current
	if effective <= 0 {
		effective = defaultVal
	}

	input, _ := pterm.DefaultInteractiveTextInput.
		WithDefaultText(fmt.Sprintf("%s (default: %d)", label, defaultVal)).
		WithDefaultValue(strconv.Itoa(effective)).
		Show()

	input = strings.TrimSpace(input)
	if input == "" {
		return effective
	}
	val, err := strconv.Atoi(input)
	if err != nil {
		pterm.Warning.Printf("Invalid number, keeping %d\n", effective)
		return effective
	}
	return val
}

// valOrDefault returns val if positive, otherwise defaultVal.
func valOrDefault(val, defaultVal int) int {
	if val > 0 {
		return val
	}
	return defaultVal
}

func init() {
	configureCmd.Flags().BoolVarP(&nonInteractive, "non-interactive", "n", false, "Bypass interactive prompts")
	configureCmd.Flags().BoolVarP(&forceInit, "force", "f", false, "Overwrite existing configuration files (resets to defaults)")
	rootCmd.AddCommand(configureCmd)
}

// ensureInitialized checks for and generates default configuration files if missing or if force is true.
func ensureInitialized(configDir, configPath, serversPath, overridesPath string, force bool) error {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", configDir, err)
	}

	configExists := fileExists(configPath)
	serversExists := fileExists(serversPath)
	overridesExists := fileExists(overridesPath)

	var createdFiles []string

	if !configExists || force {
		if err := os.WriteFile(configPath, []byte(config.DefaultConfigTemplate()), 0600); err != nil {
			return fmt.Errorf("failed to write %s: %w", configPath, err)
		}
		createdFiles = append(createdFiles, filepath.Base(configPath))
	}

	if !serversExists || force {
		if err := os.WriteFile(serversPath, []byte(config.DefaultServersTemplate()), 0600); err != nil {
			return fmt.Errorf("failed to write %s: %w", serversPath, err)
		}
		createdFiles = append(createdFiles, filepath.Base(serversPath))
	}

	if !overridesExists || force {
		if err := os.WriteFile(overridesPath, []byte(config.DefaultToolOverridesTemplate()), 0600); err != nil {
			return fmt.Errorf("failed to write %s: %w", overridesPath, err)
		}
		createdFiles = append(createdFiles, filepath.Base(overridesPath))
	}

	if len(createdFiles) > 0 {
		fmt.Printf("\n✓ Configuration initialized at %s\n", configDir)
		fmt.Println("  Files created:")
		for _, f := range createdFiles {
			fmt.Printf("    - %s\n", f)
		}
		fmt.Println()
	}

	return nil
}

// fileExists returns true if the path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
