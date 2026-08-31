//go:build !windows

package capture

type platformOpener struct{}

// Opening captures automatically is intentionally Windows-only. Other systems
// still receive the saved path in the status line.
func (platformOpener) Open(string) error { return nil }
