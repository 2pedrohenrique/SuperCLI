package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAppliesDefaultsExpandsPathsAndResolvesInputs(t *testing.T) {
	t.Setenv("SUPERCLI_TEST_ROOT", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := `
version: 1
workspaces:
  - id: app
    name: App
    processes:
      - id: api
        name: API
        command: go
        dir: "${SUPERCLI_TEST_ROOT}/api"
        error_patterns: ["(?i)error"]
    actions:
      - id: branch
        name: Branch
        command: git
        args: ["checkout", "-b", "feature-{{task}}"]
        inputs:
          - id: task
            prompt: Task
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogBufferLines != 10_000 {
		t.Fatalf("LogBufferLines = %d", cfg.LogBufferLines)
	}
	if !strings.HasSuffix(cfg.Workspaces[0].Processes[0].Dir, filepath.Join("api")) {
		t.Fatalf("Dir was not expanded: %q", cfg.Workspaces[0].Processes[0].Dir)
	}
	resolved := cfg.Workspaces[0].Actions[0].Resolve(map[string]string{"task": "123"})
	if got := resolved.Args[2]; got != "feature-123" {
		t.Fatalf("resolved argument = %q", got)
	}
}

func TestBundledConfigurationIsValid(t *testing.T) {
	path := filepath.Join("..", "bootstrap", "example.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(data))
	for _, privateMarker := range []string{"company-project", "administrator@", "root@", "192.168.", "10.1."} {
		if strings.Contains(lower, privateMarker) {
			t.Fatalf("bundled configuration contains private marker %q", privateMarker)
		}
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("bundled configuration must be installable: %v", err)
	}
	if len(cfg.Workspaces) == 0 {
		t.Fatal("bundled configuration must contain at least one workspace")
	}
}

func TestValidateRejectsDuplicateProcessAndBadPattern(t *testing.T) {
	base := Config{Version: 1, Workspaces: []Workspace{{
		ID: "app", Processes: []ProcessSpec{{ID: "api", Command: "go"}, {ID: "api", Command: "go"}},
	}}}
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate process") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	base.Workspaces[0].Processes = []ProcessSpec{{ID: "api", Command: "go", ErrorPatterns: []string{"["}}}
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "invalid pattern") {
		t.Fatalf("expected pattern error, got %v", err)
	}
}

func TestAppendWorkspaceAndProcessPersistWithoutExpandingExistingPaths(t *testing.T) {
	t.Setenv("SUPERCLI_TEST_ROOT", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := `version: 1
workspaces:
  - id: existing
    name: Existing
    processes:
      - id: api
        name: API
        command: go
        dir: "${SUPERCLI_TEST_ROOT}/api"
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	updated, err := AppendWorkspace(path, Workspace{ID: "new-app", Name: "New App", Processes: []ProcessSpec{{ID: "web", Name: "Web", Command: "npm", Args: []string{"start"}, Dir: `C:\projects\web`}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := updated.Workspace("new-app"); !ok {
		t.Fatal("new workspace was not loaded")
	}
	if _, err := AppendProcess(path, "existing", ProcessSpec{ID: "worker", Name: "Worker", Command: "go", Args: []string{"run", "."}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "${SUPERCLI_TEST_ROOT}/api") {
		t.Fatalf("existing environment path was expanded during persistence:\n%s", data)
	}
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".config.yaml.tmp-*")); err != nil || len(matches) != 0 {
		t.Fatalf("temporary configuration was not cleaned up: %v, %v", matches, err)
	}
}

func TestRemoveProcessAndWorkspacePersistConfigurationOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := `version: 1
workspaces:
  - id: one
    name: One
    processes:
      - {id: api, name: API, command: go}
      - {id: worker, name: Worker, command: go}
  - id: two
    name: Two
    processes:
      - {id: web, name: Web, command: npm}
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := RemoveProcess(path, "one", "worker")
	if err != nil {
		t.Fatal(err)
	}
	one, _ := cfg.Workspace("one")
	if len(one.Processes) != 1 || one.Processes[0].ID != "api" {
		t.Fatalf("process removal failed: %#v", one.Processes)
	}
	cfg, err = RemoveWorkspace(path, "two")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspaces) != 1 || cfg.Workspaces[0].ID != "one" {
		t.Fatalf("workspace removal failed: %#v", cfg.Workspaces)
	}
	if _, err := RemoveWorkspace(path, "one"); err == nil {
		t.Fatal("expected last-workspace protection")
	}
}
