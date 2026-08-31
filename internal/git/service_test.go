package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBranchAndCommitWorkflows(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	repo := filepath.Join(root, "repo")
	run(t, root, "git", "init", "--bare", remote)
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "git", "init", "-b", "main")
	run(t, repo, "git", "config", "user.email", "supercli@example.test")
	run(t, repo, "git", "config", "user.name", "SuperCLI Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "git", "add", "--all")
	run(t, repo, "git", "commit", "-m", "initial")
	run(t, repo, "git", "remote", "add", "origin", remote)
	run(t, repo, "git", "push", "-u", "origin", "main")

	service := New()
	if got, err := service.CurrentBranch(context.Background(), repo); err != nil || got != "main" {
		t.Fatalf("CurrentBranch() = %q, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(repo, "carried.txt"), []byte("carried\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	branch := service.Run(context.Background(), repo, Request{Operation: FeatureBranch, Task: "123", KeepChanges: true})
	if branch.Err != nil {
		t.Fatal(branch.Err)
	}
	if got := strings.TrimSpace(run(t, repo, "git", "branch", "--show-current")); got != "feature-123" {
		t.Fatalf("branch = %q", got)
	}
	if got, err := service.CurrentBranch(context.Background(), repo); err != nil || got != "feature-123" {
		t.Fatalf("CurrentBranch() after checkout = %q, %v", got, err)
	}
	if got := run(t, repo, "git", "log", "-1", "--pretty=%s"); !strings.Contains(got, "[{FEATURE}] - | feature-123") {
		t.Fatalf("carried changes commit = %q", got)
	}
	if got := strings.TrimSpace(run(t, repo, "git", "ls-remote", "--heads", "origin", "feature-123")); got == "" {
		t.Fatal("feature branch was not published")
	}
	if err := os.WriteFile(filepath.Join(repo, "change.txt"), []byte("change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commit := service.Run(context.Background(), repo, Request{Operation: FeatureCommit, Message: "native workflow"})
	if commit.Err != nil {
		t.Fatal(commit.Err)
	}
	if got := run(t, repo, "git", "log", "-1", "--pretty=%s"); !strings.Contains(got, "[{FEATURE}] - | native workflow") {
		t.Fatalf("commit message = %q", got)
	}
}

func TestCheckoutCanStashCurrentChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	run(t, repo, "git", "init", "-b", "main")
	run(t, repo, "git", "config", "user.email", "supercli@example.test")
	run(t, repo, "git", "config", "user.name", "SuperCLI Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "git", "add", "--all")
	run(t, repo, "git", "commit", "-m", "initial")
	run(t, repo, "git", "branch", "develop")
	if err := os.WriteFile(filepath.Join(repo, "private-change.txt"), []byte("change\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := New().Run(context.Background(), repo, Request{Operation: Checkout, Branch: "develop", KeepChanges: false})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if got := strings.TrimSpace(run(t, repo, "git", "branch", "--show-current")); got != "develop" {
		t.Fatalf("branch = %q", got)
	}
	if got := strings.TrimSpace(run(t, repo, "git", "status", "--porcelain")); got != "" {
		t.Fatalf("working tree was not stashed: %q", got)
	}
	if got := strings.TrimSpace(run(t, repo, "git", "stash", "list")); !strings.Contains(got, "SuperCLI: stashed before checkout develop") {
		t.Fatalf("stash was not created: %q", got)
	}
}

func TestPullAllUpdatesEveryUniqueRepositoryOnItsCurrentBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	repoA := createPullableRepository(t, root, "api", "main")
	repoB := createPullableRepository(t, root, "web", "develop")
	nestedA := filepath.Join(repoA, "cmd", "api")
	if err := os.MkdirAll(nestedA, 0o755); err != nil {
		t.Fatal(err)
	}

	result := New().PullAll(context.Background(), []PullTarget{
		{Name: "API", Dir: repoA},
		{Name: "API tests", Dir: nestedA},
		{Name: "Web", Dir: repoB},
	})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if result.Operation != PullAll {
		t.Fatalf("operation = %q", result.Operation)
	}
	for _, expected := range []string{"API", "main", "Web", "develop", "2 repositories updated"} {
		if !strings.Contains(result.Output, expected) {
			t.Fatalf("PullAll output missing %q:\n%s", expected, result.Output)
		}
	}
	if strings.Contains(result.Output, "API tests") {
		t.Fatalf("duplicate worktree was pulled twice:\n%s", result.Output)
	}
}

func TestPullAllContinuesWhenOneProcessIsNotAGitWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	repo := createPullableRepository(t, root, "valid", "main")
	result := New().PullAll(context.Background(), []PullTarget{
		{Name: "Missing", Dir: filepath.Join(root, "missing")},
		{Name: "Valid", Dir: repo},
	})
	if result.Err == nil || !strings.Contains(result.Output, "Valid") || !strings.Contains(result.Output, "failed") {
		t.Fatalf("partial PullAll result = %#v", result)
	}
}

func createPullableRepository(t *testing.T, root, name, branch string) string {
	t.Helper()
	remote := filepath.Join(root, name+"-remote.git")
	repo := filepath.Join(root, name)
	run(t, root, "git", "init", "--bare", remote)
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "git", "init", "-b", branch)
	run(t, repo, "git", "config", "user.email", "supercli@example.test")
	run(t, repo, "git", "config", "user.name", "SuperCLI Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte(name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "git", "add", "--all")
	run(t, repo, "git", "commit", "-m", "initial")
	run(t, repo, "git", "remote", "add", "origin", remote)
	run(t, repo, "git", "push", "-u", "origin", branch)
	return repo
}

func run(t *testing.T, dir, command string, args ...string) string {
	t.Helper()
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", command, args, err, output)
	}
	return string(output)
}
