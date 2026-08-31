// Package pluginapi defines the stable, versioned contract used by community
// plugins. Plugins remain separate processes/commands, keeping the host portable
// across Windows, Linux and macOS.
package pluginapi

import (
	"fmt"
	"regexp"
	"strings"
)

const CurrentAPIVersion = "supercli.dev/v1"

type Manifest struct {
	APIVersion  string   `yaml:"api_version" json:"api_version"`
	ID          string   `yaml:"id" json:"id"`
	Name        string   `yaml:"name" json:"name"`
	Version     string   `yaml:"version" json:"version"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Homepage    string   `yaml:"homepage,omitempty" json:"homepage,omitempty"`
	Actions     []Action `yaml:"actions" json:"actions"`
}

type Action struct {
	ID          string        `yaml:"id" json:"id"`
	Name        string        `yaml:"name" json:"name"`
	Description string        `yaml:"description,omitempty" json:"description,omitempty"`
	Command     string        `yaml:"command" json:"command"`
	Args        []string      `yaml:"args,omitempty" json:"args,omitempty"`
	Dir         string        `yaml:"dir,omitempty" json:"dir,omitempty"`
	Confirm     bool          `yaml:"confirm,omitempty" json:"confirm,omitempty"`
	Interactive bool          `yaml:"interactive,omitempty" json:"interactive,omitempty"`
	Inputs      []ActionInput `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Workspaces  []string      `yaml:"workspaces,omitempty" json:"workspaces,omitempty"`
}

type ActionInput struct {
	ID      string `yaml:"id" json:"id"`
	Prompt  string `yaml:"prompt" json:"prompt"`
	Default string `yaml:"default,omitempty" json:"default,omitempty"`
}

var safeID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

func (m Manifest) Validate() error {
	if m.APIVersion != CurrentAPIVersion {
		return fmt.Errorf("unsupported api_version %q (expected %q)", m.APIVersion, CurrentAPIVersion)
	}
	if err := validateID("plugin", m.ID); err != nil {
		return err
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("plugin name is required")
	}
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("plugin version is required")
	}
	if len(m.Actions) == 0 {
		return fmt.Errorf("plugin %q must contribute at least one action", m.ID)
	}
	seen := make(map[string]struct{}, len(m.Actions))
	for _, action := range m.Actions {
		if err := validateID("action", action.ID); err != nil {
			return fmt.Errorf("plugin %q: %w", m.ID, err)
		}
		if _, ok := seen[action.ID]; ok {
			return fmt.Errorf("plugin %q: duplicate action id %q", m.ID, action.ID)
		}
		seen[action.ID] = struct{}{}
		if strings.TrimSpace(action.Name) == "" || strings.TrimSpace(action.Command) == "" {
			return fmt.Errorf("plugin %q action %q: name and command are required", m.ID, action.ID)
		}
		inputIDs := map[string]struct{}{}
		for _, input := range action.Inputs {
			if err := validateID("input", input.ID); err != nil {
				return fmt.Errorf("plugin %q action %q: %w", m.ID, action.ID, err)
			}
			if _, ok := inputIDs[input.ID]; ok {
				return fmt.Errorf("plugin %q action %q: duplicate input id %q", m.ID, action.ID, input.ID)
			}
			inputIDs[input.ID] = struct{}{}
		}
	}
	return nil
}

func validateID(kind, id string) error {
	if !safeID.MatchString(id) {
		return fmt.Errorf("%s id %q must contain only letters, numbers, underscore or hyphen", kind, id)
	}
	return nil
}
