#!/bin/bash
# leak_finder.sh - Locate forbidden writes to the IDE pipe

echo "Scanning for Stdout Leaks in magictools source..."
echo "================================================"

# 1. Direct fmt calls that default to Stdout
echo "[CHECK] Direct fmt.Print calls (should use Fprintf to Stderr or a logger):"
grep -rnE "fmt\.(Print|Printf|Println)" . | grep -vE "Fprintf|os.Stderr|logger"

# 2. Direct os.Stdout usage outside of the main transport setup
echo -e "\n[CHECK] Explicit os.Stdout references:"
grep -rn "os.Stdout" . | grep -vE "transport_setup\.go|main\.go"

# 3. Built-in Go println (the 'silent killer' of JSON-RPC)
echo -e "\n[CHECK] Built-in println calls:"
grep -rn "println(" .

# 4. Un-redirected logging
echo -e "\n[CHECK] Potential un-redirected standard log calls:"
grep -rn "log\." . | grep -vE "logger\.|log\.SetOutput"

echo -e "\n================================================"
echo "Scan Complete."
