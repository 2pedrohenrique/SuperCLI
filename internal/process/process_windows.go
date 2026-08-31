//go:build windows

package process

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

const createNewProcessGroup = 0x00000200

func prepareCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func terminateCommand(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	kill := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if output, err := kill.CombinedOutput(); err != nil {
		if fallback := cmd.Process.Kill(); fallback != nil {
			return fmt.Errorf("taskkill: %w (%s); kill: %v", err, strings.TrimSpace(string(output)), fallback)
		}
	}
	return nil
}
