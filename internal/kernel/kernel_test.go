package kernel

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type testPlugin struct {
	id       string
	events   *[]string
	startErr error
}

func (p testPlugin) ID() string { return p.id }
func (p testPlugin) Start(context.Context) error {
	*p.events = append(*p.events, "start:"+p.id)
	return p.startErr
}
func (p testPlugin) Stop(context.Context) error {
	*p.events = append(*p.events, "stop:"+p.id)
	return nil
}

func TestKernelStartsAndStopsPluginsInLifecycleOrder(t *testing.T) {
	var events []string
	kernel := New()
	for _, id := range []string{"one", "two"} {
		if err := kernel.Register(testPlugin{id: id, events: &events}); err != nil {
			t.Fatal(err)
		}
	}
	if err := kernel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := kernel.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := "[start:one start:two stop:two stop:one]"
	if got := fmt.Sprint(events); got != want {
		t.Fatalf("events = %s, want %s", got, want)
	}
}

func TestKernelRollsBackStartedPlugins(t *testing.T) {
	var events []string
	kernel := New()
	_ = kernel.Register(testPlugin{id: "good", events: &events})
	_ = kernel.Register(testPlugin{id: "bad", events: &events, startErr: errors.New("boom")})
	if err := kernel.Start(context.Background()); err == nil {
		t.Fatal("expected start error")
	}
	if got := fmt.Sprint(events); got != "[start:good start:bad stop:good]" {
		t.Fatalf("unexpected rollback: %s", got)
	}
}
