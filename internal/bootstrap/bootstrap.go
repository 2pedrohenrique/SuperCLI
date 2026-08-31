package bootstrap

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed example.yaml
var ExampleConfig []byte

func WriteExample(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("refusing to overwrite existing file %q", path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect %q: %w", path, err)
	}
	if parent := filepath.Dir(path); parent != "." {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("create config directory: %w", err)
		}
	}
	if err := os.WriteFile(path, ExampleConfig, 0o600); err != nil {
		return fmt.Errorf("write example config: %w", err)
	}
	return nil
}
