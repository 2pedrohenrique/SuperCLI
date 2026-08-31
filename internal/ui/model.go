package ui

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/2pedrohenrique/SuperCLI/internal/action"
	"github.com/2pedrohenrique/SuperCLI/internal/config"
	gitops "github.com/2pedrohenrique/SuperCLI/internal/git"
	processes "github.com/2pedrohenrique/SuperCLI/internal/process"
	terminalpkg "github.com/2pedrohenrique/SuperCLI/internal/terminal"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

const uiVersion = "0.3.1-alpha"

type Runtime interface {
	Config() config.Config
	Events() <-chan processes.Event
	StartProcess(workspaceID, processID string) error
	StopProcess(workspaceID, processID string) error
	StartWorkspace(workspaceID string) error
	StopAll() error
	Snapshot(workspaceID, processID string) processes.Snapshot
	Lines(workspaceID, processID string, n int) []string
	LineWindow(workspaceID, processID string, start, count int) []string
	Capture(workspaceID, processID string, n int) (string, error)
	ClearLogs(workspaceID, processID string) error
	RunAction(context.Context, config.Action) action.Result
	InteractiveCommand(context.Context, config.Action) *exec.Cmd
	ShellCommand(context.Context, string) *exec.Cmd
	RunGit(context.Context, string, gitops.Request) gitops.Result
	PullWorkspace(context.Context, string) gitops.Result
	CurrentBranch(context.Context, string) (string, error)
	AddWorkspace(config.Workspace) error
	AddProcess(string, config.ProcessSpec) error
	RemoveWorkspace(string) error
	RemoveProcess(string, string) error
	IsRunning() bool
}

type mode int

const (
	homeMode mode = iota
	normalMode
	whatsNewMode
	helpMode
	captureMode
	actionMode
	confirmActionMode
	confirmQuitMode
	projectMode
	gitMode
	formMode
	resultMode
	terminalMode
	confirmDeleteMode
	confirmGitMode
	confirmClearMode
)

type formPurpose int

const (
	addWorkspaceForm formPurpose = iota
	addProcessForm
	gitBranchForm
	gitCommitForm
	gitCheckoutForm
)

type formField struct {
	label       string
	placeholder string
	defaultVal  string
	optional    bool
}

type formState struct {
	purpose formPurpose
	title   string
	fields  []formField
	index   int
	values  []string
	input   string
	gitOp   gitops.Operation
}

type Model struct {
	runtime Runtime
	ctx     context.Context
	config  config.Config

	workspace int
	process   map[string]int
	scroll    map[string]int
	width     int
	height    int

	mode         mode
	menuCursor   int
	input        string
	pending      *config.Action
	pendingGit   *gitops.Request
	actionValues map[string]string
	actionInput  int
	form         formState

	status      string
	statusError bool
	resultTitle string
	resultBody  string
	resultError bool

	terminal           *terminalpkg.Session
	terminalTitle      string
	terminalFullscreen bool
	deleteProcess      bool
	branchKey          string
	branch             string
}

type processEventMsg processes.Event
type operationMsg struct {
	message string
	err     error
}
type actionResultMsg action.Result
type gitResultMsg gitops.Result
type branchMsg struct {
	key    string
	branch string
	err    error
}
type terminalEventMsg terminalpkg.Event
type terminalStartedMsg struct {
	title string
	err   error
}
type configChangedMsg struct {
	message     string
	workspaceID string
	err         error
}

func New(ctx context.Context, runtime Runtime) Model {
	return Model{runtime: runtime, ctx: ctx, config: runtime.Config(), process: make(map[string]int), scroll: make(map[string]int), status: "Workstation ready", mode: homeMode, terminal: terminalpkg.NewSession()}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(waitForEvent(m.runtime.Events()), waitForTerminal(m.terminal.Events()), m.refreshBranch())
}

func waitForEvent(events <-chan processes.Event) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return nil
		}
		return processEventMsg(event)
	}
}

func waitForTerminal(events <-chan terminalpkg.Event) tea.Cmd {
	return func() tea.Msg { event := <-events; return terminalEventMsg(event) }
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch value := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = value.Width, value.Height
		if m.terminal != nil && m.terminal.Running() {
			_ = m.terminal.Resize(m.terminalSize())
		}
	case processEventMsg:
		m.applyEvent(processes.Event(value))
		return m, waitForEvent(m.runtime.Events())
	case operationMsg:
		if value.err != nil || value.message != "" {
			m.setStatus(value.message, value.err)
		}
	case actionResultMsg:
		result := action.Result(value)
		body := result.Output
		if result.Err != nil {
			body = result.Err.Error() + "\n\n" + body
		}
		if body == "" {
			body = fmt.Sprintf("Completed in %s", result.Duration.Round(time.Millisecond))
		}
		m.showResult(result.Action.Name, body, result.Err != nil)
	case gitResultMsg:
		result := gitops.Result(value)
		body := result.Output
		if result.Err != nil {
			body = result.Err.Error() + "\n\n" + body
		}
		if body == "" {
			body = fmt.Sprintf("Git operation completed on %s", result.Branch)
		}
		m.showResult("Git • "+string(result.Operation), body, result.Err != nil)
		return m, m.refreshBranch()
	case branchMsg:
		if value.key == m.selectionKey() {
			m.branchKey = value.key
			m.branch = ""
			if value.err == nil {
				m.branch = ansi.Strip(strings.TrimSpace(value.branch))
			}
		}
	case terminalStartedMsg:
		if value.err != nil {
			m.setStatus("", value.err)
		} else {
			m.terminalTitle, m.mode, m.statusError = value.title, terminalMode, false
			_ = m.terminal.Resize(m.terminalSize())
			m.status = value.title + " opened inside SuperCLI"
		}
	case terminalEventMsg:
		event := terminalpkg.Event(value)
		if event.Kind == terminalpkg.ExitEvent {
			m.mode = normalMode
			m.terminalFullscreen = false
			if !terminalpkg.IsExpectedExit(event.Err) {
				m.setStatus("", event.Err)
			} else {
				m.setStatus(m.terminalTitle+" finished", nil)
			}
		}
		cmd := waitForTerminal(m.terminal.Events())
		if event.Kind == terminalpkg.ExitEvent {
			return m, tea.Batch(cmd, m.refreshBranch())
		}
		return m, cmd
	case configChangedMsg:
		m.setStatus(value.message, value.err)
		if value.err == nil {
			m.config = m.runtime.Config()
			m.workspace = min(m.workspace, len(m.config.Workspaces)-1)
			for index, workspace := range m.config.Workspaces {
				if workspace.ID == value.workspaceID {
					m.workspace = index
					break
				}
			}
			return m, m.refreshBranch()
		}
	case tea.PasteMsg:
		return m.handlePaste(value.Content)
	case tea.MouseMsg:
		if m.mode == terminalMode {
			m.handleTerminalMouse(value)
		}
	case tea.KeyPressMsg:
		return m.handleKey(value)
	}
	return m, nil
}

func (m Model) handlePaste(content string) (tea.Model, tea.Cmd) {
	if m.mode == terminalMode {
		m.terminal.Paste(content)
		return m, nil
	}
	content = singleLinePaste(content)
	switch m.mode {
	case captureMode:
		for _, r := range content {
			if r >= '0' && r <= '9' {
				m.input += string(r)
			}
		}
	case formMode:
		if m.pending != nil && len(m.pending.Inputs) > 0 {
			m.input += content
		} else {
			m.form.input += content
		}
	}
	return m, nil
}

func singleLinePaste(content string) string {
	content = strings.TrimRight(content, "\r\n")
	content = strings.ReplaceAll(content, "\r\n", " ")
	content = strings.ReplaceAll(content, "\r", " ")
	return strings.ReplaceAll(content, "\n", " ")
}

func (m *Model) applyEvent(event processes.Event) {
	if event.Kind == processes.AlertEvent {
		m.status = fmt.Sprintf("Issue detected in %s/%s — press c to capture context", event.WorkspaceID, event.ProcessID)
		m.statusError = true
		return
	}
	if event.Kind != processes.StateEvent {
		return
	}
	label := map[processes.State]string{processes.Starting: "starting", processes.Running: "running", processes.Stopping: "stopping", processes.Stopped: "stopped", processes.Exited: "finished", processes.Failed: "failed"}[event.State]
	m.status = fmt.Sprintf("%s/%s is %s", event.WorkspaceID, event.ProcessID, label)
	m.statusError = event.State == processes.Failed
	if event.State == processes.Failed {
		m.status += fmt.Sprintf(" (exit %d)", event.ExitCode)
	}
}

func (m *Model) setStatus(message string, err error) {
	m.status, m.statusError = message, err != nil
	if err != nil {
		m.status = err.Error()
	}
}
func (m *Model) showResult(title, body string, failed bool) {
	m.resultTitle, m.resultBody, m.resultError, m.mode = title, strings.TrimSpace(body), failed, resultMode
}

func (m Model) handleKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.mode == terminalMode {
		return m.handleTerminal(key)
	}
	if key.Keystroke() == "ctrl+c" {
		return m, m.stopAndQuit()
	}
	switch m.mode {
	case homeMode:
		return m.handleHome(key)
	case whatsNewMode:
		m.mode = homeMode
		return m, nil
	case helpMode, resultMode:
		m.mode = normalMode
		return m, nil
	case captureMode:
		return m.handleCapture(key)
	case actionMode:
		return m.handleActionMenu(key)
	case confirmActionMode:
		return m.handleActionConfirmation(key)
	case confirmQuitMode:
		return m.handleQuitConfirmation(key)
	case confirmDeleteMode:
		return m.handleDeleteConfirmation(key)
	case confirmGitMode:
		return m.handleGitConfirmation(key)
	case confirmClearMode:
		return m.handleClearConfirmation(key)
	case projectMode:
		return m.handleProjectMenu(key)
	case gitMode:
		return m.handleGitMenu(key)
	case formMode:
		return m.handleForm(key)
	default:
		return m.handleNormal(key)
	}
}

func (m Model) handleHome(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.Keystroke() {
	case "enter", "w":
		m.mode = normalMode
	case "u":
		m.mode = whatsNewMode
	case "t":
		m.mode = normalMode
		return m, m.openShell()
	case "q":
		if m.runtime.IsRunning() || (m.terminal != nil && m.terminal.Running()) {
			m.mode = confirmQuitMode
			return m, nil
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleNormal(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if text := key.Key().Text; len(text) == 1 && text[0] >= '1' && text[0] <= '9' {
		index := int(text[0] - '1')
		if index < len(m.config.Workspaces) {
			m.workspace = index
		}
		return m, m.refreshBranch()
	}
	switch key.Keystroke() {
	case "q":
		if m.runtime.IsRunning() || (m.terminal != nil && m.terminal.Running()) {
			m.mode = confirmQuitMode
			return m, nil
		}
		return m, tea.Quit
	case "?":
		m.mode = helpMode
	case "m":
		m.mode = homeMode
	case "[":
		m.workspace = cycle(m.workspace-1, len(m.config.Workspaces))
		return m, m.refreshBranch()
	case "]":
		m.workspace = cycle(m.workspace+1, len(m.config.Workspaces))
		return m, m.refreshBranch()
	case "tab", "right":
		m.moveProcess(1)
		return m, m.refreshBranch()
	case "shift+tab", "left":
		m.moveProcess(-1)
		return m, m.refreshBranch()
	case "up", "k":
		m.moveScroll(1)
	case "down", "j":
		m.moveScroll(-1)
	case "pgup":
		m.moveScroll(max(1, m.logHeight()-1))
	case "pgdown":
		m.moveScroll(-max(1, m.logHeight()-1))
	case "home":
		m.scrollToOldest()
	case "end", "f":
		m.scroll[m.selectionKey()] = 0
	case "enter":
		if w, p, ok := m.selection(); ok {
			return m, runOperation(func() error { return m.runtime.StartProcess(w.ID, p.ID) }, "Process started")
		}
	case "x":
		if w, p, ok := m.selection(); ok {
			return m, runOperation(func() error { return m.runtime.StopProcess(w.ID, p.ID) }, "")
		}
	case "a":
		w := m.currentWorkspace()
		return m, runOperation(func() error { return m.runtime.StartWorkspace(w.ID) }, "Workspace started")
	case "X":
		return m, runOperation(m.runtime.StopAll, "")
	case "c":
		if _, _, ok := m.selection(); ok {
			m.mode, m.input = captureMode, ""
		}
	case "l":
		if w, p, ok := m.selection(); ok {
			if m.runtime.Snapshot(w.ID, p.ID).LineCount == 0 {
				m.setStatus("Output is already empty", nil)
			} else {
				m.mode = confirmClearMode
			}
		}
	case "p":
		if len(m.currentWorkspace().Actions) == 0 {
			m.setStatus("No actions configured", fmt.Errorf("no actions available"))
		} else {
			m.mode, m.menuCursor = actionMode, 0
		}
	case "n":
		m.mode, m.menuCursor = projectMode, 0
	case "g":
		if _, _, ok := m.selection(); ok {
			m.mode, m.menuCursor = gitMode, 0
		}
	case "t":
		return m, m.openShell()
	}
	return m, nil
}

func (m Model) handleCapture(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.Keystroke() {
	case "esc":
		m.mode, m.input = normalMode, ""
	case "backspace":
		m.input = removeLastRune(m.input)
	case "enter":
		value := m.input
		if value == "" {
			value = "10"
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 {
			m.setStatus("Enter a positive number", fmt.Errorf("invalid line count"))
			return m, nil
		}
		w, p, ok := m.selection()
		if !ok {
			m.mode = normalMode
			return m, nil
		}
		path, err := m.runtime.Capture(w.ID, p.ID, n)
		m.mode, m.input = normalMode, ""
		if err != nil && path != "" {
			m.status, m.statusError = "Capture saved to "+path+" • "+err.Error(), true
		} else {
			m.setStatus("Capture saved to "+path, err)
		}
	default:
		text := key.Key().Text
		if text != "" && strings.IndexFunc(text, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
			m.input += text
		}
	}
	return m, nil
}

func (m Model) handleActionMenu(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	actions := m.currentWorkspace().Actions
	selected, cmd := m.menuKey(key, len(actions))
	if key.Keystroke() == "esc" {
		m.mode = normalMode
		return m, nil
	}
	if !selected {
		return m, cmd
	}
	value := actions[m.menuCursor]
	m.pending = &value
	if len(value.Inputs) > 0 {
		m.actionValues, m.actionInput, m.input = make(map[string]string), 0, ""
		m.mode = formMode
		return m, nil
	}
	if value.Confirm {
		m.mode = confirmActionMode
		return m, nil
	}
	m.mode = normalMode
	return m, m.runAction(value)
}

func (m Model) handleActionConfirmation(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.Keystroke() {
	case "y", "s":
		v := *m.pending
		m.pending, m.mode = nil, normalMode
		return m, m.runAction(v)
	case "n", "esc":
		m.pending, m.mode = nil, normalMode
	}
	return m, nil
}
func (m Model) handleQuitConfirmation(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.Keystroke() {
	case "y", "s":
		return m, m.stopAndQuit()
	case "n", "esc":
		m.mode = normalMode
	}
	return m, nil
}

func (m Model) handleDeleteConfirmation(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.Keystroke() {
	case "y", "s":
		workspace, process, ok := m.selection()
		if !ok {
			m.mode = normalMode
			return m, nil
		}
		m.mode = normalMode
		if m.deleteProcess {
			return m, func() tea.Msg {
				err := m.runtime.RemoveProcess(workspace.ID, process.ID)
				return configChangedMsg{message: "Process removed", workspaceID: workspace.ID, err: err}
			}
		}
		return m, func() tea.Msg {
			err := m.runtime.RemoveWorkspace(workspace.ID)
			return configChangedMsg{message: "Workspace removed", workspaceID: "", err: err}
		}
	case "n", "esc":
		m.mode = normalMode
	}
	return m, nil
}

func (m Model) handleGitConfirmation(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.Keystroke() {
	case "y", "s":
		request := *m.pendingGit
		m.pendingGit, m.mode = nil, normalMode
		return m, m.runGit(request)
	case "n", "esc":
		m.pendingGit, m.mode = nil, normalMode
	}
	return m, nil
}

func (m Model) handleClearConfirmation(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.Keystroke() {
	case "y", "s":
		workspace, process, ok := m.selection()
		m.mode = normalMode
		if !ok {
			return m, nil
		}
		m.scroll[m.selectionKey()] = 0
		return m, runOperation(func() error { return m.runtime.ClearLogs(workspace.ID, process.ID) }, "Output cleared")
	case "n", "esc":
		m.mode = normalMode
	}
	return m, nil
}

func (m Model) handleProjectMenu(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	selected, cmd := m.menuKey(key, 4)
	if key.Keystroke() == "esc" {
		m.mode = normalMode
		return m, nil
	}
	if !selected {
		return m, cmd
	}
	if m.menuCursor == 0 {
		m.form = formState{purpose: addWorkspaceForm, title: "Add project workspace", fields: []formField{{"Project name", "My application", "", false}, {"Project directory", `C:\path\to\project`, "", false}, {"Process name", "Backend", "Backend", false}, {"Command", "dotnet", "", false}, {"Arguments", "watch run (optional)", "", true}}, values: make([]string, 5)}
	} else if m.menuCursor == 1 {
		m.form = formState{purpose: addProcessForm, title: "Add process to " + m.currentWorkspace().Name, fields: []formField{{"Process name", "Frontend", "", false}, {"Working directory", `C:\path\to\frontend`, "", false}, {"Command", "npm", "", false}, {"Arguments", "start (optional)", "", true}}, values: make([]string, 4)}
	} else {
		m.deleteProcess = m.menuCursor == 2
		m.mode = confirmDeleteMode
		return m, nil
	}
	m.mode = formMode
	return m, nil
}

var gitMenu = []string{"pull       • update current branch", "pull all   • update every repository in this workspace", "push       • publish current branch", "checkout   • switch branch", "cb         • create and publish feature branch", "cb         • create and publish hotfix branch", "cm         • commit feature changes", "cm         • commit hotfix changes", "td         • open shell in project directory"}

func (m Model) handleGitMenu(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	selected, cmd := m.menuKey(key, len(gitMenu))
	if key.Keystroke() == "esc" {
		m.mode = normalMode
		return m, nil
	}
	if !selected {
		return m, cmd
	}
	switch m.menuCursor {
	case 0:
		m.mode = normalMode
		return m, m.runGit(gitops.Request{Operation: gitops.Pull})
	case 1:
		m.mode = normalMode
		return m, m.pullWorkspace()
	case 2:
		m.mode = normalMode
		return m, m.runGit(gitops.Request{Operation: gitops.Push})
	case 3:
		m.form = formState{purpose: gitCheckoutForm, title: "Switch branch (checkout)", fields: []formField{{"Target branch", "develop", "", false}, {"Carry current changes?", "y / n (n creates a stash)", "y", false}}, values: make([]string, 2), gitOp: gitops.Checkout}
		m.mode = formMode
	case 4, 5:
		op := gitops.FeatureBranch
		if m.menuCursor == 5 {
			op = gitops.HotfixBranch
		}
		m.form = formState{purpose: gitBranchForm, title: "Create branch (cb)", fields: []formField{{"Task/chamado", "12345", "", false}, {"Carry current changes?", "y / n", "y", false}}, values: make([]string, 2), gitOp: op}
		m.mode = formMode
	case 6, 7:
		op := gitops.FeatureCommit
		if m.menuCursor == 7 {
			op = gitops.HotfixCommit
		}
		m.form = formState{purpose: gitCommitForm, title: "Commit changes (cm)", fields: []formField{{"Commit message", "describe the change", "", false}}, values: make([]string, 1), gitOp: op}
		m.mode = formMode
	case 8:
		m.mode = normalMode
		return m, m.openShell()
	}
	return m, nil
}

func (m Model) handleForm(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.pending != nil && len(m.pending.Inputs) > 0 {
		return m.handleActionInput(key)
	}
	switch key.Keystroke() {
	case "esc":
		m.mode = normalMode
		m.form = formState{}
	case "backspace":
		m.form.input = removeLastRune(m.form.input)
	case "enter":
		field := m.form.fields[m.form.index]
		value := strings.TrimSpace(m.form.input)
		if value == "" {
			value = field.defaultVal
		}
		if value == "" && !field.optional {
			m.setStatus(field.label+" is required", fmt.Errorf("required value"))
			return m, nil
		}
		m.form.values[m.form.index] = value
		m.form.index++
		m.form.input = ""
		if m.form.index == len(m.form.fields) {
			return m.submitForm()
		}
	default:
		if text := key.Key().Text; text != "" {
			m.form.input += text
		}
	}
	return m, nil
}

func (m Model) handleActionInput(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	input := m.pending.Inputs[m.actionInput]
	switch key.Keystroke() {
	case "esc":
		m.pending, m.mode, m.input = nil, normalMode, ""
	case "backspace":
		m.input = removeLastRune(m.input)
	case "enter":
		value := strings.TrimSpace(m.input)
		if value == "" {
			value = input.Default
		}
		if value == "" {
			m.setStatus(input.Prompt+" is required", fmt.Errorf("required value"))
			return m, nil
		}
		m.actionValues[input.ID] = value
		m.actionInput++
		m.input = ""
		if m.actionInput == len(m.pending.Inputs) {
			resolved := m.pending.Resolve(m.actionValues)
			m.pending = &resolved
			if resolved.Confirm {
				m.mode = confirmActionMode
				return m, nil
			}
			m.pending, m.mode = nil, normalMode
			return m, m.runAction(resolved)
		}
	default:
		if text := key.Key().Text; text != "" {
			m.input += text
		}
	}
	return m, nil
}

func (m Model) submitForm() (tea.Model, tea.Cmd) {
	form := m.form
	m.mode, m.form = normalMode, formState{}
	switch form.purpose {
	case addWorkspaceForm:
		w := config.Workspace{ID: slug(form.values[0]), Name: form.values[0], Processes: []config.ProcessSpec{newProcess(form.values[2], form.values[1], form.values[3], form.values[4])}}
		return m, func() tea.Msg {
			err := m.runtime.AddWorkspace(w)
			return configChangedMsg{message: "Project added and saved", workspaceID: w.ID, err: err}
		}
	case addProcessForm:
		w := m.currentWorkspace()
		p := newProcess(form.values[0], form.values[1], form.values[2], form.values[3])
		return m, func() tea.Msg {
			err := m.runtime.AddProcess(w.ID, p)
			return configChangedMsg{message: "Process added and saved", workspaceID: w.ID, err: err}
		}
	case gitBranchForm:
		keep, ok := parseConfirmation(form.values[1])
		if !ok {
			return m.restoreInvalidForm(form, "Use y/s for yes or n for no")
		}
		request := gitops.Request{Operation: form.gitOp, Task: form.values[0], KeepChanges: keep}
		m.pendingGit, m.mode = &request, confirmGitMode
		return m, nil
	case gitCommitForm:
		return m, m.runGit(gitops.Request{Operation: form.gitOp, Message: form.values[0]})
	case gitCheckoutForm:
		keep, ok := parseConfirmation(form.values[1])
		if !ok {
			return m.restoreInvalidForm(form, "Use y/s for yes or n for no")
		}
		request := gitops.Request{Operation: gitops.Checkout, Branch: form.values[0], KeepChanges: keep}
		m.pendingGit, m.mode = &request, confirmGitMode
		return m, nil
	}
	return m, nil
}

func parseConfirmation(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes", "s", "sim":
		return true, true
	case "n", "no", "nao", "não":
		return false, true
	default:
		return false, false
	}
}

func (m Model) restoreInvalidForm(form formState, message string) (tea.Model, tea.Cmd) {
	form.index = max(0, len(form.fields)-1)
	form.input = form.values[form.index]
	m.form, m.mode = form, formMode
	m.setStatus(message, fmt.Errorf("invalid confirmation"))
	return m, nil
}

func newProcess(name, dir, command, args string) config.ProcessSpec {
	return config.ProcessSpec{ID: slug(name), Name: name, Dir: dir, Command: command, Args: splitArgs(args), ReadyPatterns: []string{`(?i)(ready|listening|started|compiled successfully)`}, ErrorPatterns: []string{`(?i)(build failed|\berror\b|exception|panic|fatal)`}}
}

func (m *Model) menuKey(key tea.KeyPressMsg, length int) (bool, tea.Cmd) {
	switch key.Keystroke() {
	case "up", "k":
		m.menuCursor = cycle(m.menuCursor-1, length)
	case "down", "j":
		m.menuCursor = cycle(m.menuCursor+1, length)
	case "enter":
		return true, nil
	}
	return false, nil
}
func (m Model) runAction(v config.Action) tea.Cmd {
	if v.Interactive {
		return m.startTerminal(v.Name, m.runtime.InteractiveCommand(m.ctx, v))
	}
	return func() tea.Msg { return actionResultMsg(m.runtime.RunAction(m.ctx, v)) }
}
func (m Model) runGit(r gitops.Request) tea.Cmd {
	_, p, ok := m.selection()
	if !ok {
		return func() tea.Msg { return operationMsg{err: fmt.Errorf("select a process with a project directory")} }
	}
	return func() tea.Msg { return gitResultMsg(m.runtime.RunGit(m.ctx, p.Dir, r)) }
}

func (m Model) pullWorkspace() tea.Cmd {
	workspace := m.currentWorkspace()
	return func() tea.Msg { return gitResultMsg(m.runtime.PullWorkspace(m.ctx, workspace.ID)) }
}
func (m Model) openShell() tea.Cmd {
	if m.terminal != nil && m.terminal.Running() {
		return func() tea.Msg { return terminalStartedMsg{title: m.terminalTitle} }
	}
	_, p, ok := m.selection()
	if !ok {
		return func() tea.Msg { return operationMsg{err: fmt.Errorf("select a process with a project directory")} }
	}
	return m.startTerminal("Shell • "+p.Name, m.runtime.ShellCommand(m.ctx, p.Dir))
}

func (m Model) startTerminal(title string, command *exec.Cmd) tea.Cmd {
	width, height := m.terminalSize()
	return func() tea.Msg {
		return terminalStartedMsg{title: title, err: m.terminal.Start(m.ctx, command, width, height)}
	}
}

func (m Model) terminalSize() (int, int) {
	if m.terminalFullscreen {
		return max(20, m.width), max(5, m.height-2)
	}
	_, _, mainWidth := workstationWidths(m.width)
	return max(20, mainWidth-2), max(5, m.height-8)
}

func (m Model) refreshBranch() tea.Cmd {
	_, process, ok := m.selection()
	if !ok || strings.TrimSpace(process.Dir) == "" {
		return nil
	}
	key, dir := m.selectionKey(), process.Dir
	return func() tea.Msg {
		branch, err := m.runtime.CurrentBranch(m.ctx, dir)
		return branchMsg{key: key, branch: branch, err: err}
	}
}

func (m Model) handleTerminal(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Keystroke() == "f7" {
		m.terminalFullscreen = !m.terminalFullscreen
		_ = m.terminal.Resize(m.terminalSize())
		if m.terminalFullscreen {
			m.status = "Terminal fullscreen • F7 returns to tiles"
		} else {
			m.status = "Terminal tiled • F7 enters fullscreen"
		}
		return m, nil
	}
	if key.Keystroke() == "f6" || isTerminalControl(key, ']', '\x1d') {
		m.mode = normalMode
		m.terminalFullscreen = false
		m.status = "Embedded terminal detached • press t to return"
		return m, m.refreshBranch()
	}
	if isTerminalControl(key, '\\', '\x1c') {
		err := m.terminal.Close()
		m.mode = normalMode
		m.terminalFullscreen = false
		m.setStatus("Embedded terminal closed", err)
		return m, m.refreshBranch()
	}
	k := key.Key()
	if text := terminalPrintableText(k); text != "" {
		m.terminal.SendText(text)
		return m, nil
	}
	mod := uv.KeyMod(k.Mod) &^ (uv.ModCapsLock | uv.ModNumLock | uv.ModScrollLock)
	if mod&uv.ModShift != 0 && k.Code != uv.KeyTab {
		mod &^= uv.ModShift
	}
	m.terminal.SendKey(uv.Key{Text: k.Text, Mod: mod, Code: k.Code, ShiftedCode: k.ShiftedCode, BaseCode: k.BaseCode, IsRepeat: k.IsRepeat})
	return m, nil
}

func (m Model) handleTerminalMouse(message tea.MouseMsg) {
	originX, originY := 0, 1
	if !m.terminalFullscreen {
		sidebarWidth, _, _ := workstationWidths(m.width)
		originX, originY = sidebarWidth+2, 4
	}
	width, height := m.terminalSize()
	if event := terminalMouseEvent(message, originX, originY, width, height); event != nil {
		m.terminal.SendMouse(event)
	}
}

func terminalMouseEvent(message tea.MouseMsg, originX, originY, width, height int) uv.MouseEvent {
	mouse := message.Mouse()
	mouse.X -= originX
	mouse.Y -= originY
	if mouse.X < 0 || mouse.Y < 0 || mouse.X >= width || mouse.Y >= height {
		return nil
	}
	var event uv.MouseEvent
	switch message.(type) {
	case tea.MouseClickMsg:
		event = uv.MouseClickEvent(mouse)
	case tea.MouseReleaseMsg:
		event = uv.MouseReleaseEvent(mouse)
	case tea.MouseWheelMsg:
		event = uv.MouseWheelEvent(mouse)
	case tea.MouseMotionMsg:
		event = uv.MouseMotionEvent(mouse)
	}
	return event
}

func terminalPrintableText(key tea.Key) string {
	blocked := tea.ModCtrl | tea.ModAlt | tea.ModMeta | tea.ModHyper | tea.ModSuper
	if key.Text != "" && key.Mod&blocked == 0 {
		return key.Text
	}
	return ""
}

func isTerminalControl(key tea.KeyPressMsg, printable, control rune) bool {
	k := key.Key()
	return key.Keystroke() == "ctrl+"+string(printable) ||
		(k.Mod&tea.ModCtrl != 0 && (k.Code == printable || k.BaseCode == printable)) ||
		k.Code == control || k.BaseCode == control
}
func (m Model) stopAndQuit() tea.Cmd {
	return func() tea.Msg {
		if m.terminal != nil {
			_ = m.terminal.Close()
		}
		_ = m.runtime.StopAll()
		return tea.Quit()
	}
}
func runOperation(fn func() error, success string) tea.Cmd {
	return func() tea.Msg { return operationMsg{message: success, err: fn()} }
}

func (m *Model) moveProcess(delta int) {
	w := m.currentWorkspace()
	if len(w.Processes) > 0 {
		m.process[w.ID] = cycle(m.process[w.ID]+delta, len(w.Processes))
	}
}
func (m *Model) moveScroll(delta int) {
	w, p, ok := m.selection()
	if !ok {
		return
	}
	key := m.selectionKey()
	maximum := max(0, len(m.runtime.Lines(w.ID, p.ID, -1))-m.logHeight())
	m.scroll[key] = min(maximum, max(0, m.scroll[key]+delta))
}
func (m *Model) scrollToOldest() {
	w, p, ok := m.selection()
	if ok {
		m.scroll[m.selectionKey()] = max(0, len(m.runtime.Lines(w.ID, p.ID, -1))-m.logHeight())
	}
}
func (m Model) currentWorkspace() config.Workspace {
	if len(m.config.Workspaces) == 0 {
		return config.Workspace{}
	}
	return m.config.Workspaces[min(m.workspace, len(m.config.Workspaces)-1)]
}
func (m Model) selection() (config.Workspace, config.ProcessSpec, bool) {
	w := m.currentWorkspace()
	if len(w.Processes) == 0 {
		return w, config.ProcessSpec{}, false
	}
	i := min(m.process[w.ID], len(w.Processes)-1)
	return w, w.Processes[i], true
}
func (m Model) selectionKey() string {
	w, p, ok := m.selection()
	if !ok {
		return ""
	}
	return w.ID + "/" + p.ID
}
func (m Model) logHeight() int { return max(3, m.height-9) }

var (
	colorAccent = lipgloss.Color("#EF4444")
	colorBright = lipgloss.Color("#FB7185")
	colorGreen  = lipgloss.Color("#34D399")
	colorYellow = lipgloss.Color("#FBBF24")
	colorRed    = lipgloss.Color("#F43F5E")
	colorBlue   = lipgloss.Color("#60A5FA")
	colorText   = lipgloss.Color("#E5E7EB")
	colorMuted  = lipgloss.Color("#6B7280")
	// Tiles deliberately share the application background. Nested ANSI resets
	// (especially from PTY applications) otherwise expose darker rectangles.
	colorPanel    = colorDark
	colorRaised   = lipgloss.Color("#1F2937")
	colorDark     = lipgloss.Color("#080B12")
	logoStyle     = lipgloss.NewStyle().Bold(true).Foreground(colorBright)
	mutedStyle    = lipgloss.NewStyle().Foreground(colorMuted)
	goodStyle     = lipgloss.NewStyle().Foreground(colorGreen)
	warningStyle  = lipgloss.NewStyle().Foreground(colorYellow)
	failureStyle  = lipgloss.NewStyle().Foreground(colorRed)
	infoStyle     = lipgloss.NewStyle().Foreground(colorBlue)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(colorAccent)
	modalStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent).Background(colorPanel).Foreground(colorText).Padding(1, 3)
)

func (m Model) View() tea.View {
	w, h := m.width, m.height
	if w < 1 {
		w = 100
	}
	if h < 1 {
		h = 30
	}
	var content string
	switch m.mode {
	case homeMode:
		content = m.renderHome(w, h)
	case normalMode, terminalMode:
		if m.mode == terminalMode && m.terminalFullscreen {
			content = m.renderFullscreenTerminal(w, h)
		} else {
			content = m.renderBase(w, h)
		}
	default:
		content = m.renderModalScreen(w, h)
	}
	view := tea.NewView(content)
	view.AltScreen = true
	view.WindowTitle = "SuperCLI • Developer Workstation"
	view.BackgroundColor = colorDark
	view.ForegroundColor = colorText
	if m.mode == terminalMode {
		view.MouseMode = tea.MouseModeCellMotion
		if x, y, ok := m.terminalCursorPosition(); ok {
			view.Cursor = tea.NewCursor(x, y)
			view.Cursor.Color = colorBright
			view.Cursor.Shape = tea.CursorBar
		}
	}
	return view
}

func workstationWidths(width int) (sidebarWidth, inspectorWidth, mainWidth int) {
	sidebarWidth = 24
	if width < 100 {
		sidebarWidth = 21
	}
	if width >= 118 {
		inspectorWidth = 27
		mainWidth = width - sidebarWidth - inspectorWidth - 2
		return
	}
	mainWidth = width - sidebarWidth - 1
	return
}

func (m Model) terminalCursorPosition() (int, int, bool) {
	x, y, ok := m.terminal.CursorPosition()
	if !ok {
		return 0, 0, false
	}
	if m.terminalFullscreen {
		return min(max(0, x), max(0, m.width-1)), min(max(0, y+1), max(0, m.height-2)), true
	}
	sidebarWidth, _, mainWidth := workstationWidths(m.width)
	x = sidebarWidth + 2 + x
	y = 4 + y
	return min(x, sidebarWidth+mainWidth), min(y, m.height-3), true
}

func (m Model) renderBase(width, height int) string {
	if width < 64 || height < 16 {
		return m.renderCompact(width, height)
	}
	header := fit(logoStyle.Render("◆ SUPERCLI")+mutedStyle.Render("  v"+uiVersion+"  /  DEVELOPER WORKSTATION")+mutedStyle.Render("  ─  ")+m.workspaceHealth(), width)
	w, p, selected := m.selection()
	contextLine := w.Name
	if selected {
		contextLine += mutedStyle.Render("  ›  ") + p.Name
		if branch := m.currentBranch(); branch != "" {
			contextLine += mutedStyle.Render("  •  ") + infoStyle.Render("git:"+branch)
		}
		contextLine += mutedStyle.Render("  •  ") + p.Dir
	}
	contextLine = fit(contextLine, width)
	bodyHeight := max(8, height-5)
	sidebarWidth, inspectorWidth, mainWidth := workstationWidths(width)
	gap := " "
	var tiles []string
	tiles = append(tiles, renderTile("WORKSPACES", m.renderSidebar(sidebarWidth-2, bodyHeight-3), sidebarWidth, bodyHeight, false), gap)
	if width >= 118 {
		tiles = append(tiles, renderTile(m.mainTileTitle(), m.renderMain(mainWidth-2, bodyHeight-3), mainWidth, bodyHeight, true), gap)
		tiles = append(tiles, renderTile("SESSION", m.renderInspector(inspectorWidth-2, bodyHeight-3), inspectorWidth, bodyHeight, false))
	} else {
		tiles = append(tiles, renderTile(m.mainTileTitle(), m.renderMain(mainWidth-2, bodyHeight-3), mainWidth, bodyHeight, true))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, tiles...)
	icon := goodStyle.Render("●")
	statusText := m.status
	if m.statusError {
		icon = failureStyle.Render("●")
		statusText = failureStyle.Render(statusText)
	}
	status := fit(" "+icon+"  "+statusText, width)
	footer := fit("  m home  │  [ ] workspace  │  Tab process  │  Enter start  │  a all  │  x stop  │  c capture  │  l clear  │  n manage  │  g git  │  t terminal  │  ? help  │  q quit", width)
	return strings.Join([]string{header, contextLine, body, status, mutedStyle.Render(footer)}, "\n")
}

func (m Model) renderFullscreenTerminal(width, height int) string {
	header := fit(" "+logoStyle.Render("◆ "+strings.ToUpper(m.terminalTitle))+mutedStyle.Render("  •  FULLSCREEN"), width)
	body := fillLines(strings.Split(strings.TrimRight(m.terminal.Render(), "\n"), "\n"), width, max(1, height-2))
	footer := fit(" F7 tiles  │  F6 detach  │  Ctrl+\\ close  │  Ctrl+arrows navigate words  │  Ctrl+A line start", width)
	return strings.Join([]string{header, body, mutedStyle.Render(footer)}, "\n")
}

func (m Model) renderHome(width, height int) string {
	art := `╭─◆──────────────────── DEVELOPER WORKSTATION ───────────────────◆─╮
│  ███████╗██╗   ██╗██████╗ ███████╗██████╗  ██████╗██╗     ██╗  │
│  ██╔════╝██║   ██║██╔══██╗██╔════╝██╔══██╗██╔════╝██║     ██║  │
│  ███████╗██║   ██║██████╔╝█████╗  ██████╔╝██║     ██║     ██║  │
│  ╚════██║██║   ██║██╔═══╝ ██╔══╝  ██╔══██╗██║     ██║     ██║  │
│  ███████║╚██████╔╝██║     ███████╗██║  ██║╚██████╗███████╗██║  │
│  ╚══════╝ ╚═════╝ ╚═╝     ╚══════╝╚═╝  ╚═╝ ╚═════╝╚══════╝╚═╝  │
╰─◆────────────────── COMMAND YOUR WORKSPACE ───────────────────◆─╯`
	if width < 82 {
		art = `╭─◆─ SUPERCLI ───────────────────────────╮
│   ___ _   _ ___ ___ ___  ___ _    ___  │
│  / __| | | | _ \ __| _ \/ __| |  |_ _| │
│  \__ \ |_| |  _/ _||   / (__| |__ | |  │
│  |___/\___/|_| |___|_|_\\___|____|___| │
╰────── COMMAND YOUR WORKSPACE ──────────╯`
	}
	menu := selectedStyle.Render(" ENTER  OPEN WORKSTATION ") + "   " + mutedStyle.Render("T  TERMINAL") + "   " + mutedStyle.Render("U  WHAT'S NEW") + "   " + mutedStyle.Render("Q  QUIT")
	card := logoStyle.Render(art) + "\n\n" + logoStyle.Render("SUPERCLI") + mutedStyle.Render("  /  DEVELOPER WORKSTATION") + "\n" + failureStyle.Render("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━") + "\n" + lipgloss.NewStyle().Foreground(colorText).Render("Processes, logs, Git and terminals in one place.") + "\n\n" + menu + "\n\n" + mutedStyle.Render("v"+uiVersion+"  •  WORKFLOWS  •  LOGS  •  GIT  •  TERMINAL")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, card, lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(colorDark)))
}

func (m Model) mainTileTitle() string {
	if m.mode == terminalMode {
		return strings.ToUpper(m.terminalTitle) + "  •  F7 FULLSCREEN  •  F6 DETACH"
	}
	return "OUTPUT"
}

func renderTile(title, body string, width, height int, active bool) string {
	border := colorMuted
	borderStyle := lipgloss.RoundedBorder()
	marker := "◇"
	if active {
		border = colorAccent
		borderStyle = lipgloss.DoubleBorder()
		marker = "◆"
	}
	heading := " " + logoStyle.Render(marker+" "+title) + " "
	content := heading + "\n" + body
	return lipgloss.NewStyle().Width(max(3, width)).Height(max(3, height)).Border(borderStyle).BorderForeground(border).Background(colorPanel).Render(content)
}

func (m Model) workspaceHealth() string {
	w := m.currentWorkspace()
	running := 0
	for _, process := range w.Processes {
		state := m.runtime.Snapshot(w.ID, process.ID).State
		if state == processes.Running || state == processes.Starting {
			running++
		}
	}
	if running > 0 {
		return goodStyle.Render(fmt.Sprintf("%d/%d ACTIVE", running, len(w.Processes)))
	}
	return mutedStyle.Render(fmt.Sprintf("0/%d ACTIVE", len(w.Processes)))
}

func (m Model) renderSidebar(width, height int) string {
	activeWorkspaces := 0
	for _, workspace := range m.config.Workspaces {
		for _, process := range workspace.Processes {
			state := m.runtime.Snapshot(workspace.ID, process.ID).State
			if state == processes.Running || state == processes.Starting {
				activeWorkspaces++
				break
			}
		}
	}
	lines := []string{
		" " + mutedStyle.Render(fmt.Sprintf("%02d TOTAL  •  %02d ACTIVE", len(m.config.Workspaces), activeWorkspaces)),
		mutedStyle.Render(strings.Repeat("─", width)),
	}
	for i, w := range m.config.Workspaces {
		running := 0
		for _, p := range w.Processes {
			state := m.runtime.Snapshot(w.ID, p.ID).State
			if state == processes.Running || state == processes.Starting {
				running++
			}
		}
		icon := mutedStyle.Render("◇")
		if running > 0 {
			icon = goodStyle.Render("◆")
		}
		left := fmt.Sprintf(" %s %02d %s", icon, i+1, w.Name)
		right := mutedStyle.Render(fmt.Sprintf("%d/%d", running, len(w.Processes)))
		label := columns(left, right, width)
		if i == m.workspace {
			label = logoStyle.Render("▌") + " " + columns(lipgloss.NewStyle().Bold(true).Foreground(colorText).Render(fmt.Sprintf("%02d %s", i+1, w.Name)), right, max(1, width-2))
		}
		lines = append(lines, fit(label, width))
	}
	lines = append(lines, "", mutedStyle.Render(strings.Repeat("─", width)), " "+logoStyle.Render("WORKSPACE TOOLS"), " "+mutedStyle.Render("a start all  •  n manage"), " "+mutedStyle.Render("g git       •  p actions"))
	return fillLines(lines, width, height)
}

func (m Model) renderMain(width, height int) string {
	if m.mode == terminalMode {
		return fillLines(strings.Split(strings.TrimRight(m.terminal.Render(), "\n"), "\n"), width, height)
	}
	w := m.currentWorkspace()
	var tabs strings.Builder
	for i, p := range w.Processes {
		state := m.runtime.Snapshot(w.ID, p.ID).State
		label := " " + stateIcon(state) + " " + p.Name + " "
		if i == m.process[w.ID] {
			tabs.WriteString(selectedStyle.Render(label))
		} else {
			tabs.WriteString(" " + label)
		}
	}
	lines := []string{fit(tabs.String(), width), mutedStyle.Render(strings.Repeat("─", width))}
	lines = append(lines, m.visibleLogs(width, height-2)...)
	return fillLines(lines, width, height)
}

func (m Model) renderInspector(width, height int) string {
	w := m.currentWorkspace()
	lines := []string{" " + logoStyle.Render(strings.ToUpper(w.Name)), " " + mutedStyle.Render(fmt.Sprintf("%d PROCESSES  •  %d ACTIONS", len(w.Processes), len(w.Actions))), mutedStyle.Render(strings.Repeat("─", width)), " " + mutedStyle.Render("PROCESS STATUS")}
	for index, p := range w.Processes {
		state := m.runtime.Snapshot(w.ID, p.ID).State
		marker := " "
		nameStyle := lipgloss.NewStyle().Foreground(colorText)
		if index == m.process[w.ID] {
			marker = logoStyle.Render("›")
			nameStyle = nameStyle.Bold(true)
		}
		left := fmt.Sprintf(" %s %s %s", marker, stateIcon(state), nameStyle.Render(p.Name))
		lines = append(lines, columns(left, stateStyle(state).Render(strings.ToUpper(string(state))), width))
	}
	lines = append(lines, "", mutedStyle.Render(strings.Repeat("─", width)), " "+mutedStyle.Render("GIT CONTEXT"))
	if branch := m.currentBranch(); branch != "" {
		lines = append(lines, " "+infoStyle.Render(" "+branch), " "+mutedStyle.Render("g  open workflows"))
	} else {
		lines = append(lines, mutedStyle.Render(" ○ not a Git worktree"))
	}
	lines = append(lines, "", mutedStyle.Render(strings.Repeat("─", width)), " "+mutedStyle.Render("TERMINAL"), " "+terminalState(m.terminal), " "+mutedStyle.Render("t focus  •  F7 fullscreen"))
	return fillLines(lines, width, height)
}

func stateStyle(state processes.State) lipgloss.Style {
	switch state {
	case processes.Running:
		return goodStyle
	case processes.Starting, processes.Stopping:
		return warningStyle
	case processes.Failed:
		return failureStyle
	default:
		return mutedStyle
	}
}

func columns(left, right string, width int) string {
	space := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	return fit(left+strings.Repeat(" ", space)+right, width)
}

func (m Model) currentBranch() string {
	if m.branchKey == m.selectionKey() {
		return m.branch
	}
	return ""
}

func terminalState(session *terminalpkg.Session) string {
	if session != nil && session.Running() {
		return goodStyle.Render(" ● embedded session")
	}
	return mutedStyle.Render(" ○ no active session")
}

func (m Model) visibleLogs(width, height int) []string {
	w, p, ok := m.selection()
	if !ok {
		return []string{mutedStyle.Render("  No process configured. Press n to add one.")}
	}
	lineCount := m.runtime.Snapshot(w.ID, p.ID).LineCount
	scroll := min(m.scroll[m.selectionKey()], max(0, lineCount-height))
	end := max(0, lineCount-scroll)
	start := max(0, end-height)
	visible := m.runtime.LineWindow(w.ID, p.ID, start, end-start)
	result := make([]string, 0, height)
	if scroll > 0 {
		result = append(result, warningStyle.Render(fmt.Sprintf(" ↑ %d newer lines hidden • End/f resumes follow", scroll)))
		if len(visible) > 0 {
			visible = visible[1:]
		}
	}
	for _, line := range visible {
		result = append(result, styleLog(fit(" "+line, width)))
	}
	return result
}

func (m Model) renderCompact(width, height int) string {
	message := logoStyle.Render("SUPERCLI v"+uiVersion) + "\n\nTerminal too small for the workstation layout.\n" + fmt.Sprintf("Current: %dx%d  •  Minimum: 64x16\n\n", width, height) + mutedStyle.Render("Resize the terminal or press q to quit.")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, message)
}

func (m Model) renderModalScreen(width, height int) string {
	title, body, hint := "", "", "Esc  cancel"
	switch m.mode {
	case whatsNewMode:
		title, body, hint = "What's new • v0.3.1 alpha", whatsNewText(), "Any key  back to home"
	case helpMode:
		title, body, hint = "Keyboard shortcuts", helpText(), "Any key  close"
	case captureMode:
		title = "Capture log context"
		body = "How many latest lines should be saved?\n\n" + inputLine(m.input, "10")
		hint = "Enter  save    Esc  cancel"
	case actionMode:
		title, body = "Workspace actions", menuBody(actionNames(m.currentWorkspace().Actions), m.menuCursor, 13)
		hint = "↑/↓  select    Enter  run    Esc  close"
	case confirmActionMode:
		title, body = "Confirm action", fmt.Sprintf("Run %q?", m.pending.Name)
		hint = "Y  confirm    N/Esc  cancel"
	case confirmQuitMode:
		title = "Stop services and quit?"
		body = "One or more processes are still active.\nSuperCLI will stop their complete process trees before exiting."
		hint = "Y  stop and quit    N/Esc  stay"
	case confirmClearMode:
		title = "Clear selected output?"
		body = "This removes retained log lines for the selected process.\nThe running process itself is not stopped."
		hint = "Y  clear output    N/Esc  cancel"
	case projectMode:
		title = "Project manager"
		body = menuBody([]string{"Add a new project workspace", "Add a process to the current workspace", "Remove selected process", "Remove current workspace"}, m.menuCursor, 8)
		hint = "↑/↓  select    Enter  continue    Esc  close"
	case gitMode:
		title, body = "Git workflow", menuBody(gitMenu, m.menuCursor, 10)
		hint = "↑/↓  select    Enter  run    Esc  close"
	case formMode:
		return m.renderFormScreen(width, height)
	case resultMode:
		title, body = m.resultTitle, wrapBlock(m.resultBody, max(20, min(90, width-16)), max(3, min(18, height-10)))
		if m.resultError {
			body = failureStyle.Render(body)
		}
		hint = "Any key  close"
	case confirmDeleteMode:
		title = "Remove configuration?"
		workspace, process, ok := m.selection()
		if ok && m.deleteProcess {
			body = fmt.Sprintf("Remove process %q from %q?\n\nThis changes configuration only; project files are never deleted.", process.Name, workspace.Name)
		} else {
			body = fmt.Sprintf("Remove workspace %q?\n\nThis changes configuration only; project files are never deleted.", workspace.Name)
		}
		hint = "Y  remove    N/Esc  cancel"
	case confirmGitMode:
		title = "Confirm Git workflow"
		body = gitConfirmationText(m.pendingGit)
		hint = "Y  run workflow    N/Esc  cancel"
	}
	modal := modalBlock(title, body, hint, min(96, width-8))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal, lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(colorDark)))
}

func gitConfirmationText(request *gitops.Request) string {
	if request == nil {
		return "No Git workflow selected."
	}
	policy := "carry current changes"
	if !request.KeepChanges {
		policy = "stash current changes"
	}
	if request.Operation == gitops.Checkout {
		return fmt.Sprintf("Checkout %q?\n\nWorking tree policy: %s.", request.Branch, policy)
	}
	branch, err := gitops.BranchName(*request)
	if err != nil {
		return err.Error()
	}
	if request.KeepChanges {
		return fmt.Sprintf("Create and publish %q?\n\nCurrent changes will follow, be committed with the branch tag, and pushed.", branch)
	}
	return fmt.Sprintf("Create and publish %q?\n\nCurrent changes will be stashed and left out of the new branch.", branch)
}

func whatsNewText() string {
	return `◆ Community plugin API v1: portable manifest-based action plugins.

◆ Pull All updates each unique repository in a workspace on its current branch.

◆ Captures open automatically in Notepad on Windows; output can be cleared.

◆ Terminal fullscreen (F7), visible cursor and Ctrl/Alt navigation sequences.

◆ Rebuilt Workspaces and Session panels with denser operational context.

◆ Clipboard paste now works in actions, project forms and the terminal.

◆ Terminal input supports Shift/Caps Lock; F6 or Ctrl+] detaches reliably.

◆ cb now creates, publishes and optionally commits/pushes carried changes.

◆ Git checkout can carry changes or stash them before switching branches.

◆ Log rendering reads only the visible viewport; config writes are atomic.

◆ Refined 0.3 visual hierarchy with consistent background surfaces.

◆ The selected process shows its Git branch in the workstation.

◆ Personal configuration is excluded from the public repository.

◆ Press a to start every process in the selected workspace.

◆ One failed start no longer prevents the remaining processes from starting.

◆ Unified surface colors eliminate dark gaps around styled text and PTY output.

◆ Cleaner home screen focused on the developer workstation.

◆ New red visual identity and an original SuperCLI home screen.

◆ Hyprland-inspired tiled workspace with focused borders and gaps.

◆ Embedded PTY/ConPTY terminal: PowerShell, SSH and interactive actions
  remain inside SuperCLI while logs and services keep running.

◆ Project manager can now remove processes and workspaces safely.

◆ Terminal controls: F6 or Ctrl+] detaches; Ctrl+\ closes the embedded session.`
}

func (m Model) renderFormScreen(width, height int) string {
	if m.pending != nil && len(m.pending.Inputs) > 0 {
		field := m.pending.Inputs[m.actionInput]
		body := fmt.Sprintf("Step %d of %d\n\n%s\n%s", m.actionInput+1, len(m.pending.Inputs), field.Prompt, inputLine(m.input, field.Default))
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modalBlock(m.pending.Name, body, "Enter  continue    Esc  cancel", min(88, width-8)), lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(colorDark)))
	}
	field := m.form.fields[m.form.index]
	body := fmt.Sprintf("Step %d of %d\n\n%s\n%s", m.form.index+1, len(m.form.fields), field.label, inputLine(m.form.input, field.placeholder))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modalBlock(m.form.title, body, "Enter  continue    Esc  cancel", min(88, width-8)), lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(colorDark)))
}

func modalBlock(title, body, hint string, width int) string {
	return modalStyle.Width(max(36, width-8)).Render(logoStyle.Render(title) + "\n\n" + body + "\n\n" + mutedStyle.Render(hint))
}
func inputLine(value, placeholder string) string {
	if value == "" {
		value = mutedStyle.Render(placeholder)
	}
	return lipgloss.NewStyle().Background(colorRaised).Foreground(colorText).Padding(0, 1).Width(40).Render(value + "█")
}
func actionNames(actions []config.Action) []string {
	result := make([]string, len(actions))
	for i, a := range actions {
		result[i] = a.Name
	}
	return result
}
func menuBody(items []string, cursor, limit int) string {
	if len(items) == 0 {
		return mutedStyle.Render("No items available")
	}
	start := max(0, min(cursor-limit/2, len(items)-limit))
	end := min(len(items), start+limit)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		line := "   " + items[i]
		if i == cursor {
			line = selectedStyle.Render(" › " + items[i] + " ")
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func helpText() string {
	return `NAVIGATION                    PROCESSES
[ / ]  workspace              Enter  start
1..9   jump to workspace      x      stop
Tab    next process           a / X  start / stop all
↑ ↓    scroll logs
PgUp   scroll page            WORKFLOW
Home   oldest logs            c      capture logs
End/f  follow output          n      add project
                               l      clear output
                               p      actions
GIT & TOOLS                    ?      help
g      pull/push/checkout/cb/cm m     home
t      embedded terminal (td)  q      safe quit

TERMINAL FOCUS
F7 fullscreen       F6 / Ctrl+] detach      Ctrl+\ close
Ctrl+arrows navigate words; Ctrl+A and editor control keys are forwarded
Shift/Caps preserve uppercase input; clipboard paste is supported`
}

var (
	errorSeverity   = regexp.MustCompile(`(?i)(\b(error|failed|failure|fatal|panic|exception|unhandled)\b|build failed)`)
	warningSeverity = regexp.MustCompile(`(?i)(\bwarning\b|\bwarn\b|⚠)`)
	successSeverity = regexp.MustCompile(`(?i)(\b(succeeded|success|ready|listening|started)\b|compiled successfully|build succeeded)`)
)

func styleLog(line string) string {
	plain := ansi.Strip(line)
	switch {
	case errorSeverity.MatchString(plain):
		return failureStyle.Render(line)
	case warningSeverity.MatchString(plain):
		return warningStyle.Render(line)
	case successSeverity.MatchString(plain):
		return goodStyle.Render(line)
	default:
		return line
	}
}
func stateIcon(state processes.State) string {
	switch state {
	case processes.Running:
		return goodStyle.Render("●")
	case processes.Starting, processes.Stopping:
		return warningStyle.Render("◐")
	case processes.Failed:
		return failureStyle.Render("●")
	case processes.Exited:
		return infoStyle.Render("◆")
	default:
		return mutedStyle.Render("○")
	}
}
func fillLines(lines []string, width, height int) string {
	result := make([]string, height)
	for i := range height {
		if i < len(lines) {
			result[i] = fit(lines[i], width)
		} else {
			result[i] = strings.Repeat(" ", width)
		}
	}
	return strings.Join(result, "\n")
}
func fit(value string, width int) string {
	value = ansi.Truncate(strings.ReplaceAll(value, "\t", "    "), width, "…")
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}
func fitPlain(value string, width int) string {
	value = ansi.Truncate(value, width, "…")
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}
func wrapBlock(value string, width, height int) string {
	var result []string
	for _, line := range strings.Split(value, "\n") {
		for lipgloss.Width(line) > width {
			result = append(result, ansi.Truncate(line, width, ""))
			runes := []rune(ansi.Strip(line))
			if len(runes) <= width {
				break
			}
			line = string(runes[width:])
		}
		result = append(result, line)
		if len(result) >= height {
			result[height-1] = ansi.Truncate(result[height-1], max(1, width-1), "…")
			break
		}
	}
	return strings.Join(result, "\n")
}
func cycle(value, length int) int {
	if length == 0 {
		return 0
	}
	return (value%length + length) % length
}
func removeLastRune(value string) string {
	if value == "" {
		return value
	}
	_, size := utf8.DecodeLastRuneInString(value)
	return value[:len(value)-size]
}
func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var result strings.Builder
	dash := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
			dash = false
		} else if !dash && result.Len() > 0 {
			result.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(result.String(), "-")
}
func splitArgs(value string) []string {
	var args []string
	var current strings.Builder
	var quote rune
	flush := func() {
		if current.Len() > 0 {
			args = append(args, current.String())
			current.Reset()
		}
	}
	for _, r := range strings.TrimSpace(value) {
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
		} else if unicode.IsSpace(r) {
			flush()
		} else {
			current.WriteRune(r)
		}
	}
	flush()
	return args
}
