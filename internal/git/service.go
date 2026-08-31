package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

type Operation string

const (
	Pull          Operation = "pull"
	PullAll       Operation = "pull-all"
	Push          Operation = "push"
	Checkout      Operation = "checkout"
	FeatureBranch Operation = "feature-branch"
	HotfixBranch  Operation = "hotfix-branch"
	FeatureCommit Operation = "feature-commit"
	HotfixCommit  Operation = "hotfix-commit"
)

type Request struct {
	Operation   Operation
	Task        string
	Branch      string
	Message     string
	KeepChanges bool
}

type Result struct {
	Operation Operation
	Branch    string
	Output    string
	Err       error
}

type PullTarget struct {
	Name string
	Dir  string
}

type Service struct{}

func New() Service { return Service{} }

// CurrentBranch returns the symbolic branch for a Git worktree. Detached HEAD
// states are represented by their short commit so the UI still has context.
func (Service) CurrentBranch(ctx context.Context, dir string) (string, error) {
	branch, err := command(ctx, dir, "branch", "--show-current")
	if err != nil {
		return "", fmt.Errorf("read current branch: %w", err)
	}
	if branch = strings.TrimSpace(branch); branch != "" {
		return branch, nil
	}
	commit, err := command(ctx, dir, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("read detached HEAD: %w", err)
	}
	return "detached@" + strings.TrimSpace(commit), nil
}

func (Service) Run(ctx context.Context, dir string, request Request) Result {
	result := Result{Operation: request.Operation}
	branch, err := command(ctx, dir, "branch", "--show-current")
	if err != nil {
		result.Err = fmt.Errorf("read current branch: %w", err)
		return result
	}
	result.Branch = strings.TrimSpace(branch)

	switch request.Operation {
	case Pull:
		result.Output, result.Err = command(ctx, dir, "pull", "origin", result.Branch)
	case Push:
		result.Output, result.Err = command(ctx, dir, "push", "--set-upstream", "origin", "HEAD")
	case Checkout:
		result.Output, result.Err = checkout(ctx, dir, request)
		if result.Err == nil {
			result.Branch = strings.TrimSpace(request.Branch)
		}
	case FeatureBranch, HotfixBranch:
		result.Output, result.Err = createBranch(ctx, dir, request)
		if result.Err == nil {
			result.Branch, _ = BranchName(request)
		}
	case FeatureCommit, HotfixCommit:
		result.Output, result.Err = commit(ctx, dir, request)
	case PullAll:
		result.Output, result.Err = command(ctx, dir, "pull", "origin", result.Branch)
	default:
		result.Err = fmt.Errorf("unsupported git operation %q", request.Operation)
	}
	if result.Err != nil {
		result.Err = fmt.Errorf("git %s: %w", request.Operation, result.Err)
	}
	return result
}

// PullAll discovers the repository behind each process directory, removes
// duplicates, then updates repositories concurrently on their current branch.
// One failure does not prevent the remaining repositories from being updated.
func (Service) PullAll(ctx context.Context, targets []PullTarget) Result {
	result := Result{Operation: PullAll}
	type repository struct {
		name   string
		root   string
		branch string
		output string
		err    error
	}
	repositories := make([]repository, 0, len(targets))
	seen := map[string]struct{}{}
	var discoveryErrors []error
	var discoveryOutput []string
	for _, target := range targets {
		root, err := command(ctx, target.Dir, "rev-parse", "--show-toplevel")
		if err != nil {
			err = fmt.Errorf("%s: discover repository: %w", target.Name, err)
			discoveryErrors = append(discoveryErrors, err)
			discoveryOutput = append(discoveryOutput, fmt.Sprintf("✗ %s • failed: %v", target.Name, err))
			continue
		}
		root = filepath.Clean(strings.TrimSpace(root))
		identity := root
		if runtime.GOOS == "windows" {
			identity = strings.ToLower(identity)
		}
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		branch, err := command(ctx, root, "branch", "--show-current")
		branch = strings.TrimSpace(branch)
		if err != nil || branch == "" {
			if err == nil {
				err = fmt.Errorf("detached HEAD cannot be pulled safely")
			}
			err = fmt.Errorf("%s: read current branch: %w", target.Name, err)
			discoveryErrors = append(discoveryErrors, err)
			discoveryOutput = append(discoveryOutput, fmt.Sprintf("✗ %s • failed: %v", target.Name, err))
			continue
		}
		repositories = append(repositories, repository{name: target.Name, root: root, branch: branch})
	}

	var group sync.WaitGroup
	for i := range repositories {
		group.Add(1)
		go func(repository *repository) {
			defer group.Done()
			repository.output, repository.err = command(ctx, repository.root, "pull", "origin", repository.branch)
			if repository.err != nil {
				repository.err = fmt.Errorf("%s (%s): %w", repository.name, repository.branch, repository.err)
			}
		}(&repositories[i])
	}
	group.Wait()

	lines := append([]string(nil), discoveryOutput...)
	errs := append([]error(nil), discoveryErrors...)
	succeeded := 0
	for _, repository := range repositories {
		if repository.err != nil {
			errs = append(errs, repository.err)
			lines = append(lines, fmt.Sprintf("✗ %s • %s • failed: %v", repository.name, repository.branch, repository.err))
			continue
		}
		succeeded++
		message := strings.TrimSpace(repository.output)
		if message == "" {
			message = "up to date"
		}
		lines = append(lines, fmt.Sprintf("✓ %s • %s\n  %s", repository.name, repository.branch, message))
	}
	lines = append(lines, fmt.Sprintf("\n%d repositories updated • %d failed", succeeded, len(errs)))
	result.Output = strings.Join(lines, "\n")
	result.Err = errors.Join(errs...)
	return result
}

var safeBranchSuffix = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

func BranchName(request Request) (string, error) {
	task := strings.TrimSpace(request.Task)
	if !safeBranchSuffix.MatchString(task) {
		return "", fmt.Errorf("task must use only letters, numbers, dot, underscore or hyphen")
	}
	prefix := "feature-"
	if request.Operation == HotfixBranch {
		prefix = "fix-"
	}
	return prefix + task, nil
}

func createBranch(ctx context.Context, dir string, request Request) (string, error) {
	branch, err := BranchName(request)
	if err != nil {
		return "", err
	}
	dirty, err := hasChanges(ctx, dir)
	if err != nil {
		return "", err
	}
	var output []string
	if !request.KeepChanges {
		text, err := command(ctx, dir, "stash", "push", "--include-untracked", "-m", "SuperCLI: stashed before "+branch)
		output = appendStep(output, "stash", text)
		if err != nil {
			return strings.Join(output, "\n\n"), err
		}
	}
	text, err := command(ctx, dir, "checkout", "-b", branch)
	output = appendStep(output, "checkout -b "+branch, text)
	if err != nil {
		return strings.Join(output, "\n\n"), err
	}
	text, err = command(ctx, dir, "push", "--set-upstream", "origin", branch)
	output = appendStep(output, "push --set-upstream origin "+branch, text)
	if err != nil {
		return strings.Join(output, "\n\n"), err
	}
	if request.KeepChanges && dirty {
		if text, err = command(ctx, dir, "add", "--all"); err != nil {
			return strings.Join(appendStep(output, "add --all", text), "\n\n"), err
		}
		output = appendStep(output, "add --all", text)
		tag := "FEATURE"
		if request.Operation == HotfixBranch {
			tag = "HOTFIX"
		}
		message := fmt.Sprintf("[{%s}] - | %s", tag, branch)
		text, err = command(ctx, dir, "commit", "-m", message)
		output = appendStep(output, "commit carried changes", text)
		if err != nil {
			return strings.Join(output, "\n\n"), err
		}
		text, err = command(ctx, dir, "push", "origin", branch)
		output = appendStep(output, "push origin "+branch, text)
		if err != nil {
			return strings.Join(output, "\n\n"), err
		}
	}
	return strings.Join(output, "\n\n"), nil
}

func checkout(ctx context.Context, dir string, request Request) (string, error) {
	branch := strings.TrimSpace(request.Branch)
	if branch == "" {
		return "", fmt.Errorf("target branch is required")
	}
	if _, err := command(ctx, dir, "check-ref-format", "--branch", branch); err != nil {
		return "", fmt.Errorf("invalid target branch %q: %w", branch, err)
	}
	var output []string
	if !request.KeepChanges {
		text, err := command(ctx, dir, "stash", "push", "--include-untracked", "-m", "SuperCLI: stashed before checkout "+branch)
		output = appendStep(output, "stash", text)
		if err != nil {
			return strings.Join(output, "\n\n"), err
		}
	}
	text, err := command(ctx, dir, "checkout", branch)
	output = appendStep(output, "checkout "+branch, text)
	return strings.Join(output, "\n\n"), err
}

func hasChanges(ctx context.Context, dir string) (bool, error) {
	output, err := command(ctx, dir, "status", "--porcelain", "--untracked-files=all")
	return strings.TrimSpace(output) != "", err
}

func appendStep(output []string, name, text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "ok"
	}
	return append(output, "$ git "+name+"\n"+text)
}

func commit(ctx context.Context, dir string, request Request) (string, error) {
	message := strings.TrimSpace(request.Message)
	if message == "" {
		return "", fmt.Errorf("commit message is required")
	}
	if output, err := command(ctx, dir, "add", "--all"); err != nil {
		return output, err
	}
	tag := "FEATURE"
	if request.Operation == HotfixCommit {
		tag = "HOTFIX"
	}
	return command(ctx, dir, "commit", "-m", fmt.Sprintf("[{%s}] - | %s", tag, message))
}

func command(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text != "" {
			return text, fmt.Errorf("%w: %s", err, text)
		}
		return text, err
	}
	return text, nil
}
