//go:build windows

package capture

import "os/exec"

type platformOpener struct{}

func (platformOpener) Open(path string) error {
	command := exec.Command("notepad.exe", path)
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}
