//go:build !windows

package terminal

import (
	"context"
	"os"
	"os/exec"
)

func Shell(ctx context.Context, dir string) *exec.Cmd {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, shell)
	cmd.Dir = dir
	return cmd
}
