package scripts

import (
	"os"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCIIncludesPublishedBranchAndSupportedPlatforms(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On struct {
			Push struct{ Branches []string }
		}
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct{ OS []string }
			}
		}
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(workflow.On.Push.Branches, "master") {
		t.Fatal("CI must run on pushes to the published master branch")
	}
	for _, platform := range []string{"windows-latest", "ubuntu-latest", "macos-latest"} {
		if !slices.Contains(workflow.Jobs["test"].Strategy.Matrix.OS, platform) {
			t.Errorf("CI missing %s", platform)
		}
	}
}
