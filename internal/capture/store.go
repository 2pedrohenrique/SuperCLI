package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Store struct {
	dir    string
	now    func() time.Time
	opener Opener
}

type Opener interface {
	Open(path string) error
}

type Option func(*Store)

func WithOpener(opener Opener) Option {
	return func(store *Store) { store.opener = opener }
}

func New(dir string, options ...Option) *Store {
	store := &Store{dir: dir, now: time.Now, opener: platformOpener{}}
	for _, option := range options {
		option(store)
	}
	return store
}

func (s *Store) Save(workspace, process string, lines []string) (string, error) {
	if len(lines) == 0 {
		return "", fmt.Errorf("there are no log lines to capture")
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", fmt.Errorf("create capture directory: %w", err)
	}
	timestamp := s.now().Format("20060102-150405.000")
	name := fmt.Sprintf("%s-%s-%s.txt", safe(workspace), safe(process), timestamp)
	path := filepath.Join(s.dir, name)
	header := fmt.Sprintf("SuperCLI log capture\nWorkspace: %s\nProcess: %s\nCaptured: %s\nLines: %d\n%s\n",
		workspace, process, s.now().Format(time.RFC3339), len(lines), strings.Repeat("-", 72))
	if err := os.WriteFile(path, []byte(header+strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write capture: %w", err)
	}
	return path, nil
}

func (s *Store) SaveAndOpen(workspace, process string, lines []string) (string, error) {
	path, err := s.Save(workspace, process, lines)
	if err != nil {
		return "", err
	}
	if s.opener != nil {
		if err := s.opener.Open(path); err != nil {
			return path, fmt.Errorf("open capture %q: %w", path, err)
		}
	}
	return path, nil
}

var unsafeFilename = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func safe(value string) string {
	value = strings.Trim(unsafeFilename.ReplaceAllString(value, "-"), "-")
	if value == "" {
		return "logs"
	}
	return value
}
