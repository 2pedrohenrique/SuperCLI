package kernel

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Plugin is the microkernel extension boundary. Plugins own features while the
// kernel only coordinates their lifecycle.
type Plugin interface {
	ID() string
	Start(context.Context) error
	Stop(context.Context) error
}

type Kernel struct {
	mu      sync.Mutex
	plugins []Plugin
	started []Plugin
}

func New() *Kernel { return &Kernel{} }

func (k *Kernel) Register(plugin Plugin) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, existing := range k.plugins {
		if existing.ID() == plugin.ID() {
			return fmt.Errorf("plugin %q is already registered", plugin.ID())
		}
	}
	k.plugins = append(k.plugins, plugin)
	return nil
}

func (k *Kernel) Start(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, plugin := range k.plugins {
		if err := plugin.Start(ctx); err != nil {
			_ = k.stopStarted(ctx)
			return fmt.Errorf("start plugin %q: %w", plugin.ID(), err)
		}
		k.started = append(k.started, plugin)
	}
	return nil
}

func (k *Kernel) Stop(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.stopStarted(ctx)
}

func (k *Kernel) stopStarted(ctx context.Context) error {
	var errs []error
	for i := len(k.started) - 1; i >= 0; i-- {
		if err := k.started[i].Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop plugin %q: %w", k.started[i].ID(), err))
		}
	}
	k.started = nil
	return errors.Join(errs...)
}
