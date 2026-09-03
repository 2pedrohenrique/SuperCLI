package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/2pedrohenrique/SuperCLI/internal/config"
	processes "github.com/2pedrohenrique/SuperCLI/internal/process"
	"github.com/charmbracelet/x/ansi"
)

// Unused runtime methods deliberately remain unset: rendering must only read
// the selected viewport, never copy the complete log buffer.
type outputRuntime struct{ Runtime }

func (outputRuntime) Snapshot(string, string) processes.Snapshot {
	return processes.Snapshot{LineCount: 100, State: processes.Running}
}

func (outputRuntime) LineWindow(_, _ string, start, count int) []string {
	lines := make([]string, count)
	for i := range lines {
		lines[i] = fmt.Sprintf("log-%03d", start+i)
	}
	return lines
}

func outputModel() Model {
	return Model{
		runtime: outputRuntime{}, mode: normalMode, width: 120, height: 30,
		config:  config.Config{Workspaces: []config.Workspace{{ID: "app", Name: "My app", Processes: []config.ProcessSpec{{ID: "api", Name: "API"}, {ID: "web", Name: "Web"}}}}},
		process: map[string]int{}, scroll: map[string]int{},
	}
}

func press(m Model, code rune, text string) Model {
	updated, _ := m.handleKey(tea.KeyPressMsg{Code: code, Text: text})
	return updated.(Model)
}

func TestOutputFullscreenKeepsLogsAndHidesSidePanels(t *testing.T) {
	m := press(outputModel(), tea.KeyF7, "")
	plain := ansi.Strip(m.View().Content)
	for _, want := range []string{"OUTPUT", "FULLSCREEN", "My app", "API", "Web", "log-075", "log-099", "F7/Esc"} {
		if !strings.Contains(plain, want) {
			t.Errorf("fullscreen missing %q", want)
		}
	}
	if strings.Contains(plain, "WORKSPACES") || strings.Contains(plain, "SESSION") {
		t.Fatal("fullscreen still contains side panels")
	}
	for _, key := range []rune{tea.KeyF7, tea.KeyEscape} {
		got := press(m, key, "")
		if !strings.Contains(ansi.Strip(got.View().Content), "WORKSPACES") {
			t.Fatalf("%v did not restore tiles", key)
		}
	}
}

func TestOutputFullscreenPreservesSelectionAndModalReturn(t *testing.T) {
	m := outputModel()
	m.process["app"], m.scroll["app/web"] = 1, 10
	m = press(m, tea.KeyF7, "")
	for _, key := range []string{"c", "?", "l", "g", "n"} {
		modal := press(m, rune(key[0]), key)
		got := press(modal, tea.KeyEscape, "")
		if got.mode != normalMode || !strings.Contains(ansi.Strip(got.View().Content), "FULLSCREEN") {
			t.Fatalf("%s did not return to fullscreen output", key)
		}
		if got.process["app"] != 1 || got.scroll["app/web"] != 10 || got.terminalFullscreen {
			t.Fatal("fullscreen changed selection, scroll or terminal state")
		}
	}
}

func TestOutputScrollUsesActualViewportHeight(t *testing.T) {
	for _, fullscreen := range []bool{false, true} {
		m := outputModel()
		wantHeight := 20
		if fullscreen {
			m = press(m, tea.KeyF7, "")
			wantHeight = 25
		}
		if m.logHeight() != wantHeight {
			t.Fatalf("fullscreen=%v: log height = %d, want %d", fullscreen, m.logHeight(), wantHeight)
		}
		m = press(m, tea.KeyPgUp, "")
		if m.scroll["app/api"] != wantHeight-1 {
			t.Fatal("page scroll does not match viewport")
		}
		m = press(m, tea.KeyHome, "")
		if m.scroll["app/api"] != 100-wantHeight {
			t.Fatal("oldest log offset does not match viewport")
		}
		m = press(m, tea.KeyEnd, "")
		if m.scroll["app/api"] != 0 {
			t.Fatal("End did not resume following output")
		}
	}
}

func TestFullscreenOutputFitsWindowAfterResize(t *testing.T) {
	m := press(outputModel(), tea.KeyF7, "")
	for _, size := range [][2]int{{120, 30}, {80, 24}, {40, 10}, {10, 4}, {1, 1}} {
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = updated.(Model)
		content := m.View().Content
		if lipgloss.Height(content) != size[1] || lipgloss.Width(content) != size[0] {
			t.Fatalf("view is %dx%d, want %dx%d", lipgloss.Width(content), lipgloss.Height(content), size[0], size[1])
		}
	}
}

func TestFullscreenOutputSupportsProcessSwitchingAndEmptyWorkspace(t *testing.T) {
	m := press(outputModel(), tea.KeyF7, "")
	m = press(m, tea.KeyTab, "")
	if m.process["app"] != 1 || !m.outputFullscreen {
		t.Fatal("process switching left fullscreen")
	}
	m.config.Workspaces = nil
	if !strings.Contains(ansi.Strip(m.View().Content), "No process configured") {
		t.Fatal("empty fullscreen must explain how to add a process")
	}
}
