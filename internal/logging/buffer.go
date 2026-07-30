package logging

import "github.com/maccavelli/mcplib"

// GlobalLogBuffer is the in-memory ring buffer for standard get_internal_logs
var GlobalLogBuffer = mcplib.NewLogBuffer()
