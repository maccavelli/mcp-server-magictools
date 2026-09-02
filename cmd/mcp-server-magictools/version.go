package main

import (
	"strings"
)

// RawVersion is the raw version string of the MagicTools MCP server, set by
// build flags. It defaults to "dev" so a binary built outside a tag release is
// never mistaken for one: the previous hard-coded "v4.3.2" outranked every
// real release tag, so self-update would have believed any build was
// permanently up to date.
var RawVersion = "dev"

// RawBuildKind is "release" only for a tag build. A bool cannot be set with
// the Go linker's -X flag, so this is a string and only that exact value
// counts; anything else is a local build that update refuses to replace
// without --force.
var RawBuildKind = "local"

// Version is the current version of the MagicTools MCP server.
var Version = strings.TrimPrefix(RawVersion, "v")
