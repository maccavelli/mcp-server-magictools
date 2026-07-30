package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDashboardSearch(t *testing.T) {
	runDashboardSearch("test-query")
}

func TestDashboardModel(t *testing.T) {
	m := model{}
	cmd := m.Init()
	if cmd != nil {
		t.Error("expected nil cmd")
	}

	// Update tests
	msg1 := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	newModel, _ := m.Update(msg1)
	m2 := newModel.(model)
	if m2.activeTab != 1 {
		t.Error("expected activeTab to be 1")
	}

	msg2 := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}
	newModel2, _ := m2.Update(msg2)
	m3 := newModel2.(model)
	if m3.activeTab != 0 {
		t.Error("expected activeTab to be 0")
	}

	msg3 := tea.WindowSizeMsg{Width: 100, Height: 100}
	newModel3, _ := m3.Update(msg3)
	m4 := newModel3.(model)
	if m4.windowWidth != 100 {
		t.Error("expected windowWidth to be 100")
	}

	metrics := metricsMsg{
		Snapshot: make(map[string]any),
		Logs:     []string{"log1"},
	}
	newModel4, _ := m4.Update(metrics)
	m5 := newModel4.(model)
	if len(m5.metrics.Logs) != 1 {
		t.Error("expected 1 log")
	}

	// Test quit via key
	msgQuit := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	_, quitCmd := m5.Update(msgQuit)
	if quitCmd == nil {
		t.Error("expected quit command")
	}

	// View tests
	for i := 0; i <= tabQuit; i++ {
		m5.activeTab = i
		viewStr := m5.View()
		if len(viewStr) == 0 {
			t.Errorf("expected view string for tab %d", i)
		}
	}
}
