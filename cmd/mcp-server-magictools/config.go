package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/provider"
	"github.com/maccavelli/mcplib/llmprovider"
	"github.com/maccavelli/mcplib/logging"
	"github.com/maccavelli/mcplib/wizard"
)

// discoveryTimeout bounds a live model listing so an unreachable provider
// cannot stall the wizard.
const discoveryTimeout = 20 * time.Second

var forceInit bool
var nonInteractive bool

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Interactive setup wizard for the MagicTools orchestrator",
	RunE:  runConfigure,
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
				configureFastTier(cfg, &stagedPatch.Fast)
			case "2":
				configureThinkingTier(cfg, &stagedPatch.Thinking)
			case "3":
				configureEmbeddingEngine(cfg, &stagedPatch.Embedding)
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

func configureFastTier(cfg *config.Config, patch *config.FastTierPatch) {
	pterm.DefaultSection.Println("Fast Tier LLM Configuration")

	res, err := configureTier(provider.TierFast, wizard.Result{
		Provider: cfg.Intelligence.Provider,
		APIKey:   cfg.Intelligence.APIKey,
		Model:    cfg.Intelligence.Model,
		BaseURL:  cfg.Intelligence.APIURL,
	}, true)
	if err != nil {
		pterm.Warning.Printf("Fast Tier not configured: %v\n", err)
		return
	}

	retryCount := valOrDefault(cfg.Intelligence.RetryCount, 2)
	retryDelay := valOrDefault(cfg.Intelligence.RetryDelay, 5)
	timeoutSecs := valOrDefault(cfg.Intelligence.TimeoutSeconds, 120)

	cfg.Intelligence.Provider = res.Provider
	cfg.Intelligence.Model = res.Model
	cfg.Intelligence.APIKey = res.APIKey
	cfg.Intelligence.APIURL = res.BaseURL
	cfg.Intelligence.FallbackModels = res.Fallbacks
	cfg.Intelligence.RetryCount = retryCount
	cfg.Intelligence.RetryDelay = retryDelay
	cfg.Intelligence.TimeoutSeconds = timeoutSecs

	patch.Provider = config.Set(res.Provider)
	patch.Model = config.Set(res.Model)
	patch.APIKey = config.Set(res.APIKey)
	// Written unconditionally: an empty endpoint must clear one left by a
	// previously configured provider rather than be silently retained.
	patch.APIURL = config.Set(res.BaseURL)
	patch.FallbackModels = config.Set(res.Fallbacks)
	patch.RetryCount = config.Set(retryCount)
	patch.RetryDelay = config.Set(retryDelay)
	patch.TimeoutSeconds = config.Set(timeoutSecs)

	pterm.Success.Println("Fast Tier staged!")
	printTierSummary(res)
}

// --- Option 2 — Thinking Tier LLM ---

func configureThinkingTier(cfg *config.Config, patch *config.ThinkingTierPatch) {
	pterm.DefaultSection.Println("Thinking Tier LLM Configuration")
	pterm.Info.Println("The Thinking Tier provides a dedicated model for deep reasoning tasks\n(Socratic analysis, complex code review). If not configured, the Fast\nTier model handles all requests.")

	// "Disable" is a MagicTools concept with no equivalent in the shared
	// wizard, so it is asked before handing over.
	keep, err := ptermPrompter{}.Confirm("Configure a thinking tier? (No clears it)", true)
	if err != nil {
		pterm.Warning.Printf("Thinking Tier not configured: %v\n", err)
		return
	}
	if !keep {
		cfg.Intelligence.ThinkingProvider = ""
		cfg.Intelligence.ThinkingModel = ""
		cfg.Intelligence.ThinkingAPIKey = ""
		cfg.Intelligence.ThinkingAPIURL = ""

		patch.ThinkingProvider = config.Remove[string]()
		patch.ThinkingModel = config.Remove[string]()
		patch.ThinkingAPIKey = config.Remove[string]()
		patch.ThinkingAPIURL = config.Remove[string]()

		pterm.Success.Println("Thinking Tier staged for removal.")
		return
	}

	res, err := configureTier(provider.TierThinking, wizard.Result{
		Provider: cfg.Intelligence.ThinkingProvider,
		APIKey:   cfg.Intelligence.ThinkingAPIKey,
		Model:    cfg.Intelligence.ThinkingModel,
		BaseURL:  cfg.Intelligence.ThinkingAPIURL,
	}, false)
	if err != nil {
		pterm.Warning.Printf("Thinking Tier not configured: %v\n", err)
		return
	}

	cfg.Intelligence.ThinkingProvider = res.Provider
	cfg.Intelligence.ThinkingModel = res.Model
	cfg.Intelligence.ThinkingAPIKey = res.APIKey
	cfg.Intelligence.ThinkingAPIURL = res.BaseURL

	patch.ThinkingProvider = config.Set(res.Provider)
	patch.ThinkingModel = config.Set(res.Model)
	patch.ThinkingAPIKey = config.Set(res.APIKey)
	patch.ThinkingAPIURL = config.Set(res.BaseURL)

	pterm.Success.Println("Thinking Tier staged!")
	printTierSummary(res)
}

// --- Option 3 — Embedding Engine ---

func configureEmbeddingEngine(cfg *config.Config, patch *config.EmbeddingPatch) {
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

	// The embedding tier keeps its own flow: llmprovider has no embedding
	// abstraction, so its dimension-annotated catalog and Voyage have no
	// equivalent in wizard.Result. Only the credential and endpoint prompts are
	// shared. See MADR 0004 scope boundaries and PLAN 0004 deviation D7.
	var apiKey string
	var apiURL string
	if selectedSpec.IsLocal {
		apiURL = promptEndpoint(selectedSpec, cfg.Intelligence.EmbeddingAPIURL)
	} else {
		switch {
		case provID == cfg.Intelligence.Provider && cfg.Intelligence.APIKey != "":
			apiKey = reuseTierKey(selectedSpec, "Fast Tier", cfg.Intelligence.APIKey)
		case provID == cfg.Intelligence.ThinkingProvider && cfg.Intelligence.ThinkingAPIKey != "":
			apiKey = reuseTierKey(selectedSpec, "Thinking Tier", cfg.Intelligence.ThinkingAPIKey)
		default:
			apiKey = promptTierAPIKey(selectedSpec, cfg.Intelligence.EmbeddingAPIKey)
		}
		if apiKey == "" {
			pterm.Warning.Println("No API key provided. Returning to menu.")
			return
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
	if apiURL != "" {
		cfg.Intelligence.EmbeddingAPIURL = apiURL
	}
	cfg.Intelligence.EmbeddingDimensionality = dims
	cfg.Intelligence.VectorEnabled = true

	patch.EmbeddingProvider = config.Set(provID)
	patch.EmbeddingModel = config.Set(actualModel)
	patch.EmbeddingAPIKey = config.Set(apiKey)
	if apiURL != "" {
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
		fmt.Printf("  API Key:   %s\n", logging.MaskSecret(cfg.Intelligence.APIKey))
		if cfg.Intelligence.APIURL != "" {
			fmt.Printf("  Endpoint:  %s\n", cfg.Intelligence.APIURL)
		}
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
		fmt.Printf("  API Key:   %s\n", logging.MaskSecret(cfg.Intelligence.ThinkingAPIKey))
		if cfg.Intelligence.ThinkingAPIURL != "" {
			fmt.Printf("  Endpoint:  %s\n", cfg.Intelligence.ThinkingAPIURL)
		}
	} else {
		fmt.Println("  (not configured)")
	}

	fmt.Println("\nEmbedding Engine:")
	if cfg.Intelligence.EmbeddingProvider != "" {
		fmt.Printf("  Provider:       %s\n", cfg.Intelligence.EmbeddingProvider)
		fmt.Printf("  Model:          %s\n", cfg.Intelligence.EmbeddingModel)
		fmt.Printf("  Dimensions:     %d\n", cfg.Intelligence.EmbeddingDimensionality)
		fmt.Printf("  Vector Enabled: %v\n", cfg.Intelligence.VectorEnabled)
		fmt.Printf("  API Key:        %s\n", logging.MaskSecret(cfg.Intelligence.EmbeddingAPIKey))
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

// configureTier runs the canonical wizard restricted to the providers that may
// serve one tier. The restriction is a plain id list computed here, so mcplib
// never learns what a tier is.
//
// Only providers with an llmprovider descriptor are offered: a tier that runs a
// generation needs a Provider, and Voyage — embedding-only — has none.
func configureTier(tier provider.Tier, existing wizard.Result, needFallbacks bool) (wizard.Result, error) {
	var ids []string
	for _, spec := range provider.ForTier(tier) {
		if _, ok := llmprovider.DescriptorFor(spec.ID); ok {
			ids = append(ids, spec.ID)
		}
	}
	return wizard.ConfigureLLM(context.Background(), ptermPrompter{}, wizard.Options{
		Providers:     ids,
		Existing:      existing,
		AllowEnv:      true,
		Discover:      true,
		DiscoverLimit: discoveryTimeout,
		NeedFallbacks: needFallbacks,
	})
}

func printTierSummary(res wizard.Result) {
	pterm.Info.Printf("Provider:  %s\n", res.Provider)
	pterm.Info.Printf("Model:     %s\n", res.Model)
	if res.BaseURL != "" {
		pterm.Info.Printf("Endpoint:  %s\n", res.BaseURL)
	}
	if len(res.Fallbacks) > 0 {
		pterm.Info.Printf("Fallbacks: %s\n", strings.Join(res.Fallbacks, ", "))
	}
}

// promptTierAPIKey applies the precedence environment → existing → prompt for
// the embedding tier, whose providers the shared wizard does not cover. The
// generative tiers get this from wizard.ConfigureLLM.
func promptTierAPIKey(spec provider.ProviderSpec, existingKey string) string {
	p := ptermPrompter{}
	if spec.EnvVar != "" {
		if envVal := os.Getenv(spec.EnvVar); envVal != "" {
			use, err := p.Confirm(
				fmt.Sprintf("Use %s from the environment (%s)?", spec.EnvVar, logging.MaskSecret(envVal)), true)
			if err == nil && use {
				return envVal
			}
		}
	}
	if existingKey != "" {
		keep, err := p.Confirm(
			fmt.Sprintf("Keep the existing key (%s)?", logging.MaskSecret(existingKey)), true)
		if err == nil && keep {
			return existingKey
		}
	}
	key, err := p.Secret(fmt.Sprintf("Enter your %s API key", spec.Label))
	if err != nil {
		return ""
	}
	return key
}

// reuseTierKey offers a credential already configured for another tier.
func reuseTierKey(spec provider.ProviderSpec, tierName, key string) string {
	reuse, err := ptermPrompter{}.Confirm(
		fmt.Sprintf("Reuse the %s %s key (%s)?", tierName, spec.Label, logging.MaskSecret(key)), true)
	if err != nil {
		return ""
	}
	if reuse {
		return key
	}
	return promptTierAPIKey(spec, "")
}

// promptEndpoint asks for a local provider's address and reports reachability.
// A failure is a warning, not a refusal: the service may simply not be running
// yet, and the user can still save the configuration.
func promptEndpoint(spec provider.ProviderSpec, existing string) string {
	def := existing
	if def == "" {
		if d, ok := llmprovider.DescriptorFor(spec.ID); ok {
			def = d.DefaultBaseURL
		}
	}
	apiURL, err := ptermPrompter{}.Input(fmt.Sprintf("%s endpoint", spec.Label), def)
	if err != nil {
		return def
	}
	apiURL = strings.TrimSpace(apiURL)
	if apiURL == "" {
		apiURL = def
	}

	pterm.Info.Printf("Validating connectivity to %s...\n", apiURL)
	if err := llmprovider.ValidateOllamaURL(context.Background(), apiURL); err != nil {
		pterm.Warning.Printf("Could not reach %s: %v\n", apiURL, err)
		pterm.Info.Println("Continuing anyway — you can update the endpoint later.")
	} else {
		pterm.Success.Printf("Reachable at %s\n", apiURL)
	}
	return apiURL
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

// staticModelsForProvider returns the curated generative catalog for a provider.
// It comes from mcplib now, so a model retired there — gemini-2.0-flash was
// recommended here for months after it was shut down — cannot survive locally.
func staticModelsForProvider(providerID string) []string {
	return provider.GenerativeModels(providerID)
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
