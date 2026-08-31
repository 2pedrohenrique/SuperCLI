package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/2pedrohenrique/SuperCLI/internal/config"
	gitops "github.com/2pedrohenrique/SuperCLI/internal/git"
	processes "github.com/2pedrohenrique/SuperCLI/internal/process"
	terminalpkg "github.com/2pedrohenrique/SuperCLI/internal/terminal"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestLogSeverityDoesNotTreatStderrAsError(t *testing.T) {
	normal := "00:52:00.463 ERR dotnet watch Building project"
	if got := styleLog(normal); got != normal {
		t.Fatalf("stderr-only line was styled as a failure: %q", got)
	}
	warning := "00:52:00.463 ERR dotnet watch warning ASP0019"
	if got := styleLog(warning); got == warning || ansi.Strip(got) != warning {
		t.Fatalf("warning severity was not styled correctly: %q", got)
	}
	errorLine := "00:52:00.463 OUT Build FAILED with error CS1001"
	if got := styleLog(errorLine); got == errorLine || ansi.Strip(got) != errorLine {
		t.Fatalf("error severity was not styled correctly: %q", got)
	}
}

func TestStoppedLifecycleUpdatesVisibleStatus(t *testing.T) {
	m := Model{}
	m.applyEvent(processes.Event{Kind: processes.StateEvent, WorkspaceID: "app", ProcessID: "api", State: processes.Stopped})
	if !strings.Contains(m.status, "stopped") || m.statusError {
		t.Fatalf("status = %q, error = %v", m.status, m.statusError)
	}
}

func TestModalUsesCurrentScreenInsteadOfAppendingBelowIt(t *testing.T) {
	m := Model{mode: helpMode, width: 120, height: 30}
	view := m.View().Content
	if !strings.Contains(ansi.Strip(view), "Keyboard shortcuts") {
		t.Fatal("help modal is not visible")
	}
	if lines := strings.Count(view, "\n") + 1; lines > 30 {
		t.Fatalf("modal rendered %d lines into a 30-line terminal", lines)
	}
}

func TestQuitConfirmationIsVisibleWithinTerminal(t *testing.T) {
	m := Model{mode: confirmQuitMode, width: 80, height: 24}
	view := m.View().Content
	if !strings.Contains(ansi.Strip(view), "Stop services and quit?") {
		t.Fatal("quit confirmation is not visible")
	}
	if lines := strings.Count(view, "\n") + 1; lines > 24 {
		t.Fatalf("confirmation rendered %d lines into a 24-line terminal", lines)
	}
}

func TestHomeScreenContainsBrandAndNavigation(t *testing.T) {
	m := Model{mode: homeMode, width: 120, height: 32}
	plain := ansi.Strip(m.View().Content)
	for _, expected := range []string{"SUPERCLI", "OPEN WORKSTATION", "T  TERMINAL", "WHAT'S NEW", "v0.3.1-alpha"} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("home screen missing %q", expected)
		}
	}
	for _, hidden := range []string{"N  PROJECTS", "microkernel"} {
		if strings.Contains(plain, hidden) {
			t.Fatalf("home screen should not expose %q", hidden)
		}
	}
}

func TestTerminalShortcutWorksFromHome(t *testing.T) {
	m := Model{mode: homeMode}
	updated, cmd := m.handleHome(tea.KeyPressMsg{Code: 't', Text: "t"})
	got := updated.(Model)
	if got.mode != normalMode || cmd == nil {
		t.Fatalf("home terminal shortcut returned mode %v and command %v", got.mode, cmd)
	}
}

func TestPasteWorksInActionAndProjectInputs(t *testing.T) {
	actionModel := Model{mode: formMode, pending: &config.Action{Inputs: []config.ActionInput{{ID: "message"}}}}
	updated, _ := actionModel.Update(tea.PasteMsg{Content: "copied value\r\nwith two lines\r\n"})
	if got := updated.(Model).input; got != "copied value with two lines" {
		t.Fatalf("action paste = %q", got)
	}

	projectModel := Model{mode: formMode, form: formState{fields: []formField{{label: "Path"}}}}
	updated, _ = projectModel.Update(tea.PasteMsg{Content: `C:\Projects\App`})
	if got := updated.(Model).form.input; got != `C:\Projects\App` {
		t.Fatalf("project paste = %q", got)
	}
}

func TestTerminalPrintableTextPreservesShiftAndCapsLock(t *testing.T) {
	for _, key := range []tea.Key{
		{Code: 'a', ShiftedCode: 'A', Text: "A", Mod: tea.ModShift},
		{Code: 'a', ShiftedCode: 'A', Text: "A", Mod: tea.ModCapsLock},
		{Code: '1', ShiftedCode: '!', Text: "!", Mod: tea.ModShift},
	} {
		if got := terminalPrintableText(key); got != key.Text {
			t.Fatalf("terminalPrintableText(%+v) = %q", key, got)
		}
	}
}

func TestTerminalDetachRecognizesWindowsControlCharacter(t *testing.T) {
	if !isTerminalControl(tea.KeyPressMsg{Code: '\x1d'}, ']', '\x1d') {
		t.Fatal("Ctrl+] control character was not recognized")
	}
	if !isTerminalControl(tea.KeyPressMsg{Code: ']', BaseCode: ']', Mod: tea.ModCtrl}, ']', '\x1d') {
		t.Fatal("Ctrl+] modified key was not recognized")
	}
}

func TestF6DetachesEmbeddedTerminal(t *testing.T) {
	m := Model{mode: terminalMode, process: map[string]int{}, scroll: map[string]int{}}
	updated, _ := m.handleTerminal(tea.KeyPressMsg{Code: tea.KeyF6})
	if got := updated.(Model).mode; got != normalMode {
		t.Fatalf("F6 left terminal in mode %v", got)
	}
}

func TestF7TogglesTerminalFullscreenAndResizesViewport(t *testing.T) {
	m := Model{mode: terminalMode, width: 120, height: 30, terminal: terminalpkg.NewSession(), process: map[string]int{}, scroll: map[string]int{}}
	updated, _ := m.handleTerminal(tea.KeyPressMsg{Code: tea.KeyF7})
	got := updated.(Model)
	if !got.terminalFullscreen {
		t.Fatal("F7 did not enable fullscreen")
	}
	if width, height := got.terminalSize(); width != 120 || height != 28 {
		t.Fatalf("fullscreen terminal size = %dx%d", width, height)
	}
	plain := ansi.Strip(got.View().Content)
	if !strings.Contains(plain, "FULLSCREEN") || strings.Contains(plain, "WORKSPACES") {
		t.Fatalf("fullscreen terminal view is incorrect:\n%s", plain)
	}
}

func TestGitMenuExposesPullAllAsPullVariant(t *testing.T) {
	if !strings.Contains(gitMenu[1], "pull all") || !strings.Contains(gitMenu[1], "every repository") {
		t.Fatalf("git menu pull all item = %q", gitMenu[1])
	}
}

func TestTerminalMouseCoordinatesAreRelativeToThePTY(t *testing.T) {
	event := terminalMouseEvent(tea.MouseClickMsg{X: 30, Y: 10, Button: tea.MouseLeft}, 26, 4, 80, 20)
	click, ok := event.(uv.MouseClickEvent)
	if !ok || click.X != 4 || click.Y != 6 {
		t.Fatalf("translated mouse event = %#v", event)
	}
	if event := terminalMouseEvent(tea.MouseClickMsg{X: 2, Y: 2, Button: tea.MouseLeft}, 26, 4, 80, 20); event != nil {
		t.Fatalf("outside click should not reach PTY: %#v", event)
	}
}

func TestGitWorkflowConfirmationExplainsBranchAndChangePolicy(t *testing.T) {
	branch := gitConfirmationText(&gitops.Request{Operation: gitops.FeatureBranch, Task: "123", KeepChanges: true})
	if !strings.Contains(branch, "feature-123") || !strings.Contains(branch, "committed") || !strings.Contains(branch, "pushed") {
		t.Fatalf("branch confirmation is incomplete: %q", branch)
	}
	checkout := gitConfirmationText(&gitops.Request{Operation: gitops.Checkout, Branch: "develop", KeepChanges: false})
	if !strings.Contains(checkout, "develop") || !strings.Contains(checkout, "stash") {
		t.Fatalf("checkout confirmation is incomplete: %q", checkout)
	}
}

func TestTileSurfaceMatchesApplicationBackground(t *testing.T) {
	if colorPanel != colorDark {
		t.Fatal("tile and application backgrounds must match to avoid ANSI reset artifacts")
	}
}

func TestCurrentBranchBelongsToSelectedProcess(t *testing.T) {
	m := Model{
		config:  config.Config{Workspaces: []config.Workspace{{ID: "app", Processes: []config.ProcessSpec{{ID: "api"}, {ID: "web"}}}}},
		process: map[string]int{"app": 0}, branchKey: "app/api", branch: "feature-123",
	}
	if got := m.currentBranch(); got != "feature-123" {
		t.Fatalf("currentBranch() = %q", got)
	}
	m.process["app"] = 1
	if got := m.currentBranch(); got != "" {
		t.Fatalf("branch leaked into another process: %q", got)
	}
}

func TestTileRespectsRequestedDimensions(t *testing.T) {
	tile := renderTile("OUTPUT", "hello", 42, 12, true)
	if got := lipgloss.Width(tile); got != 42 {
		t.Fatalf("tile width = %d", got)
	}
	if got := lipgloss.Height(tile); got != 12 {
		t.Fatalf("tile height = %d", got)
	}
}

func TestSplitArgsPreservesWindowsPathsAndQuotedValues(t *testing.T) {
	got := splitArgs(`--config C:\work\app.json --name "My App"`)
	want := []string{"--config", `C:\work\app.json`, "--name", "My App"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("splitArgs = %#v, want %#v", got, want)
	}
}
