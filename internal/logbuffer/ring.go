package logbuffer

import "sync"

type Ring struct {
	mu    sync.RWMutex
	lines []string
	start int
	count int
}

func New(capacity int) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{lines: make([]string, capacity)}
}

func (r *Ring) Append(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.count < len(r.lines) {
		r.lines[(r.start+r.count)%len(r.lines)] = line
		r.count++
		return
	}
	r.lines[r.start] = line
	r.start = (r.start + 1) % len(r.lines)
}

func (r *Ring) Last(n int) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if n < 0 || n > r.count {
		n = r.count
	}
	result := make([]string, n)
	first := r.count - n
	for i := range n {
		result[i] = r.lines[(r.start+first+i)%len(r.lines)]
	}
	return result
}

// Range returns at most n lines starting at the logical zero-based offset.
// It copies only string headers; the immutable line bytes remain shared.
func (r *Ring) Range(start, n int) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	start = max(0, min(start, r.count))
	if n < 0 || n > r.count-start {
		n = r.count - start
	}
	result := make([]string, n)
	for i := range n {
		result[i] = r.lines[(r.start+start+i)%len(r.lines)]
	}
	return result
}

func (r *Ring) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.count
}

// Clear removes retained references as well as resetting the logical window,
// allowing large log strings to be reclaimed by the garbage collector.
func (r *Ring) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	clear(r.lines)
	r.start = 0
	r.count = 0
}
