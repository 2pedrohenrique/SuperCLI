package capture

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

type recordingOpener struct {
	path string
	err  error
}

func (o *recordingOpener) Open(path string) error {
	o.path = path
	return o.err
}

func TestSaveCreatesPrivateTextCapture(t *testing.T) {
	store := New(t.TempDir())
	store.now = func() time.Time { return time.Date(2026, 8, 22, 1, 2, 3, 0, time.UTC) }
	path, err := store.Save("My Workspace", "API/dev", []string{"line one", "line two"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"Workspace: My Workspace", "Process: API/dev", "line one", "line two"} {
		if !strings.Contains(text, want) {
			t.Fatalf("capture missing %q:\n%s", want, text)
		}
	}
}

func TestSaveRejectsEmptyCapture(t *testing.T) {
	if _, err := New(t.TempDir()).Save("app", "api", nil); err == nil {
		t.Fatal("expected empty capture error")
	}
}

func TestSaveAndOpenLaunchesThePlatformViewer(t *testing.T) {
	opener := &recordingOpener{}
	store := New(t.TempDir(), WithOpener(opener))
	path, err := store.SaveAndOpen("app", "api", []string{"captured"})
	if err != nil {
		t.Fatal(err)
	}
	if opener.path != path {
		t.Fatalf("opened %q, want %q", opener.path, path)
	}
}

func TestSaveAndOpenKeepsTheCapturePathWhenOpeningFails(t *testing.T) {
	opener := &recordingOpener{err: errors.New("viewer unavailable")}
	store := New(t.TempDir(), WithOpener(opener))
	path, err := store.SaveAndOpen("app", "api", []string{"captured"})
	if path == "" || err == nil || !strings.Contains(err.Error(), "viewer unavailable") {
		t.Fatalf("SaveAndOpen() = %q, %v", path, err)
	}
}
