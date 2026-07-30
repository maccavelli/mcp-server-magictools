// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"github.com/spf13/cobra"
)

// DBWipeFunc is a callback for the database wipe logic
var DBWipeFunc func(dbPath string) error

// DBSyncFunc is a callback for the database offline reindex logic
var DBSyncFunc func(dbPath string) error

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Manage orchestrator database",
}

var wipeCmd = &cobra.Command{
	Use:   "wipe",
	Short: "WIPE the physical BadgerDB AND Bleve Index directory explicitly",
	RunE: func(cmd *cobra.Command, args []string) error {
		return DBWipeFunc(DBPath)
	},
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Forcefully re-align the Bleve search index safely from BadgerDB directly",
	RunE: func(cmd *cobra.Command, args []string) error {
		return DBSyncFunc(DBPath)
	},
}

func init() {
	rootCmd.AddCommand(dbCmd)
	dbCmd.AddCommand(wipeCmd)
	dbCmd.AddCommand(syncCmd)
}
