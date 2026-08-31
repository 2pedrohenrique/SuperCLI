package logbuffer

import (
	"fmt"
	"sync"
	"testing"
)

func TestRingKeepsNewestLines(t *testing.T) {
	ring := New(3)
	for _, line := range []string{"one", "two", "three", "four"} {
		ring.Append(line)
	}
	want := []string{"two", "three", "four"}
	got := ring.Last(-1)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("Last(-1) = %v, want %v", got, want)
	}
	if got := ring.Last(2); fmt.Sprint(got) != fmt.Sprint([]string{"three", "four"}) {
		t.Fatalf("Last(2) = %v", got)
	}
	if got := ring.Range(1, 2); fmt.Sprint(got) != fmt.Sprint([]string{"three", "four"}) {
		t.Fatalf("Range(1, 2) = %v", got)
	}
	if got := ring.Range(2, 10); fmt.Sprint(got) != fmt.Sprint([]string{"four"}) {
		t.Fatalf("Range(2, 10) = %v", got)
	}
}

func TestRingSupportsConcurrentWriters(t *testing.T) {
	ring := New(1000)
	var group sync.WaitGroup
	for worker := range 10 {
		group.Add(1)
		go func() {
			defer group.Done()
			for line := range 100 {
				ring.Append(fmt.Sprintf("%d-%d", worker, line))
			}
		}()
	}
	group.Wait()
	if got := ring.Len(); got != 1000 {
		t.Fatalf("Len() = %d, want 1000", got)
	}
}

func TestClearReleasesAllRetainedLines(t *testing.T) {
	ring := New(3)
	ring.Append("one")
	ring.Append("two")
	ring.Clear()
	if got := ring.Len(); got != 0 {
		t.Fatalf("Len() after Clear() = %d", got)
	}
	if got := ring.Last(-1); len(got) != 0 {
		t.Fatalf("Last() after Clear() = %#v", got)
	}
	ring.Append("fresh")
	if got := ring.Last(-1); len(got) != 1 || got[0] != "fresh" {
		t.Fatalf("ring was not reusable: %#v", got)
	}
}
