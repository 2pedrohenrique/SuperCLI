package scripts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestTrackedContentAndHistoryAreSanitized(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	root := repositoryRoot(t)
	privateTerms := privateFingerprints(t)
	secretPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)gh[pousr]_[a-z0-9]{20,}`),
		regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		regexp.MustCompile(`(?i)(password|secret|token)\s*[:=]\s*["'][^"']{8,}["']`),
	}

	filesOutput := gitOutput(t, root, "ls-files", "-z")
	for _, relative := range bytes.Split(filesOutput, []byte{0}) {
		if len(relative) == 0 {
			continue
		}
		name := filepath.ToSlash(string(relative))
		base := strings.ToLower(filepath.Base(name))
		if base == ".env" || strings.HasPrefix(base, ".env.") || name == "supercli.yaml" || strings.HasSuffix(name, ".local.yaml") {
			t.Errorf("personal configuration is tracked: %s", name)
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		assertSanitized(t, name, content, privateTerms, secretPatterns)
	}

	history := gitOutput(t, root, "log", "--all", "--format=%an%n%ae%n%B")
	assertSanitized(t, "Git history metadata", history, privateTerms, secretPatterns)
	objects := gitOutput(t, root, "rev-list", "--objects", "--all")
	assertSanitized(t, "Git history paths", objects, privateTerms, secretPatterns)
	seen := map[string]struct{}{}
	for _, line := range bytes.Split(objects, []byte{'\n'}) {
		fields := bytes.Fields(line)
		if len(fields) == 0 {
			continue
		}
		hash := string(fields[0])
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		if string(bytes.TrimSpace(gitOutput(t, root, "cat-file", "-t", hash))) != "blob" {
			continue
		}
		content := gitOutput(t, root, "cat-file", "blob", hash)
		assertSanitized(t, "Git history object "+hash, content, privateTerms, secretPatterns)
	}
}

func TestGoSourcesAreFormatted(t *testing.T) {
	root := repositoryRoot(t)
	output := gitOutput(t, root, "ls-files", "*.go")
	for _, name := range strings.Fields(string(output)) {
		path := filepath.Join(root, filepath.FromSlash(name))
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("gofmt", path)
		after, err := cmd.Output()
		if err != nil {
			t.Fatalf("gofmt %s: %v", name, err)
		}
		if !bytes.Equal(before, after) {
			t.Errorf("%s is not gofmt-formatted", name)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func gitOutput(t *testing.T, root string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return output
}

type privateFingerprint struct {
	length int
	digest [sha256.Size]byte
}

// Store fingerprints, not the private identities that the check protects.
func privateFingerprints(t *testing.T) []privateFingerprint {
	t.Helper()
	encoded := []struct {
		length int
		digest string
	}{
		{7, "0739c71c641c62f5be557aad2ccdae56ad4234aaac5c869a79ae11ae84c3db56"},
		{9, "9f0fc5c032c3c1c3f020dcd30a9ad0410ce79b332fcdc28dc06aa684fb51867a"},
		{5, "c805c62f6dcc86ef4e03e4f1ad2a77bd094491fe10189fab36a264e177b6864b"},
	}
	result := make([]privateFingerprint, len(encoded))
	for i, value := range encoded {
		decoded, err := hex.DecodeString(value.digest)
		if err != nil || len(decoded) != sha256.Size {
			t.Fatal("invalid private-term fingerprint")
		}
		result[i].length = value.length
		copy(result[i].digest[:], decoded)
	}
	return result
}

func assertSanitized(t *testing.T, source string, content []byte, terms []privateFingerprint, secrets []*regexp.Regexp) {
	t.Helper()
	lower := strings.ToLower(string(content))
	normalized := strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "", "_", "", "-", "", ".", "").Replace(lower)
	for _, term := range terms {
		for offset := 0; offset+term.length <= len(normalized); offset++ {
			if sha256.Sum256([]byte(normalized[offset:offset+term.length])) == term.digest {
				t.Errorf("%s contains a private term", source)
				break
			}
		}
	}
	for _, pattern := range secrets {
		if pattern.Match(content) {
			t.Errorf("%s contains a possible credential", source)
		}
	}
}
