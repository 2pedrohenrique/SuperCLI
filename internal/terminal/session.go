package terminal

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/charmbracelet/x/xpty"
)

type EventKind int

const (
	outputQuietPeriod  = 250 * time.Millisecond
	outputDrainTimeout = 2 * time.Second
)

const (
	OutputEvent EventKind = iota
	ExitEvent
)

type Event struct {
	Kind EventKind
	Err  error
}

type Session struct {
	mu       sync.RWMutex
	pty      xpty.Pty
	emulator *vt.SafeEmulator
	cmd      *exec.Cmd
	cancel   context.CancelFunc
	running  bool
	events   chan Event
}

func NewSession() *Session { return &Session{events: make(chan Event, 32)} }

func (s *Session) Start(parent context.Context, cmd *exec.Cmd, width, height int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return fmt.Errorf("an embedded terminal is already running")
	}
	width, height = max(20, width), max(5, height)
	pty, err := xpty.NewPty(width, height)
	if err != nil {
		return fmt.Errorf("create PTY: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	emulator := vt.NewSafeEmulator(width, height)
	// Match SuperCLI's foreground and background so SGR resets and terminal
	// color queries do not introduce black blocks inside the tiled surface.
	emulator.SetDefaultForegroundColor(color.RGBA{R: 0xE5, G: 0xE7, B: 0xEB, A: 0xFF})
	emulator.SetDefaultBackgroundColor(color.RGBA{R: 0x08, G: 0x0B, B: 0x12, A: 0xFF})
	emulator.SetForegroundColor(nil)
	emulator.SetBackgroundColor(nil)
	emulator.SetScrollbackSize(10_000)
	if err := pty.Start(cmd); err != nil {
		cancel()
		_ = emulator.Close()
		_ = pty.Close()
		return fmt.Errorf("start embedded terminal: %w", err)
	}
	s.pty, s.emulator, s.cmd, s.cancel, s.running = pty, emulator, cmd, cancel, true
	outputDone := make(chan struct{})
	outputActivity := make(chan struct{}, 1)
	go s.copyOutput(pty, emulator, outputActivity, outputDone)
	go func() { _, _ = io.Copy(pty, emulator) }()
	go s.wait(ctx, pty, emulator, cmd, cancel, outputActivity, outputDone)
	return nil
}

func (s *Session) copyOutput(pty xpty.Pty, emulator *vt.SafeEmulator, activity chan<- struct{}, done chan<- struct{}) {
	defer close(done)
	buffer := make([]byte, 32*1024)
	for {
		n, err := pty.Read(buffer)
		if n > 0 {
			_, _ = emulator.Write(buffer[:n])
			s.emit(Event{Kind: OutputEvent})
			select {
			case activity <- struct{}{}:
			default:
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) wait(ctx context.Context, pty xpty.Pty, emulator *vt.SafeEmulator, cmd *exec.Cmd, cancel context.CancelFunc, outputActivity <-chan struct{}, outputDone <-chan struct{}) {
	err := xpty.WaitProcess(ctx, cmd)
	cancel()
	// Closing the parent's Unix slave allows EOF once the child has exited.
	// Windows ConPTY does not expose a slave, so it uses the bounded drain below.
	_, hasUnixSlave := pty.(interface{ Slave() *os.File })
	if unix, ok := pty.(interface{ Slave() *os.File }); ok {
		_ = unix.Slave().Close()
	}
	// A short-lived command can exit before the output goroutine is scheduled.
	// Let it consume the PTY's final bytes before closing the emulator or
	// emitting ExitEvent, so callers never observe an empty completed session.
	if hasUnixSlave {
		select {
		case <-outputDone:
		case <-time.After(outputDrainTimeout):
		}
	} else {
		waitForQuietOutput(outputActivity, outputDone)
	}
	_ = pty.Close()
	_ = emulator.Close()
	s.mu.Lock()
	if s.cmd == cmd {
		s.running = false
		s.pty, s.cmd, s.cancel = nil, nil, nil
	}
	s.mu.Unlock()
	s.emit(Event{Kind: ExitEvent, Err: err})
}

func waitForQuietOutput(activity <-chan struct{}, done <-chan struct{}) {
	quiet := time.NewTimer(outputQuietPeriod)
	deadline := time.NewTimer(outputDrainTimeout)
	defer quiet.Stop()
	defer deadline.Stop()
	for {
		select {
		case <-activity:
			if !quiet.Stop() {
				select {
				case <-quiet.C:
				default:
				}
			}
			quiet.Reset(outputQuietPeriod)
		case <-done:
			return
		case <-quiet.C:
			return
		case <-deadline.C:
			return
		}
	}
}

func (s *Session) emit(event Event) {
	select {
	case s.events <- event:
	default:
		if event.Kind == ExitEvent {
			go func() { s.events <- event }()
		}
	}
}

func (s *Session) Events() <-chan Event { return s.events }

func (s *Session) SendKey(key uv.Key) {
	if sequence, ok := modifiedKeySequence(key); ok {
		s.mu.RLock()
		pty := s.pty
		s.mu.RUnlock()
		if pty != nil {
			_, _ = io.WriteString(pty, sequence)
		}
		return
	}
	s.mu.RLock()
	emulator := s.emulator
	s.mu.RUnlock()
	if emulator != nil {
		emulator.SendKey(uv.KeyPressEvent(key))
	}
}

// modifiedKeySequence fills the gap left by the VT encoder for modified
// navigation keys and normalizes Windows control-character key events.
func modifiedKeySequence(key uv.Key) (string, bool) {
	mod := key.Mod &^ (uv.ModCapsLock | uv.ModNumLock | uv.ModScrollLock)
	code := key.Code
	if code >= '\x01' && code <= '\x1a' {
		return string(code), true
	}
	if mod&uv.ModCtrl != 0 {
		if key.BaseCode >= 'a' && key.BaseCode <= 'z' {
			code = key.BaseCode
		}
		if code >= 'a' && code <= 'z' {
			return string(rune(code-'a') + 1), true
		}
	}
	modifier := 0
	switch mod & (uv.ModShift | uv.ModAlt | uv.ModCtrl) {
	case uv.ModShift:
		modifier = 2
	case uv.ModAlt:
		modifier = 3
	case uv.ModShift | uv.ModAlt:
		modifier = 4
	case uv.ModCtrl:
		modifier = 5
	case uv.ModShift | uv.ModCtrl:
		modifier = 6
	case uv.ModAlt | uv.ModCtrl:
		modifier = 7
	case uv.ModShift | uv.ModAlt | uv.ModCtrl:
		modifier = 8
	}
	if modifier == 0 {
		return "", false
	}
	final := ""
	switch code {
	case uv.KeyUp:
		final = "A"
	case uv.KeyDown:
		final = "B"
	case uv.KeyRight:
		final = "C"
	case uv.KeyLeft:
		final = "D"
	case uv.KeyHome:
		final = "H"
	case uv.KeyEnd:
		final = "F"
	default:
		return "", false
	}
	return fmt.Sprintf("\x1b[1;%d%s", modifier, final), true
}

func (s *Session) SendText(text string) {
	s.mu.RLock()
	emulator := s.emulator
	s.mu.RUnlock()
	if emulator != nil {
		emulator.SendText(text)
	}
}

func (s *Session) Paste(text string) {
	s.mu.RLock()
	emulator := s.emulator
	s.mu.RUnlock()
	if emulator != nil {
		emulator.Paste(text)
	}
}

func (s *Session) SendMouse(event uv.MouseEvent) {
	s.mu.RLock()
	emulator := s.emulator
	s.mu.RUnlock()
	if emulator != nil {
		emulator.SendMouse(event)
	}
}

func IsExpectedExit(err error) bool { return err == nil || errors.Is(err, context.Canceled) }

func (s *Session) Render() string {
	s.mu.RLock()
	emulator := s.emulator
	s.mu.RUnlock()
	if emulator == nil {
		return ""
	}
	return emulator.Render()
}

func (s *Session) CursorPosition() (x, y int, ok bool) {
	s.mu.RLock()
	emulator := s.emulator
	s.mu.RUnlock()
	if emulator == nil {
		return 0, 0, false
	}
	position := emulator.CursorPosition()
	return position.X, position.Y, true
}

func (s *Session) Resize(width, height int) error {
	width, height = max(20, width), max(5, height)
	s.mu.RLock()
	pty, emulator := s.pty, s.emulator
	s.mu.RUnlock()
	if pty == nil || emulator == nil {
		return nil
	}
	emulator.Resize(width, height)
	return pty.Resize(width, height)
}

func (s *Session) Running() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *Session) Close() error {
	s.mu.RLock()
	cancel, pty := s.cancel, s.pty
	s.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
	if pty != nil {
		return pty.Close()
	}
	return nil
}
