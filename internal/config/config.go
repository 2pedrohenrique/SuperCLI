package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const CurrentVersion = 1

type Config struct {
	Version        int           `yaml:"version"`
	CaptureDir     string        `yaml:"capture_dir"`
	LogBufferLines int           `yaml:"log_buffer_lines"`
	Notifications  Notifications `yaml:"notifications"`
	Workspaces     []Workspace   `yaml:"workspaces"`
}

type Notifications struct {
	Enabled bool `yaml:"enabled"`
}

type Workspace struct {
	ID        string        `yaml:"id"`
	Name      string        `yaml:"name"`
	Processes []ProcessSpec `yaml:"processes"`
	Actions   []Action      `yaml:"actions"`
}

type ProcessSpec struct {
	ID            string            `yaml:"id"`
	Name          string            `yaml:"name"`
	Command       string            `yaml:"command"`
	Args          []string          `yaml:"args"`
	Dir           string            `yaml:"dir"`
	Env           map[string]string `yaml:"env"`
	AutoStart     bool              `yaml:"auto_start"`
	ErrorPatterns []string          `yaml:"error_patterns"`
	ReadyPatterns []string          `yaml:"ready_patterns"`
}

type Action struct {
	ID          string        `yaml:"id"`
	Name        string        `yaml:"name"`
	Command     string        `yaml:"command"`
	Args        []string      `yaml:"args"`
	Dir         string        `yaml:"dir"`
	Confirm     bool          `yaml:"confirm"`
	Interactive bool          `yaml:"interactive"`
	Inputs      []ActionInput `yaml:"inputs"`
}

type ActionInput struct {
	ID      string `yaml:"id"`
	Prompt  string `yaml:"prompt"`
	Default string `yaml:"default"`
}

func (a Action) Resolve(values map[string]string) Action {
	replace := func(value string) string {
		for id, input := range values {
			value = strings.ReplaceAll(value, "{{"+id+"}}", input)
		}
		return value
	}
	a.Command = replace(a.Command)
	a.Dir = replace(a.Dir)
	for i := range a.Args {
		a.Args[i] = replace(a.Args[i])
	}
	a.Inputs = nil
	return a
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	var cfg Config
	if err := decode(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	cfg.applyDefaults()
	cfg.expandPaths()
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

// AppendWorkspace persists a new workspace without expanding the existing
// environment-variable based paths, then returns the normalized configuration.
func AppendWorkspace(path string, workspace Workspace) (Config, error) {
	raw, err := loadRaw(path)
	if err != nil {
		return Config{}, err
	}
	raw.Workspaces = append(raw.Workspaces, workspace)
	if err := raw.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}
	if err := write(path, raw); err != nil {
		return Config{}, err
	}
	return Load(path)
}

func AppendProcess(path, workspaceID string, process ProcessSpec) (Config, error) {
	raw, err := loadRaw(path)
	if err != nil {
		return Config{}, err
	}
	found := false
	for i := range raw.Workspaces {
		if raw.Workspaces[i].ID == workspaceID {
			raw.Workspaces[i].Processes = append(raw.Workspaces[i].Processes, process)
			found = true
			break
		}
	}
	if !found {
		return Config{}, fmt.Errorf("unknown workspace %q", workspaceID)
	}
	if err := raw.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}
	if err := write(path, raw); err != nil {
		return Config{}, err
	}
	return Load(path)
}

func RemoveWorkspace(path, workspaceID string) (Config, error) {
	raw, err := loadRaw(path)
	if err != nil {
		return Config{}, err
	}
	if len(raw.Workspaces) == 1 {
		return Config{}, errors.New("cannot remove the last workspace")
	}
	found := false
	kept := raw.Workspaces[:0]
	for _, workspace := range raw.Workspaces {
		if workspace.ID == workspaceID {
			found = true
			continue
		}
		kept = append(kept, workspace)
	}
	if !found {
		return Config{}, fmt.Errorf("unknown workspace %q", workspaceID)
	}
	raw.Workspaces = kept
	if err := raw.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}
	if err := write(path, raw); err != nil {
		return Config{}, err
	}
	return Load(path)
}

func RemoveProcess(path, workspaceID, processID string) (Config, error) {
	raw, err := loadRaw(path)
	if err != nil {
		return Config{}, err
	}
	found := false
	for wi := range raw.Workspaces {
		workspace := &raw.Workspaces[wi]
		if workspace.ID != workspaceID {
			continue
		}
		if len(workspace.Processes) == 1 {
			return Config{}, errors.New("cannot remove the last process; remove its workspace instead")
		}
		kept := workspace.Processes[:0]
		for _, process := range workspace.Processes {
			if process.ID == processID {
				found = true
				continue
			}
			kept = append(kept, process)
		}
		workspace.Processes = kept
		break
	}
	if !found {
		return Config{}, fmt.Errorf("unknown process %s/%s", workspaceID, processID)
	}
	if err := raw.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}
	if err := write(path, raw); err != nil {
		return Config{}, err
	}
	return Load(path)
}

func loadRaw(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	var cfg Config
	if err := decode(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	cfg.applyDefaults()
	return cfg, nil
}

func decode(data []byte, destination *Config) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple YAML documents are not supported")
		}
		return err
	}
	return nil
}

func write(path string, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect temporary config: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("flush temporary config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace config %q: %w", path, err)
	}
	return nil
}

func (c *Config) applyDefaults() {
	if c.Version == 0 {
		c.Version = CurrentVersion
	}
	if c.LogBufferLines <= 0 {
		c.LogBufferLines = 10_000
	}
	if strings.TrimSpace(c.CaptureDir) == "" {
		c.CaptureDir = filepath.Join(os.TempDir(), "supercli", "captures")
	}
}

func (c *Config) expandPaths() {
	c.CaptureDir = expandPath(c.CaptureDir)
	for wi := range c.Workspaces {
		for pi := range c.Workspaces[wi].Processes {
			p := &c.Workspaces[wi].Processes[pi]
			p.Dir = expandPath(p.Dir)
			for k, v := range p.Env {
				p.Env[k] = os.ExpandEnv(v)
			}
		}
		for ai := range c.Workspaces[wi].Actions {
			c.Workspaces[wi].Actions[ai].Dir = expandPath(c.Workspaces[wi].Actions[ai].Dir)
		}
	}
}

func expandPath(value string) string {
	value = os.ExpandEnv(strings.TrimSpace(value))
	if value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			value = filepath.Join(home, strings.TrimLeft(value[1:], `/\`))
		}
	}
	if value == "" {
		return ""
	}
	return filepath.Clean(value)
}

func (c Config) Validate() error {
	if c.Version != CurrentVersion {
		return fmt.Errorf("unsupported version %d (expected %d)", c.Version, CurrentVersion)
	}
	if len(c.Workspaces) == 0 {
		return errors.New("at least one workspace is required")
	}
	workspaceIDs := map[string]struct{}{}
	for _, w := range c.Workspaces {
		if err := validID("workspace", w.ID); err != nil {
			return err
		}
		if _, exists := workspaceIDs[w.ID]; exists {
			return fmt.Errorf("duplicate workspace id %q", w.ID)
		}
		workspaceIDs[w.ID] = struct{}{}
		if len(w.Processes) == 0 {
			return fmt.Errorf("workspace %q has no processes", w.ID)
		}
		ids := map[string]struct{}{}
		for _, p := range w.Processes {
			if err := validID("process", p.ID); err != nil {
				return fmt.Errorf("workspace %q: %w", w.ID, err)
			}
			if _, exists := ids[p.ID]; exists {
				return fmt.Errorf("workspace %q: duplicate process id %q", w.ID, p.ID)
			}
			ids[p.ID] = struct{}{}
			if strings.TrimSpace(p.Command) == "" {
				return fmt.Errorf("workspace %q process %q: command is required", w.ID, p.ID)
			}
			for _, pattern := range append(append([]string{}, p.ErrorPatterns...), p.ReadyPatterns...) {
				if _, err := regexp.Compile(pattern); err != nil {
					return fmt.Errorf("workspace %q process %q: invalid pattern %q: %w", w.ID, p.ID, pattern, err)
				}
			}
		}
		actionIDs := map[string]struct{}{}
		for _, a := range w.Actions {
			if err := validID("action", a.ID); err != nil {
				return fmt.Errorf("workspace %q: %w", w.ID, err)
			}
			if _, exists := actionIDs[a.ID]; exists {
				return fmt.Errorf("workspace %q: duplicate action id %q", w.ID, a.ID)
			}
			actionIDs[a.ID] = struct{}{}
			if strings.TrimSpace(a.Command) == "" {
				return fmt.Errorf("workspace %q action %q: command is required", w.ID, a.ID)
			}
			inputIDs := map[string]struct{}{}
			for _, input := range a.Inputs {
				if err := validID("input", input.ID); err != nil {
					return fmt.Errorf("workspace %q action %q: %w", w.ID, a.ID, err)
				}
				if _, exists := inputIDs[input.ID]; exists {
					return fmt.Errorf("workspace %q action %q: duplicate input %q", w.ID, a.ID, input.ID)
				}
				inputIDs[input.ID] = struct{}{}
			}
		}
	}
	return nil
}

func validID(kind, id string) error {
	if matched, _ := regexp.MatchString(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`, id); !matched {
		return fmt.Errorf("%s id %q must contain only letters, numbers, underscore or hyphen", kind, id)
	}
	return nil
}

func (c Config) Workspace(id string) (Workspace, bool) {
	for _, w := range c.Workspaces {
		if w.ID == id {
			return w, true
		}
	}
	return Workspace{}, false
}
