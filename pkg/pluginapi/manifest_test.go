package pluginapi

import "testing"

func TestManifestValidation(t *testing.T) {
	valid := Manifest{
		APIVersion: CurrentAPIVersion,
		ID:         "docker-tools",
		Name:       "Docker Tools",
		Version:    "0.1.0",
		Actions:    []Action{{ID: "compose-up", Name: "Compose up", Command: "docker"}},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid manifest: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{"unsupported api", func(m *Manifest) { m.APIVersion = "supercli.dev/v99" }},
		{"unsafe id", func(m *Manifest) { m.ID = "../escape" }},
		{"missing action command", func(m *Manifest) { m.Actions[0].Command = "" }},
		{"duplicate action", func(m *Manifest) { m.Actions = append(m.Actions, m.Actions[0]) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := valid
			manifest.Actions = append([]Action(nil), valid.Actions...)
			test.mutate(&manifest)
			if err := manifest.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
