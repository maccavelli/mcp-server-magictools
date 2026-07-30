// Package dag provides functionality for the dag subsystem.
package dag

import (
	"fmt"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

// TopologicalSort implements Kahn's Algorithm to sort tools based on their DependsOn field.
func TopologicalSort(tools []*db.ToolRecord) ([]*db.ToolRecord, error) {
	// Build the graph and calculate in-degrees
	inDegree := make(map[string]int)
	adjList := make(map[string][]string) // A -> B (A must run before B)
	toolMap := make(map[string]*db.ToolRecord)

	for _, t := range tools {
		if t == nil {
			continue
		}
		toolMap[t.URN] = t
		inDegree[t.URN] = 0 // Initialize in-degree
	}

	// Build edges
	for _, t := range tools {
		if t == nil {
			continue
		}
		for _, dep := range t.DependsOn {
			// If Tool B DependsOn Tool A, then A -> B.
			adjList[dep] = append(adjList[dep], t.URN)
			inDegree[t.URN]++
		}
	}

	// Find nodes with no incoming edges
	var queue []string
	for urn, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, urn)
		}
	}

	var sortedURNs []string
	var count int

	// Kahn's algorithm
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		sortedURNs = append(sortedURNs, u)
		count++

		for _, v := range adjList[u] {
			inDegree[v]--
			if inDegree[v] == 0 {
				queue = append(queue, v)
			}
		}
	}

	if count != len(toolMap) {
		return nil, fmt.Errorf("cycle detected in DAG")
	}

	// Map URNs back to ToolRecords
	var sortedTools []*db.ToolRecord
	for _, urn := range sortedURNs {
		sortedTools = append(sortedTools, toolMap[urn])
	}

	return sortedTools, nil
}
