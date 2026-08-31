package workstation

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/2pedrohenrique/SuperCLI/internal/action"
	"github.com/2pedrohenrique/SuperCLI/internal/capture"
	"github.com/2pedrohenrique/SuperCLI/internal/config"
	processes "github.com/2pedrohenrique/SuperCLI/internal/process"
)

type silentNotifier struct{}

func (silentNotifier) Notify(string, string) error { return nil }

func TestWorkspaceHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_WORKSTATION_HELPER") != "1" {
		return
	}
	time.Sleep(30 * time.Second)
}

func TestStartWorkspaceStartsAllProcessesAndSkipsRunningOnes(t *testing.T) {
	cfg := workspaceTestConfig("first", "second")
	plugin, supervisor := newTestPlugin(t, cfg)
	defer plugin.StopAll() //nolint:errcheck

	if err := plugin.StartWorkspace("app"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "second"} {
		if got := supervisor.Snapshot("app", id).State; got != processes.Running {
			t.Fatalf("process %s state = %s", id, got)
		}
	}
	if err := plugin.StartWorkspace("app"); err != nil {
		t.Fatalf("starting an already active workspace: %v", err)
	}
}

func TestStartWorkspaceContinuesAfterOneProcessFails(t *testing.T) {
	cfg := workspaceTestConfig("good")
	cfg.Workspaces[0].Processes = append([]config.ProcessSpec{{
		ID: "bad", Name: "Bad", Command: "supercli-command-that-does-not-exist",
	}}, cfg.Workspaces[0].Processes...)
	plugin, supervisor := newTestPlugin(t, cfg)
	defer plugin.StopAll() //nolint:errcheck

	err := plugin.StartWorkspace("app")
	if err == nil || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("expected bad process error, got %v", err)
	}
	if got := supervisor.Snapshot("app", "good").State; got != processes.Running {
		t.Fatalf("good process was not started after failure: %s", got)
	}
}

func workspaceTestConfig(ids ...string) config.Config {
	specs := make([]config.ProcessSpec, 0, len(ids))
	for _, id := range ids {
		specs = append(specs, config.ProcessSpec{
			ID: id, Name: id, Command: os.Args[0],
			Args: []string{"-test.run=TestWorkspaceHelperProcess"},
			Env:  map[string]string{"GO_WANT_WORKSTATION_HELPER": "1"},
		})
	}
	return config.Config{Version: 1, LogBufferLines: 100, Workspaces: []config.Workspace{{ID: "app", Name: "App", Processes: specs}}}
}

func newTestPlugin(t *testing.T, cfg config.Config) (*Plugin, *processes.Supervisor) {
	t.Helper()
	supervisor := processes.New(cfg, silentNotifier{})
	plugin := New(cfg, "", supervisor, capture.New(t.TempDir()), action.NewRunner())
	if err := plugin.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return plugin, supervisor
}
