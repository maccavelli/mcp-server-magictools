// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

var (
	CfgPath      string
	DBPath       string
	LogPath      string
	NoOptimize   bool
	Debug        bool
	LogLevelFlag string
	ShowVersion  bool

	// Internal state for pipe hijacking
	RealStdout *os.File
)

// HijackStdout redirects os.Stdout to os.Stderr and saves the original.
func HijackStdout() {
	if RealStdout != nil {
		return // Already hijacked
	}
	RealStdout = os.Stdout
	os.Stdout = os.Stderr
}

var rootCmd = &cobra.Command{
	Use:   cmdName,
	Short: "MagicTools MCP Orchestrator",
	Long:  `A high-performance MCP orchestrator that manages multiple sub-servers.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if DBPath == "" {
			DBPath = config.DefaultDataPath()
		}
		if LogPath == "" {
			LogPath = config.DefaultLogPath()
		}
	},
}

// Execute is undocumented but satisfies standard structural requirements.
func Execute() {
	rootCmd.Version = Version
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&CfgPath, "config", "", "Path to IDE mcp_config.json")
	rootCmd.PersistentFlags().StringVar(&DBPath, "db", "", "Path to BadgerDB")
	rootCmd.PersistentFlags().StringVar(&LogPath, "log", "", "Path to log file")

	// Override default value strings for help output
	rootCmd.PersistentFlags().Lookup("db").DefValue = "<OS_CONFIG_DIR>/mcp-server-magictools/db"
	rootCmd.PersistentFlags().Lookup("log").DefValue = "<OS_CONFIG_DIR>/mcp-server-magictools/magictools.log"

	rootCmd.PersistentFlags().BoolVar(&NoOptimize, "no-optimize", false, "Disable SqueezeWriter and minification")
	rootCmd.PersistentFlags().BoolVar(&Debug, "debug", false, "Enable full trace logging (forces TRACE level)")
	rootCmd.PersistentFlags().StringVar(&LogLevelFlag, "log-level", "", "Set log level (ERROR, WARN, INFO, DEBUG, TRACE)")
	rootCmd.Flags().BoolVarP(&ShowVersion, "version", "v", false, "Print version info and exit")
}
