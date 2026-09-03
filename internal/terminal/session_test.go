package terminal

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestSessionRendersPTYOutput(t *testing.T) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("powershell.exe", "-NoProfile", "-Command", "Write-Output SUPERCLI_PTY_OK")
	} else {
		cmd = exec.Command("sh", "-c", "printf 'SUPERCLI_PTY_OK\\n'")
	}
	session := NewSession()
	if err := session.Start(context.Background(), cmd, 80, 20); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event := <-session.Events():
			if event.Kind == ExitEvent {
				if event.Err != nil {
					t.Fatal(event.Err)
				}
				if !strings.Contains(session.Render(), "SUPERCLI_PTY_OK") {
					t.Fatalf("process exited before its final PTY output was rendered:\n%s", session.Render())
				}
				return
			}
		case <-deadline:
			t.Fatalf("PTY output not rendered:\n%s", session.Render())
		}
	}
}

func TestModifiedKeySequenceSupportsShellAndEditorNavigation(t *testing.T) {
	tests := []struct {
		name string
		key  uv.Key
		want string
	}{
		{"ctrl+a", uv.Key{Code: 'a', Mod: uv.ModCtrl}, "\x01"},
		{"windows ctrl+a", uv.Key{Code: '\x01', BaseCode: 'a'}, "\x01"},
		{"ctrl+left", uv.Key{Code: uv.KeyLeft, Mod: uv.ModCtrl}, "\x1b[1;5D"},
		{"ctrl+right", uv.Key{Code: uv.KeyRight, Mod: uv.ModCtrl}, "\x1b[1;5C"},
		{"alt+left", uv.Key{Code: uv.KeyLeft, Mod: uv.ModAlt}, "\x1b[1;3D"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := modifiedKeySequence(test.key)
			if !ok || got != test.want {
				t.Fatalf("modifiedKeySequence(%+v) = %q, %v", test.key, got, ok)
			}
		})
	}
}

func TestSessionForwardsInteractiveInput(t *testing.T) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("powershell.exe", "-NoProfile", "-Command", "$value=Read-Host; Write-Output INPUT_$value")
	} else {
		cmd = exec.Command("sh", "-c", "read value; printf 'INPUT_%s\\n' \"$value\"")
	}
	session := NewSession()
	if err := session.Start(context.Background(), cmd, 80, 20); err != nil {
		t.Fatal(err)
	}
	session.SendText("hello\r")
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-session.Events():
			if strings.Contains(session.Render(), "INPUT_hello") {
				return
			}
		case <-deadline:
			t.Fatalf("interactive PTY input not rendered:\n%s", session.Render())
		}
	}
}
