package process

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/2pedrohenrique/SuperCLI/internal/config"
)

type recordingNotifier struct {
	mu       sync.Mutex
	messages []string
}

func (n *recordingNotifier) Notify(title, message string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.messages = append(n.messages, title+":"+message)
	return nil
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_SUPERCLI_HELPER") != "1" {
		return
	}
	fmt.Println("server ready")
	if os.Getenv("GO_SUPERCLI_HELPER_MODE") == "sleep" {
		time.Sleep(30 * time.Second)
		return
	}
	fmt.Fprintln(os.Stderr, "BUILD ERROR example")
	os.Exit(7)
}

func TestStoppedProcessReportsStoppedInsteadOfFailure(t *testing.T) {
	notifier := &recordingNotifier{}
	cfg := config.Config{Version: 1, LogBufferLines: 20, Workspaces: []config.Workspace{{ID: "app", Processes: []config.ProcessSpec{{ID: "api", Name: "API", Command: os.Args[0], Args: []string{"-test.run=TestHelperProcess"}, Env: map[string]string{"GO_WANT_SUPERCLI_HELPER": "1", "GO_SUPERCLI_HELPER_MODE": "sleep"}}}}}}
	supervisor := New(cfg, notifier)
	if err := supervisor.Start(context.Background(), "app", "api"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-supervisor.Events():
			if event.Kind == StateEvent && event.State == Running {
				if err := supervisor.Stop("app", "api"); err != nil {
					t.Fatal(err)
				}
			}
			if event.Kind == StateEvent && event.State == Stopped {
				return
			}
			if event.Kind == StateEvent && event.State == Failed {
				t.Fatalf("user stop reported failure: %v", event.Err)
			}
		case <-deadline:
			t.Fatal("timed out waiting for stopped state")
		}
	}
}

func TestSupervisorStreamsOutputDetectsErrorsAndReportsExit(t *testing.T) {
	notifier := &recordingNotifier{}
	cfg := config.Config{
		Version: 1, LogBufferLines: 100,
		Workspaces: []config.Workspace{
			{
				ID: "app",
				Processes: []config.ProcessSpec{
					{
						ID: "api", Name: "API", Command: os.Args[0],
						Args:          []string{"-test.run=TestHelperProcess"},
						Env:           map[string]string{"GO_WANT_SUPERCLI_HELPER": "1"},
						ReadyPatterns: []string{"server ready"}, ErrorPatterns: []string{"BUILD ERROR"},
					},
				},
			},
		},
	}
	supervisor := New(cfg, notifier)
	if err := supervisor.Start(context.Background(), "app", "api"); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(5 * time.Second)
	seenAlert := false
	for {
		select {
		case event := <-supervisor.Events():
			seenAlert = seenAlert || event.Kind == AlertEvent
			if event.Kind == StateEvent && event.State == Failed {
				if event.ExitCode != 7 {
					t.Fatalf("exit code = %d", event.ExitCode)
				}
				if !seenAlert {
					t.Fatal("expected alert event")
				}
				lines := strings.Join(supervisor.Lines("app", "api", -1), "\n")
				if !strings.Contains(lines, "server ready") || !strings.Contains(lines, "BUILD ERROR") {
					t.Fatalf("missing streamed output:\n%s", lines)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for helper process")
		}
	}
}

func TestLogEventBackpressureNeverBlocksSupervisor(t *testing.T) {
	supervisor := &Supervisor{events: make(chan Event, 1)}
	done := make(chan struct{})
	go func() {
		supervisor.emitLocked(Event{Kind: LogEvent})
		supervisor.emitLocked(Event{Kind: LogEvent})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("log event backpressure blocked the supervisor")
	}
}

func TestClearLogsResetsOnlyTheSelectedProcess(t *testing.T) {
	cfg := config.Config{Version: 1, LogBufferLines: 10, Workspaces: []config.Workspace{{
		ID: "app", Processes: []config.ProcessSpec{{ID: "api", Command: "go"}, {ID: "web", Command: "go"}},
	}}}
	supervisor := New(cfg, &recordingNotifier{})
	supervisor.sessions[key("app", "api")].buffer.Append("api log")
	supervisor.sessions[key("app", "web")].buffer.Append("web log")
	if err := supervisor.ClearLogs("app", "api"); err != nil {
		t.Fatal(err)
	}
	if got := supervisor.Snapshot("app", "api").LineCount; got != 0 {
		t.Fatalf("api line count = %d", got)
	}
	if got := supervisor.Snapshot("app", "web").LineCount; got != 1 {
		t.Fatalf("web line count = %d", got)
	}
}
