package pluginhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/2pedrohenrique/SuperCLI/internal/config"
)

func TestDiscoverAndMergeCommunityActions(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "docker-tools")
	if err := os.MkdirAll(filepath.Join(pluginDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `api_version: supercli.dev/v1
id: docker-tools
name: Docker Tools
version: 0.1.0
description: Useful Docker workflows
actions:
  - id: inspect
    name: Inspect workspace
    command: ./bin/docker-tools
    args: ["--root", "{{workspace_dir}}"]
    dir: "{{plugin_dir}}"
    confirm: true
    workspaces: [app]
`
	if err := os.WriteFile(filepath.Join(pluginDir, ManifestFilename), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	plugins, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 1 || plugins[0].Manifest.ID != "docker-tools" {
		t.Fatalf("plugins = %#v", plugins)
	}
	cfg := config.Config{Version: 1, Workspaces: []config.Workspace{
		{ID: "app", Name: "App", Processes: []config.ProcessSpec{{ID: "api", Command: "go", Dir: filepath.Join(root, "app")}}},
		{ID: "other", Name: "Other", Processes: []config.ProcessSpec{{ID: "api", Command: "go", Dir: filepath.Join(root, "other")}}},
	}}
	merged, err := Merge(cfg, plugins)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Workspaces[0].Actions) != 1 || len(merged.Workspaces[1].Actions) != 0 {
		t.Fatalf("plugin workspace filtering failed: %#v", merged.Workspaces)
	}
	if len(cfg.Workspaces[0].Actions) != 0 {
		t.Fatal("Merge mutated the user's base configuration")
	}
	action := merged.Workspaces[0].Actions[0]
	if action.ID != "docker-tools--inspect" || !strings.Contains(action.Name, "Docker Tools") {
		t.Fatalf("action identity = %#v", action)
	}
	if action.Command != filepath.Join(pluginDir, "bin", executableName("docker-tools")) {
		t.Fatalf("command = %q", action.Command)
	}
	if action.Dir != pluginDir || action.Args[1] != filepath.Join(root, "app") {
		t.Fatalf("resolved action = %#v", action)
	}
}

func TestDiscoverRejectsManifestDirectoryMismatch(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "unexpected")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "api_version: supercli.dev/v1\nid: expected\nname: Expected\nversion: 1.0.0\nactions:\n  - id: run\n    name: Run\n    command: tool\n"
	if err := os.WriteFile(filepath.Join(dir, ManifestFilename), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(root); err == nil {
		t.Fatal("expected directory mismatch to be rejected")
	}
}

func TestBundledGitInsightsPluginUsesThePublicContract(t *testing.T) {
	root := filepath.Join("..", "..", "plugins")
	plugins, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 1 || plugins[0].Manifest.ID != "git-insights" || len(plugins[0].Manifest.Actions) != 2 {
		t.Fatalf("bundled plugins = %#v", plugins)
	}
}
