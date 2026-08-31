package pluginhost

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/2pedrohenrique/SuperCLI/internal/config"
	"github.com/2pedrohenrique/SuperCLI/pkg/pluginapi"
	"gopkg.in/yaml.v3"
)

const ManifestFilename = "plugin.yaml"

type Plugin struct {
	Dir      string
	Manifest pluginapi.Manifest
}

// Discover loads one plugin from each direct child directory. A missing root
// is equivalent to an empty plugin collection, which keeps first-run startup simple.
func Discover(root string) ([]Plugin, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read plugin directory %q: %w", root, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	plugins := make([]Plugin, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		manifest, err := loadManifest(filepath.Join(dir, ManifestFilename))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if manifest.ID != entry.Name() {
			return nil, fmt.Errorf("plugin %q: directory must be named %q", entry.Name(), manifest.ID)
		}
		plugins = append(plugins, Plugin{Dir: dir, Manifest: manifest})
	}
	return plugins, nil
}

func loadManifest(path string) (pluginapi.Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return pluginapi.Manifest{}, err
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var manifest pluginapi.Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return pluginapi.Manifest{}, fmt.Errorf("parse plugin manifest %q: %w", path, err)
	}
	if err := manifest.Validate(); err != nil {
		return pluginapi.Manifest{}, fmt.Errorf("invalid plugin manifest %q: %w", path, err)
	}
	return manifest, nil
}

// Merge adapts public plugin actions into the application's existing action
// palette without mutating the user's persisted configuration.
func Merge(cfg config.Config, plugins []Plugin) (config.Config, error) {
	cfg = cloneConfig(cfg)
	for wi := range cfg.Workspaces {
		workspace := &cfg.Workspaces[wi]
		workspaceDir := ""
		if len(workspace.Processes) > 0 {
			workspaceDir = workspace.Processes[0].Dir
		}
		ids := make(map[string]struct{}, len(workspace.Actions))
		for _, action := range workspace.Actions {
			ids[action.ID] = struct{}{}
		}
		for _, plugin := range plugins {
			for _, contribution := range plugin.Manifest.Actions {
				if !supportsWorkspace(contribution.Workspaces, workspace.ID) {
					continue
				}
				action, err := adaptAction(plugin, contribution, workspaceDir)
				if err != nil {
					return config.Config{}, err
				}
				if _, exists := ids[action.ID]; exists {
					return config.Config{}, fmt.Errorf("workspace %q: plugin action id collision %q", workspace.ID, action.ID)
				}
				ids[action.ID] = struct{}{}
				workspace.Actions = append(workspace.Actions, action)
			}
		}
	}
	return cfg, nil
}

func cloneConfig(cfg config.Config) config.Config {
	workspaces := make([]config.Workspace, len(cfg.Workspaces))
	copy(workspaces, cfg.Workspaces)
	for i := range workspaces {
		workspaces[i].Processes = append([]config.ProcessSpec(nil), workspaces[i].Processes...)
		workspaces[i].Actions = append([]config.Action(nil), workspaces[i].Actions...)
	}
	cfg.Workspaces = workspaces
	return cfg
}

func adaptAction(plugin Plugin, value pluginapi.Action, workspaceDir string) (config.Action, error) {
	replace := func(text string) string {
		text = strings.ReplaceAll(text, "{{plugin_dir}}", plugin.Dir)
		return strings.ReplaceAll(text, "{{workspace_dir}}", workspaceDir)
	}
	command := replace(value.Command)
	if strings.HasPrefix(command, "./") || strings.HasPrefix(command, `.\`) {
		command = filepath.Join(plugin.Dir, strings.TrimLeft(command, `./\`))
		if runtime.GOOS == "windows" && filepath.Ext(command) == "" {
			command += ".exe"
		}
		if !inside(plugin.Dir, command) {
			return config.Action{}, fmt.Errorf("plugin %q action %q escapes its plugin directory", plugin.Manifest.ID, value.ID)
		}
	}
	args := make([]string, len(value.Args))
	for i, arg := range value.Args {
		args[i] = replace(arg)
	}
	inputs := make([]config.ActionInput, len(value.Inputs))
	for i, input := range value.Inputs {
		inputs[i] = config.ActionInput{ID: input.ID, Prompt: input.Prompt, Default: input.Default}
	}
	return config.Action{
		ID: plugin.Manifest.ID + "--" + value.ID, Name: "[" + plugin.Manifest.Name + "] " + value.Name,
		Command: command, Args: args, Dir: replace(value.Dir), Confirm: value.Confirm,
		Interactive: value.Interactive, Inputs: inputs,
	}, nil
}

func supportsWorkspace(filter []string, id string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, candidate := range filter {
		if candidate == "*" || candidate == id {
			return true
		}
	}
	return false
}

func inside(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
