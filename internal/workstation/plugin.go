package workstation

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"

	"github.com/2pedrohenrique/SuperCLI/internal/action"
	"github.com/2pedrohenrique/SuperCLI/internal/capture"
	"github.com/2pedrohenrique/SuperCLI/internal/config"
	gitops "github.com/2pedrohenrique/SuperCLI/internal/git"
	"github.com/2pedrohenrique/SuperCLI/internal/pluginhost"
	processes "github.com/2pedrohenrique/SuperCLI/internal/process"
	"github.com/2pedrohenrique/SuperCLI/internal/terminal"
)

type Plugin struct {
	mu         sync.RWMutex
	config     config.Config
	configPath string
	supervisor *processes.Supervisor
	captures   *capture.Store
	actions    action.Runner
	git        gitops.Service
	ctx        context.Context
	community  []pluginhost.Plugin
}

func New(cfg config.Config, configPath string, supervisor *processes.Supervisor, captures *capture.Store, actions action.Runner) *Plugin {
	return &Plugin{config: cfg, configPath: configPath, supervisor: supervisor, captures: captures, actions: actions, git: gitops.New()}
}

func NewWithCommunityPlugins(cfg config.Config, configPath string, supervisor *processes.Supervisor, captures *capture.Store, actions action.Runner, plugins []pluginhost.Plugin) (*Plugin, error) {
	merged, err := pluginhost.Merge(cfg, plugins)
	if err != nil {
		return nil, err
	}
	plugin := New(merged, configPath, supervisor, captures, actions)
	plugin.community = append([]pluginhost.Plugin(nil), plugins...)
	return plugin, nil
}

func (p *Plugin) mergeCommunity(cfg config.Config) (config.Config, error) {
	return pluginhost.Merge(cfg, p.community)
}

func (*Plugin) ID() string { return "workstation" }

func (p *Plugin) Start(ctx context.Context) error {
	p.ctx = ctx
	for _, workspace := range p.config.Workspaces {
		for _, spec := range workspace.Processes {
			if spec.AutoStart {
				if err := p.supervisor.Start(ctx, workspace.ID, spec.ID); err != nil {
					_ = p.supervisor.StopAll()
					return fmt.Errorf("auto-start %s/%s: %w", workspace.ID, spec.ID, err)
				}
			}
		}
	}
	return nil
}

func (p *Plugin) Stop(context.Context) error { return p.supervisor.StopAll() }

func (p *Plugin) Config() config.Config {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config
}

func (p *Plugin) AddWorkspace(workspace config.Workspace) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	updated, err := config.AppendWorkspace(p.configPath, workspace)
	if err != nil {
		return err
	}
	normalized, _ := updated.Workspace(workspace.ID)
	if err := p.supervisor.RegisterWorkspace(normalized); err != nil {
		return err
	}
	p.config, err = p.mergeCommunity(updated)
	if err != nil {
		return err
	}
	return nil
}

func (p *Plugin) AddProcess(workspaceID string, spec config.ProcessSpec) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	updated, err := config.AppendProcess(p.configPath, workspaceID, spec)
	if err != nil {
		return err
	}
	workspace, _ := updated.Workspace(workspaceID)
	var normalized config.ProcessSpec
	for _, candidate := range workspace.Processes {
		if candidate.ID == spec.ID {
			normalized = candidate
			break
		}
	}
	if err := p.supervisor.RegisterProcess(workspaceID, normalized); err != nil {
		return err
	}
	p.config, err = p.mergeCommunity(updated)
	if err != nil {
		return err
	}
	return nil
}

func (p *Plugin) RemoveWorkspace(workspaceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	workspace, ok := p.config.Workspace(workspaceID)
	if !ok {
		return fmt.Errorf("unknown workspace %q", workspaceID)
	}
	if err := p.supervisor.CanUnregisterWorkspace(workspace); err != nil {
		return err
	}
	updated, err := config.RemoveWorkspace(p.configPath, workspaceID)
	if err != nil {
		return err
	}
	if err := p.supervisor.UnregisterWorkspace(workspace); err != nil {
		return err
	}
	p.config, err = p.mergeCommunity(updated)
	if err != nil {
		return err
	}
	return nil
}

func (p *Plugin) RemoveProcess(workspaceID, processID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.supervisor.CanUnregister(workspaceID, processID); err != nil {
		return err
	}
	updated, err := config.RemoveProcess(p.configPath, workspaceID, processID)
	if err != nil {
		return err
	}
	if err := p.supervisor.UnregisterProcess(workspaceID, processID); err != nil {
		return err
	}
	p.config, err = p.mergeCommunity(updated)
	if err != nil {
		return err
	}
	return nil
}

func (p *Plugin) Events() <-chan processes.Event { return p.supervisor.Events() }

func (p *Plugin) StartProcess(workspaceID, processID string) error {
	return p.supervisor.Start(p.ctx, workspaceID, processID)
}

func (p *Plugin) StopProcess(workspaceID, processID string) error {
	return p.supervisor.Stop(workspaceID, processID)
}

func (p *Plugin) StartWorkspace(workspaceID string) error {
	workspace, ok := p.config.Workspace(workspaceID)
	if !ok {
		return fmt.Errorf("unknown workspace %q", workspaceID)
	}
	var errs []error
	for _, spec := range workspace.Processes {
		snapshot := p.supervisor.Snapshot(workspaceID, spec.ID)
		if snapshot.State == processes.Running || snapshot.State == processes.Starting {
			continue
		}
		if err := p.supervisor.Start(p.ctx, workspaceID, spec.ID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (p *Plugin) StopAll() error { return p.supervisor.StopAll() }

func (p *Plugin) Snapshot(workspaceID, processID string) processes.Snapshot {
	return p.supervisor.Snapshot(workspaceID, processID)
}

func (p *Plugin) Lines(workspaceID, processID string, n int) []string {
	return p.supervisor.Lines(workspaceID, processID, n)
}

func (p *Plugin) LineWindow(workspaceID, processID string, start, count int) []string {
	return p.supervisor.LineWindow(workspaceID, processID, start, count)
}

func (p *Plugin) Capture(workspaceID, processID string, n int) (string, error) {
	return p.captures.SaveAndOpen(workspaceID, processID, p.Lines(workspaceID, processID, n))
}

func (p *Plugin) ClearLogs(workspaceID, processID string) error {
	return p.supervisor.ClearLogs(workspaceID, processID)
}

func (p *Plugin) RunAction(ctx context.Context, value config.Action) action.Result {
	return p.actions.Run(ctx, value)
}

func (p *Plugin) InteractiveCommand(ctx context.Context, value config.Action) *exec.Cmd {
	return p.actions.Command(ctx, value)
}

func (p *Plugin) ShellCommand(ctx context.Context, dir string) *exec.Cmd {
	return terminal.Shell(ctx, dir)
}

func (p *Plugin) RunGit(ctx context.Context, dir string, request gitops.Request) gitops.Result {
	return p.git.Run(ctx, dir, request)
}

func (p *Plugin) PullWorkspace(ctx context.Context, workspaceID string) gitops.Result {
	p.mu.RLock()
	workspace, ok := p.config.Workspace(workspaceID)
	p.mu.RUnlock()
	if !ok {
		return gitops.Result{Operation: gitops.PullAll, Err: fmt.Errorf("unknown workspace %q", workspaceID)}
	}
	targets := make([]gitops.PullTarget, 0, len(workspace.Processes))
	for _, process := range workspace.Processes {
		targets = append(targets, gitops.PullTarget{Name: process.Name, Dir: process.Dir})
	}
	return p.git.PullAll(ctx, targets)
}

func (p *Plugin) CurrentBranch(ctx context.Context, dir string) (string, error) {
	return p.git.CurrentBranch(ctx, dir)
}

func (p *Plugin) IsRunning() bool { return p.supervisor.IsRunning() }
