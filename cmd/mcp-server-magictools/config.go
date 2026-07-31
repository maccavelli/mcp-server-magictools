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

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/provider"
	"github.com/maccavelli/mcplib/llmprovider"
)

var forceInit bool
var nonInteractive bool

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Interactive setup wizard for the MagicTools orchestrator",
	RunE:  runConfigure,
}

// providerEnvVars maps provider names to their standard environment variable names.
var providerEnvVars = map[string]string{
	"gemini": "GEMINI_API_KEY",
	"openai": "OPENAI_API_KEY",
	"claude": "CLAUDE_API_KEY",
	"voyage": "VOYAGE_API_KEY",
}

func runConfigure(cmd *cobra.Command, args []string) error {
	if forceInit {
		return fmt.Errorf("configure does not support --force; use 'init --force' to reset configuration files")
	}

	if nonInteractive {
		return fmt.Errorf("--non-interactive is deprecated for configure. To initialize missing files, run 'init'")
	}

	paths, err := config.ResolvePaths(CfgPath)
	if err != nil {
		return err
	}

	if strings.HasSuffix(paths.Config, ".json") {
		return fmt.Errorf("configure targets must be YAML config files; .json is supported only for serve")
	}

	if err := ensureInitialized(paths.Dir, paths.Config, paths.Servers, paths.Overrides, false); err != nil {
		return err
	}

	cfg, err := config.New(Version, CfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("interactive terminal required for configure wizard")
	}

	reader := bufio.NewReader(os.Stdin)
	var stagedPatch config.ConfigurationPatch

	for {
		pterm.DefaultHeader.WithFullWidth().Println("MagicTools Configuration Wizard")
		pterm.Info.Println("Configure your MagicTools orchestrator. Changes are staged in-memory\nand written atomically when you choose 'Save and Exit'.")

		options := []string{
			"1) Fast Tier LLM        — Primary model for hydration & intelligence",
			"2) Thinking Tier LLM    — Dedicated model for deep reasoning (optional)",
			"3) Embedding Engine     — Vector search model for semantic alignment",
			"4) Shared LLM Backplane — Centralized LLM service for sub-servers",
			"5) Show Current Config  — Display active & staged configuration",
			"6) Save and Exit        — Commit all staged changes to config.yaml",
			"0) Exit without saving  — Discard staged changes",
		}

		choice, _ := pterm.DefaultInteractiveSelect.
			WithDefaultText("Select option").
			WithOptions(options).
			Show()

		if len(choice) > 0 {
			switch choice[:1] {
			case "1":
				configureFastTier(cfg, &stagedPatch.Fast, reader)
			case "2":
				configureThinkingTier(cfg, &stagedPatch.Thinking, reader)
			case "3":
				configureEmbeddingEngine(cfg, &stagedPatch.Embedding, reader)
			case "4":
				configureBackplane(cfg, &stagedPatch.Backplane)
			case "5":
				showCurrentConfig(cfg)
			case "6":
				if stagedPatch.IsEmpty() {
					pterm.Info.Println("No changes to save.")
					return nil
				}
				store := config.NewStore(paths)
				res, err := store.Apply(cmd.Context(), stagedPatch)
				if err != nil {
					pterm.Error.Printf("Failed to save configuration: %v\nYour changes were NOT saved.\n", err)
					return err
				}
				if res.Changed {
					pterm.Success.Println("Configuration saved successfully!")
				} else {
					pterm.Info.Println("No configuration changes were detected.")
				}
				return nil
			case "0":
				pterm.Info.Println("Exited wizard. No changes were written to config.yaml")
				return nil
			}
		}
	}
}

// --- Option 1 — Fast Tier LLM ---

func configureFastTier(cfg *config.Config, patch *config.FastTierPatch, reader *bufio.Reader) {
	pterm.DefaultSection.Println("Fast Tier LLM Configuration")

	spec, ok := selectProviderForTier(provider.TierFast)
	if !ok {
		return
	}
	provID := spec.ID

	var apiKey string
	if spec.IsLocal {
		ollamaURL := promptOllamaURL()
		cfg.Intelligence.APIURL = ollamaURL
		patch.APIURL = config.Set(ollamaURL)
	} else {
		apiKey = resolveAPIKey(cfg.Intelligence.APIKey, provID, reader)
		if apiKey == "" {
			fmt.Println("No API key provided. Returning to menu.")
			return
		}
	}

	// Discover models
	pterm.Info.Println("Fetching available models...")
	ctx := context.Background()
	models, err := llmprovider.ListAvailableModels(ctx, provID, apiKey)
	if err != nil || len(models) == 0 {
		pterm.Warning.Println("Could not reach API, using default model list.")
		models = staticModelsForProvider(provID)
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

	// Stage changes
	cfg.Intelligence.Provider = provID
	cfg.Intelligence.APIKey = apiKey
	cfg.Intelligence.Model = selectedModel
	cfg.Intelligence.FallbackModels = fallbacks

	retryCount := valOrDefault(cfg.Intelligence.RetryCount, 2)
	retryDelay := valOrDefault(cfg.Intelligence.RetryDelay, 5)
	timeoutSecs := valOrDefault(cfg.Intelligence.TimeoutSeconds, 120)

	cfg.Intelligence.RetryCount = retryCount
	cfg.Intelligence.RetryDelay = retryDelay
	cfg.Intelligence.TimeoutSeconds = timeoutSecs

	patch.Provider = config.Set(provID)
	patch.Model = config.Set(selectedModel)
	patch.APIKey = config.Set(apiKey)
	patch.FallbackModels = config.Set(fallbacks)
	patch.RetryCount = config.Set(retryCount)
	patch.RetryDelay = config.Set(retryDelay)
	patch.TimeoutSeconds = config.Set(timeoutSecs)

	pterm.Success.Println("Fast Tier staged!")
	pterm.Info.Printf("Provider:  %s\n", provID)
	pterm.Info.Printf("Model:     %s\n", selectedModel)
	if len(fallbacks) > 0 {
		pterm.Info.Printf("Fallbacks: %s\n", strings.Join(fallbacks, ", "))
	}
}

// --- Option 2 — Thinking Tier LLM ---

func configureThinkingTier(cfg *config.Config, patch *config.ThinkingTierPatch, reader *bufio.Reader) {
	pterm.DefaultSection.Println("Thinking Tier LLM Configuration")
	pterm.Info.Println("The Thinking Tier provides a dedicated model for deep reasoning tasks\n(Socratic analysis, complex code review). If not configured, the Fast\nTier model handles all requests.")

	specs := provider.ForTier(provider.TierThinking)
	var options []string
	for i, s := range specs {
		options = append(options, fmt.Sprintf("%d) %s", i+1, s.Label))
	}
	options = append(options, "0) None/Clear (disable thinking tier)")

	choice, _ := pterm.DefaultInteractiveSelect.
		WithDefaultText("Select provider").
		WithOptions(options).
		Show()

	if len(choice) > 0 && strings.HasPrefix(choice, "0)") {
		cfg.Intelligence.ThinkingProvider = ""
		cfg.Intelligence.ThinkingModel = ""
		cfg.Intelligence.ThinkingAPIKey = ""

		patch.ThinkingProvider = config.Remove[string]()
		patch.ThinkingModel = config.Remove[string]()
		patch.ThinkingAPIKey = config.Remove[string]()

		pterm.Success.Println("Thinking Tier staged for removal.")
		return
	}

	var selectedSpec provider.ProviderSpec
	for i, s := range specs {
		if strings.HasPrefix(choice, fmt.Sprintf("%d)", i+1)) {
			selectedSpec = s
			break
		}
	}

	if selectedSpec.ID == "" {
		pterm.Warning.Println("Invalid choice. Returning to menu.")
		return
	}
	provID := selectedSpec.ID

	var apiKey string
	if !selectedSpec.IsLocal {
		if provID == cfg.Intelligence.Provider && cfg.Intelligence.APIKey != "" {
			fmt.Printf("You already have a %s API key configured for the Fast Tier.\n", provID)
			fmt.Print("Press Enter to reuse it, or enter a different key: ")
			override := readHiddenSecret(reader)
			if override != "" {
				apiKey = override
			} else {
				apiKey = cfg.Intelligence.APIKey
			}
		} else {
			apiKey = resolveAPIKey(cfg.Intelligence.ThinkingAPIKey, provID, reader)
			if apiKey == "" {
				pterm.Warning.Println("No API key provided. Returning to menu.")
				return
			}
		}
	}

	// Discover models — present top 3 thinking-capable
	pterm.Info.Println("Fetching available models...")
	ctx := context.Background()
	models, err := llmprovider.ListAvailableModels(ctx, provID, apiKey)
	if err != nil || len(models) == 0 {
		models = staticModelsForProvider(provID)
	}
	if len(models) > 3 {
		models = models[:3]
	}

	selectedModel := selectModel(models)
	if selectedModel == "" {
		pterm.Info.Println("No model selected. Returning to menu.")
		return
	}

	cfg.Intelligence.ThinkingProvider = provID
	cfg.Intelligence.ThinkingModel = selectedModel
	cfg.Intelligence.ThinkingAPIKey = apiKey

	patch.ThinkingProvider = config.Set(provID)
	patch.ThinkingModel = config.Set(selectedModel)
	patch.ThinkingAPIKey = config.Set(apiKey)

	pterm.Success.Println("Thinking Tier staged!")
	pterm.Info.Printf("Provider: %s\n", provID)
	pterm.Info.Printf("Model:    %s\n", selectedModel)
}

// --- Option 3 — Embedding Engine ---

func configureEmbeddingEngine(cfg *config.Config, patch *config.EmbeddingPatch, reader *bufio.Reader) {
	pterm.DefaultSection.Println("Embedding Engine Configuration")

	specs := provider.ForTier(provider.TierEmbedding)
	var options []string
	for i, s := range specs {
		options = append(options, fmt.Sprintf("%d) %s", i+1, s.Label))
	}
	options = append(options, "0) None/Clear (disable vector search)")

	choiceStr, _ := pterm.DefaultInteractiveSelect.
		WithDefaultText("Which Provider would you like to use for Vector Embeddings?").
		WithOptions(options).
		Show()

	if len(choiceStr) > 0 && strings.HasPrefix(choiceStr, "0)") {
		cfg.Intelligence.EmbeddingProvider = ""
		cfg.Intelligence.EmbeddingModel = ""
		cfg.Intelligence.EmbeddingAPIKey = ""
		cfg.Intelligence.EmbeddingAPIURL = ""
		cfg.Intelligence.EmbeddingDimensionality = 0
		cfg.Intelligence.VectorEnabled = false

		patch.EmbeddingProvider = config.Remove[string]()
		patch.EmbeddingModel = config.Remove[string]()
		patch.EmbeddingAPIKey = config.Remove[string]()
		patch.EmbeddingAPIURL = config.Remove[string]()
		patch.EmbeddingDimensionality = config.Remove[int]()
		patch.VectorEnabled = config.Set(false)

		pterm.Success.Println("Vector search staged for removal.")
		return
	}

	var selectedSpec provider.ProviderSpec
	for i, s := range specs {
		if strings.HasPrefix(choiceStr, fmt.Sprintf("%d)", i+1)) {
			selectedSpec = s
			break
		}
	}

	if selectedSpec.ID == "" {
		pterm.Warning.Println("Invalid choice. Returning to menu.")
		return
	}
	provID := selectedSpec.ID
	models := selectedSpec.StaticModels[provider.TierEmbedding]
	dimsMap := selectedSpec.Dimensions

	var apiKey string
	var apiURL string
	if selectedSpec.IsLocal {
		apiURL = promptOllamaURL()
	} else {
		if provID == cfg.Intelligence.Provider && cfg.Intelligence.APIKey != "" {
			fmt.Printf("You already have a %s API key configured for the Fast Tier.\n", provID)
			fmt.Print("Press Enter to reuse it, or enter a different key: ")
			override := readHiddenSecret(reader)
			if override != "" {
				apiKey = override
			} else {
				apiKey = cfg.Intelligence.APIKey
			}
		} else if provID == cfg.Intelligence.ThinkingProvider && cfg.Intelligence.ThinkingAPIKey != "" {
			fmt.Printf("You already have a %s API key configured for the Thinking Tier.\n", provID)
			fmt.Print("Press Enter to reuse it, or enter a different key: ")
			override := readHiddenSecret(reader)
			if override != "" {
				apiKey = override
			} else {
				apiKey = cfg.Intelligence.ThinkingAPIKey
			}
		} else {
			apiKey = resolveAPIKey(cfg.Intelligence.EmbeddingAPIKey, provID, reader)
			if apiKey == "" {
				pterm.Warning.Println("No API key provided. Returning to menu.")
				return
			}
		}
	}

	var modelOptions []string
	for i, m := range models {
		label := ""
		if i == 0 && provID == "gemini" {
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
	actualModel := strings.Split(selectedDisplay, " ")[0]

	if dims == 0 {
		dimOptions := []string{"384", "512", "768", "1024", "1536"}
		dimStr, _ := pterm.DefaultInteractiveSelect.
			WithDefaultText("Select dimensionality for custom model").
			WithOptions(dimOptions).
			WithDefaultOption("768").
			Show()
		fmt.Sscanf(strings.TrimSpace(dimStr), "%d", &dims)
	}

	if cfg.Intelligence.EmbeddingDimensionality > 0 && cfg.Intelligence.EmbeddingDimensionality != dims {
		pterm.Warning.Println("Changing embedding dimensions requires rebuilding the vector index.\nThe index will be rebuilt on next server start.")
	}

	cfg.Intelligence.EmbeddingProvider = provID
	cfg.Intelligence.EmbeddingModel = actualModel
	cfg.Intelligence.EmbeddingAPIKey = apiKey
	if provID == "ollama" {
		cfg.Intelligence.EmbeddingAPIURL = apiURL
	}
	cfg.Intelligence.EmbeddingDimensionality = dims
	cfg.Intelligence.VectorEnabled = true

	patch.EmbeddingProvider = config.Set(provID)
	patch.EmbeddingModel = config.Set(actualModel)
	patch.EmbeddingAPIKey = config.Set(apiKey)
	if provID == "ollama" {
		patch.EmbeddingAPIURL = config.Set(apiURL)
	}
	patch.EmbeddingDimensionality = config.Set(dims)
	patch.VectorEnabled = config.Set(true)

	pterm.Success.Println("Embedding Engine staged!")
	pterm.Info.Printf("Provider:   %s\n", provID)
	pterm.Info.Printf("Model:      %s\n", actualModel)
	pterm.Info.Printf("Dimensions: %d\n", dims)
	pterm.Info.Printf("Enabled:    %v\n", cfg.Intelligence.VectorEnabled)
}

// --- Option 4 — Shared LLM Backplane ---

func configureBackplane(cfg *config.Config, patch *config.BackplanePatch) {
	pterm.DefaultSection.Println("Shared LLM Backplane Configuration")
	pterm.Info.Println("The shared LLM backplane allows all sub-servers to use the orchestrator's\nconfigured LLM instead of running their own. This centralizes rate limiting,\ntoken tracking, and eliminates the need for sub-servers to have their own\nAPI keys.")

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
		patch.SharedLLMEnabled = config.Set(true)
	case "Disable shared LLM backplane":
		cfg.Intelligence.SharedLLMEnabled = false
		patch.SharedLLMEnabled = config.Set(false)
		pterm.Success.Println("Shared LLM Backplane staged as disabled.")
		return
	default:
		// Keep current
	}

	if cfg.Intelligence.SharedLLMEnabled {
		llmPort := promptInt("LLM Port", cfg.Intelligence.LLMPort, 48081)
		maxConc := promptInt("Max Concurrent Requests", cfg.Intelligence.MaxConcurrent, 4)
		maxRPM := promptInt("Max RPM", cfg.Intelligence.MaxRPM, 30)
		maxBurst := promptInt("Max Burst/sec", cfg.Intelligence.MaxBurstPerSecond, 5)
		subTokenMax := promptInt("Sub-Server Token Threshold", cfg.Intelligence.SubServerTokenMax, 500000)
		orphanTTL := promptInt("Orphan Stream TTL (minutes)", cfg.Intelligence.OrphanStreamTTL, 5)

		cfg.Intelligence.LLMPort = llmPort
		cfg.Intelligence.MaxConcurrent = maxConc
		cfg.Intelligence.MaxRPM = maxRPM
		cfg.Intelligence.MaxBurstPerSecond = maxBurst
		cfg.Intelligence.SubServerTokenMax = subTokenMax
		cfg.Intelligence.OrphanStreamTTL = orphanTTL

		patch.LLMPort = config.Set(llmPort)
		patch.MaxConcurrent = config.Set(maxConc)
		patch.MaxRPM = config.Set(maxRPM)
		patch.MaxBurstPerSecond = config.Set(maxBurst)
		patch.SubServerTokenMax = config.Set(subTokenMax)
		patch.OrphanStreamTTL = config.Set(orphanTTL)
	}

	pterm.Success.Println("Shared LLM Backplane staged!")
	pterm.Info.Printf("Enabled: %v\n", cfg.Intelligence.SharedLLMEnabled)
	if cfg.Intelligence.SharedLLMEnabled {
		pterm.Info.Printf("Port:    %d\n", cfg.Intelligence.LLMPort)
	}
}

// --- Option 5 — Show Current Config ---

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

func readHiddenSecret(reader *bufio.Reader) string {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		result, _ := pterm.DefaultInteractiveTextInput.WithMask("*").Show()
		return strings.TrimSpace(result)
	}
	pterm.Warning.Println("Terminal does not support hidden input. Your key will be visible as you type.")
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

func maskKey(key string) string {
	if key == "" {
		return "—"
	}
	if len(key) >= 5 {
		return "****" + key[len(key)-4:]
	}
	return "****"
}

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

func selectProviderForTier(tier provider.Tier) (provider.ProviderSpec, bool) {
	specs := provider.ForTier(tier)
	var options []string
	for i, s := range specs {
		options = append(options, fmt.Sprintf("%d) %s", i+1, s.Label))
	}

	choice, _ := pterm.DefaultInteractiveSelect.
		WithDefaultText("Which LLM Provider would you like to use?").
		WithOptions(options).
		Show()

	for i, s := range specs {
		if strings.HasPrefix(choice, fmt.Sprintf("%d)", i+1)) {
			return s, true
		}
	}
	return provider.ProviderSpec{}, false
}

func resolveAPIKey(existingKey, providerID string, reader *bufio.Reader) string {
	envVar := providerEnvVars[providerID]
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
			WithDefaultText(fmt.Sprintf("API key sources available for %s", providerID)).
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

	pterm.Print(fmt.Sprintf("\nEnter your %s API Key: ", providerID))
	return readHiddenSecret(reader)
}

func promptOllamaURL() string {
	apiURL, _ := pterm.DefaultInteractiveTextInput.
		WithDefaultText("Enter Ollama API URL").
		WithDefaultValue("http://localhost:11434").
		Show()
	apiURL = strings.TrimSpace(apiURL)
	if apiURL == "" {
		apiURL = "http://localhost:11434"
	}

	pterm.Info.Println("Validating Ollama connectivity...")
	if err := llmprovider.ValidateOllamaURL(context.Background(), apiURL); err != nil {
		pterm.Warning.Printf("Could not reach Ollama at %s: %v\n", apiURL, err)
		pterm.Info.Println("Continuing anyway — you can update the URL later.")
	} else {
		pterm.Success.Printf("Ollama reachable at %s\n", apiURL)
	}
	return apiURL
}

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

func staticModelsForProvider(providerID string) []string {
	spec, ok := provider.Get(providerID)
	if !ok {
		return nil
	}
	if fastModels, ok := spec.StaticModels[provider.TierFast]; ok {
		return fastModels
	}
	return nil
}

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

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
