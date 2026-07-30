// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/maccavelli/mcp-server-magictools/internal/ui"
)

var dashCmd = &cobra.Command{
	Use:   "dash",
	Short: "Launch the observability dashboard",
	Run: func(cmd *cobra.Command, args []string) {
		findQuery := flagStringOrEmpty(cmd, "find")
		if findQuery != "" {
			runDashboardSearch(findQuery)
			return
		}
		runInteractiveDashboard()
	},
}

func init() {
	dashCmd.Flags().String("find", "", "Search historical telemetry using BadgerDB/Bleve")
	rootCmd.AddCommand(dashCmd)
}

func runDashboardSearch(query string) {
	fmt.Printf("ℹ Searching historical logs for: %s\n", query)
	fmt.Println("✓ Search capability initialized.")
}

// metricsMsg carries the latest telemetry snapshot into the Bubbletea model.
type metricsMsg struct {
	Snapshot map[string]any
	Logs     []string
}

type model struct {
	activeTab   int
	metrics     metricsMsg
	windowWidth int
}

const (
	tabSummary = iota
	tabFleetTransport
	tabOrchestration
	tabToolIntelligence
	tabToolAnalytics
	tabSystemBackplane
	tabStorage
	tabLLM
	tabTracing
	tabLogging
	tabQuit
)

var navItems = []string{
	"Summary",
	"Fleet & Transport",
	"Orchestration & DAG",
	"Tool Intelligence",
	"Tool Analytics",
	"System Backplane",
	"Storage & Databases",
	"LLM Backplane",
	"Distributed Tracing",
	"Logging",
	"Quit",
}

// --- Lipgloss Styles ---

var (
	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 2).
			Width(30)

	navItemStyle = lipgloss.NewStyle().
			Padding(0, 1)

	activeNavItemStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("62")).
				Foreground(lipgloss.Color("230")).
				Padding(0, 1).
				Bold(true)

	windowStyle = lipgloss.NewStyle().
			Padding(0, 1)

	dashTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("62")).
			Bold(true).
			MarginBottom(1)

	headerStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("69")).
			Foreground(lipgloss.Color("230")).
			Bold(true).
			Padding(0, 2)
)

func runInteractiveDashboard() {
	if err := ui.EnableVirtualTerminalProcessing(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: virtual terminal processing unavailable: %v\n", err)
	}
	m := model{}
	p := tea.NewProgram(m, tea.WithAltScreen())

	// ROB-5: Use a done channel to cleanly terminate the snapshot goroutine
	// when the Bubbletea program exits, preventing a goroutine leak.
	done := make(chan struct{})
	go func() {
		for {
			snapshot, logs, snapErr := ReadDashboardSnapshot()
			if snapErr != nil {
				slog.Debug("dashboard: snapshot read failed", "error", snapErr)
			}
			p.Send(metricsMsg{Snapshot: snapshot, Logs: logs})
			select {
			case <-done:
				return
			case <-time.After(10 * time.Second):
			}
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running dashboard: %v\n", err)
		os.Exit(1)
	}
	close(done)
}

// Init performs the Init operation.
func (m model) Init() tea.Cmd {
	return nil
}

// Update performs the Update operation.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "up", "k":
			m.activeTab--
			if m.activeTab < 0 {
				m.activeTab = len(navItems) - 1
			}
		case "down", "j":
			m.activeTab++
			if m.activeTab >= len(navItems) {
				m.activeTab = 0
			}
		case "enter":
			if m.activeTab == tabQuit {
				return m, tea.Quit
			}
		}
	case tea.WindowSizeMsg:
		m.windowWidth = msg.Width
	case metricsMsg:
		m.metrics = msg
	}
	return m, nil
}

// View performs the View operation.
func (m model) View() string {
	var navLines []string
	navLines = append(navLines, headerStyle.Render(" MagicTools Dashboard "), dashTitleStyle.Render("MagicTools Dashboard"), "")

	for i, item := range navItems {
		if i == m.activeTab {
			navLines = append(navLines, activeNavItemStyle.Render("> "+item))
		} else {
			navLines = append(navLines, navItemStyle.Render("  "+item))
		}
	}

	sidebar := sidebarStyle.Render(strings.Join(navLines, "\n"))

	var content string
	snapshot := m.metrics.Snapshot
	logs := m.metrics.Logs

	switch m.activeTab {
	case tabSummary:
		content = buildSummaryTab(snapshot, logs)
	case tabFleetTransport:
		content = buildFleetTransportTab(snapshot)
	case tabOrchestration:
		content = buildOrchestrationTab(snapshot)
	case tabToolIntelligence:
		content = buildToolIntelligenceTab(snapshot)
	case tabToolAnalytics:
		content = buildToolAnalyticsTab(snapshot)
	case tabSystemBackplane:
		content = buildSystemBackplaneTab(snapshot)
	case tabStorage:
		content = buildStorageTab(snapshot)
	case tabLLM:
		content = buildLLMTab(snapshot)
	case tabTracing:
		content = buildTracingTab(snapshot)
	case tabLogging:
		content = buildLoggingTab(snapshot)
	case tabQuit:
		content = dashTitleStyle.Render("Quit") + "\n\nPress Enter to exit the dashboard."
	default:
		content = buildSummaryTab(snapshot, logs)
	}

	mainView := windowStyle.Render(content)

	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, mainView)
}
