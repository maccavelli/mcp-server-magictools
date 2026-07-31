package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

var (
	forceReset bool
	yesPrompt  bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize missing MagicTools configuration files",
	Long:  `Creates missing configuration files (config.yaml, servers.yaml, tool_overrides.yaml) with default templates. Use --force to reset existing files after confirmation.`,
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	paths, err := config.ResolvePaths(CfgPath)
	if err != nil {
		return err
	}

	if strings.HasSuffix(paths.Config, ".json") {
		return fmt.Errorf("init targets must be YAML config files; .json is supported only for serve")
	}

	if forceReset {
		if !yesPrompt {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("interactive confirmation required for init --force in non-terminal mode; pass --yes to confirm")
			}
			fmt.Printf("WARNING: init --force will overwrite existing configuration files:\n")
			fmt.Printf("  - %s\n", paths.Config)
			fmt.Printf("  - %s\n", paths.Servers)
			fmt.Printf("  - %s\n", paths.Overrides)
			fmt.Print("Are you sure you want to proceed? [y/N]: ")

			reader := bufio.NewReader(os.Stdin)
			input, err := reader.ReadString('\n')
			if err != nil && input == "" {
				pterm.Info.Println("Initialization cancelled.")
				return nil
			}
			input = strings.ToLower(strings.TrimSpace(input))
			if input != "y" && input != "yes" {
				pterm.Info.Println("Initialization cancelled. Existing files were not modified.")
				return nil
			}
		}
	}

	return ensureInitialized(paths.Dir, paths.Config, paths.Servers, paths.Overrides, forceReset)
}

func init() {
	initCmd.Flags().BoolVarP(&forceReset, "force", "f", false, "Overwrite existing configuration files (requires confirmation or --yes)")
	initCmd.Flags().BoolVarP(&yesPrompt, "yes", "y", false, "Confirm reset without interactive prompt")
	rootCmd.AddCommand(initCmd)
}
