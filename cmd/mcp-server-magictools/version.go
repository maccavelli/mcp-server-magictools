package main

import (
	"strings"
)

// RawVersion is the raw version string of the MagicTools MCP server, typically set by build flags.
var RawVersion = "v4.3.2"

// Version is the current version of the MagicTools MCP server.
var Version = strings.TrimPrefix(RawVersion, "v")
