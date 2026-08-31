package action

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/2pedrohenrique/SuperCLI/internal/config"
)

type Result struct {
	Action   config.Action
	Output   string
	Duration time.Duration
	Err      error
}

type Runner struct{}

func NewRunner() Runner { return Runner{} }

func (Runner) Command(ctx context.Context, value config.Action) *exec.Cmd {
	cmd := exec.CommandContext(ctx, value.Command, value.Args...)
	cmd.Dir = value.Dir
	cmd.Env = os.Environ()
	return cmd
}

func (r Runner) Run(ctx context.Context, action config.Action) Result {
	started := time.Now()
	cmd := r.Command(ctx, action)
	output, err := cmd.CombinedOutput()
	return Result{
		Action:   action,
		Output:   strings.TrimSpace(string(output)),
		Duration: time.Since(started),
		Err:      wrapError(action, err),
	}
}

func wrapError(action config.Action, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("action %q failed: %w", action.Name, err)
}
