package process

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/2pedrohenrique/SuperCLI/internal/config"
	"github.com/2pedrohenrique/SuperCLI/internal/logbuffer"
	"github.com/2pedrohenrique/SuperCLI/internal/notify"
)

type State string

const (
	Stopped  State = "stopped"
	Starting State = "starting"
	Running  State = "running"
	Stopping State = "stopping"
	Exited   State = "exited"
	Failed   State = "failed"
)

type EventKind string

const (
	LogEvent   EventKind = "log"
	StateEvent EventKind = "state"
	AlertEvent EventKind = "alert"
)

type Event struct {
	Kind        EventKind
	WorkspaceID string
	ProcessID   string
	State       State
	Stream      string
	Line        string
	Err         error
	ExitCode    int
	At          time.Time
}

type Snapshot struct {
	State     State
	StartedAt time.Time
	ExitCode  int
	Err       error
	LineCount int
}

type Supervisor struct {
	mu       sync.RWMutex
	sessions map[string]*session
	events   chan Event
	notifier notify.Notifier
	capacity int
}

type session struct {
	workspaceID string
	spec        config.ProcessSpec
	buffer      *logbuffer.Ring
	errorRE     []*regexp.Regexp
	readyRE     []*regexp.Regexp
	state       State
	startedAt   time.Time
	exitCode    int
	err         error
	cmd         *exec.Cmd
	cancel      context.CancelFunc
	alerted     bool
}

func New(cfg config.Config, notifier notify.Notifier) *Supervisor {
	s := &Supervisor{
		sessions: make(map[string]*session),
		events:   make(chan Event, 2048),
		notifier: notifier,
		capacity: cfg.LogBufferLines,
	}
	for _, workspace := range cfg.Workspaces {
		for _, spec := range workspace.Processes {
			s.sessions[key(workspace.ID, spec.ID)] = &session{
				workspaceID: workspace.ID,
				spec:        spec,
				buffer:      logbuffer.New(cfg.LogBufferLines),
				errorRE:     compile(spec.ErrorPatterns),
				readyRE:     compile(spec.ReadyPatterns),
				state:       Stopped,
				exitCode:    -1,
			}
		}
	}
	return s
}

func (s *Supervisor) RegisterWorkspace(workspace config.Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, spec := range workspace.Processes {
		if _, exists := s.sessions[key(workspace.ID, spec.ID)]; exists {
			return fmt.Errorf("process %s/%s is already registered", workspace.ID, spec.ID)
		}
	}
	for _, spec := range workspace.Processes {
		s.sessions[key(workspace.ID, spec.ID)] = &session{
			workspaceID: workspace.ID,
			spec:        spec,
			buffer:      logbuffer.New(s.capacity),
			errorRE:     compile(spec.ErrorPatterns),
			readyRE:     compile(spec.ReadyPatterns),
			state:       Stopped,
			exitCode:    -1,
		}
	}
	return nil
}

func (s *Supervisor) RegisterProcess(workspaceID string, spec config.ProcessSpec) error {
	return s.RegisterWorkspace(config.Workspace{ID: workspaceID, Processes: []config.ProcessSpec{spec}})
}

func (s *Supervisor) CanUnregister(workspaceID, processID string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[key(workspaceID, processID)]
	if !ok {
		return fmt.Errorf("unknown process %s/%s", workspaceID, processID)
	}
	if sess.cmd != nil || sess.state == Running || sess.state == Starting || sess.state == Stopping {
		return fmt.Errorf("stop %s/%s before removing it", workspaceID, processID)
	}
	return nil
}

func (s *Supervisor) UnregisterProcess(workspaceID, processID string) error {
	if err := s.CanUnregister(workspaceID, processID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, key(workspaceID, processID))
	return nil
}

func (s *Supervisor) CanUnregisterWorkspace(workspace config.Workspace) error {
	for _, spec := range workspace.Processes {
		if err := s.CanUnregister(workspace.ID, spec.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Supervisor) UnregisterWorkspace(workspace config.Workspace) error {
	if err := s.CanUnregisterWorkspace(workspace); err != nil {
		return err
	}
	for _, spec := range workspace.Processes {
		if err := s.UnregisterProcess(workspace.ID, spec.ID); err != nil {
			return err
		}
	}
	return nil
}

func compile(patterns []string) []*regexp.Regexp {
	result := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		result = append(result, regexp.MustCompile(pattern))
	}
	return result
}

func key(workspaceID, processID string) string { return workspaceID + "\x00" + processID }

func (s *Supervisor) Events() <-chan Event { return s.events }

func (s *Supervisor) Start(parent context.Context, workspaceID, processID string) error {
	s.mu.Lock()
	sess, ok := s.sessions[key(workspaceID, processID)]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("unknown process %s/%s", workspaceID, processID)
	}
	if sess.state == Running || sess.state == Starting || sess.state == Stopping {
		s.mu.Unlock()
		return fmt.Errorf("process %s/%s is already %s", workspaceID, processID, sess.state)
	}

	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, sess.spec.Command, sess.spec.Args...)
	cmd.Dir = sess.spec.Dir
	cmd.Env = append([]string{}, os.Environ()...)
	for name, value := range sess.spec.Env {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	prepareCommand(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		s.mu.Unlock()
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		s.mu.Unlock()
		return fmt.Errorf("stderr pipe: %w", err)
	}

	sess.state = Starting
	sess.startedAt = time.Now()
	sess.exitCode = -1
	sess.err = nil
	sess.cmd = cmd
	sess.cancel = cancel
	sess.alerted = false
	startedAt := sess.startedAt
	s.emitLocked(Event{Kind: StateEvent, WorkspaceID: workspaceID, ProcessID: processID, State: Starting, At: startedAt})
	if err := cmd.Start(); err != nil {
		sess.state = Failed
		sess.err = err
		sess.cmd = nil
		sess.cancel = nil
		cancel()
		s.emitLocked(Event{Kind: StateEvent, WorkspaceID: workspaceID, ProcessID: processID, State: Failed, Err: err, ExitCode: -1, At: time.Now()})
		s.mu.Unlock()
		return fmt.Errorf("start %s/%s: %w", workspaceID, processID, err)
	}
	if len(sess.readyRE) == 0 {
		sess.state = Running
		s.emitLocked(Event{Kind: StateEvent, WorkspaceID: workspaceID, ProcessID: processID, State: Running, At: time.Now()})
	}
	s.mu.Unlock()

	var readers sync.WaitGroup
	readers.Add(2)
	go func() {
		defer readers.Done()
		s.readStream(sess, "OUT", stdout)
	}()
	go func() {
		defer readers.Done()
		s.readStream(sess, "ERR", stderr)
	}()
	go s.wait(sess, cmd, cancel, &readers)
	return nil
}

func (s *Supervisor) readStream(sess *session, stream string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		s.record(sess, stream, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		s.record(sess, "SYS", "log stream error: "+err.Error())
	}
}

func (s *Supervisor) record(sess *session, stream, text string) {
	now := time.Now()
	line := fmt.Sprintf("%s │ %s", now.Format("15:04:05.000"), text)
	sess.buffer.Append(line)

	s.mu.Lock()
	if sess.state == Starting && matches(sess.readyRE, text) {
		sess.state = Running
		s.emitLocked(Event{Kind: StateEvent, WorkspaceID: sess.workspaceID, ProcessID: sess.spec.ID, State: Running, At: now})
	}
	alert := !sess.alerted && matches(sess.errorRE, text)
	if alert {
		sess.alerted = true
	}
	s.emitLocked(Event{Kind: LogEvent, WorkspaceID: sess.workspaceID, ProcessID: sess.spec.ID, Stream: stream, Line: line, At: now})
	if alert {
		s.emitLocked(Event{Kind: AlertEvent, WorkspaceID: sess.workspaceID, ProcessID: sess.spec.ID, Line: text, At: now})
	}
	s.mu.Unlock()

	if alert {
		go func() {
			_ = s.notifier.Notify("SuperCLI: build error", fmt.Sprintf("%s/%s: %s", sess.workspaceID, sess.spec.Name, shorten(text, 180)))
		}()
	}
}

func matches(patterns []*regexp.Regexp, text string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

func shorten(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max-1] + "…"
}

func (s *Supervisor) wait(sess *session, cmd *exec.Cmd, cancel context.CancelFunc, readers *sync.WaitGroup) {
	err := cmd.Wait()
	readers.Wait()
	cancel()
	exitCode := cmd.ProcessState.ExitCode()

	s.mu.Lock()
	// Ignore an obsolete waiter from a previous run.
	if sess.cmd != cmd {
		s.mu.Unlock()
		return
	}
	state := Exited
	if sess.state == Stopping {
		state = Stopped
	}
	if sess.state != Stopping && err != nil && !errors.Is(err, context.Canceled) {
		state = Failed
	}
	sess.state = state
	sess.exitCode = exitCode
	sess.err = err
	sess.cmd = nil
	sess.cancel = nil
	s.emitLocked(Event{Kind: StateEvent, WorkspaceID: sess.workspaceID, ProcessID: sess.spec.ID, State: state, Err: err, ExitCode: exitCode, At: time.Now()})
	s.mu.Unlock()

	if state == Failed {
		go func() {
			_ = s.notifier.Notify("SuperCLI: process failed", fmt.Sprintf("%s/%s exited with code %d", sess.workspaceID, sess.spec.Name, exitCode))
		}()
	}
}

func (s *Supervisor) emitLocked(event Event) {
	select {
	case s.events <- event:
	default:
		// Log lines remain available in the ring buffer, so dropping only their
		// repaint signal is safe under extreme output. Lifecycle and alert events
		// are delivered later without blocking a supervisor lock.
		if event.Kind != LogEvent {
			go func() { s.events <- event }()
		}
	}
}

func (s *Supervisor) Stop(workspaceID, processID string) error {
	s.mu.Lock()
	sess, ok := s.sessions[key(workspaceID, processID)]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("unknown process %s/%s", workspaceID, processID)
	}
	if sess.cmd == nil || (sess.state != Running && sess.state != Starting) {
		s.mu.Unlock()
		return fmt.Errorf("process %s/%s is not running", workspaceID, processID)
	}
	cmd := sess.cmd
	sess.state = Stopping
	s.emitLocked(Event{Kind: StateEvent, WorkspaceID: workspaceID, ProcessID: processID, State: Stopping, At: time.Now()})
	s.mu.Unlock()
	if err := terminateCommand(cmd); err != nil {
		return fmt.Errorf("stop %s/%s: %w", workspaceID, processID, err)
	}
	return nil
}

func (s *Supervisor) StopAll() error {
	type target struct{ workspace, process string }
	s.mu.RLock()
	var targets []target
	for _, sess := range s.sessions {
		if sess.cmd != nil && (sess.state == Running || sess.state == Starting) {
			targets = append(targets, target{sess.workspaceID, sess.spec.ID})
		}
	}
	s.mu.RUnlock()
	var errs []error
	for _, target := range targets {
		if err := s.Stop(target.workspace, target.process); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Supervisor) Snapshot(workspaceID, processID string) Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[key(workspaceID, processID)]
	if !ok {
		return Snapshot{State: Failed, Err: fmt.Errorf("unknown process"), ExitCode: -1}
	}
	return Snapshot{State: sess.state, StartedAt: sess.startedAt, ExitCode: sess.exitCode, Err: sess.err, LineCount: sess.buffer.Len()}
}

func (s *Supervisor) Lines(workspaceID, processID string, n int) []string {
	s.mu.RLock()
	sess := s.sessions[key(workspaceID, processID)]
	s.mu.RUnlock()
	if sess == nil {
		return nil
	}
	return sess.buffer.Last(n)
}

func (s *Supervisor) LineWindow(workspaceID, processID string, start, count int) []string {
	s.mu.RLock()
	sess := s.sessions[key(workspaceID, processID)]
	s.mu.RUnlock()
	if sess == nil {
		return nil
	}
	return sess.buffer.Range(start, count)
}

func (s *Supervisor) ClearLogs(workspaceID, processID string) error {
	s.mu.RLock()
	sess := s.sessions[key(workspaceID, processID)]
	s.mu.RUnlock()
	if sess == nil {
		return fmt.Errorf("unknown process %s/%s", workspaceID, processID)
	}
	sess.buffer.Clear()
	return nil
}

func (s *Supervisor) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sess := range s.sessions {
		if sess.state == Running || sess.state == Starting || sess.state == Stopping {
			return true
		}
	}
	return false
}
