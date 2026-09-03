package scripts

import (
	"os"
	"path/filepath"
	"regexp"
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

func TestRepositoryYAMLMetadataIsValid(t *testing.T) {
	paths, err := filepath.Glob("../.github/**/*.yml")
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, "../.github/workflows/ci.yml", "../.github/dependabot.yml")
	seen := map[string]struct{}{}
	for _, path := range paths {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var document any
		if err := yaml.Unmarshal(data, &document); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}

func TestCIActionsArePinnedToCommits(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	reference := regexp.MustCompile(`(?m)^\s*- uses: [^@\s]+@([0-9a-f]{40})(?:\s+#.*)?$`)
	uses := regexp.MustCompile(`(?m)^\s*- uses:`).FindAll(data, -1)
	if len(uses) == 0 || len(reference.FindAllSubmatch(data, -1)) != len(uses) {
		t.Fatal("every third-party CI action must be pinned to a full commit SHA")
	}
}
