//go:build windows

package terminal

import (
	"context"
	"os/exec"
)

func Shell(ctx context.Context, dir string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo")
	cmd.Dir = dir
	return cmd
}
